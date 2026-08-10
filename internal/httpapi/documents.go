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

const maxDocumentUploadBytes = 50 << 20 // 50MB, per plan Section 3.4

// inlineSafeContentTypes lists MIME types the browser can be trusted to
// display inline instead of downloading. Deliberately excludes
// image/svg+xml — SVG can embed <script> and browsers execute it when
// rendered inline, making it an XSS vector unlike the raster/document types
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

type documentResponse struct {
	ID          string  `json:"id"`
	TripID      string  `json:"trip_id"`
	ItemID      *string `json:"item_id"`
	Filename    string  `json:"filename"`
	ContentType *string `json:"content_type"`
	SizeBytes   int64   `json:"size_bytes"`
	UploadedAt  string  `json:"uploaded_at"`
	Note        *string `json:"note"`
	DownloadURL string  `json:"download_url"`
}

func documentToResponse(d db.Document) documentResponse {
	return documentResponse{
		ID:          d.ID,
		TripID:      d.TripID,
		ItemID:      d.ItemID,
		Filename:    d.Filename,
		ContentType: d.ContentType,
		SizeBytes:   d.SizeBytes,
		UploadedAt:  d.UploadedAt.UTC().Format(time.RFC3339),
		Note:        d.Note,
		DownloadURL: fmt.Sprintf("/api/documents/%s/download", d.ID),
	}
}

// uploadDocument handles the shared multipart-upload logic for both
// trip-level and item-level documents; itemID is nil for trip-level.
func (s *Server) uploadDocument(w http.ResponseWriter, r *http.Request, tripID string, itemID *string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentUploadBytes)
	if err := r.ParseMultipartForm(maxDocumentUploadBytes); err != nil {
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
		key = fmt.Sprintf("%s/documents/%s-%s", tripID, id, filename)
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

	doc, err := s.Store.CreateDocument(r.Context(), db.CreateDocumentParams{
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
		writeError(w, http.StatusInternalServerError, "could not save document")
		return
	}
	writeJSON(w, http.StatusCreated, documentToResponse(doc))
}

func (s *Server) handleListTripDocuments(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}
	docs, err := s.Store.ListTripDocuments(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list documents")
		return
	}
	resp := make([]documentResponse, len(docs))
	for i, d := range docs {
		resp[i] = documentToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUploadTripDocument(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}
	s.uploadDocument(w, r, trip.ID, nil)
}

func (s *Server) handleListItemDocuments(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}
	docs, err := s.Store.ListItemDocuments(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list documents")
		return
	}
	resp := make([]documentResponse, len(docs))
	for i, d := range docs {
		resp[i] = documentToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUploadItemDocument(w http.ResponseWriter, r *http.Request) {
	item, ok := s.loadOwnedItem(w, r)
	if !ok {
		return
	}
	s.uploadDocument(w, r, item.TripID, &item.ID)
}

// loadOwnedDocument fetches the document named by {docId} and confirms the
// current user owns its trip.
func (s *Server) loadOwnedDocument(w http.ResponseWriter, r *http.Request) (db.Document, bool) {
	docID := chi.URLParam(r, "docId")
	doc, err := s.Store.GetDocumentByID(r.Context(), docID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "document not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load document")
		}
		return db.Document{}, false
	}
	if !s.hasTripAccess(r, doc.TripID) {
		writeError(w, http.StatusNotFound, "document not found")
		return db.Document{}, false
	}
	return doc, true
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.loadOwnedDocument(w, r)
	if !ok {
		return
	}
	deleted, err := s.Store.DeleteDocument(r.Context(), doc.ID, doc.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete document")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}
	_ = s.Blob.Delete(r.Context(), doc.StoragePath)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDownloadDocument(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.loadOwnedDocument(w, r)
	if !ok {
		return
	}

	f, err := s.Blob.Open(r.Context(), doc.StoragePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "document file not found")
		return
	}
	defer f.Close()

	if doc.ContentType != nil {
		w.Header().Set("Content-Type", *doc.ContentType)
	}
	disposition := "attachment"
	if doc.ContentType != nil && isInlineSafeContentType(*doc.ContentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, sanitizeForHeader(doc.Filename)))
	http.ServeContent(w, r, doc.Filename, doc.UploadedAt, f)
}

func sanitizeForHeader(s string) string {
	return strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(s)
}
