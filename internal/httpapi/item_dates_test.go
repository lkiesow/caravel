package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"caravel/internal/db"
)

func TestCollapseDateRanges(t *testing.T) {
	r := func(start, end string) itemDateRangeResponse {
		return itemDateRangeResponse{StartDate: start, EndDate: end}
	}

	tests := []struct {
		name  string
		dates []string
		want  []itemDateRangeResponse
	}{
		{
			name:  "no dates is an empty list, not null",
			dates: nil,
			want:  []itemDateRangeResponse{},
		},
		{
			name:  "one day is a range that starts and ends on itself",
			dates: []string{"2026-09-05"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-05")},
		},
		{
			name:  "consecutive days collapse, both ends inclusive",
			dates: []string{"2026-09-05", "2026-09-06", "2026-09-07"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-07")},
		},
		{
			name:  "a gap splits the run",
			dates: []string{"2026-09-05", "2026-09-07"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-05"), r("2026-09-07", "2026-09-07")},
		},
		{
			name:  "a day removed from the middle leaves two ranges",
			dates: []string{"2026-09-05", "2026-09-07", "2026-09-08"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-05"), r("2026-09-07", "2026-09-08")},
		},
		{
			// String arithmetic on the last two characters would emit two
			// ranges here. This is the case that says the walk parses.
			name:  "a month boundary is one run",
			dates: []string{"2026-01-30", "2026-01-31", "2026-02-01"},
			want:  []itemDateRangeResponse{r("2026-01-30", "2026-02-01")},
		},
		{
			name:  "a year boundary is one run",
			dates: []string{"2026-12-31", "2027-01-01"},
			want:  []itemDateRangeResponse{r("2026-12-31", "2027-01-01")},
		},
		{
			// 2028 is a leap year, so the 29th exists and the run is unbroken.
			name:  "a leap day is one run",
			dates: []string{"2028-02-28", "2028-02-29", "2028-03-01"},
			want:  []itemDateRangeResponse{r("2028-02-28", "2028-03-01")},
		},
		{
			// 2027 has no 29th, so the 28th is followed directly by March.
			name:  "a non-leap year runs February straight into March",
			dates: []string{"2027-02-28", "2027-03-01"},
			want:  []itemDateRangeResponse{r("2027-02-28", "2027-03-01")},
		},
		{
			// An item on one day twice is legal - nothing constrains the pair -
			// and must read as one day, not as two ranges.
			name:  "duplicates reduce to one date",
			dates: []string{"2026-09-05", "2026-09-05", "2026-09-06"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-06")},
		},
		{
			name:  "input order does not matter",
			dates: []string{"2026-09-07", "2026-09-05", "2026-09-06"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-07")},
		},
		{
			name:  "ranges come back in date order",
			dates: []string{"2026-10-02", "2026-09-05", "2026-09-06", "2026-10-01"},
			want:  []itemDateRangeResponse{r("2026-09-05", "2026-09-06"), r("2026-10-01", "2026-10-02")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collapseDateRanges(tt.dates)
			if got == nil {
				t.Fatal("collapseDateRanges returned nil; the JSON must be [] rather than null")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("collapseDateRanges(%v)\n got %+v\nwant %+v", tt.dates, got, tt.want)
			}
		})
	}
}

// The query behind a location's dates, exercised against whichever dialect the
// suite is pointed at. This is the milestone's real proof: itinerary_days.date
// is TEXT on SQLite and DATE on Postgres, so the generated row type differs by
// dialect and only `make test-postgres` can say the conversion is right.
func TestListItineraryDatesByItem(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")
	other := ts.createItem(cookie, tripID, "Skogafoss")

	// Out of order on purpose, and 09-07 twice: the store returns what is
	// there, and collapsing is the caller's job.
	for _, date := range []string{"2026-09-07", "2026-09-05", "2026-09-06", "2026-09-07"} {
		dayID := ts.mustCreate(
			http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/"+date, cookie,
			`{"notes":null}`, http.StatusOK,
		)
		ts.mustCreate(
			http.MethodPost, "/api/itinerary/days/"+dayID+"/entries", cookie,
			`{"item_id":"`+hotel+`"}`, http.StatusCreated,
		)
	}
	// A second location on one of the same days, to prove the filter bites.
	dayID := ts.mustCreate(
		http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/2026-09-06", cookie,
		`{"notes":null}`, http.StatusOK,
	)
	ts.mustCreate(
		http.MethodPost, "/api/itinerary/days/"+dayID+"/entries", cookie,
		`{"item_id":"`+other+`"}`, http.StatusCreated,
	)

	rows, err := ts.Store.ListItineraryDatesByItem(context.Background(), hotel)
	if err != nil {
		t.Fatalf("ListItineraryDatesByItem: %v", err)
	}

	// Four appearances, ordered by date, with the 7th twice. Sorted output is
	// what lets the collapse walk the list once.
	var dates []string
	for _, row := range rows {
		dates = append(dates, row.Date)
	}
	want := []string{"2026-09-05", "2026-09-06", "2026-09-07", "2026-09-07"}
	if !reflect.DeepEqual(dates, want) {
		t.Errorf("dates: got %v, want %v", dates, want)
	}

	// The date has to survive as a plain YYYY-MM-DD string on both dialects;
	// on Postgres it arrives as a time.Time and is formatted back.
	for _, row := range rows {
		if _, err := time.Parse(isoDate, row.Date); err != nil {
			t.Errorf("date %q is not a plain ISO date: %v", row.Date, err)
		}
		if row.ItemID != hotel {
			t.Errorf("got a row for item %s, want only %s", row.ItemID, hotel)
		}
		if row.EntryID == "" || row.DayID == "" {
			t.Errorf("row is missing the ids the reconcile deletes by: %+v", row)
		}
	}

	// And the ranges the location page would show.
	got := collapseDateRanges(dates)
	wantRanges := []itemDateRangeResponse{{StartDate: "2026-09-05", EndDate: "2026-09-07"}}
	if !reflect.DeepEqual(got, wantRanges) {
		t.Errorf("collapsed: got %+v, want %+v", got, wantRanges)
	}

	// Two entries on one day are two rows there but one date in the range, so
	// the sort_order of both has to come through intact for the reconcile.
	seventh := 0
	for _, row := range rows {
		if row.Date == "2026-09-07" {
			seventh++
		}
	}
	if seventh != 2 {
		t.Errorf("got %d rows on the 7th, want the duplicate preserved", seventh)
	}
}

// setDates PATCHes a location's date ranges and returns the ranges it reports
// back. rangesJSON is the "dates" array on its own.
func (ts *testServer) setDates(cookie *http.Cookie, itemID, rangesJSON string) []itemDateRangeResponse {
	ts.t.Helper()
	body := `{"title":"Hotel Ranga","category":"stay","tags":["hotel"],"dates":` + rangesJSON + `}`
	w := ts.do(http.MethodPatch, "/api/items/"+itemID, cookie, body)
	if w.Code != http.StatusOK {
		ts.t.Fatalf("patch dates: got %d, body %s", w.Code, w.Body.String())
	}
	return decode[itemDetailResponse](ts.t, w).Dates
}

// dayByDate finds one day in the itinerary response.
func (ts *testServer) dayByDate(cookie *http.Cookie, tripID, date string) itineraryDayResponse {
	ts.t.Helper()
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/itinerary", cookie, "")
	if w.Code != http.StatusOK {
		ts.t.Fatalf("get itinerary: got %d, body %s", w.Code, w.Body.String())
	}
	for _, d := range decode[[]itineraryDayResponse](ts.t, w) {
		if d.Date == date {
			return d
		}
	}
	ts.t.Fatalf("no day %s in the itinerary", date)
	return itineraryDayResponse{}
}

// Setting a location's dates puts it on those itinerary days. This is the
// behaviour the whole stage exists for: before it, the days stayed empty.
func TestSettingDatesPutsTheLocationOnThoseDays(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")

	got := ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-07"}]`)
	want := []itemDateRangeResponse{{StartDate: "2026-09-05", EndDate: "2026-09-07"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dates: got %+v, want %+v", got, want)
	}

	// Both endpoints inclusive: the 7th is a day the location is on, not the
	// day after the last one.
	for _, date := range []string{"2026-09-05", "2026-09-06", "2026-09-07"} {
		day := ts.dayByDate(cookie, tripID, date)
		if len(day.Entries) != 1 || day.Entries[0].ItemID != hotel {
			t.Errorf("%s: got %d entries %+v, want the hotel", date, len(day.Entries), day.Entries)
		}
	}

	// And the other direction: removing the middle day in the itinerary makes
	// the location report two ranges.
	sixth := ts.dayByDate(cookie, tripID, "2026-09-06")
	ts.mustCreateNoID(http.MethodDelete,
		"/api/itinerary/days/"+*sixth.ID+"/entries/"+sixth.Entries[0].ID, cookie, "", http.StatusNoContent)

	w := ts.do(http.MethodGet, "/api/items/"+hotel, cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get item: got %d", w.Code)
	}
	got = decode[itemDetailResponse](t, w).Dates
	want = []itemDateRangeResponse{
		{StartDate: "2026-09-05", EndDate: "2026-09-05"},
		{StartDate: "2026-09-07", EndDate: "2026-09-07"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after removing the 6th: got %+v, want %+v", got, want)
	}
}

// The invariant the reconcile exists for: a day that stays keeps its position
// and its note. A delete-all-then-recreate would pass every other test in this
// file and fail this one.
func TestReconcileItemDatesKeepsUntouchedDays(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")
	museum := ts.createItem(cookie, tripID, "Museum")

	ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-07"}]`)

	// Arrange the middle day the way a user would in the itinerary tab: put
	// something else on it, move the hotel to the bottom, and write a note on
	// the entry and on the day.
	sixth := ts.dayByDate(cookie, tripID, "2026-09-06")
	hotelEntry := sixth.Entries[0].ID
	museumEntry := ts.mustCreate(http.MethodPost, "/api/itinerary/days/"+*sixth.ID+"/entries", cookie,
		`{"item_id":"`+museum+`","note":"opens at ten"}`, http.StatusCreated)
	ts.mustCreateNoID(http.MethodPut, "/api/itinerary/days/"+*sixth.ID+"/entries/order", cookie,
		`{"entry_ids":["`+museumEntry+`","`+hotelEntry+`"]}`, http.StatusOK)
	ts.mustCreateNoID(http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/2026-09-06", cookie,
		`{"notes":"long day"}`, http.StatusOK)

	// Now extend the stay by a day from the location editor. The 6th is in
	// both the old and the new set, so nothing about it may change.
	got := ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-08"}]`)
	want := []itemDateRangeResponse{{StartDate: "2026-09-05", EndDate: "2026-09-08"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dates: got %+v, want %+v", got, want)
	}

	after := ts.dayByDate(cookie, tripID, "2026-09-06")
	if after.ID == nil || *after.ID != *sixth.ID {
		t.Errorf("the day row was replaced: %v -> %v", *sixth.ID, after.ID)
	}
	if after.Notes == nil || *after.Notes != "long day" {
		t.Errorf("day notes = %v, want them untouched", after.Notes)
	}
	if len(after.Entries) != 2 {
		t.Fatalf("got %d entries on the 6th, want 2", len(after.Entries))
	}
	// The order the user set, and the same rows: a recreate would put the
	// hotel back on top with a fresh id.
	if after.Entries[0].ID != museumEntry || after.Entries[1].ID != hotelEntry {
		t.Errorf("entries were rewritten: got %+v", after.Entries)
	}
	if after.Entries[0].Note == nil || *after.Entries[0].Note != "opens at ten" {
		t.Errorf("entry note = %v, want it untouched", after.Entries[0].Note)
	}

	// The new day landed, appended at the end of its own day.
	eighth := ts.dayByDate(cookie, tripID, "2026-09-08")
	if len(eighth.Entries) != 1 || eighth.Entries[0].ItemID != hotel {
		t.Errorf("the 8th: got %+v, want the hotel", eighth.Entries)
	}
}

// Removing a date takes the location off that day, but must not take the day
// itself when somebody has written on it.
func TestReconcileItemDatesRemovesDaysCarefully(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")

	ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-07"}]`)
	ts.mustCreateNoID(http.MethodPut, "/api/trips/"+tripID+"/itinerary/days/2026-09-07", cookie,
		`{"notes":"checkout"}`, http.StatusOK)

	// Shorten the stay: the 6th had nothing but the hotel, the 7th has a note.
	ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-05"}]`)

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/itinerary", cookie, "")
	days := decode[[]itineraryDayResponse](t, w)
	byDate := map[string]itineraryDayResponse{}
	for _, d := range days {
		byDate[d.Date] = d
	}

	// The trip has no start/end date, so only days with a row show at all.
	if _, ok := byDate["2026-09-06"]; ok {
		t.Errorf("the 6th is still in the itinerary; an emptied day with no notes should go")
	}
	seventh, ok := byDate["2026-09-07"]
	if !ok {
		t.Fatal("the 7th is gone, and it had notes on it")
	}
	if seventh.Notes == nil || *seventh.Notes != "checkout" {
		t.Errorf("the 7th notes = %v, want them kept", seventh.Notes)
	}
	if len(seventh.Entries) != 0 {
		t.Errorf("the 7th still has %d entries, want the hotel removed", len(seventh.Entries))
	}
}

// Omitting the dates key leaves the itinerary alone, and an empty list clears
// it. The same absent-versus-empty contract the other nested blocks have — but
// here "present" reaches into a shared structure, which is why the editor must
// only send it when the user touched the dates.
func TestItemDatesAbsentVersusEmpty(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")

	ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-06"}]`)

	w := ts.do(http.MethodPatch, "/api/items/"+hotel, cookie,
		`{"title":"Hotel Ranga","category":"stay","tags":["hotel"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch without dates: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[itemDetailResponse](t, w).Dates; len(got) != 1 {
		t.Errorf("omitting dates changed them: %+v", got)
	}

	if got := ts.setDates(cookie, hotel, `[]`); len(got) != 0 {
		t.Errorf("an empty list left %+v", got)
	}
	w = ts.do(http.MethodGet, "/api/trips/"+tripID+"/itinerary", cookie, "")
	if days := decode[[]itineraryDayResponse](t, w); len(days) != 0 {
		t.Errorf("got %d itinerary days, want the emptied ones gone: %+v", len(days), days)
	}
}

// Stage 26 Milestone 3: the locations list carries the dates too, so the tab
// can show them on the cards and filter and sort on them without asking per
// card. The rule the backlog entry stated is the one being held to here -- one
// trip-wide query, bucketed in Go.
type countingDateStore struct {
	db.Store
	byItem atomic.Int64
	byTrip atomic.Int64
}

func (s *countingDateStore) ListItineraryDatesByItem(ctx context.Context, itemID string) ([]db.ItemItineraryDate, error) {
	s.byItem.Add(1)
	return s.Store.ListItineraryDatesByItem(ctx, itemID)
}

func (s *countingDateStore) ListItemDatesByTrip(ctx context.Context, tripID string) ([]db.ItemItineraryDate, error) {
	s.byTrip.Add(1)
	return s.Store.ListItemDatesByTrip(ctx, tripID)
}

func TestListItemsCarriesCollapsedDatesInOneQuery(t *testing.T) {
	var counter *countingDateStore
	ts := newTestServerWith(t, func(s db.Store) db.Store {
		counter = &countingDateStore{Store: s}
		return counter
	}, nil)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")

	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")
	ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-07"}]`)

	// Two separate stretches, so the collapse has something to do that a
	// single range would not prove: these must come back as two ranges, not
	// one spanning the gap and not five days.
	split := ts.createItem(cookie, tripID, "Geysir")
	ts.setDates(cookie, split, `[{"start_date":"2026-09-05","end_date":"2026-09-06"},{"start_date":"2026-09-09","end_date":"2026-09-09"}]`)

	undated := ts.createItem(cookie, tripID, "Someday")

	counter.byItem.Store(0)
	counter.byTrip.Store(0)

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, body %s", w.Code, w.Body.String())
	}
	got := decode[[]itemResponse](t, w)

	byID := map[string][]itemDateRangeResponse{}
	for _, it := range got {
		if it.Dates == nil {
			t.Errorf("%s has null dates; the field must always be an array", it.Title)
		}
		byID[it.ID] = it.Dates
	}

	if want := []itemDateRangeResponse{{StartDate: "2026-09-05", EndDate: "2026-09-07"}}; !reflect.DeepEqual(byID[hotel], want) {
		t.Errorf("hotel dates: got %+v, want %+v", byID[hotel], want)
	}
	want := []itemDateRangeResponse{
		{StartDate: "2026-09-05", EndDate: "2026-09-06"},
		{StartDate: "2026-09-09", EndDate: "2026-09-09"},
	}
	if !reflect.DeepEqual(byID[split], want) {
		t.Errorf("split dates: got %+v, want %+v", byID[split], want)
	}
	if len(byID[undated]) != 0 {
		t.Errorf("undated location got dates: %+v", byID[undated])
	}

	if n := counter.byTrip.Load(); n != 1 {
		t.Errorf("trip-wide date query ran %d times, want exactly 1", n)
	}
	if n := counter.byItem.Load(); n != 0 {
		t.Errorf("per-location date query ran %d times for a 3-location list; the list must not use it", n)
	}
}

// The detail endpoint keeps its own per-item read: Dates moved up to
// itemResponse, and a field that is populated for the list but empty on the
// page it was built for would be a quiet regression.
func TestItemDetailStillCarriesDates(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	hotel := ts.createItem(cookie, tripID, "Hotel Ranga")
	ts.setDates(cookie, hotel, `[{"start_date":"2026-09-05","end_date":"2026-09-07"}]`)

	w := ts.do(http.MethodGet, "/api/items/"+hotel, cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: got %d", w.Code)
	}
	want := []itemDateRangeResponse{{StartDate: "2026-09-05", EndDate: "2026-09-07"}}
	if got := decode[itemDetailResponse](t, w).Dates; !reflect.DeepEqual(got, want) {
		t.Errorf("detail dates: got %+v, want %+v", got, want)
	}
}
