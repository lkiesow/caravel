package httpapi

import (
	"context"
	"testing"
	"time"

	"caravel/internal/db"

	"github.com/google/uuid"
)

// Store-level coverage for the trip_currencies table and the expenses.currency
// column added in Stage 32 Milestone 1, in the same package and for the same
// reason expense_store_test.go is: the harness here already builds a real
// Server over a real, migrated database.
//
// Like the expense round trip above it, this names every field on purpose. A
// hand-written adapter that drops a field on read is invisible to a compile,
// and there are now two of them per table.

func TestTripCurrencyStoreRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_, tripID := seedExpenseTrip(t, ts)

	// 1 JPY = 0.0058 EUR, folded to minor units: one yen is 0.58 cents.
	const jpyRate = int64(580_000_000)
	createdAt := time.Date(2026, 8, 19, 10, 30, 0, 0, time.UTC)

	created, err := ts.Store.CreateTripCurrency(ctx, db.CreateTripCurrencyParams{
		TripID:    tripID,
		Code:      "JPY",
		RatePPB:   jpyRate,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create trip currency: %v", err)
	}
	if created.TripID != tripID {
		t.Errorf("trip id: got %q, want %q", created.TripID, tripID)
	}
	if created.Code != "JPY" {
		t.Errorf("code: got %q, want JPY", created.Code)
	}
	if created.RatePPB != jpyRate {
		t.Errorf("rate: got %d, want %d", created.RatePPB, jpyRate)
	}
	if !created.CreatedAt.Equal(createdAt) {
		t.Errorf("created at: got %v, want %v", created.CreatedAt, createdAt)
	}

	listed, err := ts.Store.ListTripCurrencies(ctx, tripID)
	if err != nil {
		t.Fatalf("list trip currencies: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d currencies, want 1", len(listed))
	}
	if listed[0].Code != "JPY" || listed[0].RatePPB != jpyRate {
		t.Errorf("listed: got %q at %d, want JPY at %d", listed[0].Code, listed[0].RatePPB, jpyRate)
	}
	if !listed[0].CreatedAt.Equal(createdAt) {
		t.Errorf("listed created at: got %v, want %v", listed[0].CreatedAt, createdAt)
	}
}

// The set is replaced wholesale rather than patched, so a save is a delete
// followed by inserts. This is that sequence, and the ordering the list
// promises.
func TestTripCurrenciesReplaceWholesale(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_, tripID := seedExpenseTrip(t, ts)

	now := time.Now().UTC()
	for _, c := range []struct {
		code string
		rate int64
	}{{"JPY", 580_000_000}, {"USD", 920_000_000}} {
		if _, err := ts.Store.CreateTripCurrency(ctx, db.CreateTripCurrencyParams{
			TripID: tripID, Code: c.code, RatePPB: c.rate, CreatedAt: now,
		}); err != nil {
			t.Fatalf("create %s: %v", c.code, err)
		}
	}

	listed, err := ts.Store.ListTripCurrencies(ctx, tripID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Ordered by code, not by insertion: JPY was written first and still sorts
	// first, so assert the pair rather than only the length.
	if len(listed) != 2 || listed[0].Code != "JPY" || listed[1].Code != "USD" {
		t.Fatalf("listed %v, want JPY then USD", listed)
	}

	if err := ts.Store.DeleteTripCurrenciesByTrip(ctx, tripID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ts.Store.CreateTripCurrency(ctx, db.CreateTripCurrencyParams{
		TripID: tripID, Code: "JPY", RatePPB: 600_000_000, CreatedAt: now,
	}); err != nil {
		t.Fatalf("recreate: %v", err)
	}

	listed, err = ts.Store.ListTripCurrencies(ctx, tripID)
	if err != nil {
		t.Fatalf("list after replace: %v", err)
	}
	if len(listed) != 1 || listed[0].Code != "JPY" {
		t.Fatalf("after replace: %v, want only JPY", listed)
	}
	if listed[0].RatePPB != 600_000_000 {
		t.Errorf("rate after replace: got %d, want 600000000", listed[0].RatePPB)
	}
}

func TestTripCurrenciesGoWithTheTrip(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	ownerID, tripID := seedExpenseTrip(t, ts)

	if _, err := ts.Store.CreateTripCurrency(ctx, db.CreateTripCurrencyParams{
		TripID: tripID, Code: "JPY", RatePPB: 580_000_000, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create trip currency: %v", err)
	}
	if _, err := ts.Store.DeleteTrip(ctx, tripID, ownerID); err != nil {
		t.Fatalf("delete trip: %v", err)
	}

	listed, err := ts.Store.ListTripCurrencies(ctx, tripID)
	if err != nil {
		t.Fatalf("list after trip delete: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("the trip is gone but %d currencies survived it", len(listed))
	}
}

// NULL means the trip main currency, and it is what every expense recorded
// before Stage 32 holds -- so the nil case matters as much as the set one.
func TestExpenseCurrencyRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	jpy := "JPY"
	cases := []struct {
		name  string
		given *string
	}{
		{"the trip main currency", nil},
		{"a currency of its own", &jpy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
				ID:          uuid.NewString(),
				TripID:      tripID,
				Title:       "Ramen",
				AmountMinor: 1200,
				Currency:    tc.given,
				SpentOn:     "2026-08-19",
				PayerUserID: &userID,
				CreatedAt:   time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("create expense: %v", err)
			}
			assertCurrency(t, "created", created.Currency, tc.given)

			fetched, err := ts.Store.GetExpenseByID(ctx, created.ID)
			if err != nil {
				t.Fatalf("get expense: %v", err)
			}
			assertCurrency(t, "fetched", fetched.Currency, tc.given)

			listed, err := ts.Store.ListExpensesByTrip(ctx, tripID)
			if err != nil {
				t.Fatalf("list expenses: %v", err)
			}
			var found bool
			for _, row := range listed {
				if row.ID == created.ID {
					found = true
					assertCurrency(t, "listed", row.Currency, tc.given)
				}
			}
			if !found {
				t.Fatal("the created expense did not appear in ListExpensesByTrip")
			}
		})
	}
}

// An update is a whole-expense edit, so it must be able to move a row in both
// directions -- into a currency and back out to the trip default.
func TestExpenseCurrencyIsEditedAsAWhole(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      tripID,
		Title:       "Ramen",
		AmountMinor: 1200,
		SpentOn:     "2026-08-19",
		PayerUserID: &userID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}

	jpy := "JPY"
	updated, err := ts.Store.UpdateExpense(ctx, db.UpdateExpenseParams{
		ID: created.ID, TripID: tripID, Title: "Ramen", AmountMinor: 1200,
		Currency: &jpy, SpentOn: "2026-08-19", PayerUserID: &userID,
	})
	if err != nil {
		t.Fatalf("update into JPY: %v", err)
	}
	assertCurrency(t, "after update", updated.Currency, &jpy)

	back, err := ts.Store.UpdateExpense(ctx, db.UpdateExpenseParams{
		ID: created.ID, TripID: tripID, Title: "Ramen", AmountMinor: 1200,
		Currency: nil, SpentOn: "2026-08-19", PayerUserID: &userID,
	})
	if err != nil {
		t.Fatalf("update back to the trip currency: %v", err)
	}
	assertCurrency(t, "after clearing", back.Currency, nil)
}

func TestCountExpensesByCurrency(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	jpy, usd := "JPY", "USD"
	for _, currency := range []*string{nil, &jpy, &jpy, &usd} {
		if _, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
			ID:          uuid.NewString(),
			TripID:      tripID,
			Title:       "Something",
			AmountMinor: 500,
			Currency:    currency,
			SpentOn:     "2026-08-19",
			PayerUserID: &userID,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("create expense: %v", err)
		}
	}

	usage, err := ts.Store.CountExpensesByCurrency(ctx, tripID)
	if err != nil {
		t.Fatalf("count by currency: %v", err)
	}
	// The main-currency row holds NULL and must not appear: it is not a code
	// that can be removed, so counting it would guard nothing.
	want := map[string]int64{"JPY": 2, "USD": 1}
	if len(usage) != len(want) {
		t.Fatalf("got %d rows (%v), want %d", len(usage), usage, len(want))
	}
	for _, row := range usage {
		if want[row.Code] != row.ExpenseCount {
			t.Errorf("%s: got %d, want %d", row.Code, row.ExpenseCount, want[row.Code])
		}
	}
}

func assertCurrency(t *testing.T, stage string, got, want *string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s currency: got %q, want nil", stage, *got)
	case want != nil && got == nil:
		t.Errorf("%s currency: got nil, want %q", stage, *want)
	case want != nil && got != nil && *got != *want:
		t.Errorf("%s currency: got %q, want %q", stage, *got, *want)
	}
}
