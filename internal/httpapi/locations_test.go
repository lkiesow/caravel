package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"caravel/internal/db"
)

// Coverage for the nested create/update contract added in Stage 09 Milestone 1:
// one request commits a location plus its geo, links and dates, in a single
// transaction. The standalone location and links endpoints are unchanged and
// keep their own coverage via ownership_test.go.
//
// Since Stage 25 the dates block is not a set of rows on the location but the
// itinerary days it appears on, so a "date" here is a range with no id, and
// writing one writes itinerary entries. TestReconcileLocationDates below covers
// what that costs; these tests only need the contract to still hold.

// nestedLocation is the part of locationDetailResponse these tests assert on,
// decoded the way a client sees it (JSON) rather than by reaching into the
// handler's own structs.
type nestedLocation struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Geo      *struct {
		Lat     *float64 `json:"lat"`
		Lng     *float64 `json:"lng"`
		Address *string  `json:"address"`
	} `json:"geo"`
	Links []struct {
		ID        string  `json:"id"`
		URL       string  `json:"url"`
		Label     *string `json:"label"`
		SortOrder int     `json:"sort_order"`
	} `json:"links"`
	Dates []struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	} `json:"dates"`
}

const nestedCreateBody = `{
	"title": "Foss Hotel",
	"category": "stay",
	"tags": ["hotel"],
	"geo": {"lat": 64.146, "lng": -21.94, "address": "Reykjavik"},
	"links": [
		{"url": "https://example.com/booking", "label": "Booking"},
		{"url": "https://example.com/map"}
	],
	"dates": [{"start_date": "2026-08-19", "end_date": "2026-08-21"}]
}`

// createNested posts nestedCreateBody and returns the decoded response.
func createNested(ts *testServer, cookie *http.Cookie, tripID string) nestedLocation {
	ts.t.Helper()
	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie, nestedCreateBody)
	if w.Code != http.StatusCreated {
		ts.t.Fatalf("create nested location: got %d, want 201, body %s", w.Code, w.Body.String())
	}
	return decode[nestedLocation](ts.t, w)
}

func TestCreateLocationWithNestedSubResources(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	created := createNested(ts, cookie, tripID)

	// The create response carries everything back, so the client needs no
	// follow-up GET — that's why the handler returns the detail shape.
	assertNested(t, "create response", created)

	// ...and it is actually persisted, not just echoed.
	w := ts.do(http.MethodGet, "/api/locations/"+created.ID, cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get location: got %d, want 200", w.Code)
	}
	assertNested(t, "subsequent GET", decode[nestedLocation](t, w))
}

func assertNested(t *testing.T, where string, got nestedLocation) {
	t.Helper()

	if got.Geo == nil || got.Geo.Lat == nil || *got.Geo.Lat != 64.146 {
		t.Errorf("%s: geo not saved: %+v", where, got.Geo)
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
	// Three consecutive days on the itinerary, read back as the one inclusive
	// range that was written.
	if len(got.Dates) != 1 || got.Dates[0].StartDate != "2026-08-19" || got.Dates[0].EndDate != "2026-08-21" {
		t.Errorf("%s: dates not saved: %+v", where, got.Dates)
	}
}

func TestUpdateLocationLeavesOmittedSubResourcesIntact(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	created := createNested(ts, cookie, tripID)

	// A PATCH with no nested keys at all — what a caller editing only the
	// basic fields sends. Nothing hanging off the location may be touched.
	w := ts.do(http.MethodPatch, "/api/locations/"+created.ID, cookie,
		`{"title":"Foss Hotel Reykjavik","category":"stay","tags":["hotel"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch location: got %d, want 200, body %s", w.Code, w.Body.String())
	}
	got := decode[nestedLocation](t, w)
	if got.Title != "Foss Hotel Reykjavik" {
		t.Errorf("title = %q, want the patched one", got.Title)
	}
	assertNested(t, "patch without nested keys", got)
}

func TestUpdateLocationReplacesSubResourceSets(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	created := createNested(ts, cookie, tripID)

	w := ts.do(http.MethodPatch, "/api/locations/"+created.ID, cookie, `{
		"title": "Foss Hotel",
		"category": "stay",
		"tags": ["hotel"],
		"geo": {"lat": 65.0, "lng": -22.0, "address": null},
		"links": [{"url": "https://example.com/only"}],
		"dates": []
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch location: got %d, want 200, body %s", w.Code, w.Body.String())
	}
	got := decode[nestedLocation](t, w)

	if got.Geo == nil || got.Geo.Lat == nil || *got.Geo.Lat != 65.0 {
		t.Errorf("geo not upserted: %+v", got.Geo)
	}
	if got.Geo != nil && got.Geo.Address != nil {
		t.Errorf("address = %q, want cleared by the explicit null", *got.Geo.Address)
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

func TestCreateLocationRejectsInvalidNestedValues(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	cases := map[string]string{
		"blank link url":   `{"title":"X","category":"site","links":[{"url":"  "}]}`,
		"bad start date":   `{"title":"X","category":"site","dates":[{"start_date":"19.08.2026"}]}`,
		"missing start":    `{"title":"X","category":"site","dates":[{"end_date":"2026-08-19"}]}`,
		"bad end date":     `{"title":"X","category":"site","dates":[{"start_date":"2026-08-19","end_date":"nope"}]}`,
		"end before start": `{"title":"X","category":"site","dates":[{"start_date":"2026-08-21","end_date":"2026-08-19"}]}`,
		"absurd span":      `{"title":"X","category":"site","dates":[{"start_date":"2026-08-19","end_date":"2126-08-19"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie, body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400, body %s", w.Code, w.Body.String())
			}
		})
	}

	// Rejected up front means nothing was written at all — not even the
	// location.
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/locations", cookie, "")
	if locations := decode[[]map[string]any](t, w); len(locations) != 0 {
		t.Errorf("got %d locations after rejected creates, want 0", len(locations))
	}
}

// failingStore makes one Store method fail on demand, so a rollback can be
// tested against something the real SQLite store won't refuse (the sub-resource
// tables have no constraints to violate). Its WithTx re-wraps the
// transaction-bound Store it is handed, otherwise the injected failure would
// not be visible inside the transaction under test.
type failingStore struct {
	db.Store
	failCreateLocationLink bool
	// failCreateFile proves the multipart create is atomic across *all* of
	// its writes: it fires after the location, its nested rows, the media asset
	// and the image attachment have all been written inside the transaction.
	failCreateFile bool
}

func (f failingStore) WithTx(ctx context.Context, fn func(db.Store) error) error {
	return f.Store.WithTx(ctx, func(tx db.Store) error {
		return fn(failingStore{Store: tx, failCreateLocationLink: f.failCreateLocationLink, failCreateFile: f.failCreateFile})
	})
}

func (f failingStore) CreateLocationLink(ctx context.Context, p db.CreateLocationLinkParams) (db.LocationLink, error) {
	if f.failCreateLocationLink {
		return db.LocationLink{}, errors.New("injected CreateLocationLink failure")
	}
	return f.Store.CreateLocationLink(ctx, p)
}

func (f failingStore) CreateFile(ctx context.Context, p db.CreateFileParams) (db.File, error) {
	if f.failCreateFile {
		return db.File{}, errors.New("injected CreateFile failure")
	}
	return f.Store.CreateFile(ctx, p)
}

func TestCreateLocationRollsBackWhenANestedWriteFails(t *testing.T) {
	ts := newTestServerWithStore(t, func(s db.Store) db.Store {
		return failingStore{Store: s, failCreateLocationLink: true}
	})
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie, nestedCreateBody)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500, body %s", w.Code, w.Body.String())
	}

	// The whole point of the transaction: the location row that was inserted
	// before the link failed must be gone. Before Milestone 1 this left a
	// half-populated location behind (with its coordinates, without its
	// links) and the client had to clean up.
	w = ts.do(http.MethodGet, "/api/trips/"+tripID+"/locations", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list items: got %d, want 200", w.Code)
	}
	if locations := decode[[]map[string]any](t, w); len(locations) != 0 {
		t.Errorf("got %d locations after the failed create, want 0 — the transaction did not roll back", len(locations))
	}
}

// Stage 13 Milestone 7: the locations list carries coordinates so the tab can
// filter by distance client-side. It deliberately ignores show_on_map, which
// governs whether a place is drawn on the map and says nothing about whether
// it has a position.
func TestListLocationsCarriesCoordinatesIgnoringShowOnMap(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	located := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie,
		`{"title":"Kirkjufell","category":"site","geo":{"lat":64.9269,"lng":-23.3086}}`, http.StatusCreated)
	// Same, but explicitly hidden from the map. It still has a position.
	hidden := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie,
		`{"title":"Hidden but placed","category":"stay","show_on_map":false,"geo":{"lat":64.1466,"lng":-21.9426}}`, http.StatusCreated)
	// Address only, no coordinates: not far away, unmeasurable.
	addressOnly := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie,
		`{"title":"Somewhere vague","category":"site","geo":{"address":"past the bridge"}}`, http.StatusCreated)
	none := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie,
		`{"title":"No location at all","category":"site"}`, http.StatusCreated)

	rec := ts.do(http.MethodGet, "/api/trips/"+tripID+"/locations", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var locations []locationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &locations); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]locationResponse{}
	for _, it := range locations {
		byID[it.ID] = it
	}

	if got := byID[located]; got.Lat == nil || got.Lng == nil {
		t.Errorf("a located location should carry coordinates, got %+v", got)
	} else if *got.Lat != 64.9269 || *got.Lng != -23.3086 {
		t.Errorf("coordinates = %v,%v", *got.Lat, *got.Lng)
	}
	// The whole point of not reusing ListMapLocations.
	if got := byID[hidden]; got.Lat == nil {
		t.Error("show_on_map=false must not hide a location's coordinates from the list")
	}
	for name, id := range map[string]string{"address-only": addressOnly, "no location": none} {
		if got := byID[id]; got.Lat != nil || got.Lng != nil {
			t.Errorf("%s should have no coordinates, got %v,%v", name, got.Lat, got.Lng)
		}
	}
}

// The area category, added in migration 0010, has to survive the write: a
// value the API accepts but the CHECK constraint refuses is a 500, and the
// SQLite side of that migration rebuilds the whole items table to allow it.
func TestAreaCategoryRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/locations", cookie,
		`{"title":"Snaefellsnes","category":"area","geo":{"lat":64.87,"lng":-23.35}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create area location: got %d, want 201, body %s", w.Code, w.Body.String())
	}
	if got := decode[nestedLocation](t, w); got.Category != "area" {
		t.Errorf("category = %q, want area", got.Category)
	}

	// And the list filter, which validates the query parameter against the
	// same map.
	w = ts.do(http.MethodGet, "/api/trips/"+tripID+"/locations?category=area", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("filter by area: got %d, want 200, body %s", w.Code, w.Body.String())
	}
	if locations := decode[[]map[string]any](t, w); len(locations) != 1 {
		t.Errorf("got %d locations filtered by area, want 1", len(locations))
	}
}

// The item routes were dropped in Stage 49, not aliased -- the frontend is the
// only client, and it moved in the same commit (the precedent is Stage 11
// dropping /documents for /files). Asked with a real trip and a real location,
// so a 404 here means the route is gone rather than the record.
func TestItemRoutesAreGone(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	locationID := ts.createLocation(cookie, tripID, "Kirkjufell")

	if w := ts.do(http.MethodGet, "/api/locations/"+locationID, cookie, ""); w.Code != http.StatusOK {
		t.Fatalf("GET the location under its new route: got %d, want 200", w.Code)
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/items/" + locationID, ""},
		{http.MethodPatch, "/api/items/" + locationID, `{"title":"x","category":"site"}`},
		{http.MethodDelete, "/api/items/" + locationID, ""},
		{http.MethodPut, "/api/items/" + locationID + "/location", `{"lat":1,"lng":2}`},
		{http.MethodPut, "/api/locations/" + locationID + "/location", `{"lat":1,"lng":2}`},
		{http.MethodGet, "/api/trips/" + tripID + "/items", ""},
		{http.MethodPost, "/api/trips/" + tripID + "/items", `{"title":"x","category":"site"}`},
		{http.MethodPost, "/api/trips/" + tripID + "/items/batch", `{"items":[]}`},
	} {
		if w := ts.do(tc.method, tc.path, cookie, tc.body); w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, w.Code)
		}
	}
	// And nothing was written through them.
	if w := ts.do(http.MethodGet, "/api/locations/"+locationID, cookie, ""); !strings.Contains(w.Body.String(), `"title":"Kirkjufell"`) {
		t.Errorf("the location changed through a dropped route: %s", w.Body.String())
	}
}
