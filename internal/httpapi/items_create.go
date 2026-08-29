package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/db"
	"caravel/internal/imaging"
)

// Creating a location used to take three requests: the item, then the cover
// photo, then each file. The first was transactional and the rest were not,
// so a cover that failed to fetch left a location behind with no picture --
// and, because the editor did not adopt the item it had just created,
// pressing Save again created a *second* one (Stage 23 Milestone 0 records
// the whole shape of it).
//
// The multipart variant below makes the whole location one request that
// either happens or does not. The JSON path is untouched: readJSON refuses
// unknown fields, and the item tests, the assistant and every other caller
// depend on it.

// maxItemCreateBytes caps a create request carrying a cover photo and
// attachments.
//
// It is deliberately not the sum of the per-part limits: an image of 50MB and
// four files of 50MB each would be a quarter of a gigabyte in one request,
// which is not a location being created, it is something else. 100MB is a
// generous cover plus a generous document, and a location wanting more than
// that can be created first and have the rest added from its own page, where
// POST /items/{id}/files still takes them one at a time.
const maxItemCreateBytes = 100 << 20

// pendingImage is a cover photo that has been fetched or decoded and stored
// in the blob store, waiting only for its database rows.
type pendingImage struct {
	assetID     string
	key         string
	contentType string
	width       int
	height      int
	// kind is "upload" for a posted file and "url" for one fetched from a
	// URL, matching what the standalone media endpoints record.
	kind        string
	externalURL *string
	sourceURL   *string
	credit      *string
	license     *string
}

// pendingFile is an attachment already written to the blob store.
type pendingFile struct {
	id          string
	filename    string
	key         string
	contentType string
	size        int64
	note        *string
	visibility  db.FileVisibility
}

// createItemMultipart handles POST /api/trips/{tripId}/items when the body is
// multipart: the item JSON in an "item" part, an optional cover as either an
// "image" file part or an "image_url" value, and zero or more "file" parts.
//
// The ordering is what makes it atomic. Everything is parsed, validated,
// decoded and fetched first, so every rejection happens before a single write.
// Then the blobs go down. Then one transaction writes the item, its nested
// location/links/dates, the media asset, the image attachment and every file
// row together.
//
// The one impurity is that a blob written in step two is orphaned if the
// transaction in step three rolls back. That is the impurity the code already
// had -- handleUploadMedia stores before CreateMediaAsset -- and it is
// invisible to the user: nothing references it and no location exists. Doing
// better would mean writing blobs inside the transaction, which cannot be
// rolled back either because the blob store is a filesystem.
func (s *Server) createItemMultipart(w http.ResponseWriter, r *http.Request, trip db.Trip) {
	r.Body = http.MaxBytesReader(w, r.Body, maxItemCreateBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request too large or invalid multipart form")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	// The item itself, as the same JSON the non-multipart path takes. Decoded
	// with the same strictness, so a typo in a field name is caught here too.
	raw := r.FormValue("item")
	if strings.TrimSpace(raw) == "" {
		writeError(w, http.StatusBadRequest, "missing item field")
		return
	}
	var req itemRequest
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item field: "+err.Error())
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The item ID is minted here rather than inside the transaction because a
	// file's storage key contains it (see uploadFile), and the blobs are
	// written before the transaction opens.
	itemID := uuid.NewString()

	image, status, err := s.stageImage(r, trip.ID)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	files, status, err := s.stageFiles(r, trip.ID, itemID)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	item, err := s.createItemTx(r.Context(), trip, itemID, req, image, files)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create item")
		return
	}
	writeJSON(w, http.StatusCreated, s.buildItemDetail(r, item))
}

// stageImage validates and stores the cover, if there is one. It returns a
// nil pendingImage when the request carries neither an "image" part nor an
// "image_url" value, and an HTTP status alongside any error so the caller can
// tell "you sent a bad image" from "we could not store it".
func (s *Server) stageImage(r *http.Request, tripID string) (*pendingImage, int, error) {
	file, header, ferr := r.FormFile("image")
	rawURL := strings.TrimSpace(r.FormValue("image_url"))

	switch {
	case ferr == nil:
		defer file.Close()
		if rawURL != "" {
			return nil, http.StatusBadRequest, fmt.Errorf("send either an image file or an image_url, not both")
		}
		if header.Size > maxImageUploadBytes {
			return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("image exceeds maximum size of %d bytes", maxImageUploadBytes)
		}
		result, err := imaging.DecodeAndResize(file)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("could not decode image: %w", err)
		}
		return s.storeImage(r.Context(), tripID, result, "upload", nil, nil, nil, nil)

	case rawURL != "":
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, http.StatusBadRequest, fmt.Errorf("image_url must be a valid http(s) URL")
		}
		// Provenance is sanitised exactly as handleCreateMediaURL does it: a
		// malformed source URL is dropped rather than refused, because the
		// image is the point and losing the picture over a bad credit field
		// would be the wrong trade.
		sourceURL := strings.TrimSpace(r.FormValue("source_url"))
		if sourceURL != "" {
			if p, perr := url.Parse(sourceURL); perr != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
				sourceURL = ""
			}
		}
		credit := truncateBytes(strings.TrimSpace(r.FormValue("credit")), maxCreditBytes)
		license := truncateBytes(strings.TrimSpace(r.FormValue("license")), maxCreditBytes)

		result, err := fetchImage(r.Context(), rawURL)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("could not fetch image from url: %w", err)
		}
		return s.storeImage(r.Context(), tripID, result, "url", &rawURL, optional(sourceURL), optional(credit), optional(license))

	default:
		return nil, 0, nil
	}
}

// storeImage writes the decoded bytes to the blob store and describes the
// rows the transaction will need.
func (s *Server) storeImage(ctx context.Context, tripID string, result imaging.Result, kind string,
	externalURL, sourceURL, credit, license *string) (*pendingImage, int, error) {

	id := uuid.NewString()
	key := fmt.Sprintf("%s/images/%s%s", tripID, id, extensionForContentType(result.ContentType))
	if _, err := s.Blob.Put(ctx, key, bytes.NewReader(result.Data)); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("could not store image")
	}
	return &pendingImage{
		assetID:     id,
		key:         key,
		contentType: result.ContentType,
		width:       result.Width,
		height:      result.Height,
		kind:        kind,
		externalURL: externalURL,
		sourceURL:   sourceURL,
		credit:      credit,
		license:     license,
	}, 0, nil
}

// stageFiles validates and stores the attachments. "file_note" and
// "file_visibility" are positional: the nth value belongs to the nth file
// part, which is how the browser sends them and the only ordering multipart
// gives us.
func (s *Server) stageFiles(r *http.Request, tripID, itemID string) ([]pendingFile, int, error) {
	if r.MultipartForm == nil {
		return nil, 0, nil
	}
	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		return nil, 0, nil
	}
	notes := r.MultipartForm.Value["file_note"]
	visibilities := r.MultipartForm.Value["file_visibility"]

	staged := make([]pendingFile, 0, len(headers))
	for i, header := range headers {
		if header.Size > maxFileUploadBytes {
			return nil, http.StatusRequestEntityTooLarge,
				fmt.Errorf("file %q exceeds maximum size of %d bytes", header.Filename, maxFileUploadBytes)
		}
		pf, err := s.stageOneFile(r.Context(), tripID, itemID, header, at(notes, i), at(visibilities, i))
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		staged = append(staged, pf)
	}
	return staged, 0, nil
}

func (s *Server) stageOneFile(ctx context.Context, tripID, itemID string,
	header *multipart.FileHeader, note, visibility string) (pendingFile, error) {

	f, err := header.Open()
	if err != nil {
		return pendingFile{}, fmt.Errorf("could not read uploaded file")
	}
	defer f.Close()

	// Sniffed rather than trusted, for the reason sniffContentType gives: a
	// mislabeled upload must not earn inline-display treatment.
	contentType, err := sniffContentType(f)
	if err != nil {
		return pendingFile{}, fmt.Errorf("could not read uploaded file")
	}

	id := uuid.NewString()
	filename := filepath.Base(header.Filename)
	key := fmt.Sprintf("%s/items/%s/%s-%s", tripID, itemID, id, filename)
	size, err := s.Blob.Put(ctx, key, f)
	if err != nil {
		return pendingFile{}, fmt.Errorf("could not store file")
	}

	vis := db.FileVisibility(strings.TrimSpace(visibility))
	if !vis.Valid() {
		// An absent or unrecognised value means "trip" -- the default, and the
		// failure direction that produces a visible file rather than a
		// silently hidden one, same as the standalone upload endpoint.
		vis = db.FileVisibilityTrip
	}
	var notePtr *string
	if trimmed := strings.TrimSpace(note); trimmed != "" {
		notePtr = &trimmed
	}

	return pendingFile{
		id:          id,
		filename:    filename,
		key:         key,
		contentType: contentType,
		size:        size,
		note:        notePtr,
		visibility:  vis,
	}, nil
}

// createItemTx is the single write: the item, its nested rows, the cover and
// the attachments, all inside one transaction.
func (s *Server) createItemTx(ctx context.Context, trip db.Trip, itemID string, req itemRequest,
	image *pendingImage, files []pendingFile) (db.Item, error) {

	showOnMap := true
	if req.ShowOnMap != nil {
		showOnMap = *req.ShowOnMap
	}
	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	uploader, hasUploader := auth.UserFromContext(ctx)
	now := time.Now().UTC()

	var item db.Item
	err := s.Store.WithTx(ctx, func(store db.Store) error {
		created, err := store.CreateItem(ctx, db.CreateItemParams{
			ID:        itemID,
			TripID:    trip.ID,
			Category:  req.Category,
				Title:     req.Title,
			Notes:     req.Notes,
			ShowOnMap: showOnMap,
			SortOrder: sortOrder,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := writeItemNested(ctx, store, created, req); err != nil {
			return err
		}

		if image != nil {
			asset, err := store.CreateMediaAsset(ctx, db.CreateMediaAssetParams{
				ID:          image.assetID,
				TripID:      trip.ID,
				Kind:        image.kind,
				StoragePath: &image.key,
				ExternalURL: image.externalURL,
				ContentType: &image.contentType,
				Width:       &image.width,
				Height:      &image.height,
				SourceURL:   image.sourceURL,
				Credit:      image.credit,
				License:     image.license,
				CreatedAt:   now,
			})
			if err != nil {
				return err
			}
			updated, err := store.SetItemImage(ctx, created.ID, trip.ID, &asset.ID, now)
			if err != nil {
				return err
			}
			created = updated
		}

		for _, f := range files {
			var ownerID *string
			if hasUploader {
				ownerID = &uploader.ID
			}
			if _, err := store.CreateFile(ctx, db.CreateFileParams{
				ID:          f.id,
				TripID:      trip.ID,
				ItemID:      &created.ID,
				Filename:    f.filename,
				StoragePath: f.key,
				ContentType: &f.contentType,
				Visibility:  f.visibility,
				OwnerUserID: ownerID,
				SizeBytes:   f.size,
				UploadedAt:  now,
				Note:        f.note,
			}); err != nil {
				return err
			}
		}

		item = created
		return nil
	})
	return item, err
}

// at returns the nth value of a positional multipart field, or "" when the
// client sent fewer of them than it sent files.
func at(values []string, i int) string {
	if i < len(values) {
		return values[i]
	}
	return ""
}
