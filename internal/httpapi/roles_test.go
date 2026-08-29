package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"caravel/internal/db"
)

// The role matrix: for every trip-scoped route, what each of the four kinds of
// caller gets.
//
// ownership_test.go covers one axis of this — a stranger must get 404 and must
// not learn the resource exists — and stays as it is, because that contract is
// worth pinning on its own. This file covers the axis Stage 14 added: a caller
// who *does* have a role, but not enough of one.
//
// The two failure directions answer differently, and that asymmetry is the
// thing under test (see the note at the top of authz.go):
//
//	stranger -> 404, because existence must not leak
//	viewer writing -> 403, because they can already read the trip and a 404
//	                  would be a lie they cannot act on
//
// On the allowed side this file asserts only "not 403 and not 404" rather than
// an exact status: the routes answer 200/201/204 variously, and pinning each
// one here would make this a response-shape test that breaks for reasons that
// have nothing to do with authorization. TestEditorWritesActuallyLand below
// closes the obvious gap in that — that a permitted write really happened
// rather than merely being permitted.

// roleFixture is a trip owned by `owner`, with a child of every kind, plus an
// `actor` session holding the role under test. actor is a *different* user in
// every case, including the owner case, where they are the owner themselves.
type roleFixture struct {
	ts          *testServer
	owner       *http.Cookie
	actor       *http.Cookie
	tripID      string
	itemID      string
	checklistID string
	fileID      string
	mediaID     string
	dayID       string
	entryID     string
	expenseID   string
}

// A 1x1 PNG, so the image pipeline has something valid to decode.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// setupRole builds a fresh trip and gives `actor` the named role on it. Pass
// db.RoleOwner to make the actor the owner, or "" to leave them a stranger.
//
// Fresh per call rather than shared across the matrix: half the routes under
// test are destructive, and a fixture that has already been deleted by an
// earlier row answers 404 for reasons that look exactly like an authorization
// failure.
func setupRole(t *testing.T, role db.TripRole) *roleFixture {
	t.Helper()

	ts := newTestServer(t)
	owner := ts.login("owner")

	f := &roleFixture{ts: ts, owner: owner}
	f.tripID = ts.createTrip(owner, "Owner's trip")
	f.itemID = ts.createItem(owner, f.tripID, "Owner's location")
	f.checklistID = ts.mustCreate(
		http.MethodPost, "/api/trips/"+f.tripID+"/checklists", owner,
		`{"title":"Packing"}`, http.StatusCreated,
	)

	w := ts.upload("/api/trips/"+f.tripID+"/files", owner, "secret.txt", "text/plain", []byte("owner's file"))
	if w.Code != http.StatusCreated {
		t.Fatalf("upload file: got %d, body %s", w.Code, w.Body.String())
	}
	f.fileID = decode[map[string]any](t, w)["id"].(string)

	w = ts.upload("/api/trips/"+f.tripID+"/media", owner, "cover.png", "image/png", onePixelPNG)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload media: got %d, body %s", w.Code, w.Body.String())
	}
	f.mediaID = decode[map[string]any](t, w)["id"].(string)

	f.expenseID = ts.mustCreate(
		http.MethodPost, "/api/trips/"+f.tripID+"/expenses", owner,
		`{"title":"Owner's ferry","amount_minor":1500,"spent_on":"2026-08-20"}`, http.StatusCreated,
	)

	// An explicit itinerary day, so the /api/itinerary/days/{dayId} routes have
	// a real row to aim at. Days inside a trip's date range are synthesized and
	// have no id.
	w = ts.do(http.MethodPut, "/api/trips/"+f.tripID+"/itinerary/days/2026-08-20", owner, `{"notes":"arrive"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create itinerary day: got %d, body %s", w.Code, w.Body.String())
	}
	f.dayID = *decode[struct {
		ID *string `json:"id"`
	}](t, w).ID

	// One entry on that day, so the per-entry routes have something to name.
	f.entryID = ts.mustCreate(
		http.MethodPost, "/api/itinerary/days/"+f.dayID+"/entries", owner,
		`{"item_id":"`+f.itemID+`"}`, http.StatusCreated,
	)

	if role == db.RoleOwner {
		f.actor = owner
		return f
	}
	f.actor = ts.login("actor")
	if role != "" {
		// Straight to the store: the members API does not exist yet (Stage 14
		// Milestone 3), and this milestone is about what the *seam* does with a
		// membership, not about how one gets created.
		user, err := ts.Store.GetUserByUsername(context.Background(), "actor")
		if err != nil {
			t.Fatalf("look up actor: %v", err)
		}
		if _, err := ts.Store.UpsertTripMember(context.Background(), f.tripID, user.ID, role, time.Now().UTC()); err != nil {
			t.Fatalf("add actor as %s: %v", role, err)
		}
	}
	return f
}

// route is one row of the matrix: a request, and the minimum role it needs.
// Both path and body are built from the fixture, so a permitted write is a
// genuine success rather than a 400 from a placeholder field — otherwise the
// "allowed" assertion would pass on a route that rejects everything.
type route struct {
	method string
	path   func(f *roleFixture) string
	body   func(f *roleFixture) string
	min    db.TripRole
}

// lit is a body that does not depend on the fixture.
func lit(body string) func(*roleFixture) string {
	return func(*roleFixture) string { return body }
}

// testRank duplicates db.TripRole's ordering on purpose.
//
// The first version of this test asked db.TripRole.AtLeast which outcome to
// expect, which made it a tautology: breaking AtLeast to `return true` flipped
// the production check *and* the expectation together, and the matrix passed
// while every viewer write was being permitted. Only TestRoleMatrixUploads,
// which hardcodes its expected statuses, caught it. So the ordering is spelled
// out again here — a second, independent statement of the rule, which is the
// only kind of expectation that can disagree with the code.
var testRank = map[db.TripRole]int{db.RoleViewer: 1, db.RoleEditor: 2, db.RoleOwner: 3}

// permitted reports whether actor should be allowed a route needing min,
// computed without consulting the code under test.
func permitted(actor, min db.TripRole) bool {
	return testRank[actor] > 0 && testRank[actor] >= testRank[min]
}

func roleRoutes() []route {
	trip := func(suffix string) func(*roleFixture) string {
		return func(f *roleFixture) string { return "/api/trips/" + f.tripID + suffix }
	}
	item := func(suffix string) func(*roleFixture) string {
		return func(f *roleFixture) string { return "/api/items/" + f.itemID + suffix }
	}
	return []route{
		// Reads — a viewer must be able to do all of these.
		{http.MethodGet, trip(""), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/items"), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/tags"), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/map"), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/itinerary"), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/files"), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/checklists"), lit(""), db.RoleViewer},
		{http.MethodGet, trip("/expenses"), lit(""), db.RoleViewer},
		{http.MethodGet, item(""), lit(""), db.RoleViewer},
		{http.MethodGet, item("/files"), lit(""), db.RoleViewer},
		{http.MethodGet, func(f *roleFixture) string { return "/api/files/" + f.fileID + "/download" }, lit(""), db.RoleViewer},
		{http.MethodGet, func(f *roleFixture) string { return "/api/media/" + f.mediaID + "/file" }, lit(""), db.RoleViewer},

		// Writes — an editor may, a viewer may not.
		{http.MethodPatch, trip(""), lit(`{"title":"edited"}`), db.RoleEditor},
		{http.MethodPut, trip("/preview-image"), func(f *roleFixture) string { return `{"media_asset_id":"` + f.mediaID + `"}` }, db.RoleEditor},
		{http.MethodPost, trip("/media/url"), lit(`{"url":"https://example.com/x.png"}`), db.RoleEditor},
		{http.MethodPost, trip("/items"), lit(`{"title":"new","category":"site","tags":["landmark"]}`), db.RoleEditor},
		{http.MethodPost, trip("/checklists"), lit(`{"title":"new list"}`), db.RoleEditor},
		{http.MethodPost, trip("/expenses"), lit(`{"title":"new expense","amount_minor":250,"spent_on":"2026-08-21"}`), db.RoleEditor},
		{http.MethodPut, trip("/itinerary/days/2026-08-21"), lit(`{"notes":"n"}`), db.RoleEditor},
		// The dates block rides along on the item PATCH since Stage 25, where it
		// writes itinerary entries — so this row gates those too.
		{http.MethodPatch, item(""), lit(`{"title":"edited","category":"site","tags":["landmark"],"dates":[{"start_date":"2026-08-21"}]}`), db.RoleEditor},
		{http.MethodPut, item("/location"), lit(`{"lat":1,"lng":2}`), db.RoleEditor},
		{http.MethodPut, item("/image"), func(f *roleFixture) string { return `{"media_asset_id":"` + f.mediaID + `"}` }, db.RoleEditor},
		{http.MethodPost, item("/links"), lit(`{"url":"https://example.com","label":"x"}`), db.RoleEditor},
		{http.MethodPatch, func(f *roleFixture) string { return "/api/files/" + f.fileID }, lit(`{"note":"n"}`), db.RoleEditor},
		{http.MethodPost, func(f *roleFixture) string { return "/api/checklists/" + f.checklistID + "/items" }, lit(`{"text":"t"}`), db.RoleEditor},
		{http.MethodPatch, func(f *roleFixture) string { return "/api/expenses/" + f.expenseID },
			lit(`{"title":"edited","amount_minor":1600,"spent_on":"2026-08-20"}`), db.RoleEditor},
		{http.MethodPost, func(f *roleFixture) string { return "/api/itinerary/days/" + f.dayID + "/entries" },
			func(f *roleFixture) string { return `{"item_id":"` + f.itemID + `"}` }, db.RoleEditor},
		{http.MethodPatch, func(f *roleFixture) string { return "/api/itinerary/days/" + f.dayID + "/entries/" + f.entryID },
			lit(`{"to_date":"2026-08-22"}`), db.RoleEditor},
		// Deletes, last because they destroy the fixture — but each row gets a
		// fresh one anyway, so the ordering is only belt and braces.
		{http.MethodDelete, item(""), lit(""), db.RoleEditor},
		{http.MethodDelete, func(f *roleFixture) string { return "/api/files/" + f.fileID }, lit(""), db.RoleEditor},
		{http.MethodDelete, func(f *roleFixture) string { return "/api/checklists/" + f.checklistID }, lit(""), db.RoleEditor},
		{http.MethodDelete, func(f *roleFixture) string { return "/api/expenses/" + f.expenseID }, lit(""), db.RoleEditor},
		{http.MethodDelete, func(f *roleFixture) string { return "/api/itinerary/days/" + f.dayID }, lit(""), db.RoleEditor},

		// Owner only.
		{http.MethodDelete, trip(""), lit(""), db.RoleOwner},
	}
}

func TestRoleMatrix(t *testing.T) {
	for _, actorRole := range []db.TripRole{"", db.RoleViewer, db.RoleEditor, db.RoleOwner} {
		name := string(actorRole)
		if name == "" {
			name = "stranger"
		}
		t.Run(name, func(t *testing.T) {
			for _, rt := range roleRoutes() {
				f := setupRole(t, actorRole)
				path := rt.path(f)
				w := f.ts.do(rt.method, path, f.actor, rt.body(f))

				switch {
				case actorRole == "":
					// No role at all: 404, and nothing of the owner's in the body.
					if w.Code != http.StatusNotFound {
						t.Errorf("%s %s as stranger: got %d, want 404 — body %s",
							rt.method, path, w.Code, w.Body.String())
					}
					for _, secret := range []string{"Owner's trip", "Owner's location", "Packing", "secret.txt", "owner's file", "arrive", "Owner's ferry"} {
						if strings.Contains(w.Body.String(), secret) {
							t.Errorf("%s %s leaked %q to a stranger: %s", rt.method, path, secret, w.Body.String())
						}
					}
				case !permitted(actorRole, rt.min):
					// A role, but not enough of one: 403, never 404.
					if w.Code != http.StatusForbidden {
						t.Errorf("%s %s as %s (needs %s): got %d, want 403 — body %s",
							rt.method, path, actorRole, rt.min, w.Code, w.Body.String())
					}
				default:
					// Permitted. Anything but the two authorization codes: a 400
					// from a deliberately thin body is fine here, the point is
					// that the request was allowed through.
					if w.Code == http.StatusForbidden || w.Code == http.StatusNotFound {
						t.Errorf("%s %s as %s (needs %s): got %d, want it allowed — body %s",
							rt.method, path, actorRole, rt.min, w.Code, w.Body.String())
					}
				}
			}
		})
	}
}

// Multipart uploads don't go through the table (they need their own body
// builder), so they get their own pass over the same three outcomes.
func TestRoleMatrixUploads(t *testing.T) {
	for _, tc := range []struct {
		role db.TripRole
		want int
	}{
		{"", http.StatusNotFound},
		{db.RoleViewer, http.StatusForbidden},
		{db.RoleEditor, http.StatusCreated},
		{db.RoleOwner, http.StatusCreated},
	} {
		name := string(tc.role)
		if name == "" {
			name = "stranger"
		}
		t.Run(name, func(t *testing.T) {
			f := setupRole(t, tc.role)
			for _, path := range []string{
				"/api/trips/" + f.tripID + "/files",
				"/api/items/" + f.itemID + "/files",
				"/api/trips/" + f.tripID + "/media",
			} {
				content, filename, ct := []byte("x"), "f.txt", "text/plain"
				if strings.HasSuffix(path, "/media") {
					content, filename, ct = onePixelPNG, "i.png", "image/png"
				}
				w := f.ts.upload(path, f.actor, filename, ct, content)
				if w.Code != tc.want {
					t.Errorf("POST %s as %s: got %d, want %d — body %s",
						path, name, w.Code, tc.want, w.Body.String())
				}
			}
		})
	}
}

// An allowed write must actually take effect. The matrix asserts only that a
// permitted request is not refused, which on its own would still pass if
// dropping owner_id from UpdateTrip had turned the UPDATE into a no-op that
// answers 200 — the exact failure mode that change risked.
func TestEditorWritesActuallyLand(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	w := f.ts.do(http.MethodPatch, "/api/trips/"+f.tripID, f.actor, `{"title":"Edited by the editor"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("editor PATCH trip: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["title"]; got != "Edited by the editor" {
		t.Errorf("PATCH response title is %v, want the new title", got)
	}
	// Re-read as the owner: the response echoing the new title would look the
	// same whether or not the row changed.
	w = f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.owner, "")
	if got := decode[map[string]any](t, w)["title"]; got != "Edited by the editor" {
		t.Errorf("owner re-reads title as %v — the editor's write did not persist", got)
	}

	// The cover photo is the other query that lost its owner_id predicate.
	w = f.ts.do(http.MethodPut, "/api/trips/"+f.tripID+"/preview-image", f.actor,
		`{"media_asset_id":"`+f.mediaID+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("editor set cover photo: got %d, body %s", w.Code, w.Body.String())
	}
	w = f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.owner, "")
	if got := decode[map[string]any](t, w)["preview_image_id"]; got != f.mediaID {
		t.Errorf("preview_image_id is %v, want %s — the editor's write did not persist", got, f.mediaID)
	}
}

// A media asset id arrives in a request body, so no route param authorizes it.
// Before Stage 14 nothing checked that it belonged to the trip being edited —
// harmless while one owner held both trips, a cross-trip read once they don't.
func TestMediaAssetFromAnotherTripIsRejected(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	// A second trip, owned by the actor, with its own media asset. The actor is
	// an editor on the first trip and the owner of this one, so both ends of
	// the request are legitimately theirs — only the *pairing* is not.
	otherTrip := f.ts.createTrip(f.actor, "Actor's own trip")
	w := f.ts.upload("/api/trips/"+otherTrip+"/media", f.actor, "mine.png", "image/png", onePixelPNG)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload media to actor's own trip: got %d, body %s", w.Code, w.Body.String())
	}
	otherMedia := decode[map[string]any](t, w)["id"].(string)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/trips/" + f.tripID + "/preview-image"},
		{http.MethodPut, "/api/items/" + f.itemID + "/image"},
	} {
		w := f.ts.do(tc.method, tc.path, f.actor, `{"media_asset_id":"`+otherMedia+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s %s with another trip's asset: got %d, want 400 — body %s",
				tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// And the trip must be unchanged: a rejected request that still wrote the
	// row would be the worst of both.
	w = f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.owner, "")
	if got := decode[map[string]any](t, w)["preview_image_id"]; got != nil {
		t.Errorf("preview_image_id is %v, want null — the rejected write landed anyway", got)
	}
}

// The trips list must include trips the user is a *member* of, not just the
// ones they own. TestTripListIsScopedToOwner in ownership_test.go covers the
// other half — a stranger sees nothing — and both matter: the list is the only
// route where authorization lives in the SQL rather than in the seam.
func TestTripListIncludesSharedTrips(t *testing.T) {
	f := setupRole(t, db.RoleViewer)

	// A trip of the actor's own, so the assertion distinguishes "the shared one
	// appears" from "everything appears".
	ownTripID := f.ts.createTrip(f.actor, "Actor's own trip")

	trips := decode[[]map[string]any](t, f.ts.do(http.MethodGet, "/api/trips", f.actor, ""))
	byID := map[string]map[string]any{}
	for _, tr := range trips {
		byID[tr["id"].(string)] = tr
	}
	if len(trips) != 2 {
		t.Fatalf("actor sees %d trip(s), want 2 (own + shared): %v", len(trips), trips)
	}

	shared, ok := byID[f.tripID]
	if !ok {
		t.Fatalf("the shared trip is missing from the actor's list: %v", trips)
	}
	if shared["role"] != "viewer" {
		t.Errorf("shared trip role is %v, want viewer", shared["role"])
	}
	owner, _ := shared["owner"].(map[string]any)
	if owner == nil || owner["username"] != "owner" {
		t.Errorf("shared trip owner is %v, want the owner's name so the card can say who shared it", shared["owner"])
	}

	own := byID[ownTripID]
	if own["role"] != "owner" {
		t.Errorf("own trip role is %v, want owner", own["role"])
	}
	// Omitted on your own trip: it would only tell you your own name, and the
	// client uses its presence as "this was shared with me".
	if own["owner"] != nil {
		t.Errorf("own trip carries owner %v, want null", own["owner"])
	}

	// The owner's own list must not have grown a duplicate from the LEFT JOIN.
	ownerTrips := decode[[]map[string]any](t, f.ts.do(http.MethodGet, "/api/trips", f.owner, ""))
	if len(ownerTrips) != 1 {
		t.Errorf("owner sees %d trip(s), want 1 — the membership join duplicated a row", len(ownerTrips))
	}
}

// Every single-trip response carries the reading user's own role, so the client
// can decide what to render instead of discovering it from a refused write.
func TestTripPayloadCarriesReaderRole(t *testing.T) {
	for _, role := range []db.TripRole{db.RoleViewer, db.RoleEditor, db.RoleOwner} {
		t.Run(string(role), func(t *testing.T) {
			f := setupRole(t, role)
			trip := decode[map[string]any](t, f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.actor, ""))
			if trip["role"] != string(role) {
				t.Errorf("GET trip as %s reports role %v", role, trip["role"])
			}
			if role == db.RoleOwner {
				if trip["owner"] != nil {
					t.Errorf("owner reading own trip gets owner=%v, want null", trip["owner"])
				}
				return
			}
			owner, _ := trip["owner"].(map[string]any)
			if owner == nil || owner["username"] != "owner" {
				t.Errorf("as %s, owner is %v, want the owner's name", role, trip["owner"])
			}
			// A label, not an identity: handing every collaborator the owner's
			// user id would disclose more than the feature needs.
			if _, leaked := owner["id"]; leaked {
				t.Errorf("owner block leaks a user id: %v", owner)
			}
		})
	}
}
