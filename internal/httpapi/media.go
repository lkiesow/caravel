package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/db"
	"caravel/internal/imaging"
)

const maxImageUploadBytes = 15 << 20 // 15MB, per plan Section 3.4

type mediaAssetResponse struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	URL         string  `json:"url"`
	ContentType *string `json:"content_type"`
	Width       *int    `json:"width"`
	Height      *int    `json:"height"`
}

func mediaAssetToResponse(m db.MediaAsset) mediaAssetResponse {
	resp := mediaAssetResponse{ID: m.ID, Kind: m.Kind, ContentType: m.ContentType, Width: m.Width, Height: m.Height}
	if m.Kind == "url" && m.ExternalURL != nil {
		resp.URL = *m.ExternalURL
	} else {
		resp.URL = fmt.Sprintf("/api/media/%s/file", m.ID)
	}
	return resp
}

// handleUploadMedia handles POST /api/trips/{tripId}/media (multipart, field "file").
func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImageUploadBytes)
	if err := r.ParseMultipartForm(maxImageUploadBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large or invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	result, err := imaging.DecodeAndResize(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not decode image: "+err.Error())
		return
	}

	id := uuid.NewString()
	ext := extensionForContentType(result.ContentType)
	key := fmt.Sprintf("%s/images/%s%s", trip.ID, id, ext)

	if _, err := s.Blob.Put(r.Context(), key, bytes.NewReader(result.Data)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not store image")
		return
	}

	asset, err := s.Store.CreateMediaAsset(r.Context(), db.CreateMediaAssetParams{
		ID:          id,
		TripID:      trip.ID,
		Kind:        "upload",
		StoragePath: &key,
		ContentType: &result.ContentType,
		Width:       &result.Width,
		Height:      &result.Height,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save media asset")
		return
	}
	writeJSON(w, http.StatusCreated, mediaAssetToResponse(asset))
}

type mediaURLRequest struct {
	URL string `json:"url"`
}

// handleCreateMediaURL handles POST /api/trips/{tripId}/media/url.
func (s *Server) handleCreateMediaURL(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}

	var req mediaURLRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "url must be a valid http(s) URL")
		return
	}

	asset, err := s.Store.CreateMediaAsset(r.Context(), db.CreateMediaAssetParams{
		ID:          uuid.NewString(),
		TripID:      trip.ID,
		Kind:        "url",
		ExternalURL: &req.URL,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save media asset")
		return
	}
	writeJSON(w, http.StatusCreated, mediaAssetToResponse(asset))
}

// handleServeMedia handles GET /api/media/{mediaId}/file — only meaningful
// for kind="upload" assets; kind="url" assets are served directly from
// their ExternalURL by the frontend and never hit this route.
func (s *Server) handleServeMedia(w http.ResponseWriter, r *http.Request) {
	mediaID := chi.URLParam(r, "mediaId")

	asset, err := s.Store.GetMediaAssetByID(r.Context(), mediaID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "media not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load media")
		}
		return
	}
	if !s.hasTripAccess(r, asset.TripID) {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	if asset.Kind != "upload" || asset.StoragePath == nil {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}

	f, err := s.Blob.Open(r.Context(), *asset.StoragePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "media file not found")
		return
	}
	defer f.Close()

	if asset.ContentType != nil {
		w.Header().Set("Content-Type", *asset.ContentType)
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, mediaID, asset.CreatedAt, f)
}

// hasTripAccess reports whether the current request's user owns tripID —
// used by handlers keyed on a trip ID that isn't itself the {tripId} route
// param (e.g. resolving a media asset's owning trip).
func (s *Server) hasTripAccess(r *http.Request, tripID string) bool {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return false
	}
	trip, err := s.Store.GetTripByID(r.Context(), tripID)
	if err != nil {
		return false
	}
	return trip.OwnerID == user.ID
}

func extensionForContentType(ct string) string {
	switch ct {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}
