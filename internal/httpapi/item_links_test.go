package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// A link URL becomes an href in the client, so the set of schemes it may carry
// is a security boundary rather than a matter of taste. Until Stage 27 the
// only rule was non-empty, which made "javascript:alert(1)" a storable,
// clickable link -- and on a shared trip that is one editor planting script
// that runs with another member's session.

var badLinkURLs = []struct{ name, url string }{
	{"javascript", "javascript:alert(1)"},
	{"javascript in mixed case", "JavaScript:alert(1)"},
	{"javascript with leading space", "  javascript:alert(1)"},
	{"data", "data:text/html,<script>alert(1)</script>"},
	{"vbscript", "vbscript:msgbox(1)"},
	{"a scheme with no host", "https:"},
	{"a relative path", "/trips"},
	{"empty", "   "},
}

// The nested path, which serves create, PATCH and the batch endpoint alike.
func TestNestedLinkRejectsAnUnsafeScheme(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	for _, c := range badLinkURLs {
		t.Run(c.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"title":"X","category":"site","links":[{"url":%q}]}`, c.url)
			if w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, body); w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestNestedLinkAcceptsHTTPAndHTTPS(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	for _, u := range []string{"https://example.com/a", "http://example.com", "HTTPS://EXAMPLE.COM/x"} {
		body := fmt.Sprintf(`{"title":"X","category":"site","links":[{"url":%q}]}`, u)
		if w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, body); w.Code != http.StatusCreated {
			t.Errorf("%q: status = %d, want 201, body %s", u, w.Code, w.Body.String())
		}
	}
}

// PATCH writes the same column and so needs the same rule. Worth its own test:
// a guard applied only on create is a guard somebody edits their way around.
func TestPatchLinkRejectsAnUnsafeScheme(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	itemID := ts.createItem(cookie, tripID, "Kex Hostel")

	body := `{"title":"Kex Hostel","category":"site","links":[{"url":"javascript:alert(1)"}]}`
	if w := ts.do(http.MethodPatch, "/api/items/"+itemID, cookie, body); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
	}
}

// The batch endpoint inherits the rule through itemRequest.validate, and
// rejects the whole request rather than writing the other locations.
func TestBatchLinkRejectsAnUnsafeScheme(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	body := batchBody(siteJSON("Fine"), `{"title":"Second","category":"site","links":[{"url":"javascript:alert(1)"}]}`)
	if w := ts.postBatch(cookie, tripID, body); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
	}
	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 0 {
		t.Fatalf("the trip has %d locations, want none", len(listed))
	}
}

// The standalone endpoint writes to the same column from a different handler,
// which is exactly how a check applied in one place gets bypassed.
func TestStandaloneLinkEndpointRejectsAnUnsafeScheme(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	itemID := ts.createItem(cookie, tripID, "Kex Hostel")

	for _, c := range badLinkURLs {
		t.Run(c.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"url":%q}`, c.url)
			if w := ts.do(http.MethodPost, "/api/items/"+itemID+"/links", cookie, body); w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
			}
		})
	}

	if w := ts.do(http.MethodPost, "/api/items/"+itemID+"/links", cookie, `{"url":"https://example.com"}`); w.Code != http.StatusCreated {
		t.Errorf("a good link: status = %d, want 201, body %s", w.Code, w.Body.String())
	}
}
