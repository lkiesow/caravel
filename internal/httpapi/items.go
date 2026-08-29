package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/db"
)

var validCategories = map[string]bool{"site": true, "stay": true, "transport": true}

type itemResponse struct {
	ID        string  `json:"id"`
	TripID    string  `json:"trip_id"`
	Category  string  `json:"category"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Notes     *string `json:"notes"`
	NotesHTML *string `json:"notes_html"`
	ImageID   *string `json:"image_id"`
	ImageURL  *string `json:"image_url"`
	// ImageCredit is who the cover is owed to, or null -- which it is for
	// every image somebody uploaded themselves. Carried on the item rather
	// than only on the media asset because this is where it gets rendered,
	// and a second request to find out whether a credit exists would mean the
	// page either flickers or waits.
	ImageCredit *imageCreditResponse `json:"image_credit"`
	ShowOnMap   bool                 `json:"show_on_map"`
	SortOrder   int                  `json:"sort_order"`
	// Lat/Lng are set only on the list endpoint, and only for items that have
	// both. The list used to carry no position at all, which meant the
	// locations tab could not filter by distance without a second request
	// (Stage 13 Milestone 7). Flat rather than a nested "location" object
	// because there is no address here - the detail endpoint remains the place
	// to get a whole location.
	Lat *float64 `json:"lat,omitempty"`
	Lng *float64 `json:"lng,omitempty"`
	// Tags is always present and never null, on the list as well as the
	// detail: the locations tab filters on it client-side, and a field that
	// is sometimes absent would mean every caller writing the same guard.
	// itemToResponse leaves it empty and both handlers fill it in -- the
	// list from one trip-wide query, the detail from its own read.
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func (s *Server) itemToResponse(ctx context.Context, i db.Item) itemResponse {
	resp := itemResponse{
		ID:        i.ID,
		TripID:    i.TripID,
		Category:  i.Category,
		Type:      i.Type,
		Title:     i.Title,
		Notes:     i.Notes,
		NotesHTML: renderNotesHTML(i.Notes),
		ImageID:   i.ImageID,
		ShowOnMap: i.ShowOnMap,
		SortOrder: i.SortOrder,
		Tags:      []string{},
		CreatedAt: i.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: i.UpdatedAt.UTC().Format(time.RFC3339),
	}
	resp.ImageURL, resp.ImageCredit = s.resolveImage(ctx, i.ImageID)
	return resp
}

type itemLocationResponse struct {
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	Address *string  `json:"address"`
}

type itemLinkResponse struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Label     *string `json:"label"`
	SortOrder int     `json:"sort_order"`
}

type itemDetailResponse struct {
	itemResponse
	Location *itemLocationResponse   `json:"location"`
	Links    []itemLinkResponse      `json:"links"`
	Dates    []itemDateRangeResponse `json:"dates"`
}

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}

	var category *string
	if c := r.URL.Query().Get("category"); c != "" {
		if !validCategories[c] {
			writeError(w, http.StatusBadRequest, "invalid category filter")
			return
		}
		category = &c
	}

	items, err := s.Store.ListItemsByTrip(r.Context(), trip.ID, category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list items")
		return
	}

	// One extra query for the whole trip rather than one per item. A failure
	// here costs the distance filter, not the list, so it is not fatal: the
	// tab still renders every location, just without coordinates to measure.
	coordinates := map[string]db.ItemCoordinate{}
	if located, err := s.Store.ListItemCoordinates(r.Context(), trip.ID); err == nil {
		for _, c := range located {
			coordinates[c.ItemID] = c
		}
	}

	// Likewise one query for the trip rather than one per item, and likewise
	// not fatal: a failure here costs the tag filter, not the list.
	tags := map[string][]string{}
	if rows, err := s.Store.ListItemTagsByTrip(r.Context(), trip.ID); err == nil {
		tags = tagsByItem(rows)
	}

	resp := make([]itemResponse, len(items))
	for i, it := range items {
		resp[i] = s.itemToResponse(r.Context(), it)
		if c, ok := coordinates[it.ID]; ok {
			lat, lng := c.Lat, c.Lng
			resp[i].Lat, resp[i].Lng = &lat, &lng
		}
		if t, ok := tags[it.ID]; ok {
			resp[i].Tags = t
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type itemRequest struct {
	Category  string  `json:"category"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Notes     *string `json:"notes"`
	ShowOnMap *bool   `json:"show_on_map"`
	SortOrder *int    `json:"sort_order"`

	// Optional nested sub-resources, so one request can commit an item and
	// everything hanging off it in a single transaction. Each is a pointer
	// so "absent" and "present but empty" stay distinguishable: absent
	// leaves that sub-resource untouched, present replaces it (an empty
	// list clears it). The standalone /location and /links endpoints still
	// exist and still work; these are additive.
	//
	// Location is an upsert (item_locations.item_id is UNIQUE). Links are
	// replace-the-set rather than merge, because there is no per-row update
	// endpoint anywhere — editing a link has always meant delete plus re-add
	// — so the client edits them as a list and sends the list it wants. Array
	// order becomes sort_order for links.
	//
	// Dates are the exception, and the difference matters. Since Stage 25 they
	// are not rows of their own but a view of the itinerary days this location
	// appears on, so "present replaces it" is honoured by reconciling the day
	// set — see reconcileItemDates — rather than by deleting and recreating.
	// The consequence for callers is that sending this key asserts the
	// location complete itinerary membership: a client that did not touch the
	// dates should omit it, not echo back what it read.
	Location *itemLocationRequest    `json:"location"`
	Links    *[]itemLinkRequest      `json:"links"`
	Dates    *[]itemDateRangeRequest `json:"dates"`
	Tags     *[]string               `json:"tags"`
}

func (req itemRequest) validate() error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("title is required")
	}
	if !validCategories[req.Category] {
		return errors.New("category must be one of: site, stay, transport")
	}
	// Validate the nested blocks up front so a bad link or date is a 400
	// before anything is written, rather than a rolled-back 500.
	if req.Links != nil {
		for _, l := range *req.Links {
			if strings.TrimSpace(l.URL) == "" {
				return errors.New("every link needs a url")
			}
		}
	}
	if req.Dates != nil {
		if err := validateItemDateRanges(*req.Dates); err != nil {
			return err
		}
	}
	// Normalized before validating, so a set that is only over the limit
	// because it repeats a tag or pads one with spaces is accepted and
	// cleaned rather than refused.
	if req.Tags != nil {
		if err := validateTags(normalizeTags(*req.Tags)); err != nil {
			return err
		}
	}
	return nil
}

// writeItemNested applies a request's optional nested location/links/dates to
// an existing item. It takes the Store to use rather than reading s.Store, so
// the callers can hand it a transaction-bound one and have the whole item
// commit or not at all.
func writeItemNested(ctx context.Context, store db.Store, item db.Item, req itemRequest) error {
	if req.Location != nil {
		if _, err := store.UpsertItemLocation(ctx, db.UpsertItemLocationParams{
			ID:      uuid.NewString(),
			ItemID:  item.ID,
			Lat:     req.Location.Lat,
			Lng:     req.Location.Lng,
			Address: req.Location.Address,
		}); err != nil {
			return err
		}
	}

	if req.Links != nil {
		existing, err := store.ListItemLinksByItem(ctx, item.ID)
		if err != nil {
			return err
		}
		for _, l := range existing {
			if _, err := store.DeleteItemLink(ctx, l.ID, item.ID); err != nil {
				return err
			}
		}
		for i, l := range *req.Links {
			if _, err := store.CreateItemLink(ctx, db.CreateItemLinkParams{
				ID:        uuid.NewString(),
				ItemID:    item.ID,
				URL:       l.URL,
				Label:     l.Label,
				SortOrder: i,
			}); err != nil {
				return err
			}
		}
	}

	if req.Dates != nil {
		if err := reconcileItemDates(ctx, store, item, *req.Dates); err != nil {
			return err
		}
	}

	if req.Tags != nil {
		if err := writeItemTags(ctx, store, item.ID, normalizeTags(*req.Tags)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	// A multipart body carries the cover photo and the files alongside the
	// item, so the whole location commits or does not -- see items_create.go.
	// The JSON path below stays exactly as it was: it is what the assistant
	// and every other caller send, and readJSON's unknown-field strictness is
	// part of its contract.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		s.createItemMultipart(w, r, trip)
		return
	}

	var req itemRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Same transaction as the multipart path, with no image and no files.
	item, err := s.createItemTx(r.Context(), trip, uuid.NewString(), req, nil, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create item")
		return
	}
	// The detail shape, not the bare item: a create can now carry nested
	// location/links/dates, and the client needs them (with their generated
	// IDs) back without a second GET.
	writeJSON(w, http.StatusCreated, s.buildItemDetail(r, item))
}

func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleViewer)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.buildItemDetail(r, item))
}

func (s *Server) buildItemDetail(r *http.Request, item db.Item) itemDetailResponse {
	detail := itemDetailResponse{itemResponse: s.itemToResponse(r.Context(), item), Links: []itemLinkResponse{}, Dates: []itemDateRangeResponse{}}

	if loc, err := s.Store.GetItemLocationByItemID(r.Context(), item.ID); err == nil {
		detail.Location = &itemLocationResponse{Lat: loc.Lat, Lng: loc.Lng, Address: loc.Address}
	}

	if links, err := s.Store.ListItemLinksByItem(r.Context(), item.ID); err == nil {
		for _, l := range links {
			detail.Links = append(detail.Links, itemLinkResponse{ID: l.ID, URL: l.URL, Label: l.Label, SortOrder: l.SortOrder})
		}
	}

	// Tolerant in the same way, and for the same reason.
	if tags, err := s.Store.ListItemTagsByItem(r.Context(), item.ID); err == nil {
		detail.Tags = tags
	}

	// The days this location is on in the itinerary, collapsed into ranges.
	// Tolerant of a failure the way the two blocks above are: losing the dates
	// costs a card on the page, not the location.
	if rows, err := s.Store.ListItineraryDatesByItem(r.Context(), item.ID); err == nil {
		dates := make([]string, len(rows))
		for i, row := range rows {
			dates[i] = row.Date
		}
		detail.Dates = collapseDateRanges(dates)
	}

	return detail
}

func (s *Server) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req itemRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	showOnMap := item.ShowOnMap
	if req.ShowOnMap != nil {
		showOnMap = *req.ShowOnMap
	}
	sortOrder := item.SortOrder
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	var updated db.Item
	err := s.Store.WithTx(r.Context(), func(store db.Store) error {
		saved, err := store.UpdateItem(r.Context(), db.UpdateItemParams{
			ID:        item.ID,
			TripID:    item.TripID,
			Category:  req.Category,
			Type:      req.Type,
			Title:     req.Title,
			Notes:     req.Notes,
			ShowOnMap: showOnMap,
			SortOrder: sortOrder,
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		if err := writeItemNested(r.Context(), store, saved, req); err != nil {
			return err
		}
		updated = saved
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update item")
		return
	}
	writeJSON(w, http.StatusOK, s.buildItemDetail(r, updated))
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if _, err := s.Store.DeleteItem(r.Context(), item.ID, item.TripID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type itemLocationRequest struct {
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	Address *string  `json:"address"`
}

func (s *Server) handlePutItemLocation(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req itemLocationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	loc, err := s.Store.UpsertItemLocation(r.Context(), db.UpsertItemLocationParams{
		ID:      uuid.NewString(),
		ItemID:  item.ID,
		Lat:     req.Lat,
		Lng:     req.Lng,
		Address: req.Address,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save location")
		return
	}
	writeJSON(w, http.StatusOK, itemLocationResponse{Lat: loc.Lat, Lng: loc.Lng, Address: loc.Address})
}

type itemLinkRequest struct {
	URL   string  `json:"url"`
	Label *string `json:"label"`
}

func (s *Server) handleCreateItemLink(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req itemLinkRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	link, err := s.Store.CreateItemLink(r.Context(), db.CreateItemLinkParams{
		ID:     uuid.NewString(),
		ItemID: item.ID,
		URL:    req.URL,
		Label:  req.Label,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create link")
		return
	}
	writeJSON(w, http.StatusCreated, itemLinkResponse{ID: link.ID, URL: link.URL, Label: link.Label, SortOrder: link.SortOrder})
}

func (s *Server) handleDeleteItemLink(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}
	linkID := chi.URLParam(r, "linkId")
	deleted, err := s.Store.DeleteItemLink(r.Context(), linkID, item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete link")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetItemImage(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req setPreviewImageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Same check as handleSetTripPreviewImage: the asset id arrives in the
	// body, so the route authorized the *item*, not the asset. A nil id
	// clears the image and names nothing to check.
	if req.MediaAssetID != nil {
		asset, err := s.Store.GetMediaAssetByID(r.Context(), *req.MediaAssetID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "media asset not found")
			} else {
				writeError(w, http.StatusInternalServerError, "could not load media asset")
			}
			return
		}
		if !s.requireSameTrip(w, asset.TripID, item.TripID, "media asset belongs to another trip") {
			return
		}
	}

	updated, err := s.Store.SetItemImage(r.Context(), item.ID, item.TripID, req.MediaAssetID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set image")
		return
	}
	writeJSON(w, http.StatusOK, s.itemToResponse(r.Context(), updated))
}
