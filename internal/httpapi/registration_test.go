package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Registration closes itself after the first account, and that first account
// becomes the instance's administrator.
//
// The test harness opens signup explicitly (see newTestServerWithStore), so
// every test here closes it again first — otherwise it would be asserting the
// harness's convenience rather than the production default.
func closeSignup(t *testing.T, ts *testServer) {
	t.Helper()
	if err := ts.setOpenSignup(context.Background(), false); err != nil {
		t.Fatalf("close signup: %v", err)
	}
}

func register(ts *testServer, username string) *httptest.ResponseRecorder {
	return ts.do(http.MethodPost, "/api/auth/register", nil,
		`{"username":"`+username+`","password":"password123"}`)
}

// A fresh instance must be able to create its first account even though
// registration is closed by default — otherwise a new deployment is bricked
// behind its own default with no way in.
func TestFirstAccountIsAllowedAndBecomesAdmin(t *testing.T) {
	ts := newTestServer(t)
	closeSignup(t, ts)

	w := register(ts, "founder")
	if w.Code != http.StatusOK {
		t.Fatalf("first registration on a closed instance: got %d, want 200 — body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["is_admin"]; got != true {
		t.Errorf("first account is_admin=%v, want true", got)
	}

	// And the door shuts behind them.
	if w := register(ts, "second"); w.Code != http.StatusForbidden {
		t.Errorf("second registration on a closed instance: got %d, want 403 — body %s", w.Code, w.Body.String())
	}

	// A second account created while the door is open is *not* an admin: the
	// rule is "first account", not "any account created before the setting
	// changed".
	if err := ts.setOpenSignup(context.Background(), true); err != nil {
		t.Fatalf("open signup: %v", err)
	}
	w = register(ts, "third")
	if w.Code != http.StatusOK {
		t.Fatalf("registration with signup open: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["is_admin"]; got != false {
		t.Errorf("later account is_admin=%v, want false", got)
	}
}

// The setting is the only switch. There is no environment variable to disagree
// with it any more, which was the point of removing CARAVEL_OPEN_SIGNUP.
func TestOpenSignupSettingGatesRegistration(t *testing.T) {
	ts := newTestServer(t)
	ts.login("founder") // so the instance is no longer empty

	closeSignup(t, ts)
	if w := register(ts, "closed"); w.Code != http.StatusForbidden {
		t.Errorf("closed: got %d, want 403", w.Code)
	}
	if err := ts.setOpenSignup(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if w := register(ts, "opened"); w.Code != http.StatusOK {
		t.Errorf("opened: got %d, want 200 — body %s", w.Code, w.Body.String())
	}
	closeSignup(t, ts)
	if w := register(ts, "closed-again"); w.Code != http.StatusForbidden {
		t.Errorf("closed again: got %d, want 403", w.Code)
	}
}

// The login page reads this before anyone has a session, so it must answer
// unauthenticated — and it must agree with what /auth/register will actually
// do, or the page offers a form that 403s.
func TestAuthConfigReportsRegistrationState(t *testing.T) {
	ts := newTestServer(t)
	closeSignup(t, ts)

	config := func() bool {
		t.Helper()
		w := ts.do(http.MethodGet, "/api/auth/config", nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET /auth/config: got %d, want 200 — body %s", w.Code, w.Body.String())
		}
		return decode[map[string]any](t, w)["open_signup"] == true
	}

	// Empty instance: closed setting, but the first account is still possible,
	// so the page must offer registration. This is the case a naive
	// implementation gets wrong by reporting the setting rather than the
	// outcome.
	if !config() {
		t.Error("on an empty closed instance /auth/config says closed, but the first registration would succeed")
	}
	if w := register(ts, "founder"); w.Code != http.StatusOK {
		t.Fatalf("first registration: got %d", w.Code)
	}
	if config() {
		t.Error("after the first account, /auth/config still says open")
	}
	if err := ts.setOpenSignup(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !config() {
		t.Error("with the setting on, /auth/config says closed")
	}
}

// is_admin rides along on /auth/me the way `geocoding` does, so the client can
// decide whether to show the admin entry without a second request.
func TestAuthMeCarriesIsAdmin(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.login("founder") // first account
	plain := ts.login("regular")

	for _, tc := range []struct {
		name   string
		cookie *http.Cookie
		want   bool
	}{
		{"first account", admin, true},
		{"later account", plain, false},
	} {
		w := ts.do(http.MethodGet, "/api/auth/me", tc.cookie, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: GET /auth/me got %d", tc.name, w.Code)
		}
		if got := decode[map[string]any](t, w)["is_admin"]; got != tc.want {
			t.Errorf("%s: is_admin=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// An admin gets no access to other people's trips. is_admin governs accounts,
// not data — if this ever changes, "personal" stops meaning anything.
func TestAdminHasNoAccessToOtherUsersTrips(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.login("founder") // first account, therefore admin
	owner := ts.login("owner")

	tripID := ts.createTrip(owner, "Not the admin's trip")
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/trips/" + tripID},
		{http.MethodPatch, "/api/trips/" + tripID},
		{http.MethodDelete, "/api/trips/" + tripID},
		{http.MethodGet, "/api/trips/" + tripID + "/files"},
	} {
		w := ts.do(tc.method, tc.path, admin, `{"title":"x"}`)
		if w.Code != http.StatusNotFound {
			t.Errorf("admin %s %s: got %d, want 404 — an admin is not a superuser",
				tc.method, tc.path, w.Code)
		}
	}
	if trips := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips", admin, "")); len(trips) != 0 {
		t.Errorf("admin's trip list has %d entries, want 0", len(trips))
	}
}

// openSignupEnabled fails closed for anything it cannot read as true, and this
// pins it — a gap the first version of this file left open, found by breaking
// the function and watching every test still pass. The seeded row means the
// happy path never exercises the error branches, so they need reaching
// deliberately.
//
// All three states below are reachable: a value written by a future version or
// a hand-edited database, an empty string, and a missing row (a setting added
// without a default, or this one deleted). None may be read as "open".
func TestOpenSignupFailsClosed(t *testing.T) {
	for _, value := range []string{"yes", "", "TRUE-ish", "0.5", "maybe"} {
		t.Run("value="+value, func(t *testing.T) {
			ts := newTestServer(t)
			ts.login("founder") // not an empty instance, so no bootstrap
			if err := ts.Store.SetAppSetting(context.Background(), settingOpenSignup, value); err != nil {
				t.Fatal(err)
			}
			if w := register(ts, "sneaky"); w.Code != http.StatusForbidden {
				t.Errorf("with open_signup=%q: got %d, want 403", value, w.Code)
			}
			w := ts.do(http.MethodGet, "/api/auth/config", nil, "")
			if decode[map[string]any](t, w)["open_signup"] == true {
				t.Errorf("with open_signup=%q, /auth/config reports open", value)
			}
		})
	}

	// "true" and "1" must still work, or failing closed would mean never
	// opening — a guard that refuses everything is not a guard.
	for _, value := range []string{"true", "1", "TRUE"} {
		t.Run("accepts="+value, func(t *testing.T) {
			ts := newTestServer(t)
			ts.login("founder")
			if err := ts.Store.SetAppSetting(context.Background(), settingOpenSignup, value); err != nil {
				t.Fatal(err)
			}
			if w := register(ts, "welcome"); w.Code != http.StatusOK {
				t.Errorf("with open_signup=%q: got %d, want 200 — body %s", value, w.Code, w.Body.String())
			}
		})
	}
}
