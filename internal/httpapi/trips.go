package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/db"
	"caravel/internal/markdown"
)

type tripResponse struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	StartDate       *string `json:"start_date"`
	EndDate         *string `json:"end_date"`
	Subtitle        *string `json:"subtitle"`
	PreviewImageID  *string `json:"preview_image_id"`
	PreviewImageURL *string `json:"preview_image_url"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func (s *Server) tripToResponse(ctx context.Context, t db.Trip) tripResponse {
	resp := tripResponse{
		ID:             t.ID,
		Title:          t.Title,
		StartDate:      t.StartDate,
		EndDate:        t.EndDate,
		Subtitle:       t.Subtitle,
		PreviewImageID: t.PreviewImageID,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	resp.PreviewImageURL = s.resolveImageURL(ctx, t.PreviewImageID)
	return resp
}

// renderNotesHTML renders an item's notes markdown to sanitized HTML,
// rendered fresh on every response rather than cached in the database —
// notes are short-form text, so a goldmark+bluemonday pass costs
// microseconds, and rendering on read avoids a second column that could
// drift from the source markdown.
func renderNotesHTML(notes *string) *string {
	if notes == nil {
		return nil
	}
	html, err := markdown.ToSafeHTML(*notes)
	if err != nil {
		return nil
	}
	return &html
}

func (s *Server) handleListTrips(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())

	trips, err := s.Store.ListTripsByOwner(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list trips")
		return
	}

	resp := make([]tripResponse, len(trips))
	for i, t := range trips {
		resp[i] = s.tripToResponse(r.Context(), t)
	}
	writeJSON(w, http.StatusOK, resp)
}

type tripRequest struct {
	Title     string  `json:"title"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	Subtitle  *string `json:"subtitle"`
}

func (req tripRequest) validate() error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("title is required")
	}
	if err := validateDate(req.StartDate); err != nil {
		return err
	}
	if err := validateDate(req.EndDate); err != nil {
		return err
	}
	// An inverted range isn't just cosmetic: the trip header renders it
	// verbatim ("20 Aug – 1 Aug 2026"), and handleGetItinerary's
	// datesInRange returns nil for it, so the itinerary silently drops every
	// day that has no content of its own.
	if req.StartDate != nil && req.EndDate != nil {
		start, err1 := time.Parse("2006-01-02", *req.StartDate)
		end, err2 := time.Parse("2006-01-02", *req.EndDate)
		if err1 == nil && err2 == nil && end.Before(start) {
			return errors.New("end date must not be before start date")
		}
	}
	return nil
}

func validateDate(d *string) error {
	if d == nil {
		return nil
	}
	if _, err := time.Parse("2006-01-02", *d); err != nil {
		return errors.New("dates must be in YYYY-MM-DD format")
	}
	return nil
}

func (s *Server) handleCreateTrip(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())

	var req tripRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UTC()
	trip, err := s.Store.CreateTrip(r.Context(), db.CreateTripParams{
		ID:        uuid.NewString(),
		OwnerID:   user.ID,
		Title:     req.Title,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Subtitle:  req.Subtitle,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create trip")
		return
	}
	writeJSON(w, http.StatusCreated, s.tripToResponse(r.Context(), trip))
}

func (s *Server) handleGetTrip(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.tripToResponse(r.Context(), trip))
}

func (s *Server) handleUpdateTrip(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req tripRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.Store.UpdateTrip(r.Context(), db.UpdateTripParams{
		ID:        trip.ID,
		Title:     req.Title,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Subtitle:  req.Subtitle,
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update trip")
		return
	}
	writeJSON(w, http.StatusOK, s.tripToResponse(r.Context(), updated))
}

type setPreviewImageRequest struct {
	MediaAssetID *string `json:"media_asset_id"`
}

func (s *Server) handleSetTripPreviewImage(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req setPreviewImageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The asset id comes from the request body, so no route param authorized
	// it: confirm it belongs to *this* trip before pointing the trip at it.
	// A nil id clears the cover photo and names no asset to check.
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
		if !s.requireSameTrip(w, asset.TripID, trip.ID, "media asset belongs to another trip") {
			return
		}
	}

	updated, err := s.Store.SetTripPreviewImage(r.Context(), trip.ID, req.MediaAssetID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set preview image")
		return
	}
	writeJSON(w, http.StatusOK, s.tripToResponse(r.Context(), updated))
}

func (s *Server) handleDeleteTrip(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleOwner)
	if !ok {
		return
	}
	if _, err := s.Store.DeleteTrip(r.Context(), trip.ID, trip.OwnerID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete trip")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
