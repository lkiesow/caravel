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

type checklistItemResponse struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Checked   bool   `json:"checked"`
	SortOrder int    `json:"sort_order"`
}

type checklistResponse struct {
	ID        string                  `json:"id"`
	TripID    string                  `json:"trip_id"`
	Title     string                  `json:"title"`
	SortOrder int                     `json:"sort_order"`
	Items     []checklistItemResponse `json:"items"`
}

func checklistItemToResponse(i db.ChecklistItem) checklistItemResponse {
	return checklistItemResponse{ID: i.ID, Text: i.Text, Checked: i.Checked, SortOrder: i.SortOrder}
}

func (s *Server) checklistToResponse(ctx context.Context, c db.Checklist) (checklistResponse, error) {
	items, err := s.Store.ListChecklistItemsByChecklist(ctx, c.ID)
	if err != nil {
		return checklistResponse{}, err
	}
	itemResponses := make([]checklistItemResponse, len(items))
	for i, item := range items {
		itemResponses[i] = checklistItemToResponse(item)
	}
	return checklistResponse{ID: c.ID, TripID: c.TripID, Title: c.Title, SortOrder: c.SortOrder, Items: itemResponses}, nil
}

func (s *Server) handleListChecklists(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	checklists, err := s.Store.ListChecklistsByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list checklists")
		return
	}
	resp := make([]checklistResponse, len(checklists))
	for i, c := range checklists {
		cr, err := s.checklistToResponse(r.Context(), c)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list checklists")
			return
		}
		resp[i] = cr
	}
	writeJSON(w, http.StatusOK, resp)
}

type checklistRequest struct {
	Title string `json:"title"`
}

func (s *Server) handleCreateChecklist(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req checklistRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	existing, err := s.Store.ListChecklistsByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist")
		return
	}

	checklist, err := s.Store.CreateChecklist(r.Context(), db.CreateChecklistParams{
		ID:        uuid.NewString(),
		TripID:    trip.ID,
		Title:     req.Title,
		SortOrder: len(existing),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist")
		return
	}
	resp, err := s.checklistToResponse(r.Context(), checklist)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleDeleteChecklist(w http.ResponseWriter, r *http.Request) {
	checklist, _, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	deleted, err := s.Store.DeleteChecklist(r.Context(), checklist.ID, checklist.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete checklist")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "checklist not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type checklistItemRequest struct {
	Text string `json:"text"`
}

func (s *Server) handleCreateChecklistItem(w http.ResponseWriter, r *http.Request) {
	checklist, _, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req checklistItemRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	existing, err := s.Store.ListChecklistItemsByChecklist(r.Context(), checklist.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist item")
		return
	}

	item, err := s.Store.CreateChecklistItem(r.Context(), db.CreateChecklistItemParams{
		ID:          uuid.NewString(),
		ChecklistID: checklist.ID,
		Text:        req.Text,
		SortOrder:   len(existing),
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist item")
		return
	}
	writeJSON(w, http.StatusCreated, checklistItemToResponse(item))
}

type checklistItemCheckedRequest struct {
	Checked bool `json:"checked"`
}

func (s *Server) handleSetChecklistItemChecked(w http.ResponseWriter, r *http.Request) {
	checklist, _, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req checklistItemCheckedRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	itemID := chi.URLParam(r, "itemId")
	item, err := s.Store.SetChecklistItemChecked(r.Context(), itemID, checklist.ID, req.Checked)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "checklist item not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not update checklist item")
		}
		return
	}
	writeJSON(w, http.StatusOK, checklistItemToResponse(item))
}

func (s *Server) handleDeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	checklist, _, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	itemID := chi.URLParam(r, "itemId")
	deleted, err := s.Store.DeleteChecklistItem(r.Context(), itemID, checklist.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete checklist item")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "checklist item not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
