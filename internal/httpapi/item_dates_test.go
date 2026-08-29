package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"
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
