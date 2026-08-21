package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
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
	// Visibility is "personal" or "trip". Always sent, so the client never has
	// to infer a default.
	Visibility string `json:"visibility"`
	// IsMine says whether the reading user uploaded this file, which is what
	// decides whether they may change its visibility. Sent rather than an owner
	// id: the client only ever asks "may I", and naming the uploader of every
	// shared file would be a wider disclosure than the feature needs.
	IsMine bool `json:"is_mine"`
}

func fileToResponse(d db.File, readerID string) fileResponse {
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
		Visibility:  string(d.Visibility),
		IsMine:      d.OwnerUserID != nil && *d.OwnerUserID == readerID,
	}
}

// fileDetailToResponse wraps the plain mapper rather than repeating it, so
// a new field on fileResponse can't end up set on one path and not the
// other.
func fileDetailToResponse(d db.FileDetail, readerID string) fileResponse {
	resp := fileToResponse(d.File, readerID)
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

	// Visibility rides along in the multipart form, since this is the one
	// request that creates the file and the choice is made before it is sent.
	// An absent or unrecognised value means "trip" — the default, and the
	// failure direction that produces a visible file rather than a silently
	// hidden one. Nothing is lost by guessing wrong: the uploader can change it
	// from the row menu afterwards.
	visibility := db.FileVisibility(strings.TrimSpace(r.FormValue("visibility")))
	if !visibility.Valid() {
		visibility = db.FileVisibilityTrip
	}
	uploader, _ := auth.UserFromContext(r.Context())

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
		Visibility:  visibility,
		OwnerUserID: &uploader.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file")
		return
	}
	writeJSON(w, http.StatusCreated, fileToResponse(row, uploader.ID))
}

func (s *Server) handleListTripFiles(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	files, err := s.Store.ListTripFiles(r.Context(), trip.ID, me.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	resp := make([]fileResponse, len(files))
	for i, d := range files {
		resp[i] = fileDetailToResponse(d, me.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUploadTripFile(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}
	s.uploadFile(w, r, trip.ID, nil)
}

func (s *Server) handleListItemFiles(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleViewer)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	files, err := s.Store.ListItemFiles(r.Context(), item.ID, me.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	resp := make([]fileResponse, len(files))
	for i, d := range files {
		resp[i] = fileToResponse(d, me.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUploadItemFile(w http.ResponseWriter, r *http.Request) {
	item, _, ok := s.loadItem(w, r, db.RoleEditor)
	if !ok {
		return
	}
	s.uploadFile(w, r, item.TripID, &item.ID)
}

// A note is a pointer so the request can tell "leave it alone" apart from
// "clear it" — except that this endpoint has exactly one field, so an absent
// note and a null one mean the same thing here: clear. An empty or
// whitespace-only string clears it too, trimmed the same way the upload path
// trims it, so a note can't come back as " ".
type fileNoteRequest struct {
	Note *string `json:"note"`
}

func (s *Server) handleUpdateFileNote(w http.ResponseWriter, r *http.Request) {
	file, _, ok := s.loadFile(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req fileNoteRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var notePtr *string
	if req.Note != nil {
		if note := strings.TrimSpace(*req.Note); note != "" {
			notePtr = &note
		}
	}

	updated, err := s.Store.UpdateFileNote(r.Context(), file.ID, file.TripID, notePtr)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not update file")
		}
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, fileToResponse(updated, me.ID))
}

type fileVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

// handleSetFileVisibility is the one file route restricted to the uploader
// rather than to a trip role.
//
// An editor may rename or delete a shared file — that is what editing a trip
// means — but making someone else's file public, or hiding it from the trip, is
// a decision about *their* document. So this checks ownership on top of the
// editor role rather than instead of it: a viewer cannot reach it either, since
// they cannot write anything.
func (s *Server) handleSetFileVisibility(w http.ResponseWriter, r *http.Request) {
	file, _, ok := s.loadFile(w, r, db.RoleEditor)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	if file.OwnerUserID == nil || *file.OwnerUserID != me.ID {
		writeErrorCode(w, http.StatusForbidden, "not_file_owner",
			"only the person who uploaded a file can change who sees it")
		return
	}

	var req fileVisibilityRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	visibility := db.FileVisibility(req.Visibility)
	if !visibility.Valid() {
		writeError(w, http.StatusBadRequest, "visibility must be personal or trip")
		return
	}

	updated, err := s.Store.SetFileVisibility(r.Context(), file.ID, file.TripID, visibility)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not update file")
		}
		return
	}
	writeJSON(w, http.StatusOK, fileToResponse(updated, me.ID))
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	file, _, ok := s.loadFile(w, r, db.RoleEditor)
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
	file, _, ok := s.loadFile(w, r, db.RoleViewer)
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
