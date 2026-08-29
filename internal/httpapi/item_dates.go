package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"caravel/internal/db"
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

// The longest span one request may write, counted both per range and across
// all of them. Not pedantry: every day in a range becomes an itinerary day and
// an entry, written inside the transaction that saves the location, so a
// mistyped year would turn one save into tens of thousands of inserts holding
// a write lock the whole time. A year and a bit is more than any trip and far
// less than an accident.
const maxItemDateSpan = 370

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

// itemDateRangeRequest is one range as the client sends it. EndDate is optional
// and absent means a single day, which is what an "add date" form with an empty
// end field produces.
type itemDateRangeRequest struct {
	StartDate string  `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

// validateItemDateRanges checks the ranges before anything is written, so a bad
// date is a 400 rather than a rolled-back 500 — the property the links and
// dates blocks beside it already had.
func validateItemDateRanges(ranges []itemDateRangeRequest) error {
	total := 0
	for _, r := range ranges {
		start, err := time.Parse(isoDate, r.StartDate)
		if err != nil {
			return errors.New("every date needs a start_date in YYYY-MM-DD format")
		}
		end := start
		if r.EndDate != nil {
			end, err = time.Parse(isoDate, *r.EndDate)
			if err != nil {
				return errors.New("dates must be in YYYY-MM-DD format")
			}
		}
		if end.Before(start) {
			return errors.New("end_date must not be before start_date")
		}
		days := int(end.Sub(start).Hours()/24) + 1
		if days > maxItemDateSpan {
			return fmt.Errorf("a date range may not be longer than %d days", maxItemDateSpan)
		}
		total += days
	}
	if total > maxItemDateSpan {
		return fmt.Errorf("a location may not span more than %d days", maxItemDateSpan)
	}
	return nil
}

// expandDateRanges walks each range out into the days it covers, inclusive of
// both ends. Overlapping or repeated ranges union rather than colliding, so the
// client does not have to normalise what it sends.
func expandDateRanges(ranges []itemDateRangeRequest) (map[string]bool, error) {
	dates := map[string]bool{}
	for _, r := range ranges {
		start, err := time.Parse(isoDate, r.StartDate)
		if err != nil {
			return nil, err
		}
		end := start
		if r.EndDate != nil {
			if end, err = time.Parse(isoDate, *r.EndDate); err != nil {
				return nil, err
			}
		}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			dates[d.Format(isoDate)] = true
		}
	}
	return dates, nil
}

// reconcileItemDates makes the set of itinerary days a location appears on
// match the ranges the client submitted.
//
// Deliberately a diff, and not the delete-all-then-recreate that links use a
// few lines above in writeItemNested. An itinerary entry carries a position
// within its day and a note, neither of which the location editor knows
// anything about — so rewriting the set on every save would quietly discard
// somebody else's arrangement of a day the user never touched. Only days that
// are genuinely new or genuinely gone are written; a save that did not change
// the dates writes nothing at all.
//
// Takes the store it is given rather than reaching for s.Store: it runs inside
// the transaction that saves the item, and WithTx does not nest.
func reconcileItemDates(ctx context.Context, store db.Store, item db.Item, ranges []itemDateRangeRequest) error {
	desired, err := expandDateRanges(ranges)
	if err != nil {
		return err
	}

	current, err := store.ListItineraryDatesByItem(ctx, item.ID)
	if err != nil {
		return err
	}
	// A slice per date, not a single row: nothing constrains
	// (itinerary_day_id, item_id), so a location can already be on one day
	// twice and "remove that day" has to mean all of them.
	byDate := map[string][]db.ItemItineraryDate{}
	for _, row := range current {
		byDate[row.Date] = append(byDate[row.Date], row)
	}

	// Sorted rather than ranging the maps directly, so the writes happen in a
	// stable order whatever the map iteration does. It costs nothing and makes
	// a failure reproducible.
	gone := make([]string, 0, len(byDate))
	for date := range byDate {
		if !desired[date] {
			gone = append(gone, date)
		}
	}
	sort.Strings(gone)

	for _, date := range gone {
		rows := byDate[date]
		dayID := rows[0].DayID
		for _, row := range rows {
			if _, err := store.DeleteItineraryEntry(ctx, row.EntryID, row.DayID); err != nil {
				return err
			}
		}

		remaining, err := store.ListItineraryEntriesByDay(ctx, dayID)
		if err != nil {
			return err
		}
		if len(remaining) > 0 {
			// Close the hole the removal left, exactly as a move does to the
			// day it left behind.
			if err := renumberItineraryDay(ctx, store, dayID); err != nil {
				return err
			}
			continue
		}

		// Nothing left on the day. Removing it is the inverse of the lazy
		// creation below — but only when it has no notes of its own: a day
		// row deletes its entries by cascade, and losing somebody's written
		// plan for a day because a location stopped being on it would be a
		// far worse trade than an empty row. An empty in-range day is invisible
		// anyway, since handleGetItinerary synthesises a placeholder for it.
		day, err := store.GetItineraryDayByID(ctx, dayID)
		if err != nil {
			return err
		}
		if day.Notes == nil {
			if _, err := store.DeleteItineraryDay(ctx, dayID, item.TripID); err != nil {
				return err
			}
		}
	}

	added := make([]string, 0, len(desired))
	for date := range desired {
		if _, ok := byDate[date]; !ok {
			added = append(added, date)
		}
	}
	sort.Strings(added)

	for _, date := range added {
		// EnsureItineraryDay, never UpsertItineraryDayNotes: the upsert would
		// write the notes it is passed, and passing nil would blank the notes
		// of a day that already exists. handleMoveItineraryEntry makes the same
		// choice for the same reason.
		day, err := store.EnsureItineraryDay(ctx, uuid.NewString(), item.TripID, date)
		if err != nil {
			return err
		}
		existing, err := store.ListItineraryEntriesByDay(ctx, day.ID)
		if err != nil {
			return err
		}
		// Appended at the end of the day, numbered from the count, which is
		// what handleCreateItineraryEntry does — so adding a hotel by setting
		// its dates and adding it from the itinerary tab land in the same
		// place. The day is not renumbered: appending is correct even on a day
		// whose stored order has gaps, and rewriting the order of a day the
		// user did not touch is a surprise.
		if _, err := store.CreateItineraryEntry(ctx, db.CreateItineraryEntryParams{
			ID:             uuid.NewString(),
			ItineraryDayID: day.ID,
			ItemID:         item.ID,
			SortOrder:      len(existing),
		}); err != nil {
			return err
		}
	}

	// Dates in both sets are deliberately untouched: no delete, no insert, no
	// renumber. That is the whole point of the diff.
	return nil
}
