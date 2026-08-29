package httpapi

import (
	"sort"
	"time"
)

// A location's dates are a view of the itinerary, not a table of their own.
//
// Stage 25 removed item_dates. What a location page calls its dates is now the
// set of itinerary days the location appears on, with runs of consecutive days
// collapsed into ranges — so a hotel that is on the 5th, 6th and 7th reads as
// "5–7 September" rather than as three rows, and moving it off the 6th in the
// itinerary makes the location say "5 September" and "7 September".
//
// Both endpoints are inclusive. Checking out on the 7th still means being there
// on the 7th, which is what a person means by "the 5th to the 7th"; it also
// makes the collapse and its inverse the same arithmetic in both directions.

// itemDateRangeResponse is one run of consecutive days. There is no id: nothing
// addresses a range, and the rows underneath it are itinerary entries.
type itemDateRangeResponse struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

const isoDate = "2006-01-02"

// collapseDateRanges turns the days a location appears on into inclusive
// ranges. The input may be unordered and may repeat a date — an item can sit on
// one day twice, since nothing constrains the pair — so it is reduced to a set
// first.
//
// Always returns a non-nil slice, so the JSON is [] rather than null, matching
// the links and files lists beside it.
func collapseDateRanges(dates []string) []itemDateRangeResponse {
	unique := make(map[string]struct{}, len(dates))
	for _, d := range dates {
		unique[d] = struct{}{}
	}

	sorted := make([]string, 0, len(unique))
	for d := range unique {
		sorted = append(sorted, d)
	}
	// ISO dates are zero-padded, so lexical order is chronological order — the
	// same property handleGetItinerary relies on when it sorts the day list.
	sort.Strings(sorted)

	ranges := []itemDateRangeResponse{}
	for _, date := range sorted {
		// Extend the open range when this date is the previous one plus a day.
		// Real date arithmetic rather than string arithmetic: "2026-01-31" is
		// followed by "2026-02-01", which no amount of incrementing the last
		// two characters will produce.
		//
		// A date that will not parse cannot come out of the database, but it
		// breaks the run rather than propagating: one unreadable row should
		// cost its own range, not the whole card.
		if n := len(ranges); n > 0 {
			if prev, err := time.Parse(isoDate, ranges[n-1].EndDate); err == nil {
				if prev.AddDate(0, 0, 1).Format(isoDate) == date {
					ranges[n-1].EndDate = date
					continue
				}
			}
		}
		ranges = append(ranges, itemDateRangeResponse{StartDate: date, EndDate: date})
	}
	return ranges
}
