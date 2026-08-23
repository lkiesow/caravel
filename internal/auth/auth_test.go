package auth_test

import (
	"context"
	"testing"

	"caravel/internal/auth"
	"caravel/internal/db"
	"caravel/internal/dbtest"
)

// SetPassword is the seeder's primitive (cmd/seed's ensureUser), so it gets a
// test of its own: it is the one password path with no current-password check,
// and the reason it exists is that re-seeding has to make the documented dev
// credentials true again even after someone changed them.
func TestSetPasswordResetsWithoutTheCurrentOne(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "demo", "first-password", "Demo"); err != nil {
		t.Fatalf("register: %v", err)
	}
	user, err := store.GetUserByUsername(ctx, "demo")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	// A live session, which must survive: re-seeding should not log the
	// developer out of the browser they are testing in.
	token, _, err := svc.StartSession(ctx, user.ID, "test", "127.0.0.1")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	if err := svc.SetPassword(ctx, "demo", "second-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	if _, err := svc.Authenticate(ctx, "demo", "second-password"); err != nil {
		t.Fatalf("new password should authenticate: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "demo", "first-password"); err == nil {
		t.Fatal("the old password still authenticates")
	}
	if _, err := svc.ValidateSession(ctx, token); err != nil {
		t.Fatalf("session should survive a seeder reset: %v", err)
	}
}

// ChangePassword, by contrast, invalidates every session - covered end to end in
// internal/httpapi/password_test.go; asserted here at the service level so the
// difference between the two entry points is pinned in one place.
func TestChangePasswordEndsEverySession(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "demo", "first-password", "Demo"); err != nil {
		t.Fatalf("register: %v", err)
	}
	user, err := store.GetUserByUsername(ctx, "demo")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	token, _, err := svc.StartSession(ctx, user.ID, "test", "127.0.0.1")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	if err := svc.ChangePassword(ctx, user, "first-password", "second-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := svc.ValidateSession(ctx, token); err == nil {
		t.Fatal("the session should have been invalidated")
	}
}

func newService(t *testing.T) (*auth.Service, db.Store) {
	t.Helper()

	driver, conn := dbtest.Open(t)

	store, err := db.NewStore(driver, conn)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return auth.NewService(store), store
}
