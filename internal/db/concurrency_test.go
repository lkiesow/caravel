package db_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"caravel/internal/db"
	"caravel/internal/dbtest"
)

// writers is how many transactions each test starts at once. Twelve parallel
// batch adds were enough to fail ten of them before the fix.
const writers = 16

func openStore(t *testing.T) db.Store {
	t.Helper()
	driver, conn := dbtest.Open(t)
	store, err := db.NewStore(driver, conn)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

// runConcurrently starts n copies of fn together and returns their errors.
func runConcurrently(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// Transactions that read before they write must not fail just because another
// writer committed first.
//
// They used to, on SQLite: WithTx began a deferred transaction, the read took
// a WAL snapshot, and when another writer had committed in the meantime the
// upgrade to a write lock could never succeed -- SQLITE_BUSY_SNAPSHOT (517),
// returned at once, because busy_timeout has nothing to wait for. Every
// handler that looks something up before writing was exposed to it; this is
// Auth.Register's shape, count then insert.
func TestConcurrentReadThenWriteTransactionsAllCommit(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	errs := runConcurrently(writers, func(i int) error {
		return store.WithTx(ctx, func(tx db.Store) error {
			if _, err := tx.CountUsers(ctx); err != nil {
				return fmt.Errorf("count: %w", err)
			}
			_, err := tx.CreateUser(ctx, db.CreateUserParams{
				ID:          fmt.Sprintf("user-%d", i),
				Username:    fmt.Sprintf("user%d", i),
				DisplayName: fmt.Sprintf("User %d", i),
				CreatedAt:   now,
				UpdatedAt:   now,
			})
			if err != nil {
				return fmt.Errorf("create: %w", err)
			}
			return nil
		})
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}

	count, err := store.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != writers {
		t.Errorf("users = %d, want %d", count, writers)
	}
}

// Two requests creating the same itinerary day at once must both get it.
//
// EnsureItineraryDay used to look the day up and insert it if missing, so the
// loser of the race hit the unique (trip_id, date) constraint and its request
// answered 500 -- on both dialects, since this one is not about SQLite locks.
// Each call runs in its own transaction, as the handlers call it, and holds it
// open a moment afterwards: without that the window between the lookup and the
// insert is too short to hit by chance. With it, the next caller looks the day
// up while the first insert is still uncommitted and so invisible to it.
func TestConcurrentEnsureItineraryDayReturnsOneDay(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	owner, err := store.CreateUser(ctx, db.CreateUserParams{
		ID: "owner", Username: "owner", DisplayName: "Owner", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	trip, err := store.CreateTrip(ctx, db.CreateTripParams{
		ID: "trip", OwnerID: owner.ID, Title: "Trip", Currency: "EUR", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create trip: %v", err)
	}

	ids := make([]string, writers)
	errs := runConcurrently(writers, func(i int) error {
		return store.WithTx(ctx, func(tx db.Store) error {
			day, err := tx.EnsureItineraryDay(ctx, fmt.Sprintf("day-%d", i), trip.ID, "2026-10-03")
			if err != nil {
				return err
			}
			ids[i] = day.ID
			time.Sleep(20 * time.Millisecond)
			return nil
		})
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}
	want := ""
	for i, id := range ids {
		if errs[i] != nil {
			continue
		}
		if want == "" {
			want = id
		} else if id != want {
			t.Errorf("caller %d got day %q, an earlier caller got %q", i, id, want)
		}
	}

	days, err := store.ListItineraryDaysByTrip(ctx, trip.ID)
	if err != nil {
		t.Fatalf("list days: %v", err)
	}
	if len(days) != 1 {
		t.Errorf("days = %d, want 1", len(days))
	}
}
