package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"caravel/internal/assist"
)

// fakeAssistant stands in for a configured assistant. The handler still
// refuses before it would ever call Propose (the streaming endpoint is
// Milestone 6), so what matters here is only that it is non-nil -- but it
// implements the interface properly so Milestone 6 can grow canned responses
// here rather than replacing it.
type fakeAssistant struct{}

var errFakeAssistant = errors.New("fakeAssistant.Propose was called")

func (fakeAssistant) Propose(context.Context, assist.Request, func(assist.Event)) (*assist.Proposal, error) {
	return nil, errFakeAssistant
}

func TestAssistDisabledByDefault(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	// 501 and not 404: the route exists, the capability is off. A 404 would
	// be indistinguishable from a typo in the path.
	rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/assist/location", cookie, `{}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
}

// The capability check has to come before the trip lookup, or a disabled
// instance would leak whether a trip id exists to anyone who asks.
func TestAssistDisabledAnswersBeforeAuthorizing(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	rec := ts.do(http.MethodPost, "/api/trips/does-not-exist/assist/location", cookie, `{}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d (not 404)", rec.Code, http.StatusNotImplemented)
	}
}

func TestAssistRequiresAuth(t *testing.T) {
	ts := newTestServer(t)
	ts.Assist = fakeAssistant{}

	rec := ts.do(http.MethodPost, "/api/trips/whatever/assist/location", nil, `{}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// Enabled: the seam authorizes properly. Viewer is refused, a stranger cannot
// tell the trip exists, and an editor gets through to the not-yet-built body.
func TestAssistEnabledAuthorization(t *testing.T) {
	ts := newTestServer(t)
	ts.Assist = fakeAssistant{}

	owner := ts.login("owner")
	viewer := ts.login("viewer")
	stranger := ts.login("stranger")
	tripID := ts.createTrip(owner, "Iceland")

	ts.mustCreateNoID(http.MethodPost, "/api/trips/"+tripID+"/members", owner,
		`{"username":"viewer","role":"viewer"}`, http.StatusCreated)

	t.Run("a stranger gets 404, not 403", func(t *testing.T) {
		rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/assist/location", stranger, `{}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("a viewer gets 403", func(t *testing.T) {
		// A viewer can read the trip, so 404 would be a lie they could not
		// act on. They are refused because they could not save the result and
		// because sending the trip outward is not a read-only participant's
		// call.
		rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/assist/location", viewer, `{}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("the owner reaches the handler body", func(t *testing.T) {
		// 400 rather than 403 or 404: an empty body has no mode, so this is
		// the request being *validated*, which only happens past the
		// capability check and past authorization. That is what this asserts;
		// the successful path is in assist_stream_test.go, which needs a real
		// server to read a stream from.
		rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/assist/location", owner, `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(body["error"], "mode") {
			t.Errorf("error = %q, want the body validation to have run", body["error"])
		}
	})
}

func TestAuthMeReportsAssistCapability(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		ts := newTestServer(t)
		cookie := ts.login("alice")
		if got := ts.assistCapability(cookie); got {
			t.Error("/auth/me reported assist=true with no assistant configured")
		}
	})

	t.Run("on when configured", func(t *testing.T) {
		ts := newTestServer(t)
		ts.Assist = fakeAssistant{}
		cookie := ts.login("alice")
		if got := ts.assistCapability(cookie); !got {
			t.Error("/auth/me reported assist=false with an assistant configured")
		}
	})
}

func (ts *testServer) assistCapability(cookie *http.Cookie) bool {
	ts.t.Helper()
	rec := ts.do(http.MethodGet, "/api/auth/me", cookie, "")
	if rec.Code != http.StatusOK {
		ts.t.Fatalf("/auth/me status = %d", rec.Code)
	}
	var body struct {
		Assist bool `json:"assist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		ts.t.Fatalf("decode /auth/me: %v", err)
	}
	return body.Assist
}
