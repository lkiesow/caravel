package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/db"
)

// maxTripCreateBytes bounds a multipart trip create. A trip carries a cover
// and nothing else -- no file parts, unlike a location -- so this is the image
// limit with room for the JSON part and the multipart framing, rather than the
// much larger ceiling maxItemCreateBytes needs.
const maxTripCreateBytes = 60 << 20

// createTripMultipart handles POST /api/trips when the body is multipart: the
// trip JSON in a "trip" part, and an optional cover as either an "image" file
// part or an "image_url" value.
//
// The ordering is what makes it atomic, and it is the reason this exists.
// Everything is parsed, validated, decoded and fetched first, so a cover URL
// the server cannot reach fails with no trip created. The old flow created the
// trip, then uploaded, then pointed the trip at the asset -- three requests, so
// a failure part-way left a trip with no cover, sometimes an orphan media
// asset, and a dialog after the fact.
//
// The one impurity is the same one createItemMultipart documents: a blob
// written in step two is orphaned if the transaction in step three rolls back.
// Nothing references it and no trip exists, so it is invisible to the user.
func (s *Server) createTripMultipart(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxTripCreateBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request too large or invalid multipart form")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	// The trip itself, as the same JSON the non-multipart path takes. Decoded
	// with the same strictness, so a typo in a field name is caught here too.
	raw := r.FormValue("trip")
	if strings.TrimSpace(raw) == "" {
		writeError(w, http.StatusBadRequest, "missing trip field")
		return
	}
	var req tripRequest
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid trip field: "+err.Error())
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generated before the transaction because the storage key embeds it and
	// the blob is written first, exactly as the location create does.
	tripID := uuid.NewString()

	image, status, err := s.stageImage(r, tripID)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	trip, err := s.createTripTx(r.Context(), tripID, user.ID, req, image)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create trip")
		return
	}
	writeJSON(w, http.StatusCreated, s.tripToResponse(r.Context(), trip, db.RoleOwner))
}

// createTripTx writes the trip, its cover asset and the pointer between them
// in one transaction. Shared with the JSON path, which passes a nil image.
func (s *Server) createTripTx(ctx context.Context, tripID, ownerID string,
	req tripRequest, image *pendingImage) (db.Trip, error) {

	now := time.Now().UTC()

	var trip db.Trip
	err := s.Store.WithTx(ctx, func(store db.Store) error {
		created, err := store.CreateTrip(ctx, db.CreateTripParams{
			ID:        tripID,
			OwnerID:   ownerID,
			Title:     req.Title,
			StartDate: req.StartDate,
			EndDate:   req.EndDate,
			Subtitle:  req.Subtitle,
			Currency:  currencyOrDefault(req.Currency, db.DefaultCurrency),
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			return err
		}

		if image != nil {
			asset, err := store.CreateMediaAsset(ctx, db.CreateMediaAssetParams{
				ID:          image.assetID,
				TripID:      created.ID,
				Kind:        image.kind,
				StoragePath: &image.key,
				ExternalURL: image.externalURL,
				ContentType: &image.contentType,
				Width:       &image.width,
				Height:      &image.height,
				SourceURL:   image.sourceURL,
				Credit:      image.credit,
				License:     image.license,
				CreatedAt:   now,
			})
			if err != nil {
				return err
			}
			updated, err := store.SetTripPreviewImage(ctx, created.ID, &asset.ID, now)
			if err != nil {
				return err
			}
			created = updated
		}

		trip = created
		return nil
	})
	return trip, err
}
