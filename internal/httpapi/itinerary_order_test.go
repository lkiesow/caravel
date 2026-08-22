package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"caravel/internal/db"
)

// Ordering inside an itinerary day.
//
// The first test here is the one that would have caught the bug this milestone
// fixed: handleCreateItineraryEntry never set SortOrder, so every row in
// itinerary_entries was 0 and ListItineraryEntriesByTrip's ORDER BY sort_order
// decided nothing. Entries came back in whatever order the database chose, which
// happened to look like insertion order often enough that nobody noticed.

// itineraryFixture is a trip with one real day and several items to put on it.
type itineraryFixture struct {
	ts     *testServer
	owner  *http.Cookie
	tripID string
	dayID  string
}

func setupItinerary(t *testing.T) *itineraryFixture {
	t.Helper()
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	// An explicit day: days inside a trip's range are synthesized and carry no
	// id, so there would be nothing to aim the entry routes at.
	w := ts.do(http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/2026-08-20", owner, `{"notes":"arrive"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create day: got %d, body %s", w.Code, w.Body.String())
	}
	dayID := *decode[struct {
		ID *string `json:"id"`
	}](t, w).ID

	return &itineraryFixture{ts: ts, owner: owner, tripID: tripID, dayID: dayID}
}

// addEntry puts an item on the fixture's day and returns the entry id.
func (f *itineraryFixture) addEntry(t *testing.T, title string) string {
	t.Helper()
	itemID := f.ts.createItem(f.owner, f.tripID, title)
	return f.ts.mustCreate(http.MethodPost, "/api/itinerary/days/"+f.dayID+"/entries", f.owner,
		fmt.Sprintf(`{"item_id":%q}`, itemID), http.StatusCreated)
}

// entryTitles reads the day's entries back through the itinerary payload, which
// is the order the client actually renders.
func (f *itineraryFixture) entryTitles(t *testing.T, as *http.Cookie) []string {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/itinerary", as, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get itinerary: got %d, body %s", w.Code, w.Body.String())
	}
	days := decode[[]struct {
		ID      *string `json:"id"`
		Entries []struct {
			ItemTitle string `json:"item_title"`
			SortOrder int    `json:"sort_order"`
		} `json:"entries"`
	}](t, w)
	for _, d := range days {
		if d.ID != nil && *d.ID == f.dayID {
			titles := make([]string, len(d.Entries))
			for i, e := range d.Entries {
				titles[i] = e.ItemTitle
			}
			return titles
		}
	}
	t.Fatalf("day %s not in the itinerary payload", f.dayID)
	return nil
}

func (f *itineraryFixture) sortOrders(t *testing.T) []int {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/itinerary", f.owner, "")
	days := decode[[]struct {
		ID      *string `json:"id"`
		Entries []struct {
			SortOrder int `json:"sort_order"`
		} `json:"entries"`
	}](t, w)
	for _, d := range days {
		if d.ID != nil && *d.ID == f.dayID {
			out := make([]int, len(d.Entries))
			for i, e := range d.Entries {
				out[i] = e.SortOrder
			}
			return out
		}
	}
	t.Fatal("day not found")
	return nil
}

// The regression test for the bug. Three entries added in a known order come
// back in that order, and carry distinct sort_orders rather than three zeroes.
func TestItineraryEntriesKeepInsertionOrder(t *testing.T) {
	f := setupItinerary(t)

	for _, title := range []string{"Breakfast", "Museum", "Dinner"} {
		f.addEntry(t, title)
	}

	if got := f.entryTitles(t, f.owner); !equalStrings(got, []string{"Breakfast", "Museum", "Dinner"}) {
		t.Errorf("entries came back as %v, want insertion order", got)
	}
	// The distinctness is the real assertion: with every row at 0 the titles
	// above can still happen to come out right, which is exactly why the bug
	// survived so long.
	if got := f.sortOrders(t); !equalInts(got, []int{0, 1, 2}) {
		t.Errorf("sort_orders are %v, want 0,1,2 — all-equal values leave the order undefined", got)
	}
}

func TestReorderItineraryEntries(t *testing.T) {
	f := setupItinerary(t)

	breakfast := f.addEntry(t, "Breakfast")
	museum := f.addEntry(t, "Museum")
	dinner := f.addEntry(t, "Dinner")

	// Dinner to the front, which no single swap would produce.
	body := fmt.Sprintf(`{"entry_ids":[%q,%q,%q]}`, dinner, breakfast, museum)
	w := f.ts.do(http.MethodPut, "/api/itinerary/days/"+f.dayID+"/entries/order", f.owner, body)
	if w.Code != http.StatusOK {
		t.Fatalf("reorder: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[struct {
		EntryIDs []string `json:"entry_ids"`
	}](t, w).EntryIDs; !equalStrings(got, []string{dinner, breakfast, museum}) {
		t.Errorf("the response reports order %v, want the requested one", got)
	}

	if got := f.entryTitles(t, f.owner); !equalStrings(got, []string{"Dinner", "Breakfast", "Museum"}) {
		t.Errorf("after reorder the itinerary reads %v", got)
	}
	if got := f.sortOrders(t); !equalInts(got, []int{0, 1, 2}) {
		t.Errorf("sort_orders after reorder are %v, want renumbered 0,1,2", got)
	}
}

// A day whose rows are all 0 - every entry created before this milestone - is
// repaired by the first reorder, because the handler renumbers rather than
// swapping. Simulated by writing the zeroes through the store directly, since
// the API can no longer produce them.
func TestReorderRepairsADayOfZeroes(t *testing.T) {
	f := setupItinerary(t)

	a := f.addEntry(t, "A")
	b := f.addEntry(t, "B")
	c := f.addEntry(t, "C")

	for _, id := range []string{a, b, c} {
		if _, err := f.ts.Store.SetItineraryEntrySortOrder(t.Context(), id, f.dayID, 0); err != nil {
			t.Fatalf("force sort_order 0: %v", err)
		}
	}

	body := fmt.Sprintf(`{"entry_ids":[%q,%q,%q]}`, c, a, b)
	if w := f.ts.do(http.MethodPut, "/api/itinerary/days/"+f.dayID+"/entries/order", f.owner, body); w.Code != http.StatusOK {
		t.Fatalf("reorder: got %d, body %s", w.Code, w.Body.String())
	}
	if got := f.entryTitles(t, f.owner); !equalStrings(got, []string{"C", "A", "B"}) {
		t.Errorf("a day of zeroes reordered to %v, want C,A,B", got)
	}
	if got := f.sortOrders(t); !equalInts(got, []int{0, 1, 2}) {
		t.Errorf("sort_orders are %v, want 0,1,2", got)
	}
}

// The self-validating half: the body has to name every entry on the day exactly
// once, and a rejected reorder must leave the order alone.
func TestReorderItineraryEntriesRejectsABadIDSet(t *testing.T) {
	f := setupItinerary(t)

	first := f.addEntry(t, "First")
	second := f.addEntry(t, "Second")

	// An entry on another day, to prove the check is per-day and not merely
	// per-trip.
	w := f.ts.do(http.MethodPut, "/api/trips/"+f.tripID+"/itinerary/days/2026-08-21", f.owner, `{"notes":"day two"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create second day: got %d", w.Code)
	}
	otherDayID := *decode[struct {
		ID *string `json:"id"`
	}](t, w).ID
	otherItem := f.ts.createItem(f.owner, f.tripID, "Elsewhere")
	elsewhere := f.ts.mustCreate(http.MethodPost, "/api/itinerary/days/"+otherDayID+"/entries", f.owner,
		fmt.Sprintf(`{"item_id":%q}`, otherItem), http.StatusCreated)

	for _, tc := range []struct{ name, body string }{
		{"too few", fmt.Sprintf(`{"entry_ids":[%q]}`, first)},
		{"too many", fmt.Sprintf(`{"entry_ids":[%q,%q,%q]}`, first, second, elsewhere)},
		{"a duplicate", fmt.Sprintf(`{"entry_ids":[%q,%q]}`, first, first)},
		{"an entry from another day", fmt.Sprintf(`{"entry_ids":[%q,%q]}`, first, elsewhere)},
		{"empty", `{"entry_ids":[]}`},
		{"missing field", `{}`},
		{"not json", `nonsense`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.ts.do(http.MethodPut, "/api/itinerary/days/"+f.dayID+"/entries/order", f.owner, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400 (body %s)", w.Code, w.Body.String())
			}
			if got := f.entryTitles(t, f.owner); !equalStrings(got, []string{"First", "Second"}) {
				t.Errorf("a rejected reorder changed the order to %v", got)
			}
		})
	}
}

// Reordering is a write, so a viewer cannot do it - and the day is on somebody
// else's trip, so a stranger gets 404 rather than being told it exists.
func TestReorderItineraryEntriesAuthorization(t *testing.T) {
	for _, tc := range []struct {
		role db.TripRole
		want int
	}{
		{db.RoleViewer, http.StatusForbidden},
		{"", http.StatusNotFound},
	} {
		t.Run(string(tc.role)+"/"+fmt.Sprint(tc.want), func(t *testing.T) {
			f := setupRole(t, tc.role)
			body := `{"entry_ids":[]}`
			w := f.ts.do(http.MethodPut, "/api/itinerary/days/"+f.dayID+"/entries/order", f.actor, body)
			if w.Code != tc.want {
				t.Errorf("role %q: got %d, want %d (body %s)", tc.role, w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
