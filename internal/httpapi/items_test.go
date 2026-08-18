package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"caravel/internal/db"
)

// Coverage for the nested create/update contract added in Stage 09 Milestone 1:
// one request commits an item plus its location, links and dates, in a single
// transaction. The three standalone sub-resource endpoints are unchanged and
// keep their own coverage via ownership_test.go.

// nestedItem is the part of itemDetailResponse these tests assert on, decoded
// the way a client sees it (JSON) rather than by reaching into the handler's
// own structs.
type nestedItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location *struct {
		Lat     *float64 `json:"lat"`
		Lng     *float64 `json:"lng"`
		Address *string  `json:"address"`
	} `json:"location"`
	Links []struct {
		ID        string  `json:"id"`
		URL       string  `json:"url"`
		Label     *string `json:"label"`
		SortOrder int     `json:"sort_order"`
	} `json:"links"`
	Dates []struct {
		ID        string  `json:"id"`
		StartDate *string `json:"start_date"`
		EndDate   *string `json:"end_date"`
		Label     *string `json:"label"`
	} `json:"dates"`
}

const nestedCreateBody = `{
	"title": "Foss Hotel",
	"category": "stay",
	"type": "hotel",
	"location": {"lat": 64.146, "lng": -21.94, "address": "Reykjavik"},
	"links": [
		{"url": "https://example.com/booking", "label": "Booking"},
		{"url": "https://example.com/map"}
	],
	"dates": [{"start_date": "2026-08-19", "end_date": "2026-08-21", "label": "Stay"}]
}`

// createNested posts nestedCreateBody and returns the decoded response.
func createNested(ts *testServer, cookie *http.Cookie, tripID string) nestedItem {
	ts.t.Helper()
	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, nestedCreateBody)
	if w.Code != http.StatusCreated {
		ts.t.Fatalf("create nested item: got %d, want 201, body %s", w.Code, w.Body.String())
	}
	return decode[nestedItem](ts.t, w)
}

func TestCreateItemWithNestedSubResources(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	created := createNested(ts, cookie, tripID)

	// The create response carries everything back, so the client needs no
	// follow-up GET — that's why the handler returns the detail shape.
	assertNested(t, "create response", created)

	// ...and it is actually persisted, not just echoed.
	w := ts.do(http.MethodGet, "/api/items/"+created.ID, cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get item: got %d, want 200", w.Code)
	}
	assertNested(t, "subsequent GET", decode[nestedItem](t, w))
}

func assertNested(t *testing.T, where string, got nestedItem) {
	t.Helper()

	if got.Location == nil || got.Location.Lat == nil || *got.Location.Lat != 64.146 {
		t.Errorf("%s: location not saved: %+v", where, got.Location)
	}
	if len(got.Links) != 2 {
		t.Fatalf("%s: got %d links, want 2", where, len(got.Links))
	}
	// Array order becomes sort_order, so the list round-trips in the order
	// the client sent it.
	if got.Links[0].URL != "https://example.com/booking" || got.Links[0].SortOrder != 0 {
		t.Errorf("%s: first link wrong: %+v", where, got.Links[0])
	}
	if got.Links[1].SortOrder != 1 {
		t.Errorf("%s: second link sort_order = %d, want 1", where, got.Links[1].SortOrder)
	}
	if got.Links[0].ID == "" {
		t.Errorf("%s: link has no generated id", where)
	}
	if len(got.Dates) != 1 || got.Dates[0].StartDate == nil || *got.Dates[0].StartDate != "2026-08-19" {
		t.Errorf("%s: dates not saved: %+v", where, got.Dates)
	}
}

func TestUpdateItemLeavesOmittedSubResourcesIntact(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	created := createNested(ts, cookie, tripID)

	// A PATCH with no nested keys at all — what a caller editing only the
	// basic fields sends. Nothing hanging off the item may be touched.
	w := ts.do(http.MethodPatch, "/api/items/"+created.ID, cookie,
		`{"title":"Foss Hotel Reykjavik","category":"stay","type":"hotel"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch item: got %d, want 200, body %s", w.Code, w.Body.String())
	}
	got := decode[nestedItem](t, w)
	if got.Title != "Foss Hotel Reykjavik" {
		t.Errorf("title = %q, want the patched one", got.Title)
	}
	assertNested(t, "patch without nested keys", got)
}

func TestUpdateItemReplacesSubResourceSets(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	created := createNested(ts, cookie, tripID)

	w := ts.do(http.MethodPatch, "/api/items/"+created.ID, cookie, `{
		"title": "Foss Hotel",
		"category": "stay",
		"type": "hotel",
		"location": {"lat": 65.0, "lng": -22.0, "address": null},
		"links": [{"url": "https://example.com/only"}],
		"dates": []
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch item: got %d, want 200, body %s", w.Code, w.Body.String())
	}
	got := decode[nestedItem](t, w)

	if got.Location == nil || got.Location.Lat == nil || *got.Location.Lat != 65.0 {
		t.Errorf("location not upserted: %+v", got.Location)
	}
	if got.Location != nil && got.Location.Address != nil {
		t.Errorf("address = %q, want cleared by the explicit null", *got.Location.Address)
	}
	// Replace, not merge: the two original links are gone.
	if len(got.Links) != 1 || got.Links[0].URL != "https://example.com/only" {
		t.Errorf("links not replaced: %+v", got.Links)
	}
	// An empty list is "present but empty", which clears — distinct from
	// omitting the key entirely, covered by the test above.
	if len(got.Dates) != 0 {
		t.Errorf("got %d dates, want the empty list to have cleared them", len(got.Dates))
	}
}

func TestCreateItemRejectsInvalidNestedValues(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	cases := map[string]string{
		"blank link url": `{"title":"X","category":"site","links":[{"url":"  "}]}`,
		"bad start date": `{"title":"X","category":"site","dates":[{"start_date":"19.08.2026"}]}`,
		"bad end date":   `{"title":"X","category":"site","dates":[{"start_date":"2026-08-19","end_date":"nope"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400, body %s", w.Code, w.Body.String())
			}
		})
	}

	// Rejected up front means nothing was written at all — not even the item.
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, "")
	if items := decode[[]map[string]any](t, w); len(items) != 0 {
		t.Errorf("got %d items after rejected creates, want 0", len(items))
	}
}

// failingStore makes one Store method fail on demand, so a rollback can be
// tested against something the real SQLite store won't refuse (the sub-resource
// tables have no constraints to violate). Its WithTx re-wraps the
// transaction-bound Store it is handed, otherwise the injected failure would
// not be visible inside the transaction under test.
type failingStore struct {
	db.Store
	failCreateItemLink bool
}

func (f failingStore) WithTx(ctx context.Context, fn func(db.Store) error) error {
	return f.Store.WithTx(ctx, func(tx db.Store) error {
		return fn(failingStore{Store: tx, failCreateItemLink: f.failCreateItemLink})
	})
}

func (f failingStore) CreateItemLink(ctx context.Context, p db.CreateItemLinkParams) (db.ItemLink, error) {
	if f.failCreateItemLink {
		return db.ItemLink{}, errors.New("injected CreateItemLink failure")
	}
	return f.Store.CreateItemLink(ctx, p)
}

func TestCreateItemRollsBackWhenANestedWriteFails(t *testing.T) {
	ts := newTestServerWithStore(t, func(s db.Store) db.Store {
		return failingStore{Store: s, failCreateItemLink: true}
	})
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, nestedCreateBody)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500, body %s", w.Code, w.Body.String())
	}

	// The whole point of the transaction: the item row that was inserted
	// before the link failed must be gone. Before Milestone 1 this left a
	// half-populated location behind (with its coordinates, without its
	// links) and the client had to clean up.
	w = ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list items: got %d, want 200", w.Code)
	}
	if items := decode[[]map[string]any](t, w); len(items) != 0 {
		t.Errorf("got %d items after the failed create, want 0 — the transaction did not roll back", len(items))
	}
}
