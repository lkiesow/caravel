package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/db"
)

const maxFileUploadBytes = 50 << 20 // 50MB, per plan Section 3.4

// inlineSafeContentTypes lists MIME types the browser can be trusted to
// display inline instead of downloading. Deliberately excludes
// image/svg+xml — SVG can embed <script> and browsers execute it when
// rendered inline, making it an XSS vector unlike the raster/file types
// below.
var inlineSafeContentTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"text/plain":      true,
}

func isInlineSafeContentType(contentType string) bool {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return inlineSafeContentTypes[strings.TrimSpace(contentType)]
}

// sniffContentType detects a file's MIME type from its content rather than
// trusting the client-supplied multipart Content-Type header, so a
// mislabeled upload (e.g. an HTML file declared as an image type) can't
// get inline-display treatment it shouldn't. Rewinds f back to the start
// once done so the caller can still read the full file afterward.
func sniffContentType(f io.ReadSeeker) (string, error) {
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

type fileResponse struct {
	ID          string  `json:"id"`
	TripID      string  `json:"trip_id"`
	ItemID      *string `json:"item_id"`
	Filename    string  `json:"filename"`
	ContentType *string `json:"content_type"`
	SizeBytes   int64   `json:"size_bytes"`
	UploadedAt  string  `json:"uploaded_at"`
	Note        *string `json:"note"`
	DownloadURL string  `json:"download_url"`
	// The title of the location this file is attached to, for the trip-level
	// list where trip files and location files appear together. Null for a
	// trip-level file, and null on every other endpoint: the item-level list
	// and the upload responses know their location from context, so only the
	// trip listing pays for the join.
	ItemTitle *string `json:"item_title"`
}

func fileToResponse(d db.File) fileResponse {
	return fileResponse{
		ID:          d.ID,
		TripID:      d.TripID,
		ItemID:      d.ItemID,
		Filename:    d.Filename,
		ContentType: d.ContentType,
		SizeBytes:   d.SizeBytes,
		UploadedAt:  d.UploadedAt.UTC().Format(time.RFC3339),
		Note:        d.Note,
		DownloadURL: fmt.Sprintf("/api/files/%s/download", d.ID),
	}
}

// fileDetailToResponse wraps the plain mapper rather than repeating it, so
// a new field on fileResponse can't end up set on one path and not the
// other.
func fileDetailToResponse(d db.FileDetail) fileResponse {
	resp := fileToResponse(d.File)
	resp.ItemTitle = d.ItemTitle
	return resp
}

// uploadFile handles the shared multipart-upload logic for both
// trip-level and item-level files; itemID is nil for trip-level.
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request, tripID string, itemID *string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadBytes)
	if err := r.ParseMultipartForm(maxFileUploadBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large or invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	detectedContentType, err := sniffContentType(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read uploaded file")
		return
	}

	id := uuid.NewString()
	filename := filepath.Base(header.Filename)
	var key string
	if itemID != nil {
		key = fmt.Sprintf("%s/items/%s/%s-%s", tripID, *itemID, id, filename)
	} else {
		key = fmt.Sprintf("%s/files/%s-%s", tripID, id, filename)
	}

	size, err := s.Blob.Put(r.Context(), key, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store file")
		return
	}

	contentTypePtr := &detectedContentType

	var notePtr *string
	if note := strings.TrimSpace(r.FormValue("note")); note != "" {
		notePtr = &note
	}

	row, err := s.Store.CreateFile(r.Context(), db.CreateFileParams{
		ID:          id,
		TripID:      tripID,
		ItemID:      itemID,
		Filename:    filename,
		StoragePath: key,
		ContentType: contentTypePtr,
		SizeBytes:   size,
		UploadedAt:  time.Now().UTC(),
		Note:        notePtr,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file")
		return
	}
	writeJSON(w, http.StatusCreated, fileToResponse(row))
}

func (s *Server) handleListTripFiles(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}
	files, err := s.Store.ListTripFiles(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	resp := make([]fileResponse, len(files))
	for i, d := range files {
		resp[i] = fileDetailToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUploadTripFile(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}
	s.uploadFile(w, r, trip.ID, nil)
}

func (s *Server) handleListItemFiles(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}
	files, err := s.Store.ListItemFiles(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	resp := make([]fileResponse, len(files))
	for i, d := range files {
		resp[i] = fileToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUploadItemFile(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}
	s.uploadFile(w, r, item.TripID, &item.ID)
}

// loadOwnedFile fetches the file named by {fileId} and confirms the
// current user owns its trip.
func (s *Server) loadOwnedFile(w http.ResponseWriter, r *http.Request) (db.File, bool) {
	fileID := chi.URLParam(r, "fileId")
	file, err := s.Store.GetFileByID(r.Context(), fileID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load file")
		}
		return db.File{}, false
	}
	if !s.hasTripAccess(r, file.TripID) {
		writeError(w, http.StatusNotFound, "file not found")
		return db.File{}, false
	}
	return file, true
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	file, ok := s.loadOwnedFile(w, r)
	if !ok {
		return
	}
	deleted, err := s.Store.DeleteFile(r.Context(), file.ID, file.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete file")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	_ = s.Blob.Delete(r.Context(), file.StoragePath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	file, ok := s.loadOwnedFile(w, r)
	if !ok {
		return
	}

	f, err := s.Blob.Open(r.Context(), file.StoragePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file file not found")
		return
	}
	defer f.Close()

	if file.ContentType != nil {
		w.Header().Set("Content-Type", *file.ContentType)
	}
	disposition := "attachment"
	if file.ContentType != nil && isInlineSafeContentType(*file.ContentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, sanitizeForHeader(file.Filename)))
	http.ServeContent(w, r, file.Filename, file.UploadedAt, f)
}

func sanitizeForHeader(s string) string {
	return strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(s)
}
