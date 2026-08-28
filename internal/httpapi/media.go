package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/buildinfo"
	"caravel/internal/db"
	"caravel/internal/imaging"
)

const (
	// maxImageUploadBytes bounds transfer and buffering, nothing else. It is
	// deliberately not a proxy for decode cost: an image is downscaled to
	// imaging.MaxDimension and re-encoded before storage, so a 50MB source and
	// a 3MB source occupy the same space on disk, and what a decode actually
	// costs is pixels rather than bytes -- imaging.MaxPixels guards that.
	// Matched to maxFileUploadBytes so operators have one body-size number to
	// configure in front of Caravel.
	maxImageUploadBytes = 50 << 20 // 50MB
	// imageFetchTimeout covers the whole download. It has to leave room for
	// maxImageUploadBytes over an ordinary connection; at 15s the timeout, not
	// the limit, was what a large image met first.
	imageFetchTimeout = 60 * time.Second
)

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
	// Any asset with a local StoragePath is served from this instance,
	// regardless of Kind — Kind="url" now only means "was originally added
	// by pasting a URL" (provenance), not "still served from that URL".
	// Only assets created before this fetch-and-cache behavior existed
	// (Kind="url" with no StoragePath) still fall back to hotlinking.
	if m.StoragePath == nil && m.ExternalURL != nil {
		resp.URL = *m.ExternalURL
	} else {
		resp.URL = fmt.Sprintf("/api/media/%s/file", m.ID)
	}
	return resp
}

// imageCreditResponse is who a stored image is owed to.
//
// Null for everything anybody uploaded themselves, which is most images. Sent
// as a nested object rather than three parallel nullable strings because the
// three only ever travel together and the client shows them as one line.
type imageCreditResponse struct {
	// Text is the author, as plain text. Empty when the source stated none --
	// which happens, and is not the same as there being no source.
	Text string `json:"text"`
	// License is the short name, such as "CC BY-SA 4.0".
	License string `json:"license"`
	// SourceURL is the page the image came from, which is what the credit
	// links to.
	SourceURL string `json:"source_url"`
}

// resolveImageURL looks up imageID's media asset and returns its URL, or nil
// if imageID is nil or the asset can't be found (e.g. already deleted).
func (s *Server) resolveImageURL(ctx context.Context, imageID *string) *string {
	url, _ := s.resolveImage(ctx, imageID)
	return url
}

// resolveImage returns an image's URL and its credit in one lookup, for the
// callers that show both. Kept beside resolveImageURL rather than replacing it
// because most callers render a thumbnail and have nowhere to put a credit.
func (s *Server) resolveImage(ctx context.Context, imageID *string) (*string, *imageCreditResponse) {
	if imageID == nil {
		return nil, nil
	}
	asset, err := s.Store.GetMediaAssetByID(ctx, *imageID)
	if err != nil {
		return nil, nil
	}
	url := mediaAssetToResponse(asset).URL

	// A credit exists when there is somewhere it points. An image with an
	// author and no source page cannot be credited usefully, and one with a
	// source and no named author still can -- "from this page" is a real
	// attribution.
	if asset.SourceURL == nil {
		return &url, nil
	}
	credit := &imageCreditResponse{SourceURL: *asset.SourceURL}
	if asset.Credit != nil {
		credit.Text = *asset.Credit
	}
	if asset.License != nil {
		credit.License = *asset.License
	}
	return &url, credit
}

// handleUploadMedia handles POST /api/trips/{tripId}/media (multipart, field "file").
func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
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

const (
	// maxCreditBytes caps the attribution strings. A credit line is a name and
	// perhaps a licence; anything longer is not a credit, and these are
	// rendered on a page.
	maxCreditBytes = 500
)

type mediaURLRequest struct {
	URL string `json:"url"`
	// Provenance, all optional and all empty for an image somebody pasted
	// themselves. Populated when the assistant proposed the image: SourceURL
	// is the page it came from, and Credit and License are what a freely
	// licensed photograph requires and an ordinary upload does not.
	//
	// Accepted from the client rather than re-derived here because the client
	// is passing back what the *assistant* found, and the assistant is the
	// only thing that knows -- the image URL alone says nothing about who took
	// the photograph. That does mean a caller could send any credit it likes;
	// the blast radius is a wrong attribution on that person's own trip, which
	// is the same thing they could achieve by typing one.
	SourceURL string `json:"source_url"`
	Credit    string `json:"credit"`
	License   string `json:"license"`
}

// handleCreateMediaURL handles POST /api/trips/{tripId}/media/url.
func (s *Server) handleCreateMediaURL(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
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

	// A source URL that is not a URL is dropped rather than refused: the image
	// is the point, and a malformed provenance field should not cost somebody
	// the picture. The credit strings are trimmed to a sane length for the
	// same reason -- reject the value, keep the image.
	sourceURL := strings.TrimSpace(req.SourceURL)
	if sourceURL != "" {
		if p, err := url.Parse(sourceURL); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			sourceURL = ""
		}
	}
	credit := truncateBytes(strings.TrimSpace(req.Credit), maxCreditBytes)
	license := truncateBytes(strings.TrimSpace(req.License), maxCreditBytes)

	result, err := fetchImage(r.Context(), req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not fetch image from url: "+err.Error())
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
		Kind:        "url",
		StoragePath: &key,
		ExternalURL: &req.URL,
		ContentType: &result.ContentType,
		Width:       &result.Width,
		Height:      &result.Height,
		SourceURL:   optional(sourceURL),
		Credit:      optional(credit),
		License:     optional(license),
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save media asset")
		return
	}
	writeJSON(w, http.StatusCreated, mediaAssetToResponse(asset))
}

// optional turns an empty string into nil, which is what the column means:
// "there is no credit" rather than "the credit is the empty string".
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// truncateBytes cuts on a rune boundary, because cutting mid-rune produces
// replacement characters in something that will be rendered.
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	for len(string(runes)) > max {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// fetchImage downloads rawURL (bounded by imageFetchTimeout and
// maxImageUploadBytes, same limits as a direct upload) and decodes/resizes
// it exactly like an uploaded file, so a pasted-URL image ends up stored
// and served identically to one the user uploaded directly.
func fetchImage(ctx context.Context, rawURL string) (imaging.Result, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, imageFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return imaging.Result{}, err
	}
	// Identify ourselves, the same way internal/geocode and internal/wikimedia
	// do. Not politeness: Wikimedia answers the default Go User-Agent with
	// 403, so without this every cover the assistant found on Wikipedia failed
	// to download while the same URL opened fine in a browser.
	req.Header.Set("User-Agent", "Caravel/"+buildinfo.Version+" (self-hosted trip planner)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return imaging.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return imaging.Result{}, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageUploadBytes+1))
	if err != nil {
		return imaging.Result{}, err
	}
	if len(data) > maxImageUploadBytes {
		return imaging.Result{}, fmt.Errorf("image exceeds maximum size of %d bytes", maxImageUploadBytes)
	}

	return imaging.DecodeAndResize(bytes.NewReader(data))
}

// handleServeMedia handles GET /api/media/{mediaId}/file — serves any asset
// with a local StoragePath, regardless of Kind. Only pre-Stage-03 "url" kind
// assets (created before linked images were fetched and cached locally)
// have no StoragePath and never hit this route; the frontend serves those
// directly from their ExternalURL instead.
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
	if _, _, ok := s.authorizeTrip(w, r, asset.TripID, db.RoleViewer, "media not found"); !ok {
		return
	}
	if asset.StoragePath == nil {
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
