package httpapi

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"caravel/internal/db"
)

// Coverage for tags, added in Stage 26 Milestone 1: a free list of keywords on
// a location, written as part of the same nested request that carries its
// links and dates, and read back on the detail, on the list, and as a trip
// vocabulary for the editor to suggest.

type taggedItem struct {
	ID   string   `json:"id"`
	Tags []string `json:"tags"`
}

func createTagged(ts *testServer, cookie *http.Cookie, tripID, title, tagsJSON string) taggedItem {
	ts.t.Helper()
	body := `{"title":"` + title + `","category":"site","type":"","tags":` + tagsJSON + `}`
	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, body)
	if w.Code != http.StatusCreated {
		ts.t.Fatalf("create %s: got %d, want 201, body %s", title, w.Code, w.Body.String())
	}
	return decode[taggedItem](ts.t, w)
}

func TestItemTagsRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	created := createTagged(ts, cookie, tripID, "Hallgrimskirkja", `["reykjavik","church"]`)
	// Sorted by the query, not returned in submission order.
	if got := strings.Join(created.Tags, ","); got != "church,reykjavik" {
		t.Errorf("create response tags = %q, want church,reykjavik", got)
	}

	w := ts.do(http.MethodGet, "/api/items/"+created.ID, cookie, "")
	if got := strings.Join(decode[taggedItem](t, w).Tags, ","); got != "church,reykjavik" {
		t.Errorf("GET tags = %q, want church,reykjavik", got)
	}

	// Present replaces the set as a whole, like links.
	w = ts.do(http.MethodPatch, "/api/items/"+created.ID, cookie,
		`{"title":"Hallgrimskirkja","category":"site","type":"","tags":["landmark"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: got %d, body %s", w.Code, w.Body.String())
	}
	if got := strings.Join(decode[taggedItem](t, w).Tags, ","); got != "landmark" {
		t.Errorf("after replace, tags = %q, want landmark", got)
	}
}

// The absent/empty distinction the other nested blocks make, made here too: a
// client editing only the title must not silently drop the tags, and one that
// means to clear them needs a way to say so.
func TestItemTagsAbsentVersusEmpty(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	created := createTagged(ts, cookie, tripID, "Geysir", `["geothermal"]`)

	w := ts.do(http.MethodPatch, "/api/items/"+created.ID, cookie,
		`{"title":"Geysir","category":"site","type":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch without tags: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[taggedItem](t, w).Tags; len(got) != 1 || got[0] != "geothermal" {
		t.Errorf("omitting tags changed them: %v", got)
	}

	w = ts.do(http.MethodPatch, "/api/items/"+created.ID, cookie,
		`{"title":"Geysir","category":"site","type":"","tags":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch with empty tags: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[taggedItem](t, w).Tags; len(got) != 0 {
		t.Errorf("empty tags did not clear: %v", got)
	}
}

// Tags reach the list endpoint, which is what the locations tab filters on.
// That they arrive without a query per row is asserted separately, in
// TestListItemsLoadsTagsInOneQuery.
func TestListItemsCarriesTags(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	createTagged(ts, cookie, tripID, "Geysir", `["geothermal","south"]`)
	createTagged(ts, cookie, tripID, "Kirkjufell", `["mountain"]`)
	createTagged(ts, cookie, tripID, "Untagged", `[]`)

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, body %s", w.Code, w.Body.String())
	}
	got := decode[[]taggedItem](t, w)
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3", len(got))
	}
	byTags := map[string]string{}
	for _, it := range got {
		if it.Tags == nil {
			t.Errorf("item %s has null tags; the field must always be an array", it.ID)
		}
		byTags[strings.Join(it.Tags, ",")] = it.ID
	}
	for _, want := range []string{"geothermal,south", "mountain", ""} {
		if _, ok := byTags[want]; !ok {
			t.Errorf("no item carried tags %q; got %v", want, byTags)
		}
	}
}

func TestTripTagsVocabulary(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	other := ts.createTrip(cookie, "Norway")

	createTagged(ts, cookie, tripID, "Geysir", `["south","geothermal"]`)
	createTagged(ts, cookie, tripID, "Strokkur", `["geothermal"]`)
	createTagged(ts, cookie, other, "Bergen", `["fjord"]`)

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/tags", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list tags: got %d, body %s", w.Code, w.Body.String())
	}
	// Deduplicated across locations, sorted, and scoped to the trip -- a tag
	// on another trip is another vocabulary.
	if got := strings.Join(decode[[]string](t, w), ","); got != "geothermal,south" {
		t.Errorf("trip tags = %q, want geothermal,south", got)
	}
}

func TestItemTagsNormalizationAndLimits(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	// Padding trimmed, inner whitespace collapsed, empties dropped, and
	// Museum/museum deduplicated case-insensitively keeping the first
	// spelling -- so this set of six is three tags.
	created := createTagged(ts, cookie, tripID, "Mixed",
		`["  Museum ","museum","national    park","","   "]`)
	if got := strings.Join(created.Tags, "|"); got != "Museum|national park" {
		t.Errorf("normalized tags = %q, want Museum|national park", got)
	}

	long := `"` + strings.Repeat("a", 41) + `"`
	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie,
		`{"title":"Long","category":"site","type":"","tags":[`+long+`]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("41-character tag: got %d, want 400", w.Code)
	}

	many := make([]string, 21)
	for i := range many {
		many[i] = `"t` + string(rune('a'+i)) + `"`
	}
	w = ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie,
		`{"title":"Many","category":"site","type":"","tags":[`+strings.Join(many, ",")+`]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("21 tags: got %d, want 400", w.Code)
	}

	// 40 characters of multi-byte text is 40 characters, not 13: the limit
	// counts runes, so this must be accepted.
	w = ts.do(http.MethodPost, "/api/trips/"+tripID+"/items", cookie,
		`{"title":"Runes","category":"site","type":"","tags":["`+strings.Repeat("ü", 40)+`"]}`)
	if w.Code != http.StatusCreated {
		t.Errorf("40 multi-byte characters: got %d, want 201, body %s", w.Code, w.Body.String())
	}
}

// Deleting a location takes its tags with it, so the trip vocabulary shrinks
// and no row is left pointing at nothing.
func TestDeletingItemRemovesItsTags(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	created := createTagged(ts, cookie, tripID, "Doomed", `["temporary"]`)

	if w := ts.do(http.MethodDelete, "/api/items/"+created.ID, cookie, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", w.Code)
	}
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/tags", cookie, "")
	if got := decode[[]string](t, w); len(got) != 0 {
		t.Errorf("tags survived the location: %v", got)
	}
}

// countingTagStore records how often the per-location tag read is used, so the
// list endpoint can be held to the rule the backlog entry stated for dates and
// coordinates alike: one trip-wide query, bucketed in Go. Calling the by-item
// query per card is a query per location, and it is the kind of regression
// that is invisible until a trip is large.
type countingTagStore struct {
	db.Store
	byItem atomic.Int64
	byTrip atomic.Int64
}

func (s *countingTagStore) ListItemTagsByItem(ctx context.Context, itemID string) ([]string, error) {
	s.byItem.Add(1)
	return s.Store.ListItemTagsByItem(ctx, itemID)
}

func (s *countingTagStore) ListItemTagsByTrip(ctx context.Context, tripID string) ([]db.ItemTag, error) {
	s.byTrip.Add(1)
	return s.Store.ListItemTagsByTrip(ctx, tripID)
}

func TestListItemsLoadsTagsInOneQuery(t *testing.T) {
	var counter *countingTagStore
	ts := newTestServerWithStore(t, func(s db.Store) db.Store {
		counter = &countingTagStore{Store: s}
		return counter
	})
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	for _, name := range []string{"One", "Two", "Three", "Four", "Five"} {
		createTagged(ts, cookie, tripID, name, `["shared"]`)
	}

	counter.byItem.Store(0)
	counter.byTrip.Store(0)

	if w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/items", cookie, ""); w.Code != http.StatusOK {
		t.Fatalf("list: got %d, body %s", w.Code, w.Body.String())
	}
	if got := counter.byTrip.Load(); got != 1 {
		t.Errorf("trip-wide tag query ran %d times, want exactly 1", got)
	}
	if got := counter.byItem.Load(); got != 0 {
		t.Errorf("per-location tag query ran %d times for a 5-location list; the list must not use it", got)
	}
}
