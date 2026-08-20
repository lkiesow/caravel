package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// Cross-user ownership coverage for every resource.
//
// This is the case no unit test can express — it needs a real router, real auth
// middleware and two real sessions — and it is the reason the HTTP harness in
// testing_test.go exists. Before this file, trips, items, checklists, files
// and media had *no* handler coverage at all (measured: 23 of 25 handlers at
// 0.0%), so nothing would have caught a missing ownership check on any of them.
//
// Every violation is expected to answer 404, not 403: the handlers deliberately
// report "not found" for another user's resource rather than confirming it
// exists (see loadOwnedTrip in trips.go). Asserting 404 therefore pins the
// information-disclosure behaviour as well as the access control — a change to
// 403 would leak existence and fail here.

// owned is a trip belonging to `owner`, with a child of each kind, plus a
// separate `intruder` session that must not be able to reach any of it.
type owned struct {
	ts          *testServer
	owner       *http.Cookie
	intruder    *http.Cookie
	tripID      string
	itemID      string
	checklistID string
	fileID       string
	mediaID     string
}

func setupOwned(t *testing.T) *owned {
	t.Helper()

	ts := newTestServer(t)
	owner := ts.login("owner")
	intruder := ts.login("intruder")

	tripID := ts.createTrip(owner, "Owner's trip")
	itemID := ts.createItem(owner, tripID, "Owner's location")
	checklistID := ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/checklists", owner,
		`{"title":"Packing"}`, http.StatusCreated,
	)

	w := ts.upload("/api/trips/"+tripID+"/files", owner, "secret.txt", "text/plain", []byte("owner's file"))
	if w.Code != http.StatusCreated {
		t.Fatalf("upload file: got %d, body %s", w.Code, w.Body.String())
	}
	fileID := decode[map[string]any](t, w)["id"].(string)

	// A 1x1 PNG, so the image pipeline has something valid to decode.
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
		0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
		0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	w = ts.upload("/api/trips/"+tripID+"/media", owner, "cover.png", "image/png", png)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload media: got %d, body %s", w.Code, w.Body.String())
	}
	mediaID := decode[map[string]any](t, w)["id"].(string)

	return &owned{
		ts: ts, owner: owner, intruder: intruder,
		tripID: tripID, itemID: itemID, checklistID: checklistID, fileID: fileID, mediaID: mediaID,
	}
}

// assertDenied checks that a request answers 404 and, for reads, that the
// response body does not carry the owner's data.
func (o *owned) assertDenied(t *testing.T, method, path, body string) {
	t.Helper()

	w := o.ts.do(method, path, o.intruder, body)
	if w.Code != http.StatusNotFound {
		t.Errorf("%s %s as another user: got %d, want 404 — body %s", method, path, w.Code, w.Body.String())
		return
	}
	for _, secret := range []string{"Owner's trip", "Owner's location", "Packing", "secret.txt", "owner's file"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s %s leaked %q in a 404 body: %s", method, path, secret, w.Body.String())
		}
	}
}

func TestTripRoutesRejectAnotherUser(t *testing.T) {
	o := setupOwned(t)
	base := "/api/trips/" + o.tripID

	o.assertDenied(t, http.MethodGet, base, "")
	o.assertDenied(t, http.MethodPatch, base, `{"title":"stolen"}`)
	o.assertDenied(t, http.MethodDelete, base, "")
	o.assertDenied(t, http.MethodGet, base+"/map", "")
	o.assertDenied(t, http.MethodGet, base+"/itinerary", "")

	// The trip must still be intact and still the owner's afterwards.
	w := o.ts.do(http.MethodGet, base, o.owner, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner can still read own trip: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["title"]; got != "Owner's trip" {
		t.Errorf("trip title changed to %v — an intruder write got through", got)
	}
}

func TestTripListIsScopedToOwner(t *testing.T) {
	o := setupOwned(t)

	trips := decode[[]map[string]any](t, o.ts.do(http.MethodGet, "/api/trips", o.intruder, ""))
	if len(trips) != 0 {
		t.Fatalf("intruder sees %d trip(s) in their list, want 0: %v", len(trips), trips)
	}

	trips = decode[[]map[string]any](t, o.ts.do(http.MethodGet, "/api/trips", o.owner, ""))
	if len(trips) != 1 {
		t.Fatalf("owner sees %d trip(s), want 1", len(trips))
	}
}

func TestItemRoutesRejectAnotherUser(t *testing.T) {
	o := setupOwned(t)
	item := "/api/items/" + o.itemID

	o.assertDenied(t, http.MethodGet, item, "")
	o.assertDenied(t, http.MethodPatch, item, `{"title":"stolen"}`)
	o.assertDenied(t, http.MethodDelete, item, "")
	o.assertDenied(t, http.MethodPut, item+"/location", `{"lat":1,"lng":2}`)
	o.assertDenied(t, http.MethodPost, item+"/links", `{"url":"https://example.com","label":"x"}`)
	o.assertDenied(t, http.MethodPost, item+"/dates", `{"start_date":"2026-08-20"}`)
	o.assertDenied(t, http.MethodGet, item+"/files", "")
	// Creating an item on someone else's trip goes through the trip, not the item.
	o.assertDenied(t, http.MethodPost, "/api/trips/"+o.tripID+"/items", `{"title":"x","category":"site","type":"y"}`)
	o.assertDenied(t, http.MethodGet, "/api/trips/"+o.tripID+"/items", "")

	w := o.ts.do(http.MethodGet, item, o.owner, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner can still read own item: got %d", w.Code)
	}
	if got := decode[map[string]any](t, w)["title"]; got != "Owner's location" {
		t.Errorf("item title changed to %v — an intruder write got through", got)
	}
}

func TestChecklistRoutesRejectAnotherUser(t *testing.T) {
	o := setupOwned(t)
	list := "/api/checklists/" + o.checklistID

	o.assertDenied(t, http.MethodGet, "/api/trips/"+o.tripID+"/checklists", "")
	o.assertDenied(t, http.MethodPost, "/api/trips/"+o.tripID+"/checklists", `{"title":"stolen"}`)
	o.assertDenied(t, http.MethodDelete, list, "")
	o.assertDenied(t, http.MethodPost, list+"/items", `{"text":"stolen"}`)

	// An item on the owner's checklist, to test the per-item routes.
	itemID := o.ts.mustCreate(http.MethodPost, list+"/items", o.owner, `{"text":"Passport"}`, http.StatusCreated)
	o.assertDenied(t, http.MethodPatch, list+"/items/"+itemID, `{"checked":true}`)
	o.assertDenied(t, http.MethodDelete, list+"/items/"+itemID, "")

	lists := decode[[]map[string]any](t, o.ts.do(http.MethodGet, "/api/trips/"+o.tripID+"/checklists", o.owner, ""))
	if len(lists) != 1 {
		t.Fatalf("owner sees %d checklist(s), want 1 — an intruder call mutated them", len(lists))
	}
}

func TestFileRoutesRejectAnotherUser(t *testing.T) {
	o := setupOwned(t)

	o.assertDenied(t, http.MethodGet, "/api/trips/"+o.tripID+"/files", "")
	o.assertDenied(t, http.MethodGet, "/api/files/"+o.fileID+"/download", "")
	o.assertDenied(t, http.MethodDelete, "/api/files/"+o.fileID, "")

	// Uploads are multipart, so they don't go through assertDenied.
	w := o.ts.upload("/api/trips/"+o.tripID+"/files", o.intruder, "evil.txt", "text/plain", []byte("x"))
	if w.Code != http.StatusNotFound {
		t.Errorf("upload to another user's trip: got %d, want 404 — body %s", w.Code, w.Body.String())
	}
	w = o.ts.upload("/api/items/"+o.itemID+"/files", o.intruder, "evil.txt", "text/plain", []byte("x"))
	if w.Code != http.StatusNotFound {
		t.Errorf("upload to another user's item: got %d, want 404 — body %s", w.Code, w.Body.String())
	}

	// The owner's file must still be there and still downloadable.
	files := decode[[]map[string]any](t, o.ts.do(http.MethodGet, "/api/trips/"+o.tripID+"/files", o.owner, ""))
	if len(files) != 1 {
		t.Fatalf("owner sees %d file(s), want 1 — an intruder call got through", len(files))
	}
	w = o.ts.do(http.MethodGet, "/api/files/"+o.fileID+"/download", o.owner, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner download: got %d, body %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "owner's file" {
		t.Errorf("owner download returned %q", w.Body.String())
	}
}

func TestMediaRoutesRejectAnotherUser(t *testing.T) {
	o := setupOwned(t)

	// Serving someone else's media file is the leak that matters here: the URL
	// is guessable only by having the ID, but the check must still hold.
	o.assertDenied(t, http.MethodGet, "/api/media/"+o.mediaID+"/file", "")
	o.assertDenied(t, http.MethodPost, "/api/trips/"+o.tripID+"/media/url", `{"url":"https://example.com/x.png"}`)
	o.assertDenied(t, http.MethodPut, "/api/trips/"+o.tripID+"/preview-image", `{"media_asset_id":"`+o.mediaID+`"}`)
	o.assertDenied(t, http.MethodPut, "/api/items/"+o.itemID+"/image", `{"media_asset_id":"`+o.mediaID+`"}`)

	w := o.ts.do(http.MethodGet, "/api/media/"+o.mediaID+"/file", o.owner, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner can still fetch own media: got %d, body %s", w.Code, w.Body.String())
	}
}

// Unauthenticated access is a separate axis from cross-user access: the
// middleware rejects before any handler runs, so it needs its own assertion.
func TestOwnedRoutesRequireAuth(t *testing.T) {
	o := setupOwned(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/trips"},
		{http.MethodGet, "/api/trips/" + o.tripID},
		{http.MethodGet, "/api/items/" + o.itemID},
		{http.MethodGet, "/api/trips/" + o.tripID + "/checklists"},
		{http.MethodGet, "/api/files/" + o.fileID + "/download"},
		{http.MethodGet, "/api/media/" + o.mediaID + "/file"},
	} {
		w := o.ts.do(tc.method, tc.path, nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: got %d, want 401", tc.method, tc.path, w.Code)
		}
	}
}
