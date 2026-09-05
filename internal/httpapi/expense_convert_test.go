package httpapi

import (
	"math"
	"testing"

	"caravel/internal/db"
)

// The conversion arithmetic, unit level. The HTTP tests in expenses_test.go
// cover the wiring; these cover the rules -- the same split as
// expense_balances_test.go and for the same reason.

func TestConvertMinor(t *testing.T) {
	cases := []struct {
		name   string
		amount int64
		rate   int64
		want   int64
	}{
		// The worked example from the migration: 1200 yen at 0.58 cents a yen.
		{"the JPY example", 1200, 580_000_000, 696},
		// The identity rate is the main currency's own, and must be exact
		// rather than merely close: it is what every single-currency trip in
		// the database goes through.
		{"the identity rate", 12345, db.RateOne, 12345},
		{"identity on a large amount", math.MaxInt64 / 2, db.RateOne, math.MaxInt64 / 2},

		// Rounding, at and around the half. Half away from zero is the rule a
		// person checking the arithmetic by hand will expect.
		{"rounds down below the half", 100, 1_004_000_000, 100},
		{"rounds up at exactly the half", 1, 1_500_000_000, 2},
		{"rounds up above the half", 100, 1_006_000_000, 101},
		{"rounds down just under the half", 3, 1_499_999_999, 4},

		// A real expense converts to something. Rounding a recorded amount
		// away to nothing would make a row that exists cost zero.
		{"never rounds away to nothing", 1, 1, 1},
		{"still nothing-proof at the smallest rate", 1, 1000, 1},

		// The reason this goes through math/big: the intermediate product of
		// these two overflows int64 by six orders of magnitude.
		{"an amount whose product overflows int64", 1_000_000_000_000_000, 2_000_000_000, 2_000_000_000_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := convertMinor(tc.amount, tc.rate); got != tc.want {
				t.Errorf("convertMinor(%d, %d): got %d, want %d", tc.amount, tc.rate, got, tc.want)
			}
		})
	}
}

// Half away from zero has to mean away from zero in both directions, or the
// helper is a trap for whoever lifts the positive-only restriction on amounts.
func TestConvertMinorRoundsSymmetrically(t *testing.T) {
	if got, want := convertMinor(-1, 1_500_000_000), int64(-2); got != want {
		t.Errorf("negative half: got %d, want %d", got, want)
	}
	if got, want := convertMinor(1, 1_500_000_000), int64(2); got != want {
		t.Errorf("positive half: got %d, want %d", got, want)
	}
}

// The property that matters more than any individual case, and the reason
// conversion happens before the split rather than after: converting once and
// then splitting keeps the guarantee that shares sum to exactly the amount.
// Converting each share instead would round n times.
func TestConvertedSharesStillSumToTheWhole(t *testing.T) {
	ids := []string{"a", "b", "c"}
	for _, amount := range []int64{1, 2, 1000, 1201, 99999} {
		for _, rate := range []int64{db.RateOne, 580_000_000, 1_337_000_000, 3} {
			converted := convertMinor(amount, rate)
			var sum int64
			for _, share := range splitAmount(converted, ids) {
				sum += share
			}
			if sum != converted {
				t.Errorf("amount %d at rate %d: shares sum to %d, want %d",
					amount, rate, sum, converted)
			}
		}
	}
}

func TestConvertedExpenses(t *testing.T) {
	jpy, usd := "JPY", "USD"
	expenses := []db.Expense{
		{ID: "1", AmountMinor: 1000}, // no currency: the trip's own
		{ID: "2", AmountMinor: 1200, Currency: &jpy},
		{ID: "3", AmountMinor: 500, Currency: &usd},
	}
	rates := map[string]int64{"EUR": db.RateOne, "JPY": 580_000_000, "USD": 920_000_000}

	got, err := convertedExpenses(expenses, rates, "EUR")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	want := []int64{1000, 696, 460}
	for i, w := range want {
		if got[i].AmountMinor != w {
			t.Errorf("expense %s: got %d, want %d", got[i].ID, got[i].AmountMinor, w)
		}
	}
	// The input must not have been rewritten underneath the caller: the
	// original amounts are what the rows report as paid.
	if expenses[1].AmountMinor != 1200 {
		t.Errorf("the source ledger was mutated: got %d, want 1200", expenses[1].AmountMinor)
	}
}

// A currency with no rate is an error, not a 1:1 fallback. Repricing it
// silently would report a confidently wrong total, which is the one outcome
// worth failing a request over.
func TestConvertedExpensesRefusesAnUnratedCurrency(t *testing.T) {
	thb := "THB"
	_, err := convertedExpenses(
		[]db.Expense{{ID: "1", AmountMinor: 1000, Currency: &thb}},
		map[string]int64{"EUR": db.RateOne},
		"EUR",
	)
	if err == nil {
		t.Fatal("an expense in an unrated currency converted without complaint")
	}
}

func TestExpenseCurrencyDefaultsToTheTrip(t *testing.T) {
	jpy := "JPY"
	if got := expenseCurrency(db.Expense{}, "EUR"); got != "EUR" {
		t.Errorf("no currency: got %q, want EUR", got)
	}
	if got := expenseCurrency(db.Expense{Currency: &jpy}, "EUR"); got != "JPY" {
		t.Errorf("its own currency: got %q, want JPY", got)
	}
}

func TestRateIndexIncludesTheMainCurrencyAtIdentity(t *testing.T) {
	rates := rateIndex("EUR", []tripCurrencyResponse{{Code: "JPY", RatePPB: 580_000_000}})
	if rates["EUR"] != db.RateOne {
		t.Errorf("main currency rate: got %d, want %d", rates["EUR"], db.RateOne)
	}
	if rates["JPY"] != 580_000_000 {
		t.Errorf("JPY rate: got %d, want 580000000", rates["JPY"])
	}
	if _, ok := rates["USD"]; ok {
		t.Error("an unconfigured currency has a rate")
	}
}
