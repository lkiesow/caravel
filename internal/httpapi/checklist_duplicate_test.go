package httpapi

import (
	"net/http"
	"testing"

	"caravel/internal/db"
)

// Duplicating a checklist is the reuse case: last year's packing list, minus
// the ticks. The interesting part is not the copying, it is that the
// authorization rule here is the *read* rule and not the write one - the list
// most worth copying is somebody else's.

// tick marks one item on a list, so a duplicate has a checked state to reset.
func (f *roleFixture) tick(t *testing.T, as *http.Cookie, listID, itemID string) {
	t.Helper()
	w := f.ts.do(http.MethodPatch, "/api/checklists/"+listID+"/items/"+itemID, as, `{"checked":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("tick item: got %d, body %s", w.Code, w.Body.String())
	}
}

// getList finds one checklist in the trip listing, since there is no
// GET /api/checklists/{id} to ask directly.
func (f *roleFixture) getList(t *testing.T, as *http.Cookie, listID string) map[string]any {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/checklists", as, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list checklists: got %d, body %s", w.Code, w.Body.String())
	}
	for _, c := range decode[[]map[string]any](t, w) {
		if c["id"] == listID {
			return c
		}
	}
	t.Fatalf("checklist %s not in the listing", listID)
	return nil
}

// checkedStates reads the items of a checklist response in order.
func checkedStates(t *testing.T, list map[string]any) (texts []string, checked []bool) {
	t.Helper()
	items, ok := list["items"].([]any)
	if !ok {
		t.Fatalf("checklist has no items array: %v", list["items"])
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		texts = append(texts, item["text"].(string))
		checked = append(checked, item["checked"].(bool))
	}
	return texts, checked
}

// The headline case: an editor copies a shared list they did not write. This is
// the one requireChecklistWrite would have allowed anyway; the next test is the
// one that proves the read rule is really the rule in force.
func TestDuplicateChecklistResetsTicks(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	sourceID := f.makeList(t, f.owner, "Packing", "shared")
	socks := f.addItem(t, f.owner, sourceID, "Socks")
	f.addItem(t, f.owner, sourceID, "Passport")
	f.tick(t, f.owner, sourceID, socks)

	w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor,
		`{"title":"Packing (copy)"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate: got %d, body %s", w.Code, w.Body.String())
	}
	copied := decode[map[string]any](t, w)

	if copied["id"] == sourceID {
		t.Fatal("the copy reused the source id")
	}
	if copied["title"] != "Packing (copy)" {
		t.Errorf("title is %v, want the one the client sent", copied["title"])
	}
	// Whoever made the original, the copy is the duplicator's - otherwise
	// copying a list would produce one they cannot tick.
	if copied["is_mine"] != true || copied["can_tick"] != true {
		t.Errorf("is_mine=%v can_tick=%v, want both true: the copy belongs to whoever made it",
			copied["is_mine"], copied["can_tick"])
	}
	if copied["visibility"] != "shared" {
		t.Errorf("visibility is %v, want shared to carry over unchanged", copied["visibility"])
	}

	texts, checked := checkedStates(t, copied)
	if len(texts) != 2 || texts[0] != "Socks" || texts[1] != "Passport" {
		t.Errorf("copied items are %v, want both in source order", texts)
	}
	for i, c := range checked {
		if c {
			t.Errorf("copied item %d (%s) is ticked; a duplicate resets them", i, texts[i])
		}
	}

	// And the source is untouched - in particular still ticked, which is the
	// half a "reset" could plausibly have got wrong by ticking the wrong row.
	_, sourceChecked := checkedStates(t, f.getList(t, f.owner, sourceID))
	if len(sourceChecked) != 2 || !sourceChecked[0] || sourceChecked[1] {
		t.Errorf("source checked states are %v, want [true false] unchanged", sourceChecked)
	}
}

// The rule that matters: an editor may copy a trip-visible list written by
// somebody else, even though they may not tick or rename it. Calling
// requireChecklistWrite in the handler would make this 403 - so this test is
// what pins the read rule in place.
func TestDuplicateChecklistSomeoneElsesTripVisibleList(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	sourceID := f.makeList(t, f.owner, "Route plan", "trip")
	f.addItem(t, f.owner, sourceID, "Ring road clockwise")

	// Established first, so the test states plainly what the actor cannot do to
	// this list. If renaming ever starts succeeding, the premise is gone.
	if w := f.ts.do(http.MethodPatch, "/api/checklists/"+sourceID, f.actor, `{"title":"Mine now"}`); w.Code != http.StatusForbidden {
		t.Fatalf("renaming someone else's trip-visible list: got %d, want 403", w.Code)
	}

	w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor,
		`{"title":"Route plan (copy)"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate: got %d, body %s — copying a list you can read is a create, not a write to it",
			w.Code, w.Body.String())
	}
	copied := decode[map[string]any](t, w)
	if copied["is_mine"] != true || copied["can_tick"] != true {
		t.Errorf("is_mine=%v can_tick=%v, want both true", copied["is_mine"], copied["can_tick"])
	}
	if copied["visibility"] != "trip" {
		t.Errorf("visibility is %v, want trip", copied["visibility"])
	}
	texts, _ := checkedStates(t, copied)
	if len(texts) != 1 || texts[0] != "Ring road clockwise" {
		t.Errorf("copied items are %v", texts)
	}
}

// A personal list is the duplicator's own, and stays personal.
func TestDuplicateChecklistOwnPersonalList(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	sourceID := f.makeList(t, f.actor, "My own packing", "personal")
	f.addItem(t, f.actor, sourceID, "Earplugs")

	w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor,
		`{"title":"My own packing (copy)"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate: got %d, body %s", w.Code, w.Body.String())
	}
	copied := decode[map[string]any](t, w)
	if copied["visibility"] != "personal" {
		t.Errorf("visibility is %v, want personal: copying is not how you change who sees a list",
			copied["visibility"])
	}
	// And it is still invisible to the trip owner, which is the whole point of
	// personal - a copy must not be the way one leaks.
	for _, title := range f.listTitles(t, f.owner) {
		if title == "My own packing (copy)" {
			t.Error("the copy of a personal list is visible to the trip owner")
		}
	}
}

// Somebody else's personal list answers 404, not 403: loadChecklist refuses it
// before the handler runs, and the point of a personal list is that other
// people do not learn it exists.
func TestDuplicateChecklistSomeoneElsesPersonalList(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	sourceID := f.makeList(t, f.owner, "Owner's private list", "personal")

	w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor,
		`{"title":"Nice try (copy)"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 — a personal list must not be reachable by id", w.Code)
	}
}

// A viewer may read a shared list and may not copy it: the copy would be a new
// row on a trip they have no write access to.
func TestDuplicateChecklistViewerRefused(t *testing.T) {
	f := setupRole(t, db.RoleViewer)

	sourceID := f.makeList(t, f.owner, "Packing", "shared")

	w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor,
		`{"title":"Packing (copy)"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
	// And nothing was created on the way to being refused.
	if titles := f.listTitles(t, f.owner); len(titles) != 2 {
		t.Errorf("trip has lists %v, want only the fixture's and the source", titles)
	}
}

// The title comes from the client, because "(copy)" is translated copy and the
// server has none. So an empty one is a 400 rather than a silent fallback.
func TestDuplicateChecklistRequiresTitle(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	sourceID := f.makeList(t, f.owner, "Packing", "shared")

	for _, body := range []string{`{}`, `{"title":""}`, `{"title":"   "}`, `not json`} {
		w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q: got %d, want 400", body, w.Code)
		}
	}
}

// The copy goes to the end of the trip's lists, the same rule create uses -
// otherwise two lists share a sort_order and their order is a tie.
func TestDuplicateChecklistSortOrder(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	sourceID := f.makeList(t, f.actor, "Packing", "shared")

	w := f.ts.do(http.MethodPost, "/api/checklists/"+sourceID+"/duplicate", f.actor,
		`{"title":"Packing (copy)"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate: got %d, body %s", w.Code, w.Body.String())
	}
	copied := decode[map[string]any](t, w)

	// The fixture's own "Packing" list is 0, the source is 1, so the copy is 2.
	source := f.getList(t, f.actor, sourceID)
	if copied["sort_order"] == source["sort_order"] {
		t.Errorf("copy and source share sort_order %v", copied["sort_order"])
	}
	if got := copied["sort_order"].(float64); got != 2 {
		t.Errorf("sort_order is %v, want 2 (after the fixture list and the source)", got)
	}
}
