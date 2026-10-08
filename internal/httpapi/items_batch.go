package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"caravel/internal/db"
)

// Creating several locations at once.
//
// It exists for the assistant's trip-level suggestions, where a person reviews
// six candidates, unticks two and adds the rest -- but nothing here knows that.
// It is the ordinary create, N times, in one transaction: the same itemRequest,
// the same validation, the same nested location/links/dates/tags, and the same
// detail response.
//
// Why one transaction rather than N requests from the client. Six separate
// POSTs are six chances to half-finish: a network drop after the third leaves
// the trip with three of the six places and the screen with no honest way to
// say which. Adding a reviewed list is one decision, so it is one write.
//
// What it deliberately does not do is carry cover photos or attachments. Those
// are multipart, and a batch of multipart bodies is a different endpoint with
// a different size limit and a blob-cleanup problem on rollback. A candidate's
// proposed cover is a URL, which the client applies afterwards through the
// endpoint that already fetches one.

// maxItemsPerBatch caps one request.
//
// Deliberately not assist.maxSuggestions, which is what the plan for this
// milestone said. That constant is a property of how many places are worth
// researching in one run and of what fits on a review screen; this one is a
// property of how much work one transaction should do. Tying them together
// would mean a change to the assistant's answer size silently changing the
// limits of a general-purpose endpoint. Twenty is comfortably above any
// suggest run and well below anything that should be a single write.
const maxItemsPerBatch = 20

type itemBatchRequest struct {
	Items []itemRequest `json:"items"`
}

func (s *Server) handleCreateItemsBatch(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req itemBatchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "no locations to create")
		return
	}
	if len(req.Items) > maxItemsPerBatch {
		writeError(w, http.StatusBadRequest, "too many locations in one request")
		return
	}

	// Every element is validated before the transaction opens, so a bad one is
	// a 400 with nothing written -- rather than three locations created and a
	// 400 about the fourth, which is the shape that makes a partial failure
	// impossible to explain.
	for _, item := range req.Items {
		if err := item.validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	created, err := s.createItemsTx(r, trip, req.Items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create locations")
		return
	}

	// The detail shape for each, in request order, so the client gets the
	// generated ids and nested rows back without a second GET -- exactly what
	// the single create returns, in a list.
	out := make([]itemDetailResponse, 0, len(created))
	for _, item := range created {
		out = append(out, s.buildItemDetail(r, item))
	}
	writeJSON(w, http.StatusCreated, out)
}

// createItemsTx writes every location in one transaction.
func (s *Server) createItemsTx(r *http.Request, trip db.Trip, reqs []itemRequest) ([]db.Location, error) {
	ctx := r.Context()
	out := make([]db.Location, 0, len(reqs))

	err := s.Store.WithTx(ctx, func(store db.Store) error {
		out = out[:0]

		// The batch appends to the end of the list, and relies on created_at
		// to do it -- which it can, since the whole point of the ORDER BY it
		// feeds is that creation order is the order.
		//
		// That was not always true. Until migration 0012 this picked the next
		// items.sort_order by reading the trip first, because SQLite stores
		// created_at as text in a layout that trimmed trailing zeros from the
		// fractional part and so did not sort correctly inside one second --
		// which is every row this loop writes. The layout is fixed-width now
		// (see timeLayout in internal/db/sqlite_store.go), so the column, the
		// read and the read-before-write that made this transaction prone to
		// SQLITE_BUSY_SNAPSHOT are all gone.
		for _, req := range reqs {
			item, err := createItemInStore(ctx, store, trip, uuid.NewString(), req, nil, nil)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
