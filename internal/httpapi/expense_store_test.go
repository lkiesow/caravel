package httpapi

import (
	"context"
	"testing"
	"time"

	"caravel/internal/db"

	"github.com/google/uuid"
)

// Store-level coverage for the expenses table added in Stage 17 Milestone 1.
//
// It lives in this package rather than under internal/db because that package
// has no test files at all: the harness here already builds a real Server over
// a real, migrated, per-test SQLite database, so reaching ts.Store gets the
// same store the handlers use with none of the setup written twice.
//
// The round-trip test names every field on purpose. Stage 14 twice shipped a
// hand-written adapter that silently dropped a field on read, in both
// dialects, which a compile cannot see -- so asserting the struct field by
// field is the only thing standing between that bug and a green build.

// seedExpenseTrip makes a trip owned by a fresh user and returns both.
func seedExpenseTrip(t *testing.T, ts *testServer) (userID, tripID string) {
	t.Helper()

	user, err := ts.Auth.Register(context.Background(), "spender", "password123", "Spender")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	now := time.Now().UTC()
	trip, err := ts.Store.CreateTrip(context.Background(), db.CreateTripParams{
		ID:        uuid.NewString(),
		OwnerID:   user.ID,
		Title:     "Iceland",
		Currency:  db.DefaultCurrency,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create trip: %v", err)
	}
	return user.ID, trip.ID
}

func TestExpenseRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      tripID,
		Title:       "Blue Lagoon tickets",
		AmountMinor: 12750,
		SpentOn:     "2026-08-19",
		PayerUserID: &userID,
		CreatedAt:   time.Date(2026, 8, 19, 10, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}

	// Read it back through a different query than the one that wrote it: an
	// adapter that drops a field on read usually still returns it from the
	// RETURNING clause of the insert.
	got, err := ts.Store.GetExpenseByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get expense: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("id: got %q, want %q", got.ID, created.ID)
	}
	if got.TripID != tripID {
		t.Errorf("trip_id: got %q, want %q", got.TripID, tripID)
	}
	if got.Title != "Blue Lagoon tickets" {
		t.Errorf("title: got %q, want %q", got.Title, "Blue Lagoon tickets")
	}
	if got.AmountMinor != 12750 {
		t.Errorf("amount_minor: got %d, want %d", got.AmountMinor, 12750)
	}
	if got.SpentOn != "2026-08-19" {
		t.Errorf("spent_on: got %q, want %q", got.SpentOn, "2026-08-19")
	}
	if got.PayerUserID == nil || *got.PayerUserID != userID {
		t.Errorf("payer_user_id: got %v, want %q", got.PayerUserID, userID)
	}
	if !got.CreatedAt.Equal(time.Date(2026, 8, 19, 10, 30, 0, 0, time.UTC)) {
		t.Errorf("created_at: got %v", got.CreatedAt)
	}
}

// A nil payer is a real state -- ON DELETE SET NULL produces it -- so it has to
// survive the round trip as nil rather than as an empty string.
func TestExpenseWithoutPayerRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_, tripID := seedExpenseTrip(t, ts)

	created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      tripID,
		Title:       "Parking",
		AmountMinor: 300,
		SpentOn:     "2026-08-20",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	got, err := ts.Store.GetExpenseByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get expense: %v", err)
	}
	if got.PayerUserID != nil {
		t.Errorf("payer_user_id: got %q, want nil", *got.PayerUserID)
	}
}

// Deleting the payer's account must not delete the ledger: the row survives
// with a nil payer, which is what ON DELETE SET NULL is for.
func TestDeletingPayerLeavesExpense(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	_, tripID := seedExpenseTrip(t, ts)

	// The payer is a second account rather than the trip owner: deleting the
	// owner would cascade the trip away and prove nothing about the expense.
	payer, err := ts.Auth.Register(ctx, "payer", "password123", "Payer")
	if err != nil {
		t.Fatalf("register payer: %v", err)
	}
	created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      tripID,
		Title:       "Fuel",
		AmountMinor: 8900,
		SpentOn:     "2026-08-21",
		PayerUserID: &payer.ID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	if _, err := ts.Store.DeleteUser(ctx, payer.ID); err != nil {
		t.Fatalf("delete payer: %v", err)
	}

	got, err := ts.Store.GetExpenseByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get expense after deleting payer: %v", err)
	}
	if got.PayerUserID != nil {
		t.Errorf("payer_user_id: got %q, want nil after the account was deleted", *got.PayerUserID)
	}
	if got.AmountMinor != 8900 {
		t.Errorf("amount_minor: got %d, want 8900 -- the row must survive intact", got.AmountMinor)
	}
}

// The trip total was a SUM in SQL until Stage 32, and this test asserted it
// here. It cannot be a query any more -- a trip may hold more than one
// currency and the database cannot add two of them together -- so the total is
// summed in the handler over converted rows, and its coverage moved with it to
// expenses_test.go. What is left here is the ordering, which is still the
// query's own promise.
func TestListExpensesByTrip(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	for _, e := range []struct {
		title   string
		amount  int64
		spentOn string
	}{
		{"Hostel", 4500, "2026-08-18"},
		{"Dinner", 3200, "2026-08-20"},
		{"Bus", 750, "2026-08-19"},
	} {
		if _, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
			ID:          uuid.NewString(),
			TripID:      tripID,
			Title:       e.title,
			AmountMinor: e.amount,
			SpentOn:     e.spentOn,
			PayerUserID: &userID,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("create %s: %v", e.title, err)
		}
	}

	list, err := ts.Store.ListExpensesByTrip(ctx, tripID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list: got %d expenses, want 3", len(list))
	}
	// Newest spending first.
	wantOrder := []string{"Dinner", "Bus", "Hostel"}
	for i, want := range wantOrder {
		if list[i].Title != want {
			t.Errorf("list[%d]: got %q, want %q", i, list[i].Title, want)
		}
	}
}

func TestUpdateAndDeleteExpenseAreScopedToTheirTrip(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	now := time.Now().UTC()
	other, err := ts.Store.CreateTrip(ctx, db.CreateTripParams{
		ID:        uuid.NewString(),
		OwnerID:   userID,
		Title:     "Norway",
		Currency:  db.DefaultCurrency,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create other trip: %v", err)
	}

	created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      tripID,
		Title:       "Ferry",
		AmountMinor: 15000,
		SpentOn:     "2026-08-22",
		PayerUserID: &userID,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}

	// The trip_id predicate is the point: the right id under the wrong trip
	// must not match, or a role held on one trip would reach another's rows.
	if _, err := ts.Store.UpdateExpense(ctx, db.UpdateExpenseParams{
		ID:          created.ID,
		TripID:      other.ID,
		Title:       "Hijacked",
		AmountMinor: 1,
		SpentOn:     "2026-08-22",
	}); err == nil {
		t.Error("update through the wrong trip: got nil error, want not-found")
	}
	if deleted, err := ts.Store.DeleteExpense(ctx, created.ID, other.ID); err != nil {
		t.Fatalf("delete through the wrong trip: %v", err)
	} else if deleted {
		t.Error("delete through the wrong trip reported a deletion")
	}

	updated, err := ts.Store.UpdateExpense(ctx, db.UpdateExpenseParams{
		ID:          created.ID,
		TripID:      tripID,
		Title:       "Ferry to Heimaey",
		AmountMinor: 16500,
		SpentOn:     "2026-08-23",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Ferry to Heimaey" || updated.AmountMinor != 16500 || updated.SpentOn != "2026-08-23" {
		t.Errorf("update: got %+v", updated)
	}
	if updated.PayerUserID != nil {
		t.Errorf("update: payer should have been cleared, got %q", *updated.PayerUserID)
	}

	if deleted, err := ts.Store.DeleteExpense(ctx, created.ID, tripID); err != nil {
		t.Fatalf("delete: %v", err)
	} else if !deleted {
		t.Error("delete reported no deletion")
	}
}

// Deleting a trip takes its expenses with it, through the schema's ON DELETE
// CASCADE rather than through anything the application does.
func TestDeletingTripCascadesToExpenses(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, tripID := seedExpenseTrip(t, ts)

	created, err := ts.Store.CreateExpense(ctx, db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      tripID,
		Title:       "Museum",
		AmountMinor: 2000,
		SpentOn:     "2026-08-24",
		PayerUserID: &userID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	if _, err := ts.Store.DeleteTrip(ctx, tripID, userID); err != nil {
		t.Fatalf("delete trip: %v", err)
	}
	if _, err := ts.Store.GetExpenseByID(ctx, created.ID); err == nil {
		t.Error("expense survived its trip being deleted")
	}
}

// The currency column is new on an existing table, so both directions matter:
// a created trip carries the code it was given, and an update that says
// nothing about currency must not blank it.
func TestTripCurrencyRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	userID, _ := seedExpenseTrip(t, ts)

	now := time.Now().UTC()
	trip, err := ts.Store.CreateTrip(ctx, db.CreateTripParams{
		ID:        uuid.NewString(),
		OwnerID:   userID,
		Title:     "Tokyo",
		Currency:  "JPY",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create trip: %v", err)
	}
	if trip.Currency != "JPY" {
		t.Fatalf("created currency: got %q, want JPY", trip.Currency)
	}

	fetched, err := ts.Store.GetTripByID(ctx, trip.ID)
	if err != nil {
		t.Fatalf("get trip: %v", err)
	}
	if fetched.Currency != "JPY" {
		t.Errorf("fetched currency: got %q, want JPY", fetched.Currency)
	}

	// ListTripsForUser builds its Trip inline rather than through
	// sqliteTripToDomain, so it is a second place the field can be dropped.
	listed, err := ts.Store.ListTripsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("list trips: %v", err)
	}
	var found bool
	for _, row := range listed {
		if row.ID == trip.ID {
			found = true
			if row.Currency != "JPY" {
				t.Errorf("listed currency: got %q, want JPY", row.Currency)
			}
		}
	}
	if !found {
		t.Error("the created trip did not appear in ListTripsForUser")
	}

	updated, err := ts.Store.UpdateTrip(ctx, db.UpdateTripParams{
		ID:        trip.ID,
		Title:     "Tokyo and Kyoto",
		Currency:  fetched.Currency,
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("update trip: %v", err)
	}
	if updated.Currency != "JPY" {
		t.Errorf("currency after update: got %q, want JPY", updated.Currency)
	}
}

func TestValidCurrency(t *testing.T) {
	for _, code := range []string{"EUR", "JPY", "ISK"} {
		if !db.ValidCurrency(code) {
			t.Errorf("ValidCurrency(%q) = false, want true", code)
		}
	}
	for _, code := range []string{"", "eur", "XYZ", "EURO"} {
		if db.ValidCurrency(code) {
			t.Errorf("ValidCurrency(%q) = true, want false", code)
		}
	}
}
