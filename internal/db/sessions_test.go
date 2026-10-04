package db_test

import (
	"context"
	"testing"
	"time"

	"caravel/internal/db"
	"caravel/internal/dbtest"
)

// CountActiveSessions feeds caravel_sessions_active, so what it must get right
// is the boundary: a session past its expiry is still a row until the hourly
// sweep deletes it, and must not be counted in the meantime.
func TestCountActiveSessions(t *testing.T) {
	driver, conn := dbtest.Open(t)
	store, err := db.NewStore(driver, conn)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := store.CreateUser(ctx, db.CreateUserParams{
		ID: "u1", Username: "u1", DisplayName: "U1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for id, expires := range map[string]time.Time{
		"live":    now.Add(time.Hour),
		"expired": now.Add(-time.Hour),
	} {
		if _, err := store.CreateSession(ctx, db.CreateSessionParams{
			ID: id, UserID: "u1", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: expires, LastSeenAt: now,
		}); err != nil {
			t.Fatalf("create session %s: %v", id, err)
		}
	}

	got, err := store.CountActiveSessions(ctx, now)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 1 {
		t.Errorf("CountActiveSessions = %d, want 1 (the expired row must not count)", got)
	}
}
