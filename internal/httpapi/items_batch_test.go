package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func batchBody(items ...string) string {
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

func siteJSON(title string) string {
	return `{"title":"` + title + `","category":"site"}`
}

func (ts *testServer) postBatch(cookie *http.Cookie, tripID, body string) *httptest.ResponseRecorder {
	ts.t.Helper()
	return ts.do(http.MethodPost, "/api/trips/"+tripID+"/items/batch", cookie, body)
}

func TestCreateItemsBatchCreatesEveryLocation(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	w := ts.postBatch(cookie, tripID, batchBody(
		`{"title":"Hallgrimskirkja","category":"site","tags":["church"],"notes":"A church.","location":{"lat":64.1417,"lng":-21.9266,"address":"Hallgrimstorg 1"},"links":[{"url":"https://example.com/x","label":"Site"}]}`,
		siteJSON("Braud and Co"),
		`{"title":"Kex Hostel","category":"stay"}`,
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", w.Code, w.Body.String())
	}

	out := decode[[]map[string]any](t, w)
	if len(out) != 3 {
		t.Fatalf("created = %d, want 3", len(out))
	}
	// In request order, with the generated ids and the nested rows, so the
	// client needs no second GET -- the same contract as the single create.
	if out[0]["title"] != "Hallgrimskirkja" || out[1]["title"] != "Braud and Co" || out[2]["title"] != "Kex Hostel" {
		t.Errorf("titles = %v %v %v, want request order", out[0]["title"], out[1]["title"], out[2]["title"])
	}
	for i, item := range out {
		if id, _ := item["id"].(string); id == "" {
			t.Errorf("item %d has no id", i)
		}
	}
	if out[0]["location"] == nil {
		t.Error("the nested location was not written")
	}
	if links, _ := out[0]["links"].([]any); len(links) != 1 {
		t.Errorf("links = %v, want the one that was sent", out[0]["links"])
	}
	if tags, _ := out[0]["tags"].([]any); len(tags) != 1 {
		t.Errorf("tags = %v, want the one that was sent", out[0]["tags"])
	}

	// And they are actually on the trip.
	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 3 {
		t.Fatalf("the trip has %d locations, want 3", len(listed))
	}
}

// The list is read back in the order it was sent, which is the reason the
// batch assigns sort_order explicitly: within one transaction every row lands
// in the same millisecond, and created_at is stored in a layout that is not
// lexically sortable inside a second.
func TestCreateItemsBatchKeepsRequestOrderInTheList(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	var items []string
	for i := range 8 {
		items = append(items, siteJSON(fmt.Sprintf("Place %d", i)))
	}
	if w := ts.postBatch(cookie, tripID, batchBody(items...)); w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", w.Code, w.Body.String())
	}

	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 8 {
		t.Fatalf("listed = %d, want 8", len(listed))
	}
	for i, item := range listed {
		want := fmt.Sprintf("Place %d", i)
		if item["title"] != want {
			t.Fatalf("position %d = %v, want %q", i, item["title"], want)
		}
	}
}

// A batch appends: what was already on the trip stays in front of it.
func TestCreateItemsBatchAppendsAfterWhatIsThere(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	ts.createItem(cookie, tripID, "Already here")

	if w := ts.postBatch(cookie, tripID, batchBody(siteJSON("Added"))); w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 2 || listed[0]["title"] != "Already here" || listed[1]["title"] != "Added" {
		t.Fatalf("list = %v, want the existing location first", listed)
	}
}

// One invalid element rejects the whole request, and nothing is written. This
// is the property the transaction exists for: three locations created and a
// 400 about the fourth is a state no screen can explain.
func TestCreateItemsBatchWritesNothingWhenOneElementIsInvalid(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	cases := []struct{ name, body string }{
		{"no title", batchBody(siteJSON("Fine"), `{"title":"  ","category":"site"}`)},
		{"bad category", batchBody(siteJSON("Fine"), `{"title":"Second","category":"restaurant"}`)},
		{"nested link with no url", batchBody(siteJSON("Fine"), `{"title":"Second","category":"site","links":[{"url":"  "}]}`)},
		{"bad nested date range", batchBody(siteJSON("Fine"), `{"title":"Second","category":"site","dates":[{"start_date":"2026-09-07","end_date":"2026-09-05"}]}`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := ts.postBatch(cookie, tripID, c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
			}
			listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
			if len(listed) != 0 {
				t.Fatalf("the trip has %d locations, want none: the valid element was written anyway", len(listed))
			}
		})
	}
}

func TestCreateItemsBatchRejectsAnEmptyOrOversizedList(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	if w := ts.postBatch(cookie, tripID, `{"items":[]}`); w.Code != http.StatusBadRequest {
		t.Errorf("empty list: status = %d, want 400", w.Code)
	}

	var many []string
	for i := range maxItemsPerBatch + 1 {
		many = append(many, siteJSON(fmt.Sprintf("Place %d", i)))
	}
	if w := ts.postBatch(cookie, tripID, batchBody(many...)); w.Code != http.StatusBadRequest {
		t.Errorf("oversized list: status = %d, want 400", w.Code)
	}
	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 0 {
		t.Errorf("the trip has %d locations, want none", len(listed))
	}
}

// readJSON's unknown-field strictness is part of every write endpoint's
// contract, and it has to survive being one level deeper in the body.
func TestCreateItemsBatchRefusesAnUnknownField(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	if w := ts.postBatch(cookie, tripID, batchBody(`{"title":"X","category":"site","colour":"red"}`)); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// The same authorization as the single create: a viewer cannot write, and a
// stranger cannot learn that the trip exists.
func TestCreateItemsBatchAuthorization(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	viewer := ts.login("viewer")
	stranger := ts.login("stranger")
	tripID := ts.createTrip(owner, "Iceland")
	ts.mustCreateNoID(http.MethodPost, "/api/trips/"+tripID+"/members", owner,
		`{"username":"viewer","role":"viewer"}`, http.StatusCreated)

	if w := ts.postBatch(stranger, tripID, batchBody(siteJSON("X"))); w.Code != http.StatusNotFound {
		t.Errorf("stranger: status = %d, want 404", w.Code)
	}
	if w := ts.postBatch(viewer, tripID, batchBody(siteJSON("X"))); w.Code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", w.Code)
	}
}
