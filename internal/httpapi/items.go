package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/db"
)

var validCategories = map[string]bool{"location": true, "stay": true, "transport": true}

type itemResponse struct {
	ID        string  `json:"id"`
	TripID    string  `json:"trip_id"`
	Category  string  `json:"category"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Notes     *string `json:"notes"`
	ImageID   *string `json:"image_id"`
	ImageURL  *string `json:"image_url"`
	ShowOnMap bool    `json:"show_on_map"`
	SortOrder int     `json:"sort_order"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func (s *Server) itemToResponse(ctx context.Context, i db.Item) itemResponse {
	resp := itemResponse{
		ID:        i.ID,
		TripID:    i.TripID,
		Category:  i.Category,
		Type:      i.Type,
		Title:     i.Title,
		Notes:     i.Notes,
		ImageID:   i.ImageID,
		ShowOnMap: i.ShowOnMap,
		SortOrder: i.SortOrder,
		CreatedAt: i.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: i.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if i.ImageID != nil {
		if asset, err := s.Store.GetMediaAssetByID(ctx, *i.ImageID); err == nil {
			url := mediaAssetToResponse(asset).URL
			resp.ImageURL = &url
		}
	}
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

type itemDateResponse struct {
	ID        string  `json:"id"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	Label     *string `json:"label"`
	AllDay    bool    `json:"all_day"`
	StartTime *string `json:"start_time"`
	EndTime   *string `json:"end_time"`
}

type itemDetailResponse struct {
	itemResponse
	Location *itemLocationResponse `json:"location"`
	Links    []itemLinkResponse    `json:"links"`
	Dates    []itemDateResponse    `json:"dates"`
}

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
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

	resp := make([]itemResponse, len(items))
	for i, it := range items {
		resp[i] = s.itemToResponse(r.Context(), it)
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
}

func (req itemRequest) validate() error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("title is required")
	}
	if !validCategories[req.Category] {
		return errors.New("category must be one of: location, stay, transport")
	}
	return nil
}

func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
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

	showOnMap := true
	if req.ShowOnMap != nil {
		showOnMap = *req.ShowOnMap
	}
	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	now := time.Now().UTC()
	item, err := s.Store.CreateItem(r.Context(), db.CreateItemParams{
		ID:        uuid.NewString(),
		TripID:    trip.ID,
		Category:  req.Category,
		Type:      req.Type,
		Title:     req.Title,
		Notes:     req.Notes,
		ShowOnMap: showOnMap,
		SortOrder: sortOrder,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create item")
		return
	}
	writeJSON(w, http.StatusCreated, s.itemToResponse(r.Context(), item))
}

// loadOwnedItem fetches the item named by {itemId} and confirms the current
// user owns the trip it belongs to.
func (s *Server) loadOwnedItem(w http.ResponseWriter, r *http.Request) (db.Item, bool) {
	itemID := chi.URLParam(r, "itemId")

	item, err := s.Store.GetItemByID(r.Context(), itemID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load item")
		}
		return db.Item{}, false
	}

	trip, err := s.Store.GetTripByID(r.Context(), item.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load item")
		return db.Item{}, false
	}
	user, _ := auth.UserFromContext(r.Context())
	if trip.OwnerID != user.ID {
		writeError(w, http.StatusNotFound, "item not found")
		return db.Item{}, false
	}
	return item, true
}

func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.buildItemDetail(r, item))
}

func (s *Server) buildItemDetail(r *http.Request, item db.Item) itemDetailResponse {
	detail := itemDetailResponse{itemResponse: s.itemToResponse(r.Context(), item), Links: []itemLinkResponse{}, Dates: []itemDateResponse{}}

	if loc, err := s.Store.GetItemLocationByItemID(r.Context(), item.ID); err == nil {
		detail.Location = &itemLocationResponse{Lat: loc.Lat, Lng: loc.Lng, Address: loc.Address}
	}

	if links, err := s.Store.ListItemLinksByItem(r.Context(), item.ID); err == nil {
		for _, l := range links {
			detail.Links = append(detail.Links, itemLinkResponse{ID: l.ID, URL: l.URL, Label: l.Label, SortOrder: l.SortOrder})
		}
	}

	if dates, err := s.Store.ListItemDatesByItem(r.Context(), item.ID); err == nil {
		for _, d := range dates {
			detail.Dates = append(detail.Dates, itemDateResponse{
				ID: d.ID, StartDate: d.StartDate, EndDate: d.EndDate, Label: d.Label,
				AllDay: d.AllDay, StartTime: d.StartTime, EndTime: d.EndTime,
			})
		}
	}

	return detail
}

func (s *Server) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
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

	updated, err := s.Store.UpdateItem(r.Context(), db.UpdateItemParams{
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
		writeError(w, http.StatusInternalServerError, "could not update item")
		return
	}
	writeJSON(w, http.StatusOK, s.itemToResponse(r.Context(), updated))
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
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
	item, ok := s.loadOwnedItem(w, r)
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
	item, ok := s.loadOwnedItem(w, r)
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
	item, ok := s.loadOwnedItem(w, r)
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

type itemDateRequest struct {
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	Label     *string `json:"label"`
	AllDay    *bool   `json:"all_day"`
	StartTime *string `json:"start_time"`
	EndTime   *string `json:"end_time"`
}

func (s *Server) handleCreateItemDate(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}

	var req itemDateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateDate(req.StartDate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateDate(req.EndDate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	allDay := true
	if req.AllDay != nil {
		allDay = *req.AllDay
	}

	date, err := s.Store.CreateItemDate(r.Context(), db.CreateItemDateParams{
		ID:        uuid.NewString(),
		ItemID:    item.ID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Label:     req.Label,
		AllDay:    allDay,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create date")
		return
	}
	writeJSON(w, http.StatusCreated, itemDateResponse{
		ID: date.ID, StartDate: date.StartDate, EndDate: date.EndDate, Label: date.Label,
		AllDay: date.AllDay, StartTime: date.StartTime, EndTime: date.EndTime,
	})
}

func (s *Server) handleDeleteItemDate(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}
	dateID := chi.URLParam(r, "dateId")
	deleted, err := s.Store.DeleteItemDate(r.Context(), dateID, item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete date")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "date not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetItemImage(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}

	var req setPreviewImageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := s.Store.SetItemImage(r.Context(), item.ID, item.TripID, req.MediaAssetID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set image")
		return
	}
	writeJSON(w, http.StatusOK, s.itemToResponse(r.Context(), updated))
}
