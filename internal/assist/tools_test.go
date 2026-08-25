package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"caravel/internal/geocode"
)

func callTo(name, args string) toolCall {
	var c toolCall
	c.ID = "call_" + name
	c.Type = "function"
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

// Only offering tools that can work. Describing web search to a model with no
// search backend produces a run that calls it, gets an error and wastes a turn
// discovering what the config already knew.
func TestToolDefinitionsFollowWhatIsConfigured(t *testing.T) {
	names := func(defs []toolDef) []string {
		var out []string
		for _, d := range defs {
			out = append(out, d.Name)
		}
		return out
	}

	// Four when everything is configured: the three that do work, plus propose,
	// which ends the run and is always offered.
	full := newToolset(&stubSearcher{}, newPageFetcher(), geocode.New("http://example.invalid/search"), nil, nil)
	if got := names(full.definitions()); len(got) != 4 {
		t.Errorf("definitions = %v, want all four", got)
	}

	// propose is not optional: without it there is no way to end a run in one
	// request, and the loop falls back to a second one every time.
	for _, ts := range []*toolset{
		full,
		newToolset(nil, newPageFetcher(), nil, nil, nil),
	} {
		if !slices.Contains(names(ts.definitions()), toolPropose) {
			t.Errorf("propose was not offered: %v", names(ts.definitions()))
		}
	}

	noSearch := newToolset(nil, newPageFetcher(), geocode.New("http://example.invalid/search"), nil, nil)
	for _, n := range names(noSearch.definitions()) {
		if n == toolWebSearch {
			t.Error("web search was offered with no search backend configured")
		}
	}

	noGeo := newToolset(&stubSearcher{}, newPageFetcher(), nil, nil, nil)
	for _, n := range names(noGeo.definitions()) {
		if n == toolGeocode {
			t.Error("geocoding was offered with no geocoder configured")
		}
	}

	// fetch_page needs no configuration, so it is always there -- and the
	// agent is still useful with it alone.
	if got := names(noGeo.definitions()); len(got) == 0 {
		t.Error("no tools at all were offered")
	}
}

func TestToolDefinitionSchemasAreValidJSON(t *testing.T) {
	// Hand-written literals, so a stray comma reaches a real provider as an
	// opaque 400 unless something local catches it first.
	ts := newToolset(&stubSearcher{}, newPageFetcher(), geocode.New("http://example.invalid/search"), nil, nil)
	for _, d := range ts.definitions() {
		var parsed map[string]any
		if err := json.Unmarshal(d.Parameters, &parsed); err != nil {
			t.Errorf("%s parameters are not valid JSON: %v", d.Name, err)
		}
		if d.Description == "" {
			t.Errorf("%s has no description; the model chooses tools by it", d.Name)
		}
	}
}

func TestDispatchSearch(t *testing.T) {
	ts := newToolset(&stubSearcher{}, newPageFetcher(), nil, nil, nil)
	out := ts.dispatch(context.Background(), callTo(toolWebSearch, `{"query":"Kex Hostel"}`))

	if !strings.Contains(out, "Kex Hostel") || !strings.Contains(out, "https://example.invalid/kex") {
		t.Errorf("result = %q, want titles and URLs", out)
	}
	// The query reaching the backend is what a UI test can then assert on.
	if !strings.Contains(out, "Searched for: Kex Hostel") {
		t.Errorf("result = %q, want the query to have reached the searcher", out)
	}
}

// The central decision in the dispatcher: a tool failure is information for
// the model, not a reason to abandon a paid run. Every dead link on the web
// would otherwise be a failed enrichment.
func TestDispatchTurnsFailuresIntoTextForTheModel(t *testing.T) {
	ts := newToolset(&failingSearcher{}, newPageFetcher(), nil, nil, nil)

	cases := []struct {
		name string
		call toolCall
		want string
	}{
		{"a backend that errors", callTo(toolWebSearch, `{"query":"x"}`), "did not work"},
		{"arguments that are not JSON", callTo(toolWebSearch, `not json`), "not valid JSON"},
		{"an empty query", callTo(toolWebSearch, `{"query":"  "}`), "empty"},
		{"a blocked URL", callTo(toolFetchPage, `{"url":"http://169.254.169.254/"}`), "link-local"},
		{"a tool that does not exist", callTo("summon_daemon", `{}`), "no tool called"},
		{"a tool that is not configured", callTo(toolGeocode, `{"query":"x"}`), "no tool called"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ts.dispatch(context.Background(), tc.call)
			if out == "" {
				t.Fatal("dispatch returned nothing; the model needs something to react to")
			}
			if !strings.Contains(strings.ToLower(out), strings.ToLower(tc.want)) {
				t.Errorf("result = %q, want it to mention %q", out, tc.want)
			}
		})
	}
}

type failingSearcher struct{}

func (*failingSearcher) Name() string { return "failing" }
func (*failingSearcher) Search(context.Context, string) ([]SearchResult, error) {
	return nil, errors.New("the backend is down")
}

// The model must never see coordinates: showing them invites it to copy one
// into the answer, which is exactly what the design forbids.
func TestDispatchGeocodeReturnsAddressesWithoutCoordinates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"display_name":"Skulagata 28, Reykjavik","lat":"64.1466","lon":"-21.9426"}]`)
	}))
	defer upstream.Close()

	ts := newToolset(nil, newPageFetcher(), geocode.New(upstream.URL), nil, nil)
	out := ts.dispatch(context.Background(), callTo(toolGeocode, `{"query":"Kex Hostel"}`))

	if !strings.Contains(out, "Skulagata 28") {
		t.Errorf("result = %q, want the formatted address", out)
	}
	for _, coord := range []string{"64.1466", "-21.9426", "64.14", "-21.94"} {
		if strings.Contains(out, coord) {
			t.Errorf("result = %q, want no coordinates in it (found %q)", out, coord)
		}
	}
}

func TestDispatchGeocodeWithNoMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer upstream.Close()

	ts := newToolset(nil, newPageFetcher(), geocode.New(upstream.URL), nil, nil)
	out := ts.dispatch(context.Background(), callTo(toolGeocode, `{"query":"zzzz"}`))
	if !strings.Contains(out, "No matching place") {
		t.Errorf("result = %q, want a plain no-match answer", out)
	}
}

// Sources are what the run actually read. A page that failed is not one:
// listing it would imply the proposal rests on something it does not.
func TestSourcesRecordOnlySuccessfulReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>Kex Hostel</h1><p>Skulagata 28.</p></body></html>")
	}))
	defer srv.Close()

	f := newRelaxedFetcher()
	ts := newToolset(nil, f, nil, nil, nil)

	// Through dispatch, which is what records sources; the fetcher's address
	// policy is relaxed because the test server is on loopback.
	if out := ts.dispatch(context.Background(), callTo(toolFetchPage, `{"url":"`+srv.URL+`/ok"}`)); !strings.Contains(out, "Kex Hostel") {
		t.Fatalf("fetch result = %q", out)
	}
	if out := ts.dispatch(context.Background(), callTo(toolFetchPage, `{"url":"`+srv.URL+`/gone"}`)); !strings.Contains(out, "404") {
		t.Fatalf("the 404 fetch did not report its status: %q", out)
	}

	got := ts.Sources()
	if len(got) != 1 || !strings.HasSuffix(got[0].URL, "/ok") {
		t.Fatalf("sources = %+v, want only the page that was read", got)
	}
}

func TestSourcesAreDeduplicated(t *testing.T) {
	ts := newToolset(nil, newPageFetcher(), nil, nil, nil)
	// The model routinely searches twice and re-reads a page it already found.
	ts.record(Source{Title: "Kex", URL: "https://example.invalid/kex"})
	ts.record(Source{Title: "Kex again", URL: "https://example.invalid/kex"})
	ts.record(Source{Title: "Other", URL: "https://example.invalid/other"})
	ts.record(Source{Title: "No URL", URL: ""})

	if got := ts.Sources(); len(got) != 2 {
		t.Errorf("sources = %+v, want 2 distinct", got)
	}
}

// Every tool call reports twice: a progress event when it starts, which is the
// live status line, and a step event when it ends, which is what the trace
// accumulates. Both carry i18n keys and parameters, never English sentences --
// the server does not know the user's language, and a translated string on the
// wire cannot be re-rendered when they switch locale mid-run.
func TestToolsEmitProgressEventsAsKeys(t *testing.T) {
	var events []Event
	ts := newToolset(&stubSearcher{}, newPageFetcher(), nil, func(e Event) { events = append(events, e) }, nil)
	ts.dispatch(context.Background(), callTo(toolWebSearch, `{"query":"Kex Hostel"}`))

	if len(events) != 2 {
		t.Fatalf("events = %+v, want a start and an end", events)
	}
	start, end := events[0], events[1]

	if start.Kind != EventProgress || !strings.HasPrefix(start.Key, "assist.progress.") {
		t.Errorf("start = %+v, want a progress event with a progress key", start)
	}
	if end.Kind != EventStep || !strings.HasPrefix(end.Key, "assist.step.") {
		t.Errorf("end = %+v, want a step event with a step key", end)
	}
	for _, e := range events {
		if strings.Contains(e.Key, " ") {
			t.Errorf("key = %q looks like a sentence, not a key", e.Key)
		}
		if e.Params["query"] != "Kex Hostel" {
			t.Errorf("params = %v, want the query", e.Params)
		}
	}
	if end.Failed {
		t.Error("a search that worked was reported as failed")
	}
}

// A step that did not work still appears, and says so. A trace that quietly
// omitted the page that would not load would be describing a run that did not
// happen -- and a failed read is often the whole explanation for a thin
// proposal.
func TestAFailedToolCallIsStillATracedStep(t *testing.T) {
	var events []Event
	ts := newToolset(&failingSearcher{}, newPageFetcher(), nil, func(e Event) { events = append(events, e) }, nil)
	ts.dispatch(context.Background(), callTo(toolWebSearch, `{"query":"Kex Hostel"}`))

	if len(events) != 2 {
		t.Fatalf("events = %+v, want a start and an end", events)
	}
	if !events[1].Failed {
		t.Errorf("step = %+v, want it marked failed", events[1])
	}
}

// Arguments that are not JSON at all are the case where a reader most wants
// the call listed: the model spent a turn on something malformed. The step
// appears with no parameter rather than not appearing.
func TestAnUnreadableToolCallIsStillTraced(t *testing.T) {
	var events []Event
	ts := newToolset(&stubSearcher{}, newPageFetcher(), nil, func(e Event) { events = append(events, e) }, nil)
	ts.dispatch(context.Background(), callTo(toolWebSearch, `not json at all`))

	if len(events) != 2 {
		t.Fatalf("events = %+v, want a start and an end", events)
	}
	if events[1].Params["query"] != "" {
		t.Errorf("params = %v, want none", events[1].Params)
	}
	if !events[1].Failed {
		t.Error("a call with unreadable arguments was not marked failed")
	}
}

// A full URL from a search result is long and attacker-influenced; the host is
// the part a person reads, and the part safe to put in a progress line.
func TestFetchProgressEventReportsOnlyTheHost(t *testing.T) {
	var events []Event
	ts := newToolset(nil, newPageFetcher(), nil, func(e Event) { events = append(events, e) }, nil)
	ts.dispatch(context.Background(), callTo(toolFetchPage, `{"url":"https://example.invalid/a/very/long/path?tracking=1"}`))

	if len(events) != 2 {
		t.Fatalf("events = %+v, want a start and an end", events)
	}
	for _, e := range events {
		if got := e.Params["url"]; got != "example.invalid" {
			t.Errorf("url param = %q, want just the host", got)
		}
	}
}

func TestNewSearcherSelection(t *testing.T) {
	if s, err := newSearcher(Options{}); err != nil || s != nil {
		t.Errorf("newSearcher(none) = %v, %v; want nil, nil", s, err)
	}
	s, err := newSearcher(Options{SearchProvider: "stub"})
	if err != nil || s == nil {
		t.Fatalf("newSearcher(stub) = %v, %v", s, err)
	}
	if s.Name() != "stub" {
		t.Errorf("Name() = %q", s.Name())
	}
	// config.Load validates the name, so reaching here with something else
	// means the two lists have drifted -- a bug worth surfacing loudly.
	if _, err := newSearcher(Options{SearchProvider: "altavista"}); err == nil {
		t.Error("newSearcher accepted an unknown provider")
	}
}

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		// Straight from the first live run: the official site's extracted text
		// begins with a BOM, which renders as an unexplainable smudge.
		"Opening hours\ufeff\nmore text": "Opening hours",
		"  Kex Hostel  \nSkulagata":      "Kex Hostel",
		"":                               "(untitled)",
		"\ufeff\u200b":                   "(untitled)",
	}
	for in, want := range cases {
		if got := cleanTitle(firstLine(in)); got != want {
			t.Errorf("cleanTitle(firstLine(%q)) = %q, want %q", in, got, want)
		}
	}
	// Truncation counts runes, not bytes: cutting mid-rune would produce
	// replacement characters in the sources list.
	long := strings.Repeat("\u00e9", 200)
	if got := cleanTitle(long); len([]rune(got)) != 120 {
		t.Errorf("cleanTitle(long) is %d runes, want 120", len([]rune(got)))
	}
}
