package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/auth"
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
	// Visibility is personal, trip or shared. Always sent, so the client never
	// infers a default.
	Visibility string `json:"visibility"`
	// IsMine says whether the reading user created this list, which is what
	// decides who may change its visibility. No owner id is sent: the client
	// only ever asks whether it may act.
	IsMine bool `json:"is_mine"`
	// CanTick is the one permission the client cannot work out from the other
	// two: a trip-visible list is readable by everyone and tickable only by its
	// author, so somebody looking at another person's list has to be told
	// rather than left to deduce it.
	CanTick bool `json:"can_tick"`
}

func checklistItemToResponse(i db.ChecklistItem) checklistItemResponse {
	return checklistItemResponse{ID: i.ID, Text: i.Text, Checked: i.Checked, SortOrder: i.SortOrder}
}

func (s *Server) checklistToResponse(ctx context.Context, c db.Checklist, readerID string, role db.TripRole) (checklistResponse, error) {
	items, err := s.Store.ListChecklistItemsByChecklist(ctx, c.ID)
	if err != nil {
		return checklistResponse{}, err
	}
	itemResponses := make([]checklistItemResponse, len(items))
	for i, item := range items {
		itemResponses[i] = checklistItemToResponse(item)
	}
	return checklistResponse{
		ID:         c.ID,
		TripID:     c.TripID,
		Title:      c.Title,
		SortOrder:  c.SortOrder,
		Items:      itemResponses,
		Visibility: string(c.Visibility),
		IsMine:     c.OwnerUserID != nil && *c.OwnerUserID == readerID,
		CanTick:    canModifyChecklist(c, readerID, role),
	}, nil
}

// canModifyChecklist decides who may tick, add and remove items, rename the
// list, and delete it. Not who may *see* it: that is the list predicate and
// loadChecklist.
//
// The three visibilities differ exactly here:
//
//	personal  its author only, and nobody else can see it anyway
//	trip      its author only; everyone else reads it
//	shared    any editor on the trip
//
// Changing the visibility itself is author-only in every case and is checked
// separately, because a shared list is still somebody's decision to have
// shared.
func canModifyChecklist(c db.Checklist, userID string, role db.TripRole) bool {
	if !role.AtLeast(db.RoleEditor) {
		return false
	}
	if c.Visibility == db.ChecklistShared {
		return true
	}
	return c.OwnerUserID != nil && *c.OwnerUserID == userID
}

func (s *Server) handleListChecklists(w http.ResponseWriter, r *http.Request) {
	trip, role, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	checklists, err := s.Store.ListChecklistsByTrip(r.Context(), trip.ID, me.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list checklists")
		return
	}
	resp := make([]checklistResponse, len(checklists))
	for i, c := range checklists {
		cr, err := s.checklistToResponse(r.Context(), c, me.ID, role)
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
	// Absent or unrecognised means shared, which is the direction that produces
	// a list everyone can use rather than one silently hidden from them.
	Visibility string `json:"visibility"`
}

func (s *Server) handleCreateChecklist(w http.ResponseWriter, r *http.Request) {
	trip, role, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())

	var req checklistRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	visibility := db.ChecklistVisibility(req.Visibility)
	if !visibility.Valid() {
		visibility = db.ChecklistShared
	}

	existing, err := s.Store.ListChecklistsByTrip(r.Context(), trip.ID, me.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist")
		return
	}

	checklist, err := s.Store.CreateChecklist(r.Context(), db.CreateChecklistParams{
		ID:          uuid.NewString(),
		TripID:      trip.ID,
		Title:       req.Title,
		SortOrder:   len(existing),
		CreatedAt:   time.Now().UTC(),
		Visibility:  visibility,
		OwnerUserID: &me.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist")
		return
	}
	resp, err := s.checklistToResponse(r.Context(), checklist, me.ID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create checklist")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

type duplicateChecklistRequest struct {
	// The copy's title, built by the client. The server has no user-facing copy
	// of its own and should not grow any: "(copy)" is a translated string and
	// belongs in web/locales, not here.
	Title string `json:"title"`
}

// handleDuplicateChecklist copies a list and its items. Its whole reason for
// existing is reuse across trips - last year's packing list, minus the ticks.
//
// Three decisions worth stating, because none of them is forced by the schema:
//
// The ticks reset. Both answers were defensible (splitting one list in two
// wants them kept), and reuse is the case the backlog actually described, so
// this is the one that ships - as a single menu item rather than two.
//
// The copy is *mine*, whoever made the original. Otherwise duplicating
// somebody else's trip-visible list would produce a list I cannot tick, which
// is a strange thing for an action of mine to do. Visibility carries over
// unchanged: copying is not the way to change who sees something, and
// PUT /visibility already exists for that.
//
// Authorization is the *read* rule, not the write one - deliberately no
// requireChecklistWrite call here. Copying a list I can see is a create on the
// trip, so editor is the bar, and loadChecklist has already answered 404 for
// somebody else's personal list and 403 for a viewer. requireChecklistWrite
// would additionally refuse another person's trip-visible list, which is
// exactly the list most worth copying.
func (s *Server) handleDuplicateChecklist(w http.ResponseWriter, r *http.Request) {
	source, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())

	var req duplicateChecklistRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	items, err := s.Store.ListChecklistItemsByChecklist(r.Context(), source.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not duplicate checklist")
		return
	}
	existing, err := s.Store.ListChecklistsByTrip(r.Context(), source.TripID, me.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not duplicate checklist")
		return
	}

	// One transaction for the list and every item: a copy that half succeeded
	// would leave a list whose contents silently disagree with the one it was
	// copied from, and there is no way for the client to tell.
	now := time.Now().UTC()
	var copied db.Checklist
	err = s.Store.WithTx(r.Context(), func(store db.Store) error {
		created, err := store.CreateChecklist(r.Context(), db.CreateChecklistParams{
			ID:          uuid.NewString(),
			TripID:      source.TripID,
			Title:       strings.TrimSpace(req.Title),
			SortOrder:   len(existing),
			CreatedAt:   now,
			Visibility:  source.Visibility,
			OwnerUserID: &me.ID,
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := store.CreateChecklistItem(r.Context(), db.CreateChecklistItemParams{
				ID:          uuid.NewString(),
				ChecklistID: created.ID,
				Text:        item.Text,
				SortOrder:   item.SortOrder,
				CreatedAt:   now,
			}); err != nil {
				return err
			}
		}
		copied = created
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not duplicate checklist")
		return
	}

	resp, err := s.checklistToResponse(r.Context(), copied, me.ID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not duplicate checklist")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// requireChecklistWrite is the write half of the visibility rule: loadChecklist
// already refused a list the caller cannot see, and this refuses one they can
// see but not change — somebody else's trip-visible list. 403 rather than 404,
// because they can read it and know perfectly well it is there.
func (s *Server) requireChecklistWrite(w http.ResponseWriter, r *http.Request, c db.Checklist, role db.TripRole) bool {
	me, _ := auth.UserFromContext(r.Context())
	if canModifyChecklist(c, me.ID, role) {
		return true
	}
	writeErrorCode(w, http.StatusForbidden, "not_checklist_owner",
		"only the person who made this list can change it")
	return false
}

func (s *Server) handleDeleteChecklist(w http.ResponseWriter, r *http.Request) {
	checklist, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if !s.requireChecklistWrite(w, r, checklist, role) {
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

type checklistVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

// handleSetChecklistVisibility is author-only even for a shared list. An editor
// may tick and rename a shared list — that is what sharing it meant — but
// deciding who sees it at all stays with whoever made that decision.
func (s *Server) handleSetChecklistVisibility(w http.ResponseWriter, r *http.Request) {
	checklist, _, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	if checklist.OwnerUserID == nil || *checklist.OwnerUserID != me.ID {
		writeErrorCode(w, http.StatusForbidden, "not_checklist_owner",
			"only the person who made this list can change who sees it")
		return
	}

	var req checklistVisibilityRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	visibility := db.ChecklistVisibility(req.Visibility)
	if !visibility.Valid() {
		writeError(w, http.StatusBadRequest, "visibility must be personal, trip or shared")
		return
	}

	updated, err := s.Store.SetChecklistVisibility(r.Context(), checklist.ID, checklist.TripID, visibility)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "checklist not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not update checklist")
		}
		return
	}
	s.writeChecklist(w, r, updated, me.ID, db.RoleEditor)
}

// handleRenameChecklist: a title was write-once until Stage 14 Milestone 8, so
// fixing a typo meant deleting the list and its items.
func (s *Server) handleRenameChecklist(w http.ResponseWriter, r *http.Request) {
	checklist, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if !s.requireChecklistWrite(w, r, checklist, role) {
		return
	}

	var req checklistRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	updated, err := s.Store.UpdateChecklistTitle(r.Context(), checklist.ID, checklist.TripID, strings.TrimSpace(req.Title))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "checklist not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not rename checklist")
		}
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	s.writeChecklist(w, r, updated, me.ID, role)
}

// writeChecklist is the shared tail of the handlers that return a whole list:
// building the response needs a second query for its items, and doing that in
// four places invited one of them to drift.
func (s *Server) writeChecklist(w http.ResponseWriter, r *http.Request, c db.Checklist, readerID string, role db.TripRole) {
	resp, err := s.checklistToResponse(r.Context(), c, readerID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load checklist")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type checklistItemRequest struct {
	Text string `json:"text"`
}

func (s *Server) handleCreateChecklistItem(w http.ResponseWriter, r *http.Request) {
	checklist, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if !s.requireChecklistWrite(w, r, checklist, role) {
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

// Ticking is the write the middle visibility exists to distinguish: on a
// trip-visible list everyone reads and only its author ticks, which is what
// makes it different from a shared one.
func (s *Server) handleSetChecklistItemChecked(w http.ResponseWriter, r *http.Request) {
	checklist, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if !s.requireChecklistWrite(w, r, checklist, role) {
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

// handleUpdateChecklistItemText: the other half of the write-once problem. An
// item was a line of text you could only delete and retype.
func (s *Server) handleUpdateChecklistItemText(w http.ResponseWriter, r *http.Request) {
	checklist, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if !s.requireChecklistWrite(w, r, checklist, role) {
		return
	}

	var req checklistItemRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	item, err := s.Store.UpdateChecklistItemText(r.Context(), chi.URLParam(r, "itemId"), checklist.ID, strings.TrimSpace(req.Text))
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
	checklist, role, ok := s.loadChecklist(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if !s.requireChecklistWrite(w, r, checklist, role) {
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
