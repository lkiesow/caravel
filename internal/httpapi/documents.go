package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/db"
)

const maxDocumentUploadBytes = 50 << 20 // 50MB, per plan Section 3.4

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

	contentType := header.Header.Get("Content-Type")
	var contentTypePtr *string
	if contentType != "" {
		contentTypePtr = &contentType
	}

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
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeForHeader(doc.Filename)))
	http.ServeContent(w, r, doc.Filename, doc.UploadedAt, f)
}

func sanitizeForHeader(s string) string {
	return strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(s)
}
