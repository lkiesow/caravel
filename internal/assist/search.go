package assist

import (
	"context"
	"fmt"
	"strings"
)

// Web search, behind an interface.
//
// Five backends are supported across the stage and they disagree about almost
// everything -- auth, request shape, response shape, whether they are hosted
// or something you run yourself. What they agree on is what a result *is*, so
// that is the interface: a title, a URL and a snippet, which is the lowest
// common denominator every one of them returns and the most the model needs to
// decide what to read.
//
// The normalisation matters beyond tidiness. The agent never sees
// provider-shaped JSON, so swapping providers is a change to one file and
// nothing else -- the same reasoning as geocode.Result versus the raw
// Nominatim payload. It also means a provider that disappears (these are
// scrapers and startups) costs a replacement implementation rather than a
// change to the prompt.

// SearchResult is one hit, normalised.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// Searcher is a web-search backend. Nil is valid and means no web search is
// configured: the agent then runs on OpenStreetMap and the model's own
// knowledge, which is a worse assistant but a working one.
type Searcher interface {
	Search(ctx context.Context, query string) ([]SearchResult, error)
	// Name is what appears in progress events and errors, so an operator can
	// tell which backend is misbehaving without reading the config.
	Name() string
}

// searchMaxResults is what the agent asks for and what providers are told to
// return. Enough to choose from, few enough that the list itself is not most
// of the prompt.
const searchMaxResults = 6

// newSearcher builds the configured backend, or nil if none is configured.
//
// Milestone 3 implements the stub only; Milestone 5 adds Ollama Cloud and
// Milestone 8 the remaining three. An unknown name is a startup error rather
// than a silent fallback to "no search", because config.Load has already
// validated the value -- reaching here with something else means the two lists
// have drifted, which is a bug worth surfacing loudly.
func newSearcher(opts Options) (Searcher, error) {
	switch opts.SearchProvider {
	case "":
		return nil, nil
	case "stub":
		return &stubSearcher{}, nil
	case "ollama":
		if opts.SearchKey == "" {
			return nil, fmt.Errorf("assist: search provider %q needs CARAVEL_SEARCH_KEY", opts.SearchProvider)
		}
		return newOllamaSearcher(opts.SearchKey, opts.SearchURL), nil
	default:
		return nil, fmt.Errorf("assist: unknown search provider %q", opts.SearchProvider)
	}
}

// stubSearcher answers from a fixed table, selected by
// CARAVEL_SEARCH_PROVIDER=stub.
//
// Like the stub provider, it fakes exactly one thing -- the outbound HTTP call
// -- and leaves the dispatch, the agent loop and everything downstream real.
// The results deliberately point at example.invalid, which cannot resolve, so
// a test that accidentally follows one fails rather than reaching the network.
type stubSearcher struct{}

func (*stubSearcher) Name() string { return "stub" }

func (*stubSearcher) Search(_ context.Context, query string) ([]SearchResult, error) {
	// Echoing the query into the first result is not decoration: it makes a
	// Playwright assertion able to prove the search term actually reached the
	// backend, rather than that some fixture was rendered.
	return []SearchResult{
		{
			Title:   "Kex Hostel, Reykjavik",
			URL:     "https://example.invalid/kex",
			Snippet: "A former biscuit factory on Skulagata, now a hostel with a harbour-facing bar. Searched for: " + strings.TrimSpace(query),
		},
		{
			Title:   "Visit Reykjavik - official city guide",
			URL:     "https://example.invalid/visit-reykjavik",
			Snippet: "Practical information for visitors to Reykjavik, including accommodation listings.",
		},
	}, nil
}
