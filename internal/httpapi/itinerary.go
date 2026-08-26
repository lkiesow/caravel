package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/db"
)

type itineraryEntryResponse struct {
	ID           string  `json:"id"`
	ItemID       string  `json:"item_id"`
	ItemTitle    string  `json:"item_title"`
	ItemCategory string  `json:"item_category"`
	ItemType     string  `json:"item_type"`
	ItemImageURL *string `json:"item_image_url"`
	SortOrder    int     `json:"sort_order"`
	Note         *string `json:"note"`
}

type itineraryDayResponse struct {
	ID      *string                  `json:"id"`
	Date    string                   `json:"date"`
	Notes   *string                  `json:"notes"`
	Entries []itineraryEntryResponse `json:"entries"`
}

// handleGetItinerary returns one entry per date in the trip's start/end
// range (or, if either date is unset, one entry per date that already has
// content), merging persisted itinerary_days with empty placeholders for
// days that don't exist yet — see plan Section 5.
func (s *Server) handleGetItinerary(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}

	days, err := s.Store.ListItineraryDaysByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load itinerary")
		return
	}
	entries, err := s.Store.ListItineraryEntriesByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load itinerary")
		return
	}

	entriesByDay := make(map[string][]itineraryEntryResponse)
	for _, e := range entries {
		entriesByDay[e.ItineraryDayID] = append(entriesByDay[e.ItineraryDayID], itineraryEntryResponse{
			ID:           e.ID,
			ItemID:       e.ItemID,
			ItemTitle:    e.ItemTitle,
			ItemCategory: e.ItemCategory,
			ItemType:     e.ItemType,
			ItemImageURL: s.resolveImageURL(r.Context(), e.ItemImageID),
			SortOrder:    e.SortOrder,
			Note:         e.Note,
		})
	}

	byDate := make(map[string]itineraryDayResponse)
	for _, d := range days {
		id := d.ID
		dayEntries := entriesByDay[d.ID]
		if dayEntries == nil {
			dayEntries = []itineraryEntryResponse{}
		}
		byDate[d.Date] = itineraryDayResponse{
			ID:      &id,
			Date:    d.Date,
			Notes:   d.Notes,
			Entries: dayEntries,
		}
	}

	dateRange := datesInRange(trip.StartDate, trip.EndDate)
	resp := make([]itineraryDayResponse, 0, len(dateRange)+len(days))
	seen := make(map[string]bool)

	for _, date := range dateRange {
		if day, ok := byDate[date]; ok {
			resp = append(resp, day)
		} else {
			resp = append(resp, itineraryDayResponse{Date: date, Entries: []itineraryEntryResponse{}})
		}
		seen[date] = true
	}
	// A trip with no dates (or days outside the trip's range, e.g. added
	// before the range was set) still shows any day that has content.
	for _, d := range days {
		if !seen[d.Date] {
			resp = append(resp, byDate[d.Date])
		}
	}

	// The two loops above emit the trip's own range first and everything
	// outside it afterwards, which put a day *before* the trip's start at
	// the bottom of the list. The frontend re-sorts after adding a day, so
	// this only showed up on reload. Dates are zero-padded YYYY-MM-DD, so
	// lexical order is chronological order.
	sort.Slice(resp, func(i, j int) bool { return resp[i].Date < resp[j].Date })

	writeJSON(w, http.StatusOK, resp)
}

// datesInRange returns "YYYY-MM-DD" strings from start to end inclusive, or
// nil if either bound is unset.
func datesInRange(start, end *string) []string {
	if start == nil || end == nil {
		return nil
	}
	startDate, err1 := time.Parse("2006-01-02", *start)
	endDate, err2 := time.Parse("2006-01-02", *end)
	if err1 != nil || err2 != nil || endDate.Before(startDate) {
		return nil
	}

	var dates []string
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	return dates
}

type setDayNotesRequest struct {
	Notes *string `json:"notes"`
}

// handleSetItineraryDayNotes upserts (creates if needed) the day for
// {date} and sets its notes — the only way an itinerary_days row is
// created, including implicitly so entries can be added to a day that
// doesn't exist yet (the frontend calls this with notes=null first).
func (s *Server) handleSetItineraryDayNotes(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}
	date := chi.URLParam(r, "date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeError(w, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
		return
	}

	var req setDayNotesRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	day, err := s.Store.UpsertItineraryDayNotes(r.Context(), uuid.NewString(), trip.ID, date, req.Notes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save day")
		return
	}
	id := day.ID
	writeJSON(w, http.StatusOK, itineraryDayResponse{ID: &id, Date: day.Date, Notes: day.Notes, Entries: []itineraryEntryResponse{}})
}

// handleDeleteItineraryDay removes a day and everything planned on it.
// Only days the user added explicitly can reach this: days inside the
// trip's date range are placeholders synthesized by handleGetItinerary and
// have no row to delete (nor an id the frontend could send).
func (s *Server) handleDeleteItineraryDay(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.loadItineraryDay(w, r, db.RoleEditor)
	if !ok {
		return
	}
	deleted, err := s.Store.DeleteItineraryDay(r.Context(), day.ID, day.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete day")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "day not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type createItineraryEntryRequest struct {
	ItemID string  `json:"item_id"`
	Note   *string `json:"note"`
}

func (s *Server) handleCreateItineraryEntry(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.loadItineraryDay(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req createItineraryEntryRequest
	if err := readJSON(r, &req); err != nil || req.ItemID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required")
		return
	}

	item, err := s.Store.GetItemByID(r.Context(), req.ItemID)
	if err != nil || item.TripID != day.TripID {
		writeError(w, http.StatusBadRequest, "item does not belong to this trip")
		return
	}

	// SortOrder was omitted here until Stage 15 Milestone 4, so every row in the
	// table was 0 and ListItineraryEntriesByTrip's ORDER BY sort_order was an
	// undefined tie - entries within a day came back in whatever order the
	// database felt like. Numbering from the count appends, which is what adding
	// an item to a day obviously means.
	existing, err := s.Store.ListItineraryEntriesByDay(r.Context(), day.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add item to day")
		return
	}

	entry, err := s.Store.CreateItineraryEntry(r.Context(), db.CreateItineraryEntryParams{
		ID:             uuid.NewString(),
		ItineraryDayID: day.ID,
		ItemID:         item.ID,
		SortOrder:      len(existing),
		Note:           req.Note,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add item to day")
		return
	}

	writeJSON(w, http.StatusCreated, itineraryEntryResponse{
		ID: entry.ID, ItemID: item.ID, ItemTitle: item.Title, ItemCategory: item.Category, ItemType: item.Type,
		ItemImageURL: s.resolveImageURL(r.Context(), item.ImageID),
		SortOrder:    entry.SortOrder, Note: entry.Note,
	})
}

type reorderItineraryEntriesRequest struct {
	// Every entry id on the day, in the order they should end up in.
	EntryIDs []string `json:"entry_ids"`
}

// handleReorderItineraryEntries renumbers a whole day at once.
//
// The full ordered list rather than a "move this entry up" call, for two
// reasons. It is one transactional write instead of two, so a reorder cannot be
// observed half-applied; and it is self-validating - the set of ids has to match
// the day exactly, which catches a stale client sending an order computed before
// somebody else added or removed an entry. A per-entry move would have to guess
// what to do about that.
//
// It renumbers from 0 on every call rather than swapping two values, so a day
// whose rows are all sort_order 0 (everything created before Stage 15 Milestone
// 4) is repaired by the first reorder someone performs on it.
func (s *Server) handleReorderItineraryEntries(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.loadItineraryDay(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req reorderItineraryEntriesRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	existing, err := s.Store.ListItineraryEntriesByDay(r.Context(), day.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not reorder the day")
		return
	}

	// Same size, no duplicates, and every id belongs to this day. Checked before
	// any write, so a rejected reorder changes nothing.
	if len(req.EntryIDs) != len(existing) {
		writeError(w, http.StatusBadRequest, "entry_ids must list every entry on this day exactly once")
		return
	}
	onDay := make(map[string]bool, len(existing))
	for _, e := range existing {
		onDay[e.ID] = true
	}
	seen := make(map[string]bool, len(req.EntryIDs))
	for _, id := range req.EntryIDs {
		if !onDay[id] || seen[id] {
			writeError(w, http.StatusBadRequest, "entry_ids must list every entry on this day exactly once")
			return
		}
		seen[id] = true
	}

	err = s.Store.WithTx(r.Context(), func(store db.Store) error {
		for i, id := range req.EntryIDs {
			updated, err := store.SetItineraryEntrySortOrder(r.Context(), id, day.ID, i)
			if err != nil {
				return err
			}
			if !updated {
				// Validated above, so reaching here means the day changed under
				// us mid-transaction. Rolling back is the only honest answer.
				return errItineraryEntryVanished
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errItineraryEntryVanished) {
			writeError(w, http.StatusConflict, "the day changed while it was being reordered")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not reorder the day")
		return
	}

	entries, err := s.Store.ListItineraryEntriesByDay(r.Context(), day.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not reorder the day")
		return
	}
	order := make([]string, len(entries))
	for i, e := range entries {
		order[i] = e.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry_ids": order})
}

var errItineraryEntryVanished = errors.New("itinerary entry vanished mid-reorder")

type moveItineraryEntryRequest struct {
	ToDate string `json:"to_date"`
}

type moveItineraryEntryResponse struct {
	DayID     string `json:"day_id"`
	Date      string `json:"date"`
	SortOrder int    `json:"sort_order"`
}

// handleMoveItineraryEntry reassigns an entry to another day, keeping its note
// and everything else about it -- which is the whole point. Before this, moving
// something meant deleting it from one day and adding it to the other, and the
// note went with the deletion.
//
// The path names the day the entry is on today (that is what authorizes the
// call) and the body names the date it should end up on. A *date* rather than a
// day id, deliberately: the target day may have no row yet, since a day inside
// the trip range is a placeholder synthesized by handleGetItinerary until
// something is written to it. Taking an id would mean the client had to create
// the day first, in a second request that can succeed while the move fails --
// the non-atomic shape Stage 09 spent two milestones removing from location
// creation. Taking a date lets the day be created inside the same transaction.
//
// The entry lands at the end of the target day. There is no "insert at position
// N" here: the client can reorder afterwards with the endpoint that already
// exists, and an ordering argument would be a second way to say the same thing.
func (s *Server) handleMoveItineraryEntry(w http.ResponseWriter, r *http.Request) {
	from, _, ok := s.loadItineraryDay(w, r, db.RoleEditor)
	if !ok {
		return
	}
	entryID := chi.URLParam(r, "entryId")

	var req moveItineraryEntryRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, err := time.Parse("2006-01-02", req.ToDate); err != nil {
		writeError(w, http.StatusBadRequest, "to_date must be in YYYY-MM-DD format")
		return
	}

	// The entry has to be on the day the path names before anything else
	// happens, so a wrong pairing is a 404 rather than a transaction that finds
	// out at the end. It also gives the no-op branch below something to answer
	// with.
	current, err := s.Store.ListItineraryEntriesByDay(r.Context(), from.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not move the entry")
		return
	}
	position := -1
	for i, e := range current {
		if e.ID == entryID {
			position = i
			break
		}
	}
	if position < 0 {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}

	// Moving an entry to the day it is already on is not an error and not a
	// write: answering 200 with where it is means a client that computed the
	// same date twice does not have to special-case it.
	if req.ToDate == from.Date {
		writeJSON(w, http.StatusOK, moveItineraryEntryResponse{DayID: from.ID, Date: from.Date, SortOrder: current[position].SortOrder})
		return
	}

	var landed moveItineraryEntryResponse
	err = s.Store.WithTx(r.Context(), func(store db.Store) error {
		// EnsureItineraryDay rather than UpsertItineraryDayNotes: the upsert
		// takes notes and would clear the target day's notes on the way past.
		to, err := store.EnsureItineraryDay(r.Context(), uuid.NewString(), from.TripID, req.ToDate)
		if err != nil {
			return err
		}
		existing, err := store.ListItineraryEntriesByDay(r.Context(), to.ID)
		if err != nil {
			return err
		}

		moved, err := store.SetItineraryEntryDay(r.Context(), entryID, from.ID, to.ID, len(existing))
		if err != nil {
			return err
		}
		if !moved {
			// Checked above, so reaching here means the entry left the source
			// day between the check and the write.
			return errItineraryEntryVanished
		}

		// Both days are renumbered from 0, the same repair
		// handleReorderItineraryEntries performs: the source day would
		// otherwise keep a gap where the entry was, and the target day inherits
		// whatever numbering it already had, which for a day untouched since
		// before Stage 15 Milestone 4 is every row at 0.
		if err := renumberItineraryDay(r.Context(), store, from.ID); err != nil {
			return err
		}
		if err := renumberItineraryDay(r.Context(), store, to.ID); err != nil {
			return err
		}

		after, err := store.ListItineraryEntriesByDay(r.Context(), to.ID)
		if err != nil {
			return err
		}
		landed = moveItineraryEntryResponse{DayID: to.ID, Date: to.Date}
		for _, e := range after {
			if e.ID == entryID {
				landed.SortOrder = e.SortOrder
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errItineraryEntryVanished) {
			writeError(w, http.StatusConflict, "the entry moved while it was being moved")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not move the entry")
		return
	}

	writeJSON(w, http.StatusOK, landed)
}

// renumberItineraryDay rewrites one day sort_order values as 0..n-1 in their
// current order. Shared by the move, which has two days to repair; the reorder
// handler keeps its own loop because it is writing an order the caller supplied
// rather than compacting the one that is stored.
func renumberItineraryDay(ctx context.Context, store db.Store, dayID string) error {
	entries, err := store.ListItineraryEntriesByDay(ctx, dayID)
	if err != nil {
		return err
	}
	for i, e := range entries {
		if e.SortOrder == i {
			continue
		}
		if _, err := store.SetItineraryEntrySortOrder(ctx, e.ID, dayID, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleDeleteItineraryEntry(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.loadItineraryDay(w, r, db.RoleEditor)
	if !ok {
		return
	}
	entryID := chi.URLParam(r, "entryId")
	deleted, err := s.Store.DeleteItineraryEntry(r.Context(), entryID, day.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove item from day")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
