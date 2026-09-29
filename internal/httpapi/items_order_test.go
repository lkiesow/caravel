package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// The order of the locations list is creation order, whichever path created
// the row. This is the regression migration 0012 is about: items.sort_order used to
// lead the ORDER BY, the batch endpoint wrote an increasing value and the
// single-item create every "New location" button in the app uses wrote 0, so a
// location added by hand sorted ahead of everything the assistant had added --
// landing in the middle of a list whose sort is called "As added".
func TestItemsListIsInCreationOrderAcrossBothCreatePaths(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	// Interleaved on purpose: neither path may claim a block of its own.
	ts.createItem(cookie, tripID, "First by hand")
	if w := ts.postBatch(cookie, tripID, batchBody(siteJSON("Then a batch"), siteJSON("And its sibling"))); w.Code != http.StatusCreated {
		t.Fatalf("batch status = %d, want 201, body %s", w.Code, w.Body.String())
	}
	ts.createItem(cookie, tripID, "Last by hand")

	want := []string{"First by hand", "Then a batch", "And its sibling", "Last by hand"}
	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != len(want) {
		t.Fatalf("listed %d locations, want %d", len(listed), len(want))
	}
	for i, title := range want {
		if listed[i]["title"] != title {
			t.Errorf("position %d = %v, want %q", i, listed[i]["title"], title)
		}
	}
}

// Editing a location does not move it. The list is ordered by created_at, so
// this is really a guard against a future reordering by updated_at -- which
// would be a defensible list to want, but not the one called "As added".
func TestUpdatingAnItemDoesNotMoveItInTheList(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	first := ts.createItem(cookie, tripID, "First")
	ts.createItem(cookie, tripID, "Second")

	body := `{"title":"First, renamed","category":"site"}`
	if w := ts.do(http.MethodPatch, "/api/items/"+first, cookie, body); w.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200, body %s", w.Code, w.Body.String())
	}

	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 2 || listed[0]["title"] != "First, renamed" {
		t.Fatalf("list = %v, want the edited location still first", listed)
	}
}

// sort_order is gone from the API surface as well as the table, and readJSON
// refuses unknown fields -- so a caller still sending it gets a 400 rather
// than silently having it ignored. Asserted because it is the one externally
// visible break in this change.
func TestCreateItemRejectsSortOrder(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	body := `{"title":"Kirkjufell","category":"site","sort_order":3}`
	if w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, body); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
	}
}

// Many locations in one batch keep their request order. This is the case that
// decided how the column was removed rather than fixed: every row in one
// transaction lands in the same millisecond, so the ordering rests entirely on
// created_at being stored in a layout that sorts inside a second.
func TestItemsListKeepsOrderWithinOneTransaction(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	var bodies []string
	for i := range 20 {
		bodies = append(bodies, siteJSON(fmt.Sprintf("Place %02d", i)))
	}
	if w := ts.postBatch(cookie, tripID, batchBody(bodies...)); w.Code != http.StatusCreated {
		t.Fatalf("batch status = %d, want 201, body %s", w.Code, w.Body.String())
	}

	listed := decode[[]map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""))
	if len(listed) != 20 {
		t.Fatalf("listed %d locations, want 20", len(listed))
	}
	for i, item := range listed {
		want := fmt.Sprintf("Place %02d", i)
		if item["title"] != want {
			t.Fatalf("position %d = %v, want %q", i, item["title"], want)
		}
	}
}
