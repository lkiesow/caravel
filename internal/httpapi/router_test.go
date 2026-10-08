package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// An unknown API path must answer as the API, not as the app. The root
// router's NotFound is the static tree with its SPA fallback, and chi used to
// hand it down into /api too, so these all came back 200 with index.html.
// newStaticServer has a real shell in its tree, so a regression would show up
// as that shell rather than as some other 404.
func TestUnknownAPIPathIsJSONNotFound(t *testing.T) {
	ts := newStaticServer(t, false)
	cookie := ts.login("alice")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api"},
		{http.MethodGet, "/api/"},
		{http.MethodGet, "/api/does-not-exist"},
		{http.MethodGet, "/api/auth/nonsense"},
		{http.MethodGet, "/api/trips/abc/nonsense"},
		{http.MethodPost, "/api/locations/abc/nonsense"},
	} {
		w := ts.do(tc.method, tc.path, cookie, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 — body %s", tc.method, tc.path, w.Code, w.Body.String())
			continue
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s %s: Content-Type = %q, want application/json", tc.method, tc.path, ct)
		}
		var got map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["error"] == "" {
			t.Errorf("%s %s: body %q is not a JSON error", tc.method, tc.path, w.Body.String())
		}
	}
}

// A known path with the wrong method keeps chi's own 405, with its Allow
// header -- and in particular is not swallowed by the NotFound above.
func TestWrongMethodOnAPIPathIs405(t *testing.T) {
	ts := newStaticServer(t, false)

	w := ts.do(http.MethodPost, "/api/health", nil, "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/health = %d, want 405 — body %s", w.Code, w.Body.String())
	}
	if allow := w.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("POST /api/health: Allow = %q, want it to name GET", allow)
	}
}
