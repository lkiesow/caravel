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

// tripOwnerResponse names a trip's owner to someone who is not the owner, so
// the client can say who shared it. It carries no id: this is a label, and
// handing every collaborator the owner's user id would be a wider disclosure
// than the feature needs.
type tripOwnerResponse struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

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
	// Role is the *reading* user's role on this trip, so the client can decide
	// what to render rather than discovering it from a 403.
	Role string `json:"role"`
	// Owner is present only when the reader is not the owner. On your own trip
	// it would say what you already know, and omitting it keeps the common case
	// free of an extra lookup.
	Owner *tripOwnerResponse `json:"owner"`
	// MemberCount counts people on the trip besides its owner. Zero means solo,
	// which is what decides whether the client offers per-file visibility: on a
	// trip nobody else can see, personal versus trip-visible is a question with
	// only one possible answer.
	MemberCount int64 `json:"member_count"`
}

func (s *Server) tripToResponse(ctx context.Context, t db.Trip, role db.TripRole) tripResponse {
	resp := tripResponse{
		ID:             t.ID,
		Title:          t.Title,
		StartDate:      t.StartDate,
		EndDate:        t.EndDate,
		Subtitle:       t.Subtitle,
		PreviewImageID: t.PreviewImageID,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
		Role:           string(role),
	}
	resp.PreviewImageURL = s.resolveImageURL(ctx, t.PreviewImageID)
	// One extra count on a single-trip response. A failure leaves it at zero,
	// which renders as a solo trip — the visibility control disappears rather
	// than the page failing, and the server would refuse a bad value anyway.
	if n, err := s.Store.CountTripMembers(ctx, t.ID); err == nil {
		resp.MemberCount = n
	}
	if role != db.RoleOwner {
		// One extra lookup, on a single-trip response only — the list endpoint
		// gets the owner's name from its own join instead. A failure here is
		// not worth failing the whole response over: the trip is readable, it
		// just renders without "shared by".
		if owner, err := s.Store.GetUserByID(ctx, t.OwnerID); err == nil {
			resp.Owner = &tripOwnerResponse{Username: owner.Username, DisplayName: owner.DisplayName}
		}
	}
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

	trips, err := s.Store.ListTripsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list trips")
		return
	}

	resp := make([]tripResponse, len(trips))
	for i, t := range trips {
		// Built directly rather than through tripToResponse: the query already
		// returned the role and the owner's name, and going through the helper
		// would spend a GetUserByID per shared trip re-fetching what we have.
		item := tripResponse{
			ID:             t.ID,
			Title:          t.Title,
			StartDate:      t.StartDate,
			EndDate:        t.EndDate,
			Subtitle:       t.Subtitle,
			PreviewImageID: t.PreviewImageID,
			CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
			Role:           string(t.Role),
			MemberCount:    t.MemberCount,
		}
		item.PreviewImageURL = s.resolveImageURL(r.Context(), t.PreviewImageID)
		if t.Role != db.RoleOwner {
			item.Owner = &tripOwnerResponse{Username: t.OwnerUsername, DisplayName: t.OwnerDisplayName}
		}
		resp[i] = item
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
	writeJSON(w, http.StatusCreated, s.tripToResponse(r.Context(), trip, db.RoleOwner))
}

func (s *Server) handleGetTrip(w http.ResponseWriter, r *http.Request) {
	trip, role, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.tripToResponse(r.Context(), trip, role))
}

func (s *Server) handleUpdateTrip(w http.ResponseWriter, r *http.Request) {
	trip, role, ok := s.loadTrip(w, r, db.RoleEditor)
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
	writeJSON(w, http.StatusOK, s.tripToResponse(r.Context(), updated, role))
}

type setPreviewImageRequest struct {
	MediaAssetID *string `json:"media_asset_id"`
}

func (s *Server) handleSetTripPreviewImage(w http.ResponseWriter, r *http.Request) {
	trip, role, ok := s.loadTrip(w, r, db.RoleEditor)
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
	writeJSON(w, http.StatusOK, s.tripToResponse(r.Context(), updated, role))
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
