package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// seedDayWithEntry creates a trip, an item, an itinerary day and an entry on
// that day, returning the trip and day IDs.
func (ts *testServer) seedDayWithEntry(cookie *http.Cookie, date string) (tripID, dayID string) {
	ts.t.Helper()

	w := ts.do(http.MethodPost, "/api/trips", cookie, `{"title":"Trip","start_date":"2026-08-20","end_date":"2026-08-23"}`)
	if w.Code != http.StatusCreated {
		ts.t.Fatalf("create trip: got %d, body %s", w.Code, w.Body.String())
	}
	tripID = decode[map[string]any](ts.t, w)["id"].(string)

	w = ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, `{"title":"Kirkjufell","category":"site","type":"landmark"}`)
	if w.Code != http.StatusCreated {
		ts.t.Fatalf("create item: got %d, body %s", w.Code, w.Body.String())
	}
	itemID := decode[map[string]any](ts.t, w)["id"].(string)

	w = ts.do(http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/"+date, cookie, `{"notes":"packed"}`)
	if w.Code != http.StatusOK {
		ts.t.Fatalf("create day: got %d, body %s", w.Code, w.Body.String())
	}
	dayID = *decode[itineraryDayResponse](ts.t, w).ID

	w = ts.do(http.MethodPost, "/api/itinerary/days/"+dayID+"/entries", cookie, `{"item_id":"`+itemID+`"}`)
	if w.Code != http.StatusCreated {
		ts.t.Fatalf("create entry: got %d, body %s", w.Code, w.Body.String())
	}
	return tripID, dayID
}

// itineraryDates returns the dates the itinerary endpoint reports for a trip.
func (ts *testServer) itineraryDates(cookie *http.Cookie, tripID string) []string {
	ts.t.Helper()

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/itinerary", cookie, "")
	if w.Code != http.StatusOK {
		ts.t.Fatalf("get itinerary: got %d, body %s", w.Code, w.Body.String())
	}
	days := decode[[]itineraryDayResponse](ts.t, w)
	dates := make([]string, len(days))
	for i, d := range days {
		dates[i] = d.Date
	}
	return dates
}

// A day added outside the trip's range is the case this endpoint exists for:
// a typo'd date was previously impossible to remove.
func TestDeleteItineraryDayRemovesDayAndItsEntries(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID, dayID := ts.seedDayWithEntry(cookie, "2027-01-15")

	if got := ts.itineraryDates(cookie, tripID); !contains(got, "2027-01-15") {
		t.Fatalf("day missing before delete: %v", got)
	}

	if w := ts.do(http.MethodDelete, "/api/itinerary/days/"+dayID, cookie, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete day: got %d, body %s", w.Code, w.Body.String())
	}

	if got := ts.itineraryDates(cookie, tripID); contains(got, "2027-01-15") {
		t.Errorf("day still listed after delete: %v", got)
	}

	// The entry on that day must go with it - the FK's ON DELETE CASCADE is
	// what does this, and SQLite only honours it with foreign_keys enabled
	// per connection, so it's worth asserting rather than assuming.
	entries, err := ts.Store.ListItineraryEntriesByTrip(context.Background(), tripID)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries survived their day: %+v", entries)
	}
}

// Deleting an in-range day is legal too - it clears whatever was planned on
// it, and handleGetItinerary re-synthesizes the empty placeholder.
func TestDeleteItineraryDayInRangeLeavesPlaceholder(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID, dayID := ts.seedDayWithEntry(cookie, "2026-08-21")

	if w := ts.do(http.MethodDelete, "/api/itinerary/days/"+dayID, cookie, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete day: got %d, body %s", w.Code, w.Body.String())
	}

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/itinerary", cookie, "")
	for _, d := range decode[[]itineraryDayResponse](t, w) {
		if d.Date != "2026-08-21" {
			continue
		}
		if d.ID != nil {
			t.Errorf("in-range day kept a persisted id after delete: %v", *d.ID)
		}
		if len(d.Entries) != 0 || d.Notes != nil {
			t.Errorf("in-range day kept content after delete: %+v", d)
		}
		return
	}
	t.Error("in-range day vanished from the itinerary instead of reverting to a placeholder")
}

func TestDeleteItineraryDayRejectsAnotherUsersDay(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	intruder := ts.login("intruder")
	tripID, dayID := ts.seedDayWithEntry(owner, "2027-01-15")

	// 404 rather than 403: the day's existence isn't the intruder's to learn.
	if w := ts.do(http.MethodDelete, "/api/itinerary/days/"+dayID, intruder, ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete as intruder: got %d, want 404, body %s", w.Code, w.Body.String())
	}
	if got := ts.itineraryDates(owner, tripID); !contains(got, "2027-01-15") {
		t.Errorf("owner's day was deleted by another user: %v", got)
	}
}

func TestDeleteItineraryDayRequiresAuth(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	_, dayID := ts.seedDayWithEntry(cookie, "2027-01-15")

	if w := ts.do(http.MethodDelete, "/api/itinerary/days/"+dayID, nil, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("delete without a session: got %d, want 401", w.Code)
	}
}

func TestDeleteItineraryDayUnknownID(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")

	if w := ts.do(http.MethodDelete, "/api/itinerary/days/"+uuid.NewString(), cookie, ""); w.Code != http.StatusNotFound {
		t.Errorf("delete unknown day: got %d, want 404", w.Code)
	}
}

// The entry routes moved under the day route group when DELETE on the day
// itself was added; this guards against that reshuffle breaking them.
func TestItineraryEntryRoutesStillWork(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID, dayID := ts.seedDayWithEntry(cookie, "2026-08-21")

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/itinerary", cookie, "")
	var entryID string
	for _, d := range decode[[]itineraryDayResponse](t, w) {
		if d.Date == "2026-08-21" && len(d.Entries) == 1 {
			entryID = d.Entries[0].ID
		}
	}
	if entryID == "" {
		t.Fatalf("seeded entry not found in itinerary: %s", w.Body.String())
	}

	if w := ts.do(http.MethodDelete, "/api/itinerary/days/"+dayID+"/entries/"+entryID, cookie, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete entry: got %d, body %s", w.Code, w.Body.String())
	}
	entries, err := ts.Store.ListItineraryEntriesByTrip(context.Background(), tripID)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entry not deleted: %+v", entries)
	}
}

// The itinerary is a merge of the trip's own date range with whatever days
// exist outside it, and those two groups used to be emitted one after the
// other - so a day before the trip's start landed at the bottom of the list.
func TestGetItineraryIsOrderedByDate(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")

	w := ts.do(http.MethodPost, "/api/trips", cookie, `{"title":"Trip","start_date":"2026-08-20","end_date":"2026-08-23"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create trip: got %d, body %s", w.Code, w.Body.String())
	}
	tripID := decode[map[string]any](t, w)["id"].(string)

	// Added in a deliberately unhelpful order, and on both sides of the range.
	for _, date := range []string{"2027-01-15", "2026-08-05", "2026-08-21"} {
		if w := ts.do(http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/"+date, cookie, `{"notes":"x"}`); w.Code != http.StatusOK {
			t.Fatalf("create day %s: got %d, body %s", date, w.Code, w.Body.String())
		}
	}

	got := ts.itineraryDates(cookie, tripID)
	want := []string{"2026-08-05", "2026-08-20", "2026-08-21", "2026-08-22", "2026-08-23", "2027-01-15"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
