package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Moving an entry from one day to another (Stage 22 Milestone 1).
//
// The behaviour worth pinning is not "the entry is on the other day" -- that
// much a delete-and-recreate already managed. It is that everything about the
// entry survives the move (the note above all, which the old workaround threw
// away), that both days come back contiguously numbered, and that a day with
// no row yet is a valid destination.

// moveFixture is a trip with two real days and the ids to aim at them.
type moveFixture struct {
	ts      *testServer
	owner   *http.Cookie
	tripID  string
	fromDay string
	toDay   string
}

func setupMove(t *testing.T) *moveFixture {
	t.Helper()
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")
	return &moveFixture{
		ts:      ts,
		owner:   owner,
		tripID:  tripID,
		fromDay: createDay(t, ts, owner, tripID, "2026-08-20", "arrive"),
		toDay:   createDay(t, ts, owner, tripID, "2026-08-21", "second day"),
	}
}

func createDay(t *testing.T, ts *testServer, as *http.Cookie, tripID, date, notes string) string {
	t.Helper()
	w := ts.do(http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/"+date, as, fmt.Sprintf(`{"notes":%q}`, notes))
	if w.Code != http.StatusOK {
		t.Fatalf("create day %s: got %d, body %s", date, w.Code, w.Body.String())
	}
	return *decode[struct {
		ID *string `json:"id"`
	}](t, w).ID
}

// addTo puts a freshly created location on a day, with an optional note, and
// returns the entry id.
func (f *moveFixture) addTo(t *testing.T, dayID, title, note string) string {
	t.Helper()
	itemID := f.ts.createItem(f.owner, f.tripID, title)
	body := fmt.Sprintf(`{"item_id":%q}`, itemID)
	if note != "" {
		body = fmt.Sprintf(`{"item_id":%q,"note":%q}`, itemID, note)
	}
	return f.ts.mustCreate(http.MethodPost, "/api/itinerary/days/"+dayID+"/entries", f.owner, body, http.StatusCreated)
}

func (f *moveFixture) move(t *testing.T, fromDay, entryID, toDate string) *httptest.ResponseRecorder {
	t.Helper()
	return f.ts.do(http.MethodPatch, "/api/itinerary/days/"+fromDay+"/entries/"+entryID, f.owner,
		fmt.Sprintf(`{"to_date":%q}`, toDate))
}

// itineraryDay is one day as the client sees it, read back through the payload
// the frontend actually renders rather than through the store.
type itineraryDay struct {
	ID      *string `json:"id"`
	Date    string  `json:"date"`
	Notes   *string `json:"notes"`
	Entries []struct {
		ID        string  `json:"id"`
		ItemTitle string  `json:"item_title"`
		SortOrder int     `json:"sort_order"`
		Note      *string `json:"note"`
	} `json:"entries"`
}

func (f *moveFixture) day(t *testing.T, date string) itineraryDay {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/itinerary", f.owner, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get itinerary: got %d, body %s", w.Code, w.Body.String())
	}
	for _, d := range decode[[]itineraryDay](t, w) {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("date %s not in the itinerary payload", date)
	return itineraryDay{}
}

func (d itineraryDay) titles() []string {
	out := make([]string, len(d.Entries))
	for i, e := range d.Entries {
		out[i] = e.ItemTitle
	}
	return out
}

func (d itineraryDay) orders() []int {
	out := make([]int, len(d.Entries))
	for i, e := range d.Entries {
		out[i] = e.SortOrder
	}
	return out
}

// The headline case: an entry with a note moves, keeps the note, lands at the
// end of the target day, and leaves the source day numbered from 0.
func TestMoveItineraryEntryKeepsNoteAndRenumbersBothDays(t *testing.T) {
	f := setupMove(t)

	f.addTo(t, f.fromDay, "Breakfast", "")
	museum := f.addTo(t, f.fromDay, "Museum", "book ahead")
	f.addTo(t, f.fromDay, "Dinner", "")
	f.addTo(t, f.toDay, "Pool", "")

	w := f.move(t, f.fromDay, museum, "2026-08-21")
	if w.Code != http.StatusOK {
		t.Fatalf("move: got %d, body %s", w.Code, w.Body.String())
	}
	landed := decode[struct {
		DayID     string `json:"day_id"`
		Date      string `json:"date"`
		SortOrder int    `json:"sort_order"`
	}](t, w)
	if landed.DayID != f.toDay || landed.Date != "2026-08-21" || landed.SortOrder != 1 {
		t.Errorf("landed at %+v, want day %s on 2026-08-21 at position 1", landed, f.toDay)
	}

	from := f.day(t, "2026-08-20")
	if got := from.titles(); !equalStrings(got, []string{"Breakfast", "Dinner"}) {
		t.Errorf("source day has %v, want Breakfast and Dinner", got)
	}
	// The gap the departure left has to be closed, or the day carries 0 and 2
	// and the next reorder is computing from a set nobody renumbered.
	if got := from.orders(); !equalInts(got, []int{0, 1}) {
		t.Errorf("source day sort_orders are %v, want 0,1", got)
	}

	to := f.day(t, "2026-08-21")
	if got := to.titles(); !equalStrings(got, []string{"Pool", "Museum"}) {
		t.Errorf("target day has %v, want Pool then Museum", got)
	}
	if got := to.orders(); !equalInts(got, []int{0, 1}) {
		t.Errorf("target day sort_orders are %v, want 0,1", got)
	}
	// The point of the whole feature: the note came along. Deleting and
	// re-adding, which is what people had to do before, loses it.
	moved := to.Entries[1]
	if moved.Note == nil || *moved.Note != "book ahead" {
		t.Errorf("moved entry note is %v, want it preserved", moved.Note)
	}
	if moved.ID != museum {
		t.Errorf("moved entry id is %s, want the original %s -- a move must not recreate the row", moved.ID, museum)
	}
}

// A date with no itinerary_days row yet is a valid destination: the day is
// created inside the same transaction, which is the reason the request carries
// a date rather than a day id.
func TestMoveItineraryEntryToADayThatDoesNotExistYet(t *testing.T) {
	f := setupMove(t)
	entry := f.addTo(t, f.fromDay, "Museum", "")

	w := f.move(t, f.fromDay, entry, "2026-09-30")
	if w.Code != http.StatusOK {
		t.Fatalf("move: got %d, body %s", w.Code, w.Body.String())
	}

	to := f.day(t, "2026-09-30")
	if to.ID == nil {
		t.Fatal("target day came back without an id, so no row was created")
	}
	if got := to.titles(); !equalStrings(got, []string{"Museum"}) {
		t.Errorf("new day has %v, want the moved entry", got)
	}
	if got := f.day(t, "2026-08-20").titles(); len(got) != 0 {
		t.Errorf("source day still has %v", got)
	}
}

// The trap EnsureItineraryDay exists to avoid: UpsertItineraryDayNotes with nil
// notes would have wiped the target day's notes on the way past.
func TestMoveItineraryEntryLeavesTargetDayNotesAlone(t *testing.T) {
	f := setupMove(t)
	entry := f.addTo(t, f.fromDay, "Museum", "")

	if w := f.move(t, f.fromDay, entry, "2026-08-21"); w.Code != http.StatusOK {
		t.Fatalf("move: got %d, body %s", w.Code, w.Body.String())
	}

	to := f.day(t, "2026-08-21")
	if to.Notes == nil || *to.Notes != "second day" {
		t.Errorf("target day notes are %v, want them untouched by the move", to.Notes)
	}
	if from := f.day(t, "2026-08-20"); from.Notes == nil || *from.Notes != "arrive" {
		t.Errorf("source day notes are %v, want them untouched by the move", from.Notes)
	}
}

// Moving an entry to the day it is already on is a no-op, not an error: a
// client that computed the same date twice should not have to special-case it.
func TestMoveItineraryEntryToItsOwnDayIsANoOp(t *testing.T) {
	f := setupMove(t)
	f.addTo(t, f.fromDay, "Breakfast", "")
	museum := f.addTo(t, f.fromDay, "Museum", "")

	w := f.move(t, f.fromDay, museum, "2026-08-20")
	if w.Code != http.StatusOK {
		t.Fatalf("move: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[struct {
		SortOrder int `json:"sort_order"`
	}](t, w).SortOrder; got != 1 {
		t.Errorf("no-op move reports position %d, want the position it already had (1)", got)
	}
	if got := f.day(t, "2026-08-20").titles(); !equalStrings(got, []string{"Breakfast", "Museum"}) {
		t.Errorf("day is %v after a no-op move, want it unchanged", got)
	}
}

func TestMoveItineraryEntryRejectsBadInput(t *testing.T) {
	f := setupMove(t)
	entry := f.addTo(t, f.fromDay, "Museum", "")
	otherDayEntry := f.addTo(t, f.toDay, "Pool", "")

	cases := []struct {
		name   string
		day    string
		entry  string
		toDate string
		want   int
	}{
		{"not a date", f.fromDay, entry, "the 20th", http.StatusBadRequest},
		{"empty date", f.fromDay, entry, "", http.StatusBadRequest},
		{"a timestamp, not a day", f.fromDay, entry, "2026-08-21T00:00:00Z", http.StatusBadRequest},
		// The source-day predicate: naming an entry that is on another day must
		// not move it, or the path stops meaning anything.
		{"entry belongs to another day", f.fromDay, otherDayEntry, "2026-08-22", http.StatusNotFound},
		{"no such entry", f.fromDay, "nonexistent", "2026-08-22", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := f.move(t, tc.day, tc.entry, tc.toDate)
			if w.Code != tc.want {
				t.Errorf("got %d, want %d (body %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}

	// Nothing above should have moved anything.
	if got := f.day(t, "2026-08-21").titles(); !equalStrings(got, []string{"Pool"}) {
		t.Errorf("target day is %v after five rejected moves, want just Pool", got)
	}
}

// A stranger must not learn the day exists, and an entry id from another trip
// must not be reachable through this trip's day.
func TestMoveItineraryEntryAcrossTrips(t *testing.T) {
	f := setupMove(t)
	entry := f.addTo(t, f.fromDay, "Museum", "")

	otherTrip := f.ts.createTrip(f.owner, "Norway")
	otherDay := createDay(t, f.ts, f.owner, otherTrip, "2026-08-20", "")

	// Same owner, different trip: the entry is not on that day, so it is a 404
	// for the same reason any other mismatch is.
	w := f.ts.do(http.MethodPatch, "/api/itinerary/days/"+otherDay+"/entries/"+entry, f.owner, `{"to_date":"2026-08-21"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-trip move: got %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	if got := f.day(t, "2026-08-20").titles(); !equalStrings(got, []string{"Museum"}) {
		t.Errorf("entry is %v after a cross-trip move attempt, want it where it was", got)
	}
}
