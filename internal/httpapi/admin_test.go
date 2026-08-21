package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// adminFixture: `boss` is the instance's first account and therefore its admin;
// `plain` is an ordinary user.
type adminFixture struct {
	ts    *testServer
	boss  *http.Cookie
	plain *http.Cookie
}

func setupAdmin(t *testing.T) *adminFixture {
	t.Helper()
	ts := newTestServer(t)
	return &adminFixture{ts: ts, boss: ts.login("boss"), plain: ts.login("plain")}
}

func (f *adminFixture) users(t *testing.T) map[string]map[string]any {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/admin/users", f.boss, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/users: got %d, body %s", w.Code, w.Body.String())
	}
	out := map[string]map[string]any{}
	for _, u := range decode[[]map[string]any](t, w) {
		out[u["username"].(string)] = u
	}
	return out
}

// Every admin route is closed to a non-admin and to an anonymous caller, and
// the two get different answers — 403 means "you are known and not allowed",
// 401 means "log in first", and a client acts on them differently.
func TestAdminRoutesRequireAdmin(t *testing.T) {
	f := setupAdmin(t)
	id := f.users(t)["plain"]["id"].(string)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/admin/users", ""},
		{http.MethodPost, "/api/admin/users", `{"username":"x","password":"password123"}`},
		{http.MethodPatch, "/api/admin/users/" + id, `{"display_name":"x"}`},
		{http.MethodPost, "/api/admin/users/" + id + "/password", `{"password":"password123"}`},
		{http.MethodDelete, "/api/admin/users/" + id, ""},
		{http.MethodPut, "/api/admin/settings/open-signup", `{"open_signup":true}`},
	} {
		if w := f.ts.do(tc.method, tc.path, f.plain, tc.body); w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a non-admin: got %d, want 403 — body %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if w := f.ts.do(tc.method, tc.path, nil, tc.body); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymously: got %d, want 401", tc.method, tc.path, w.Code)
		}
	}

	// And nothing happened while all that was being refused.
	if got := len(f.users(t)); got != 2 {
		t.Errorf("instance has %d users after the refusals, want 2", got)
	}
}

// The trip count is what the delete confirmation quotes, so it has to mean
// "trips this account owns" — not trips it can reach.
func TestAdminUserListTripCounts(t *testing.T) {
	f := setupAdmin(t)
	f.ts.createTrip(f.plain, "Plain's first")
	f.ts.createTrip(f.plain, "Plain's second")
	shared := f.ts.createTrip(f.boss, "Boss's trip")

	// Share the boss's trip with `plain`: it must not inflate their count.
	plainID := f.users(t)["plain"]["id"].(string)
	f.ts.mustCreateNoID(http.MethodPost, "/api/trips/"+shared+"/members", f.boss,
		`{"username":"plain","role":"editor"}`, http.StatusCreated)

	users := f.users(t)
	if got := users["plain"]["trip_count"]; got != float64(2) {
		t.Errorf("plain owns 2 trips but the list says %v — a shared trip is not an owned one", got)
	}
	if got := users["boss"]["trip_count"]; got != float64(1) {
		t.Errorf("boss owns 1 trip but the list says %v", got)
	}
	if users["boss"]["is_self"] != true || users["plain"]["is_self"] != false {
		t.Errorf("is_self is wrong: boss=%v plain=%v", users["boss"]["is_self"], users["plain"]["is_self"])
	}
	_ = plainID
}

func TestAdminCreateUser(t *testing.T) {
	f := setupAdmin(t)

	w := f.ts.do(http.MethodPost, "/api/admin/users", f.boss,
		`{"username":"newbie","display_name":"New Bie","password":"password123"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: got %d, body %s", w.Code, w.Body.String())
	}
	created := decode[map[string]any](t, w)
	if created["username"] != "newbie" || created["display_name"] != "New Bie" || created["is_admin"] != false {
		t.Errorf("created user is %v", created)
	}

	// The account must actually work — a created user who cannot log in is a
	// row, not an account.
	if w := f.ts.do(http.MethodPost, "/api/auth/login", nil,
		`{"username":"newbie","password":"password123"}`); w.Code != http.StatusOK {
		t.Errorf("newbie cannot log in: got %d, body %s", w.Code, w.Body.String())
	}

	// An admin can be created directly.
	w = f.ts.do(http.MethodPost, "/api/admin/users", f.boss,
		`{"username":"deputy","password":"password123","is_admin":true}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create admin: got %d, body %s", w.Code, w.Body.String())
	}
	if decode[map[string]any](t, w)["is_admin"] != true {
		t.Error("is_admin:true was requested and not honoured")
	}

	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"duplicate username", `{"username":"newbie","password":"password123"}`, http.StatusConflict},
		{"blank username", `{"username":"  ","password":"password123"}`, http.StatusBadRequest},
		{"short password", `{"username":"shorty","password":"short"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.ts.do(http.MethodPost, "/api/admin/users", f.boss, tc.body)
			if w.Code != tc.want {
				t.Errorf("got %d, want %d — body %s", w.Code, tc.want, w.Body.String())
			}
		})
	}

	// Creating an account while registration is closed must still work: that is
	// the whole point of the screen.
	if err := f.ts.setOpenSignup(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if w := f.ts.do(http.MethodPost, "/api/admin/users", f.boss,
		`{"username":"invited","password":"password123"}`); w.Code != http.StatusCreated {
		t.Errorf("admin creating a user with signup closed: got %d, body %s", w.Code, w.Body.String())
	}
}

func TestAdminResetPassword(t *testing.T) {
	f := setupAdmin(t)
	plainID := f.users(t)["plain"]["id"].(string)

	if w := f.ts.do(http.MethodPost, "/api/admin/users/"+plainID+"/password", f.boss,
		`{"password":"brand-new-password"}`); w.Code != http.StatusNoContent {
		t.Fatalf("reset password: got %d, body %s", w.Code, w.Body.String())
	}
	// The new password works and the old one does not — either half alone would
	// pass on a no-op.
	if w := f.ts.do(http.MethodPost, "/api/auth/login", nil,
		`{"username":"plain","password":"brand-new-password"}`); w.Code != http.StatusOK {
		t.Errorf("login with the new password: got %d", w.Code)
	}
	if w := f.ts.do(http.MethodPost, "/api/auth/login", nil,
		`{"username":"plain","password":"password123"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("login with the old password: got %d, want 401 — the reset did nothing", w.Code)
	}
	// Unlike a self-service change, an admin reset leaves sessions alone: the
	// point is to restore access, not to evict someone.
	if w := f.ts.do(http.MethodGet, "/api/auth/me", f.plain, ""); w.Code != http.StatusOK {
		t.Errorf("plain's existing session died on an admin reset: got %d", w.Code)
	}

	if w := f.ts.do(http.MethodPost, "/api/admin/users/"+plainID+"/password", f.boss,
		`{"password":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("short password: got %d, want 400", w.Code)
	}
	if w := f.ts.do(http.MethodPost, "/api/admin/users/00000000-0000-0000-0000-000000000000/password",
		f.boss, `{"password":"password123"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown user: got %d, want 404", w.Code)
	}
}

func TestAdminDeleteUserCascadesTrips(t *testing.T) {
	f := setupAdmin(t)
	tripID := f.ts.createTrip(f.plain, "Plain's trip")
	plainID := f.users(t)["plain"]["id"].(string)

	if w := f.ts.do(http.MethodDelete, "/api/admin/users/"+plainID, f.boss, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete user: got %d, body %s", w.Code, w.Body.String())
	}
	if got := len(f.users(t)); got != 1 {
		t.Errorf("instance has %d users after the delete, want 1", got)
	}
	// Their session is gone with them, and so is their trip.
	if w := f.ts.do(http.MethodGet, "/api/auth/me", f.plain, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("deleted user's session still works: got %d", w.Code)
	}
	if w := f.ts.do(http.MethodGet, "/api/trips/"+tripID, f.boss, ""); w.Code != http.StatusNotFound {
		t.Errorf("deleted user's trip survives: got %d, want 404", w.Code)
	}
	if w := f.ts.do(http.MethodPost, "/api/auth/login", nil,
		`{"username":"plain","password":"password123"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("deleted user can still log in: got %d", w.Code)
	}
}

// The guard rails, which all protect one thing: an instance must never end up
// with nobody able to administer it, because there is no recovery path from
// that short of editing the database.
func TestLastAdminGuardRails(t *testing.T) {
	f := setupAdmin(t)
	bossID := f.users(t)["boss"]["id"].(string)

	// Sole admin cannot demote themselves...
	w := f.ts.do(http.MethodPatch, "/api/admin/users/"+bossID, f.boss, `{"is_admin":false}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("sole admin self-demotion: got %d, want 409 — body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["code"]; got != "last_admin" {
		t.Errorf("code is %v, want last_admin", got)
	}
	// ...nor delete themselves.
	w = f.ts.do(http.MethodDelete, "/api/admin/users/"+bossID, f.boss, "")
	if w.Code != http.StatusConflict {
		t.Errorf("sole admin self-deletion: got %d, want 409", w.Code)
	}
	// Still an admin, still exists, still able to administer.
	if f.users(t)["boss"]["is_admin"] != true {
		t.Fatal("boss lost their admin flag to a refused request")
	}

	// With a second admin, both become possible — a guard that never lets go is
	// a lock, not a guard.
	plainID := f.users(t)["plain"]["id"].(string)
	if w := f.ts.do(http.MethodPatch, "/api/admin/users/"+plainID, f.boss, `{"is_admin":true}`); w.Code != http.StatusOK {
		t.Fatalf("promote second admin: got %d, body %s", w.Code, w.Body.String())
	}
	if w := f.ts.do(http.MethodPatch, "/api/admin/users/"+bossID, f.boss, `{"is_admin":false}`); w.Code != http.StatusOK {
		t.Errorf("self-demotion with another admin present: got %d, body %s", w.Code, w.Body.String())
	}
	// And now the demoted ex-admin is locked out of the screen.
	if w := f.ts.do(http.MethodGet, "/api/admin/users", f.boss, ""); w.Code != http.StatusForbidden {
		t.Errorf("demoted admin still reaches /admin/users: got %d, want 403", w.Code)
	}

	// Demoting *someone else* down to zero admins is the same hole and is also
	// refused: `plain` is now the only admin.
	plainCookie := f.plain
	w = f.ts.do(http.MethodPatch, "/api/admin/users/"+plainID, plainCookie, `{"is_admin":false}`)
	if w.Code != http.StatusConflict {
		t.Errorf("last admin demoting themselves: got %d, want 409", w.Code)
	}
}

// A display-name change must not silently clear the admin flag just because the
// request did not mention it — which is why is_admin is a *bool on the wire.
func TestAdminUpdateLeavesUnmentionedFieldsAlone(t *testing.T) {
	f := setupAdmin(t)
	plainID := f.users(t)["plain"]["id"].(string)

	if w := f.ts.do(http.MethodPatch, "/api/admin/users/"+plainID, f.boss, `{"is_admin":true}`); w.Code != http.StatusOK {
		t.Fatalf("promote: got %d", w.Code)
	}
	// Rename without mentioning is_admin.
	w := f.ts.do(http.MethodPatch, "/api/admin/users/"+plainID, f.boss, `{"display_name":"Renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: got %d, body %s", w.Code, w.Body.String())
	}
	got := decode[map[string]any](t, w)
	if got["display_name"] != "Renamed" {
		t.Errorf("display_name is %v", got["display_name"])
	}
	if got["is_admin"] != true {
		t.Error("a rename cleared the admin flag — is_admin must be omitted-means-unchanged")
	}
	// And an omitted display_name leaves the name alone.
	w = f.ts.do(http.MethodPatch, "/api/admin/users/"+plainID, f.boss, `{"is_admin":false}`)
	if decode[map[string]any](t, w)["display_name"] != "Renamed" {
		t.Error("a flag change cleared the display name")
	}
}

func TestAdminOpenSignupToggle(t *testing.T) {
	f := setupAdmin(t)

	for _, want := range []bool{false, true, false} {
		body := `{"open_signup":false}`
		if want {
			body = `{"open_signup":true}`
		}
		if w := f.ts.do(http.MethodPut, "/api/admin/settings/open-signup", f.boss, body); w.Code != http.StatusOK {
			t.Fatalf("set open_signup=%v: got %d, body %s", want, w.Code, w.Body.String())
		}
		// The unauthenticated endpoint the login page reads must agree.
		w := f.ts.do(http.MethodGet, "/api/auth/config", nil, "")
		if got := decode[map[string]any](t, w)["open_signup"]; got != want {
			t.Errorf("after setting %v, /auth/config reports %v", want, got)
		}
	}
}
