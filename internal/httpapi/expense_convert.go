package httpapi

import (
	"fmt"
	"math/big"

	"caravel/internal/db"
)

// Converting an expense into the trip's main currency. Stage 32 Milestone 3.
//
// Two rules decide everything downstream, and both are about ordering.
//
// First: an expense is converted exactly once, before it is split, totalled or
// balanced. Every later calculation reads the converted integer and never the
// rate again. That is what preserves the property expense_split_test.go exists
// to protect -- that the shares of an expense sum to exactly the amount.
// Converting each share instead would round n times and lose the guarantee for
// the sake of nothing.
//
// Second: the rate is a minor-unit-to-minor-unit factor, so this file needs no
// idea how many decimal places a currency has. See db.TripCurrency.

// convertMinor restates an amount in the main currency's minor unit.
//
// Computed through math/big rather than int64 because the intermediate product
// overflows: an amount of 1e15 minor units against a rate near 1e9 is 1e24,
// and int64 stops at about 9.2e18. Overflow in a ledger is not a bug to find
// in production.
//
// Rounds half away from zero -- the rule people expect when they check the
// arithmetic by hand -- and never returns less than 1. A recorded expense is
// something that happened, so a tiny amount against a small rate rounds down
// to the smallest unit that still exists rather than to nothing at all.
func convertMinor(amountMinor, ratePPB int64) int64 {
	// The identity rate is the main currency's own, and returning the input
	// untouched keeps single-currency trips bit-for-bit what they were before
	// this file existed.
	if ratePPB == db.RateOne {
		return amountMinor
	}

	product := new(big.Int).Mul(big.NewInt(amountMinor), big.NewInt(ratePPB))
	billion := big.NewInt(db.RateOne)

	// Half away from zero: add half the divisor before truncating. big.Int's
	// Quo truncates toward zero, so the half is added with the sign of the
	// product rather than unconditionally. Amounts are positive today (the
	// column has a CHECK), but a rounding helper that is only correct for
	// positive input is a trap for whoever lifts that restriction.
	half := new(big.Int).Rsh(billion, 1)
	if product.Sign() < 0 {
		product.Sub(product, half)
	} else {
		product.Add(product, half)
	}
	converted := product.Quo(product, billion)

	if converted.Sign() > 0 && !converted.IsInt64() {
		// Unreachable with maxRatePPB in force, and still not something to
		// return a silently truncated number for.
		return 0
	}
	result := converted.Int64()
	if result == 0 && amountMinor > 0 {
		return 1
	}
	return result
}

// convertedExpenses restates a whole ledger in the trip's main currency,
// returning a copy whose AmountMinor is the converted figure.
//
// A copy, so that payerTotals and computeBalances need to know nothing about
// currencies at all: they keep reading AmountMinor and are simply handed
// converted rows. That is deliberate -- it leaves exactly one place in the
// codebase where a rate is applied, and it is this function.
//
// An expense naming a currency the trip does not configure is an error rather
// than a fallback. Treating it as the main currency would reprice it at 1:1
// and report the wrong total confidently, which is the one outcome worth
// failing a request over. The Milestone 2 guard makes it unreachable through
// the API; a hand-edited database can still do it.
func convertedExpenses(expenses []db.Expense, rates map[string]int64, mainCurrency string) ([]db.Expense, error) {
	out := make([]db.Expense, len(expenses))
	for i, e := range expenses {
		rate, ok := rates[expenseCurrency(e, mainCurrency)]
		if !ok {
			return nil, fmt.Errorf("expense %s is recorded in %s, which this trip has no rate for",
				e.ID, expenseCurrency(e, mainCurrency))
		}
		e.AmountMinor = convertMinor(e.AmountMinor, rate)
		// The copy keeps its original currency code so nothing downstream can
		// mistake a converted row for one recorded in the main currency, but
		// nothing downstream reads it either.
		out[i] = e
	}
	return out, nil
}

// expenseCurrency is the effective code of an expense: its own, or the trip's
// main currency where it stores none. The empty-means-the-trip rule lives here
// and nowhere else, so no caller and no client re-implements it.
func expenseCurrency(e db.Expense, mainCurrency string) string {
	if e.Currency == nil {
		return mainCurrency
	}
	return *e.Currency
}
