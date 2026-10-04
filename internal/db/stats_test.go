package db_test

import (
	"context"
	"maps"
	"testing"
	"time"

	"caravel/internal/db"
	"caravel/internal/dbtest"
)

func TestInstanceCounts(t *testing.T) {
	driver, conn := dbtest.Open(t)
	store, err := db.NewStore(driver, conn)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Empty first: every total is zero and the category map is empty, not nil
	// or an error -- a fresh instance is scraped too.
	got, err := store.InstanceCounts(ctx)
	if err != nil {
		t.Fatalf("counts on empty db: %v", err)
	}
	if got.Users != 0 || got.Trips != 0 || got.Files != 0 || got.FileBytes != 0 || got.Expenses != 0 || len(got.LocationsByCategory) != 0 {
		t.Fatalf("empty db counts = %+v, want all zero", got)
	}

	for _, id := range []string{"u1", "u2"} {
		if _, err := store.CreateUser(ctx, db.CreateUserParams{ID: id, Username: id, DisplayName: id, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		if _, err := store.CreateTrip(ctx, db.CreateTripParams{ID: id, OwnerID: "u1", Title: id, Currency: "EUR", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create trip: %v", err)
		}
	}
	for i, cat := range []string{"stay", "stay", "site", "food"} {
		id := string(rune('a' + i))
		if _, err := store.CreateItem(ctx, db.CreateItemParams{ID: id, TripID: "t1", Category: cat, Title: id, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("create item: %v", err)
		}
	}
	for id, size := range map[string]int64{"f1": 1000, "f2": 2500} {
		if _, err := store.CreateFile(ctx, db.CreateFileParams{
			ID: id, TripID: "t2", Filename: id, StoragePath: id, Visibility: db.FileVisibilityTrip, SizeBytes: size, UploadedAt: now,
		}); err != nil {
			t.Fatalf("create file: %v", err)
		}
	}
	if _, err := store.CreateExpense(ctx, db.CreateExpenseParams{ID: "e1", TripID: "t1", Title: "Dinner", AmountMinor: 4200, SpentOn: "2026-10-04", CreatedAt: now}); err != nil {
		t.Fatalf("create expense: %v", err)
	}

	got, err = store.InstanceCounts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if got.Users != 2 || got.Trips != 3 || got.Files != 2 || got.FileBytes != 3500 || got.Expenses != 1 {
		t.Errorf("counts = %+v, want 2 users, 3 trips, 2 files of 3500 bytes, 1 expense", got)
	}
	want := map[string]int64{"stay": 2, "site": 1, "food": 1}
	if !maps.Equal(got.LocationsByCategory, want) {
		t.Errorf("LocationsByCategory = %v, want %v", got.LocationsByCategory, want)
	}
}
