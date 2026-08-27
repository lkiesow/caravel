package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"caravel/internal/assist"
	"caravel/internal/db"
	"caravel/internal/wikimedia"
)

// The image-search endpoint: who may ask, what it answers with nothing
// configured, and what it does when one of its two sources falls over.

type fakeImageSearcher struct {
	name    string
	results []assist.ImageResult
	err     error
}

func (f fakeImageSearcher) Name() string { return f.name }
func (f fakeImageSearcher) Search(context.Context, string) ([]assist.SearchResult, error) {
	return nil, nil
}
func (f fakeImageSearcher) SearchImages(context.Context, string) ([]assist.ImageResult, error) {
	return f.results, f.err
}

// textOnlySearcher is a Searcher that is not an ImageSearcher -- what Ollama
// Cloud is, and the case the type assertion in NewServer exists for.
type textOnlySearcher struct{}

func (textOnlySearcher) Name() string { return "textonly" }
func (textOnlySearcher) Search(context.Context, string) ([]assist.SearchResult, error) {
	return nil, nil
}

func imageSearchServer(t *testing.T, searcher assist.Searcher) *testServer {
	t.Helper()
	return newTestServerWithOptions(t, func(o *Options) {
		o.Wikimedia = wikimedia.New(wikimedia.StubURL)
		o.Searcher = searcher
	})
}

func TestImageSearchAnswersFromBothSources(t *testing.T) {
	ts := imageSearchServer(t, fakeImageSearcher{name: "serper", results: []assist.ImageResult{
		{Title: "A hotel", URL: "https://site.example/a.jpg", SourceURL: "https://site.example/"},
	}})
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	rec := ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q=stub", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decode[imageSearchResponse](t, rec)
	if len(got.Groups) != 2 {
		t.Fatalf("got %d groups, want Wikipedia and the web search: %+v", len(got.Groups), got.Groups)
	}
	if got.Groups[0].Source != "wikipedia" || got.Groups[1].Source != "serper" {
		t.Errorf("groups are %q and %q", got.Groups[0].Source, got.Groups[1].Source)
	}

	// The difference the grouping exists for: Wikipedia results can be
	// credited and web results cannot, and neither may pretend otherwise.
	for _, r := range got.Groups[0].Results {
		if r.License == "" || r.Credit == "" || r.SourceURL == "" {
			t.Errorf("a Wikipedia result arrived uncreditable: %+v", r)
		}
	}
	for _, r := range got.Groups[1].Results {
		if r.License != "" || r.Credit != "" {
			t.Errorf("a web search result claims a licence it cannot know: %+v", r)
		}
		if r.SourceURL == "" {
			t.Errorf("a web search result does not say where it was found: %+v", r)
		}
	}
}

// Either half alone is a working control. This is the one that matters most:
// it is what a stock instance with nothing configured has.
func TestImageSearchWorksWithNoSearchProviderAtAll(t *testing.T) {
	ts := imageSearchServer(t, nil)
	if ts.ImageSearch != nil {
		t.Fatal("no provider was configured, but an image searcher was built")
	}
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	got := decode[imageSearchResponse](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q=stub", cookie, ""))
	if len(got.Groups) != 1 || got.Groups[0].Source != "wikipedia" || len(got.Groups[0].Results) == 0 {
		t.Errorf("got %+v, want the Wikipedia group alone", got.Groups)
	}
}

// A backend that cannot search for images contributes nothing and takes
// nothing down with it.
func TestASearcherThatCannotDoImagesIsSimplyNotUsed(t *testing.T) {
	ts := imageSearchServer(t, textOnlySearcher{})
	if ts.ImageSearch != nil {
		t.Fatal("a text-only Searcher was accepted as an ImageSearcher")
	}
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	got := decode[imageSearchResponse](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q=stub", cookie, ""))
	if len(got.Groups) != 1 || got.Groups[0].Source != "wikipedia" {
		t.Errorf("got %+v, want the Wikipedia group alone", got.Groups)
	}
}

// One source failing is not the request failing.
func TestImageSearchSurvivesOneSourceFailing(t *testing.T) {
	ts := imageSearchServer(t, fakeImageSearcher{name: "serper", err: errors.New("out of credit")})
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	rec := ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q=stub", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decode[imageSearchResponse](t, rec)
	if len(got.Groups) != 1 || got.Groups[0].Source != "wikipedia" {
		t.Errorf("got %+v, want the half that worked", got.Groups)
	}
}

func TestImageSearchIsRefusedWhenNothingCanAnswer(t *testing.T) {
	ts := newTestServer(t) // no Wikimedia client, no searcher
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	rec := ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q=stub", cookie, "")
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
	// And /auth/me says so, which is what actually keeps the control off the
	// page. The 501 is a backstop.
	if ts.capability(cookie, "image_search") {
		t.Error("/auth/me advertised image search with nothing configured")
	}
}

func TestImageSearchReportsItsCapability(t *testing.T) {
	ts := imageSearchServer(t, nil)
	if !ts.capability(ts.login("alice"), "image_search") {
		t.Error("/auth/me reported image search off with Wikipedia available")
	}
}

func TestImageSearchNeedsAQueryWorthSending(t *testing.T) {
	ts := imageSearchServer(t, nil)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	for _, q := range []string{"", "a", "%20"} {
		rec := ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q="+q, cookie, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q gave %d, want 400", q, rec.Code)
		}
	}
}

// A viewer may not spend the instance owner's search quota, and this is an
// edit in any case.
func TestImageSearchIsForEditors(t *testing.T) {
	for _, tc := range []struct {
		role db.TripRole
		want int
	}{
		{db.RoleViewer, http.StatusForbidden},
		{db.RoleEditor, http.StatusOK},
	} {
		f := setupRole(t, tc.role)
		f.ts.Wikimedia = wikimedia.New(wikimedia.StubURL)
		rec := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/image-search?q=stub", f.actor, "")
		if rec.Code != tc.want {
			t.Errorf("%s got %d, want %d (%s)", tc.role, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestImageSearchIsRateLimited(t *testing.T) {
	ts := imageSearchServer(t, nil)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Japan")

	// The limiter is the thing standing between one impatient person and a
	// metered API, so it is asserted rather than assumed.
	var lastCode int
	for range 12 {
		lastCode = ts.do(http.MethodGet, "/api/trips/"+tripID+"/image-search?q=stub", cookie, "").Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("after twelve searches the status was %d, want 429", lastCode)
	}
}

// capability reads one flat capability flag off /auth/me.
func (ts *testServer) capability(cookie *http.Cookie, name string) bool {
	ts.t.Helper()
	rec := ts.do(http.MethodGet, "/api/auth/me", cookie, "")
	if rec.Code != http.StatusOK {
		ts.t.Fatalf("/auth/me status = %d", rec.Code)
	}
	// Reads through the nested object rather than the top level: the three
	// server capabilities moved under "capabilities" in Stage 22, and a helper
	// still looking at the top level would report every capability as absent
	// while passing its own type check.
	caps, _ := decode[map[string]any](ts.t, rec)["capabilities"].(map[string]any)
	return caps[name] == true
}
