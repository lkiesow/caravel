package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"caravel/internal/db"
)

// Checklist visibility has three states because a list can be *ticked*, which a
// file cannot:
//
//	personal  only its author sees it
//	trip      everyone sees it, only its author ticks it
//	shared    everyone sees it and ticks it
//
// The middle one is the whole reason this is not the files model with a third
// name: seeing a list and being able to change it are different permissions.

// makeList creates a checklist with a visibility and returns its id.
func (f *roleFixture) makeList(t *testing.T, as *http.Cookie, title, visibility string) string {
	t.Helper()
	body := `{"title":"` + title + `"`
	if visibility != "" {
		body += `,"visibility":"` + visibility + `"`
	}
	body += `}`
	w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/checklists", as, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %s list: got %d, body %s", visibility, w.Code, w.Body.String())
	}
	created := decode[map[string]any](t, w)
	if visibility != "" && created["visibility"] != visibility {
		t.Fatalf("created with visibility %v, wanted %q", created["visibility"], visibility)
	}
	return created["id"].(string)
}

func (f *roleFixture) listTitles(t *testing.T, as *http.Cookie) []string {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/checklists", as, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list checklists: got %d, body %s", w.Code, w.Body.String())
	}
	out := []string{}
	for _, c := range decode[[]map[string]any](t, w) {
		out = append(out, c["title"].(string))
	}
	return out
}

// addItem puts one item on a list and returns its id.
func (f *roleFixture) addItem(t *testing.T, as *http.Cookie, listID, text string) string {
	t.Helper()
	return f.ts.mustCreate(http.MethodPost, "/api/checklists/"+listID+"/items", as,
		`{"text":"`+text+`"}`, http.StatusCreated)
}

// The default is shared — the opposite of the files default, and deliberately:
// a checklist is usually a job being done together.
func TestChecklistVisibilityDefault(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	for _, tc := range []struct{ name, value string }{
		{"omitted", ""},
		{"nonsense", "secret"},
		{"personal-ish typo", "privat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"title":"List"}`
			if tc.value != "" {
				body = `{"title":"List","visibility":"` + tc.value + `"}`
			}
			w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/checklists", f.actor, body)
			if w.Code != http.StatusCreated {
				t.Fatalf("got %d, body %s", w.Code, w.Body.String())
			}
			if got := decode[map[string]any](t, w)["visibility"]; got != "shared" {
				t.Errorf("visibility is %v, want shared", got)
			}
		})
	}
}

// A personal list is invisible to everyone else, the trip owner included.
func TestPersonalChecklistIsInvisibleToOthers(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	mine := f.makeList(t, f.actor, "My own packing", "personal")
	theirs := f.makeList(t, f.owner, "Owner private", "personal")
	shared := f.makeList(t, f.actor, "Group shopping", "shared")

	if got := f.listTitles(t, f.actor); !contains(got, "My own packing") || contains(got, "Owner private") {
		t.Errorf("actor sees %v", got)
	}
	if got := f.listTitles(t, f.owner); !contains(got, "Owner private") || contains(got, "My own packing") {
		t.Errorf("owner sees %v — the trip owner is not exempt", got)
	}
	if got := f.listTitles(t, f.owner); !contains(got, "Group shopping") {
		t.Errorf("owner cannot see a shared list: %v", got)
	}

	// Having the id is not access.
	itemID := f.addItem(t, f.actor, mine, "socks")
	for _, req := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/checklists/" + mine, `{"title":"stolen"}`},
		{http.MethodDelete, "/api/checklists/" + mine, ""},
		{http.MethodPut, "/api/checklists/" + mine + "/visibility", `{"visibility":"shared"}`},
		{http.MethodPost, "/api/checklists/" + mine + "/items", `{"text":"x"}`},
		{http.MethodPatch, "/api/checklists/" + mine + "/items/" + itemID, `{"checked":true}`},
		{http.MethodPut, "/api/checklists/" + mine + "/items/" + itemID + "/text", `{"text":"x"}`},
		{http.MethodDelete, "/api/checklists/" + mine + "/items/" + itemID, ""},
	} {
		if w := f.ts.do(req.method, req.path, f.owner, req.body); w.Code != http.StatusNotFound {
			t.Errorf("owner %s %s: got %d, want 404 — body %s", req.method, req.path, w.Code, w.Body.String())
		}
	}
	_ = theirs
	_ = shared
}

// The middle state is what the three-value model is for: everyone reads it,
// only its author changes it.
func TestTripVisibleChecklistIsReadOnlyToOthers(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	list := f.makeList(t, f.actor, "Actor plan", "trip")
	itemID := f.addItem(t, f.actor, list, "book ferry")

	// The owner can see it...
	if got := f.listTitles(t, f.owner); !contains(got, "Actor plan") {
		t.Fatalf("owner cannot see a trip-visible list: %v", got)
	}
	// ...and can change nothing on it. 403, not 404: they can read it and know
	// it is there.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/checklists/" + list, `{"title":"stolen"}`},
		{http.MethodDelete, "/api/checklists/" + list, ""},
		{http.MethodPost, "/api/checklists/" + list + "/items", `{"text":"x"}`},
		{http.MethodPatch, "/api/checklists/" + list + "/items/" + itemID, `{"checked":true}`},
		{http.MethodPut, "/api/checklists/" + list + "/items/" + itemID + "/text", `{"text":"x"}`},
		{http.MethodDelete, "/api/checklists/" + list + "/items/" + itemID, ""},
	} {
		w := f.ts.do(req.method, req.path, f.owner, req.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("owner %s %s: got %d, want 403 — body %s", req.method, req.path, w.Code, w.Body.String())
		}
	}

	// The author can do all of it.
	if w := f.ts.do(http.MethodPatch, "/api/checklists/"+list+"/items/"+itemID, f.actor, `{"checked":true}`); w.Code != http.StatusOK {
		t.Errorf("author ticking their own trip-visible list: got %d", w.Code)
	}

	// can_tick tells the client which side of that line it is on, since it
	// cannot deduce it from visibility alone.
	forOwner := decode[[]map[string]any](t, f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/checklists", f.owner, ""))
	for _, c := range forOwner {
		if c["title"] == "Actor plan" {
			if c["can_tick"] != false || c["is_mine"] != false {
				t.Errorf("owner sees can_tick=%v is_mine=%v on someone else's trip list", c["can_tick"], c["is_mine"])
			}
		}
	}
	forActor := decode[[]map[string]any](t, f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/checklists", f.actor, ""))
	for _, c := range forActor {
		if c["title"] == "Actor plan" && (c["can_tick"] != true || c["is_mine"] != true) {
			t.Errorf("author sees can_tick=%v is_mine=%v on their own list", c["can_tick"], c["is_mine"])
		}
	}
}

// A shared list is everyone's to use — that is the difference from trip-visible,
// and the case the packing list wants.
func TestSharedChecklistIsWritableByEditors(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	list := f.makeList(t, f.owner, "Group packing", "shared")
	itemID := f.addItem(t, f.owner, list, "passports")

	for _, req := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"tick", http.MethodPatch, "/api/checklists/" + list + "/items/" + itemID, `{"checked":true}`, http.StatusOK},
		{"add an item", http.MethodPost, "/api/checklists/" + list + "/items", `{"text":"sun cream"}`, http.StatusCreated},
		{"reword an item", http.MethodPut, "/api/checklists/" + list + "/items/" + itemID + "/text", `{"text":"passports x2"}`, http.StatusOK},
		{"rename the list", http.MethodPatch, "/api/checklists/" + list, `{"title":"Group packing v2"}`, http.StatusOK},
	} {
		t.Run(req.name, func(t *testing.T) {
			w := f.ts.do(req.method, req.path, f.actor, req.body)
			if w.Code != req.want {
				t.Errorf("editor %s on a shared list: got %d, want %d — body %s", req.name, w.Code, req.want, w.Body.String())
			}
		})
	}

	// But changing who sees it stays with whoever shared it.
	w := f.ts.do(http.MethodPut, "/api/checklists/"+list+"/visibility", f.actor, `{"visibility":"personal"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("editor changing a shared list's visibility: got %d, want 403", w.Code)
	}
	if got := decode[map[string]any](t, w)["code"]; got != "not_checklist_owner" {
		t.Errorf("code is %v, want not_checklist_owner", got)
	}
}

// Moving between all three states, and that each move takes effect.
func TestChecklistVisibilityTransitions(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	list := f.makeList(t, f.actor, "Moving list", "shared")
	itemID := f.addItem(t, f.actor, list, "thing")

	set := func(v string) *httptest.ResponseRecorder {
		return f.ts.do(http.MethodPut, "/api/checklists/"+list+"/visibility", f.actor, `{"visibility":"`+v+`"}`)
	}

	// shared -> trip: the owner can still see it but no longer tick it.
	if w := set("trip"); w.Code != http.StatusOK {
		t.Fatalf("shared to trip: got %d, body %s", w.Code, w.Body.String())
	}
	if got := f.listTitles(t, f.owner); !contains(got, "Moving list") {
		t.Error("owner lost sight of a trip-visible list")
	}
	if w := f.ts.do(http.MethodPatch, "/api/checklists/"+list+"/items/"+itemID, f.owner, `{"checked":true}`); w.Code != http.StatusForbidden {
		t.Errorf("owner ticking a trip-visible list: got %d, want 403", w.Code)
	}

	// trip -> personal: the owner cannot see it at all.
	if w := set("personal"); w.Code != http.StatusOK {
		t.Fatalf("trip to personal: got %d", w.Code)
	}
	if got := f.listTitles(t, f.owner); contains(got, "Moving list") {
		t.Errorf("owner still sees a personal list: %v", got)
	}

	// personal -> shared: back to everyone.
	if w := set("shared"); w.Code != http.StatusOK {
		t.Fatalf("personal to shared: got %d", w.Code)
	}
	if got := f.listTitles(t, f.owner); !contains(got, "Moving list") {
		t.Error("owner cannot see a re-shared list")
	}
	if w := f.ts.do(http.MethodPatch, "/api/checklists/"+list+"/items/"+itemID, f.owner, `{"checked":true}`); w.Code != http.StatusOK {
		t.Errorf("owner ticking a re-shared list: got %d", w.Code)
	}

	if w := set("nonsense"); w.Code != http.StatusBadRequest {
		t.Errorf("nonsense visibility: got %d, want 400", w.Code)
	}
}

// Renaming a list and rewording an item, which had no endpoints at all before
// this milestone: both were write-once, so fixing a typo meant deleting and
// retyping.
func TestChecklistRenameAndItemEdit(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	list := f.makeList(t, f.actor, "Pakcing", "shared")
	itemID := f.addItem(t, f.actor, list, "sokcs")

	w := f.ts.do(http.MethodPatch, "/api/checklists/"+list, f.actor, `{"title":"  Packing  "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["title"]; got != "Packing" {
		t.Errorf("title is %v, want it trimmed to Packing", got)
	}
	// The items come back with it, so the client can re-render from one response.
	if items, ok := decode[map[string]any](t, w)["items"].([]any); !ok || len(items) != 1 {
		t.Errorf("rename response carries %v items, want 1", len(items))
	}

	w = f.ts.do(http.MethodPut, "/api/checklists/"+list+"/items/"+itemID+"/text", f.actor, `{"text":"socks"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("reword: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["text"]; got != "socks" {
		t.Errorf("text is %v", got)
	}
	// Rewording must not disturb the checked state — they are separate routes
	// for exactly this reason.
	if w := f.ts.do(http.MethodPatch, "/api/checklists/"+list+"/items/"+itemID, f.actor, `{"checked":true}`); w.Code != http.StatusOK {
		t.Fatal("tick failed")
	}
	w = f.ts.do(http.MethodPut, "/api/checklists/"+list+"/items/"+itemID+"/text", f.actor, `{"text":"socks x3"}`)
	if got := decode[map[string]any](t, w)["checked"]; got != true {
		t.Errorf("rewording cleared the checked state: checked=%v", got)
	}

	for _, tc := range []struct{ path, body string }{
		{"/api/checklists/" + list, `{"title":"   "}`},
		{"/api/checklists/" + list + "/items/" + itemID + "/text", `{"text":""}`},
	} {
		method := http.MethodPatch
		if tc.path != "/api/checklists/"+list {
			method = http.MethodPut
		}
		if w := f.ts.do(method, tc.path, f.actor, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s with blank text: got %d, want 400", method, tc.path, w.Code)
		}
	}
}

// Removing a member takes their personal lists as well as their personal files.
func TestRemovingMemberDeletesTheirPersonalChecklists(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	actorID := f.members(t, f.owner)["actor"]["user_id"].(string)

	f.makeList(t, f.actor, "Actor private list", "personal")
	f.makeList(t, f.actor, "Actor shared list", "shared")

	// The count in the confirmation covers files and lists together, since the
	// question it asks is one question.
	if got := f.members(t, f.owner)["actor"]["personal_file_count"]; got != float64(1) {
		t.Errorf("personal_file_count is %v, want 1", got)
	}

	if w := f.ts.do(http.MethodDelete, "/api/trips/"+f.tripID+"/members/"+actorID, f.owner, ""); w.Code != http.StatusNoContent {
		t.Fatalf("remove member: got %d, body %s", w.Code, w.Body.String())
	}
	// Re-added, because checking from the owner side proves nothing: they never
	// saw the personal list either way.
	f.ts.mustCreateNoID(http.MethodPost, "/api/trips/"+f.tripID+"/members", f.owner,
		`{"username":"actor","role":"editor"}`, http.StatusCreated)

	got := f.listTitles(t, f.actor)
	if contains(got, "Actor private list") {
		t.Errorf("the personal list survived the removal: %v", got)
	}
	if !contains(got, "Actor shared list") {
		t.Errorf("a shared list was deleted along with the membership: %v", got)
	}
}
