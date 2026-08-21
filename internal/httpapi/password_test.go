package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"caravel/internal/auth"
	"caravel/internal/db"

	"github.com/google/uuid"
)

// Password change (Stage 12 Milestone 5), which is the first write this package
// does against auth_identities.
//
// The behaviour worth pinning is not "the endpoint returns 200": it is that the
// old password stops working, that every *other* session stops working, and that
// the caller's own browser does not get logged out by its own request.

const testPassword = "password123" // what testServer.login registers with

func TestChangePasswordRejectsWrongCurrentPassword(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	w := ts.do(http.MethodPost, "/api/auth/password", cookie,
		`{"current_password":"not-my-password","new_password":"brand-new-secret"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password: got %d, want 401, body %s", w.Code, w.Body.String())
	}

	// And nothing changed: the original password still authenticates.
	if _, err := ts.Auth.Authenticate(context.Background(), "alice", testPassword); err != nil {
		t.Fatalf("original password should still work: %v", err)
	}
}

func TestChangePasswordRejectsShortNewPassword(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	w := ts.do(http.MethodPost, "/api/auth/password", cookie,
		`{"current_password":"`+testPassword+`","new_password":"short"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short new password: got %d, want 400, body %s", w.Code, w.Body.String())
	}
	if _, err := ts.Auth.Authenticate(context.Background(), "alice", testPassword); err != nil {
		t.Fatalf("original password should still work: %v", err)
	}
}

func TestChangePasswordRequiresAuth(t *testing.T) {
	ts := newTestServer(t)

	w := ts.do(http.MethodPost, "/api/auth/password", nil,
		`{"current_password":"`+testPassword+`","new_password":"brand-new-secret"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d, want 401, body %s", w.Code, w.Body.String())
	}
}

func TestChangePasswordSwapsThePasswordAndTheSessions(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	// A second device for the same account, so "logs you out everywhere else"
	// is a real assertion and not an assumption about one row.
	alice, err := ts.Store.GetUserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("look up alice: %v", err)
	}
	other := ts.session(alice.ID)
	if w := ts.do(http.MethodGet, "/api/auth/me", other, ""); w.Code != http.StatusOK {
		t.Fatalf("second session should start out valid: got %d", w.Code)
	}

	w := ts.do(http.MethodPost, "/api/auth/password", cookie,
		`{"current_password":"`+testPassword+`","new_password":"brand-new-secret"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("change password: got %d, want 200, body %s", w.Code, w.Body.String())
	}

	// The old password is gone and the new one works.
	if _, err := ts.Auth.Authenticate(context.Background(), "alice", testPassword); err == nil {
		t.Fatal("the old password still authenticates")
	}
	if _, err := ts.Auth.Authenticate(context.Background(), "alice", "brand-new-secret"); err != nil {
		t.Fatalf("the new password does not authenticate: %v", err)
	}

	// The other device is logged out...
	if w := ts.do(http.MethodGet, "/api/auth/me", other, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("other session should be invalidated: got %d, want 401", w.Code)
	}
	// ...the cookie that made the request is too, since it was deleted with the
	// rest...
	if w := ts.do(http.MethodGet, "/api/auth/me", cookie, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("the old cookie should be invalidated as well: got %d, want 401", w.Code)
	}
	// ...but the response handed back a fresh one, so the browser that did the
	// change is still logged in. Without this the endpoint would log you out of
	// the very screen you used.
	fresh := sessionCookie(t, w)
	if v := ts.do(http.MethodGet, "/api/auth/me", fresh, ""); v.Code != http.StatusOK {
		t.Fatalf("re-issued session should be valid: got %d, body %s", v.Code, v.Body.String())
	}
}

func TestMeReportsHasPassword(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	w := ts.do(http.MethodGet, "/api/auth/me", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me: got %d, body %s", w.Code, w.Body.String())
	}
	if me := decode[userResponse](t, w); !me.HasPassword {
		t.Fatal("a local account should report has_password: true")
	}
}

// An account with no local identity at all - what an externally-authenticated
// user will look like once OIDC exists - reports has_password: false, which is
// what hides the settings card, and cannot change a password it does not have.
func TestAccountWithoutLocalIdentity(t *testing.T) {
	ts := newTestServer(t)

	now := time.Now().UTC()
	user, err := ts.Store.CreateUser(context.Background(), db.CreateUserParams{
		ID:          uuid.NewString(),
		Username:    "external",
		DisplayName: "External",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := ts.Auth.StartSession(context.Background(), user.ID, "test", "127.0.0.1")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	w := ts.do(http.MethodGet, "/api/auth/me", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me: got %d, body %s", w.Code, w.Body.String())
	}
	if me := decode[userResponse](t, w); me.HasPassword {
		t.Fatal("an account with no local identity should report has_password: false")
	}

	w = ts.do(http.MethodPost, "/api/auth/password", cookie,
		`{"current_password":"whatever","new_password":"brand-new-secret"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no local password: got %d, want 400, body %s", w.Code, w.Body.String())
	}
}

// A second session for an existing user, so "logs you out everywhere else" can
// be asserted rather than assumed.
func (ts *testServer) session(userID string) *http.Cookie {
	ts.t.Helper()
	token, _, err := ts.Auth.StartSession(context.Background(), userID, "test", "127.0.0.1")
	if err != nil {
		ts.t.Fatalf("start extra session: %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: token}
}

// The session cookie a response set, so a re-issued session can be used.
func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: w.Header()}).Cookies() {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("response set no %s cookie: %v", auth.SessionCookieName, w.Header())
	return nil
}
