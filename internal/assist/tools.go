package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"caravel/internal/geocode"
)

// The names of the tools the model may call.
//
// Declared here in Milestone 2 because they are a contract between three
// things that land in different milestones: the stub provider's script (which
// calls them), the tool dispatcher (Milestone 3, which implements them) and
// the agent loop (Milestone 4, which routes between the two). Keeping the
// names in one place means the stub cannot drift into scripting a call that
// no longer exists -- a failure that would otherwise show up as an agent
// mysteriously giving up.
const (
	// toolWebSearch searches the web. Absent when no search provider is
	// configured, in which case the agent still runs -- worse, but working.
	toolWebSearch = "web_search"
	// toolFetchPage retrieves one page as text. The tool with a real attack
	// surface, guarded in fetch.go.
	toolFetchPage = "fetch_page"
	// toolGeocode resolves a place name or address to coordinates through
	// OpenStreetMap. Available to the model as a lookup, but note that the
	// coordinates that reach the proposal are resolved by the agent itself,
	// not taken from whatever the model does with this.
	toolGeocode = "geocode"
)

// The dispatcher.
//
// One map from name to function, deliberately narrow: adding a tool is a
// definition plus an entry, and nothing about the agent loop changes. That is
// the seam the backlog asked for when it ruled out MCP -- an MCP-backed tool
// could be added here later without a refactor, because "a tool" is already
// just a name, a schema and a function.

type toolFunc func(ctx context.Context, args json.RawMessage) (string, error)

// toolset holds the dependencies the tools need and the sources they touched.
// One per run, not one per server: Sources accumulates, and two concurrent
// runs must not pool theirs.
type toolset struct {
	search   Searcher
	fetch    *pageFetcher
	geocoder *geocode.Client
	events   func(Event)
	log      *slog.Logger

	mu      sync.Mutex
	sources []Source
	// seen dedups sources by URL, since the model routinely searches twice
	// and reads a page it already found.
	seen map[string]bool
}

func newToolset(search Searcher, fetch *pageFetcher, geocoder *geocode.Client, events func(Event), log *slog.Logger) *toolset {
	if events == nil {
		events = func(Event) {}
	}
	if log == nil {
		log = slog.Default()
	}
	return &toolset{search: search, fetch: fetch, geocoder: geocoder, events: events, log: log, seen: map[string]bool{}}
}

// definitions describes the available tools for the model.
//
// Only the tools that can actually work are offered. Describing web search to
// a model with no search backend configured produces a run that calls it,
// receives an error, and wastes a turn discovering what the config already
// knew.
func (t *toolset) definitions() []toolDef {
	defs := make([]toolDef, 0, 3)

	if t.search != nil {
		defs = append(defs, toolDef{
			Name:        toolWebSearch,
			Description: "Search the web. Use this first to find official pages for a place. Returns titles, URLs and snippets.",
			Parameters: json.RawMessage(`{
			  "type": "object",
			  "additionalProperties": false,
			  "required": ["query"],
			  "properties": {
			    "query": {"type": "string", "description": "The search query, e.g. the place name plus its city."}
			  }
			}`),
		})
	}

	defs = append(defs, toolDef{
		Name:        toolFetchPage,
		Description: "Fetch one web page and return its text. Use this on a URL from a search result to confirm details. Only public http and https addresses can be fetched.",
		Parameters: json.RawMessage(`{
		  "type": "object",
		  "additionalProperties": false,
		  "required": ["url"],
		  "properties": {
		    "url": {"type": "string", "description": "The absolute URL of a page found in a search result."}
		  }
		}`),
	})

	if t.geocoder != nil {
		defs = append(defs, toolDef{
			Name:        toolGeocode,
			Description: "Look up a place name or postal address in OpenStreetMap to check that it exists and is unambiguous. Returns matching places with their formatted addresses.",
			Parameters: json.RawMessage(`{
			  "type": "object",
			  "additionalProperties": false,
			  "required": ["query"],
			  "properties": {
			    "query": {"type": "string", "description": "A place name and city, or a postal address."}
			  }
			}`),
		})
	}

	return defs
}

// dispatch runs one tool call and returns the text to send back as its result.
//
// It never returns an error, and that is the important decision here: a tool
// failure is *information for the model*, not a reason to abandon the run. A
// page that 404s or an address that does not resolve should make the model try
// something else, exactly as it would if a person hit the same wall. Aborting
// instead would turn every dead link on the web into a failed enrichment.
//
// The genuinely fatal cases -- the deadline, the token budget, cancellation --
// are the agent's to enforce, and it checks them between turns.
func (t *toolset) dispatch(ctx context.Context, call toolCall) string {
	name := call.Function.Name
	args := json.RawMessage(call.Function.Arguments)

	fn, ok := t.lookup(name)
	if !ok {
		// Models occasionally invent a tool. Saying so plainly is more useful
		// than silence, and cheaper than a turn spent waiting.
		t.log.Debug("assist: tool unknown", "name", name)
		return fmt.Sprintf("There is no tool called %q. The available tools are listed in this conversation.", name)
	}

	// One record per tool call, here rather than inside each tool, so the
	// timing is measured the same way for all three and adding a fourth tool
	// gets its trace for free. The arguments carry the interesting part -- the
	// query, the URL -- so they go in whole; they are short by schema, and
	// nothing secret can reach them.
	//
	// result_bytes rather than the result: a fetched page is up to
	// fetchMaxTextBytes of third-party text, and its size is what actually
	// answers "why did this run cost so much".
	started := time.Now()
	out, err := fn(ctx, args)
	attrs := []any{
		"name", name,
		"args", string(args),
		"ms", time.Since(started).Milliseconds(),
		"ok", err == nil,
		"result_bytes", len(out),
	}
	// Only on failure. "err=<nil>" on every successful call is a column of
	// nothing in the one place a reader is scanning for something.
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	t.log.Debug("assist: tool call", attrs...)
	if err != nil {
		// The model sees why it failed, in words it can act on.
		return "That did not work: " + err.Error()
	}
	return out
}

func (t *toolset) lookup(name string) (toolFunc, bool) {
	switch name {
	case toolWebSearch:
		if t.search == nil {
			return nil, false
		}
		return t.doSearch, true
	case toolFetchPage:
		return t.doFetch, true
	case toolGeocode:
		if t.geocoder == nil {
			return nil, false
		}
		return t.doGeocode, true
	default:
		return nil, false
	}
}

func (t *toolset) doSearch(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("the arguments were not valid JSON: %w", err)
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return "", fmt.Errorf("the query was empty")
	}

	t.events(Event{Key: "assist.progress.searching", Params: map[string]string{"query": in.Query}})

	results, err := t.search.Search(ctx, in.Query)
	if err != nil {
		return "", fmt.Errorf("the search failed: %w", err)
	}
	if len(results) == 0 {
		return "No results.", nil
	}
	if len(results) > searchMaxResults {
		results = results[:searchMaxResults]
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Snippet)
	}
	return b.String(), nil
}

func (t *toolset) doFetch(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("the arguments were not valid JSON: %w", err)
	}

	t.events(Event{Key: "assist.progress.reading", Params: map[string]string{"url": hostOf(in.URL)}})

	fetched, err := t.fetch.Fetch(ctx, in.URL)
	if err != nil {
		return "", err
	}

	// Recorded only on success: a page that could not be read is not a source,
	// and listing it would imply the proposal rests on something it does not.
	//
	// The <title> when the page has one, the first line of text otherwise. The
	// fallback exists for plain-text pages and for markup too broken to parse;
	// it is second choice because a real page's first line is often furniture
	// -- "Skip to main content" made it into a live sources list before this.
	title := strings.TrimSpace(fetched.Title)
	if title == "" {
		title = firstLine(fetched.Text)
	}
	t.record(Source{Title: cleanTitle(title), URL: in.URL})
	return fetched.Text, nil
}

func (t *toolset) doGeocode(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("the arguments were not valid JSON: %w", err)
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return "", fmt.Errorf("the query was empty")
	}

	t.events(Event{Key: "assist.progress.checkingMap", Params: map[string]string{"query": in.Query}})

	results, err := t.geocoder.Search(ctx, in.Query)
	if err != nil {
		return "", fmt.Errorf("the lookup failed: %w", err)
	}
	if len(results) == 0 {
		return "No matching place was found in OpenStreetMap.", nil
	}

	// Formatted addresses only, no coordinates. The model has no use for
	// lat/lng -- the agent resolves the final position itself -- and showing
	// them invites it to copy a number into the answer, which is the exact
	// failure the design forbids.
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.DisplayName)
	}
	return b.String(), nil
}

func (t *toolset) record(s Source) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.URL == "" || t.seen[s.URL] {
		return
	}
	t.seen[s.URL] = true
	t.sources = append(t.sources, s)
}

// Sources returns what the run actually read, in the order it read it.
func (t *toolset) Sources() []Source {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Source{}, t.sources...)
}

// hostOf reduces a URL to its host for display. Progress events go to a
// browser, and a full URL from a search result is both long and
// attacker-influenced; the host is the part a person actually reads.
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

// firstLine is the fallback title for a source: the first line of the
// extracted text, used when the page has no <title>.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// cleanTitle makes a title fit to show in the sources list.
//
// Zero-width characters are stripped because they are invisible in the source
// and very visible once rendered: a live run produced "Opening hours" followed
// by a stray BOM, which the list showed as a smudge nobody could explain or
// select. Truncation counts runes, since cutting mid-rune produces replacement
// characters.
func cleanTitle(s string) string {
	out := strings.Map(func(r rune) rune {
		switch r {
		case '\ufeff', '\u200b', '\u200c', '\u200d', '\u2060':
			return -1
		}
		return r
	}, s)
	out = strings.Join(strings.Fields(out), " ")
	if len([]rune(out)) > 120 {
		out = string([]rune(out)[:120])
	}
	if out == "" {
		return "(untitled)"
	}
	return out
}
