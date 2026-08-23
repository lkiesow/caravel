package httpapi

import (
	"reflect"
	"testing"
)

// The property that matters more than any individual case: the shares always
// sum to exactly the amount. A split that loses a unit is the bug the whole
// integer-minor-unit decision exists to prevent, and division is the obvious
// way to let it back in.
func TestSplitAmountAlwaysSumsToTheWhole(t *testing.T) {
	people := []string{"a", "b", "c", "d", "e", "f", "g"}
	for _, amount := range []int64{1, 2, 3, 5, 7, 99, 100, 1000, 1001, 12345, 999999999} {
		for n := 1; n <= len(people); n++ {
			shares := splitAmount(amount, people[:n])
			var total int64
			for _, share := range shares {
				total += share
			}
			if total != amount {
				t.Errorf("%d across %d people: shares sum to %d", amount, n, total)
			}
			if len(shares) != n {
				t.Errorf("%d across %d people: got %d shares", amount, n, len(shares))
			}
			// Nobody is more than one unit from anybody else, which is what
			// makes it an equal split rather than merely a total-preserving one.
			var min, max int64 = -1, -1
			for _, share := range shares {
				if min == -1 || share < min {
					min = share
				}
				if share > max {
					max = share
				}
			}
			if max-min > 1 {
				t.Errorf("%d across %d people: shares range %d..%d, more than one unit apart", amount, n, min, max)
			}
		}
	}
}

func TestSplitAmountRemainderGoesToLowestIDs(t *testing.T) {
	// The case named in the plan: 1000 across three is 334/333/333, not
	// 333.33 three times.
	got := splitAmount(1000, []string{"carol", "alice", "bob"})
	want := map[string]int64{"alice": 334, "bob": 333, "carol": 333}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Input order must not matter: the same set of people always splits the
	// same way, or reading a list twice would produce two different balances.
	shuffled := splitAmount(1000, []string{"bob", "carol", "alice"})
	if !reflect.DeepEqual(shuffled, got) {
		t.Errorf("split depends on input order: %v vs %v", shuffled, got)
	}

	// Two units of remainder go to the two lowest.
	got = splitAmount(1001, []string{"alice", "bob", "carol"})
	want = map[string]int64{"alice": 334, "bob": 334, "carol": 333}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("1001 across three: got %v, want %v", got, want)
	}
}

// A zero-exponent currency has no sub-units to hide a rounding error in, so an
// amount of yen splits the same way any other integer does -- which is the
// point of storing minor units rather than a decimal.
func TestSplitAmountZeroExponentCurrency(t *testing.T) {
	// 1200 yen across 7 people: 171 remainder 3.
	shares := splitAmount(1200, []string{"a", "b", "c", "d", "e", "f", "g"})
	var total int64
	counts := map[int64]int{}
	for _, share := range shares {
		total += share
		counts[share]++
	}
	if total != 1200 {
		t.Errorf("total: got %d, want 1200", total)
	}
	if counts[172] != 3 || counts[171] != 4 {
		t.Errorf("expected three shares of 172 and four of 171, got %v", counts)
	}
}

func TestSplitAmountEmptySet(t *testing.T) {
	if got := splitAmount(500, nil); len(got) != 0 {
		t.Errorf("splitting between nobody: got %v, want an empty map", got)
	}
}

func TestDedupeIDs(t *testing.T) {
	got := dedupeIDs([]string{"b", "a", "b", "", "c", "a"})
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := dedupeIDs(nil); len(got) != 0 {
		t.Errorf("dedupe(nil): got %v", got)
	}
	// The duplicate must not survive into a split, where it would double that
	// person's weight.
	shares := splitAmount(300, dedupeIDs([]string{"a", "a", "b"}))
	if !reflect.DeepEqual(shares, map[string]int64{"a": 150, "b": 150}) {
		t.Errorf("a duplicated id changed the split: %v", shares)
	}
}
