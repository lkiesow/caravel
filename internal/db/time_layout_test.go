package db

import (
	"sort"
	"testing"
	"time"
)

// The stored layout has to sort as text, because that is how SQLite compares
// it: every ORDER BY created_at in this dialect is a string comparison.
//
// This is not a theoretical property. time.RFC3339Nano, which formatTime used
// until migration 0012, trims trailing zeros -- so a timestamp at .100000000
// is written ".1Z" and sorts *after* one at .120000000 written ".12Z", because
// "Z" is greater than "2". That put the locations list in the wrong order for
// any two rows written in the same second, which is every row a batch writes.
func TestFormatTimeSortsAsTextWithinASecond(t *testing.T) {
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	// Deliberately the fractions that expose the trimming: 1ms renders as
	// ".001Z" under RFC3339Nano and 120ms as ".12Z".
	offsets := []time.Duration{
		0,
		1 * time.Nanosecond,
		1 * time.Microsecond,
		1 * time.Millisecond,
		12 * time.Millisecond,
		100 * time.Millisecond,
		120 * time.Millisecond,
		999999999 * time.Nanosecond,
	}

	formatted := make([]string, len(offsets))
	for i, d := range offsets {
		formatted[i] = formatTime(base.Add(d))
	}

	sorted := append([]string(nil), formatted...)
	sort.Strings(sorted)
	for i := range formatted {
		if sorted[i] != formatted[i] {
			t.Fatalf("text order does not match time order at %d: got %q, want %q\nformatted: %q", i, sorted[i], formatted[i], formatted)
		}
	}
}

// Every stored value round-trips, including the ones written by the trimming
// layout that was in use before -- those rows are still in existing databases
// and were deliberately not migrated.
func TestParseTimeAcceptsBothLayouts(t *testing.T) {
	want := time.Date(2026, 9, 29, 12, 0, 0, 120000000, time.UTC)
	for _, s := range []string{
		formatTime(want),              // the fixed-width layout
		want.Format(time.RFC3339Nano), // what older rows hold
	} {
		if got := parseTime(s); !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v, want %v", s, got, want)
		}
	}
	// And a value with no fractional part at all, which the seconds-precision
	// columns elsewhere in the schema hold.
	whole := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if got := parseTime(whole.Format(time.RFC3339)); !got.Equal(whole) {
		t.Errorf("parseTime of a whole second = %v, want %v", got, whole)
	}
}
