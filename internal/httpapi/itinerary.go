package httpapi

import (
	"errors"
	"net/http"
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
	trip, ok := s.loadOwnedTrip(w, r)
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
	trip, ok := s.loadOwnedTrip(w, r)
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

// loadOwnedItineraryDay fetches the day named by {dayId} and confirms the
// current user owns its trip.
func (s *Server) loadOwnedItineraryDay(w http.ResponseWriter, r *http.Request) (db.ItineraryDay, bool) {
	dayID := chi.URLParam(r, "dayId")
	day, err := s.Store.GetItineraryDayByID(r.Context(), dayID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "day not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load day")
		}
		return db.ItineraryDay{}, false
	}
	if !s.hasTripAccess(r, day.TripID) {
		writeError(w, http.StatusNotFound, "day not found")
		return db.ItineraryDay{}, false
	}
	return day, true
}

type createItineraryEntryRequest struct {
	ItemID string  `json:"item_id"`
	Note   *string `json:"note"`
}

func (s *Server) handleCreateItineraryEntry(w http.ResponseWriter, r *http.Request) {
	day, ok := s.loadOwnedItineraryDay(w, r)
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

	entry, err := s.Store.CreateItineraryEntry(r.Context(), db.CreateItineraryEntryParams{
		ID:             uuid.NewString(),
		ItineraryDayID: day.ID,
		ItemID:         item.ID,
		Note:           req.Note,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not add item to day")
		return
	}

	writeJSON(w, http.StatusCreated, itineraryEntryResponse{
		ID: entry.ID, ItemID: item.ID, ItemTitle: item.Title, ItemCategory: item.Category, ItemType: item.Type,
		SortOrder: entry.SortOrder, Note: entry.Note,
	})
}

func (s *Server) handleDeleteItineraryEntry(w http.ResponseWriter, r *http.Request) {
	day, ok := s.loadOwnedItineraryDay(w, r)
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
