package assist

import (
	"context"
	"fmt"
	"strings"

	"caravel/internal/wikimedia"
)

// Web search, behind an interface.
//
// Four backends are supported and they disagree about almost everything --
// auth, request shape, response shape, whether they are hosted or something you
// run yourself, and even what the three fields are called (`url`/`content`,
// `href`/`body`, `link`/`snippet`). What they agree on is what a result *is*,
// so that is the interface: a title, a URL and a snippet, which is the lowest
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

// ImageResult is one image hit, normalised the same way SearchResult is.
//
// Note what is *not* here: a licence. A web image search finds pictures on
// pages, and neither Serper nor ddgs knows on what terms any of them may be
// used -- so an honest result carries where it was found and nothing more.
// The Wikipedia half of the image picker does carry a licence, which is
// exactly why the two are kept apart in the response rather than merged into
// one list.
type ImageResult struct {
	Title string
	// URL is the full-size image, which is what gets fetched and stored when
	// somebody picks it.
	URL string
	// ThumbURL is the preview for the grid. Often served by the search engine
	// rather than by the site, and often the only one of the two that is not
	// hotlink-blocked.
	ThumbURL      string
	Width, Height int
	// SourceURL is the page the image was found on -- the provenance stored
	// alongside the picture, and the only claim about it anyone can make.
	SourceURL string
}

// ImageSearcher is an *optional* capability a Searcher may also implement,
// discovered by type assertion rather than by a second provider registry.
//
// Optional because the backends genuinely differ: Serper and ddgs both have
// an images endpoint, Ollama Cloud has web_search and nothing else, and the
// stub has no images at all. A backend that cannot do this simply does not
// implement it and the picker falls back to Wikipedia, which needs no
// configuration and is always there.
type ImageSearcher interface {
	SearchImages(ctx context.Context, query string) ([]ImageResult, error)
}

// imageSearchMaxResults is what an image-search backend is asked for. Larger
// than searchMaxResults because these are thumbnails in a grid being judged by
// eye, not text being read by a model.
const imageSearchMaxResults = 12

// searchMaxResults is what the agent asks for and what providers are told to
// return. Enough to choose from, few enough that the list itself is not most
// of the prompt.
const searchMaxResults = 6

// newSearcher builds the configured backend, or nil if none is configured.
//
// Milestone 3 implemented the stub, Milestone 5 Ollama Cloud, Milestone 8 ddgs
// and Serper. An unknown name is a startup error rather
// than a silent fallback to "no search", because config.Load has already
// validated the value -- reaching here with something else means the two lists
// have drifted, which is a bug worth surfacing loudly.
func newSearcher(opts Options) (Searcher, error) {
	return NewSearcher(opts.SearchProvider, opts.SearchKey, opts.SearchURL)
}

// NewSearcher is newSearcher without the assistant.
//
// Exported as of Stage 21 Milestone 7, when web search stopped being the
// assistant's alone: the image picker uses the same backend, so cmd/caravel
// builds one searcher and hands it to both rather than each constructing its
// own from the same three settings.
func NewSearcher(provider, key, searchURL string) (Searcher, error) {
	opts := Options{SearchProvider: provider, SearchKey: key, SearchURL: searchURL}
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
	case "serper":
		if opts.SearchKey == "" {
			return nil, fmt.Errorf("assist: search provider %q needs CARAVEL_SEARCH_KEY", opts.SearchProvider)
		}
		return newSerperSearcher(opts.SearchKey, opts.SearchURL), nil
	case "ddgs":
		// Self-hosted, so there is no address to fall back on. config.Load
		// already refuses this combination; the check is here too because this
		// constructor is reachable from tests that do not go through Load.
		if opts.SearchURL == "" {
			return nil, fmt.Errorf("assist: search provider %q needs CARAVEL_SEARCH_URL", opts.SearchProvider)
		}
		return newDDGSSearcher(opts.SearchURL), nil
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

// SearchImages makes the stub an ImageSearcher, which is what puts a second,
// web-search group in the picker when the browser suite runs.
//
// Two results, and the second one is dead on purpose. "A dead thumbnail leaves
// an invisible cell that still clicks" is a real bug of exactly this shape --
// the image field had it for its own preview -- so the fixture has to contain
// one, or nothing could catch it coming back.
//
// The first has to load, though, or the group it is in could never be looked
// at. It borrows a picture from the stub encyclopaedia, which is the one
// loopback host in a test run that serves images.
func (*stubSearcher) SearchImages(_ context.Context, query string) ([]ImageResult, error) {
	// The same escape hatch the stub encyclopaedia has, so a test can reach
	// the "nothing at all was found" state with both sources configured.
	if strings.Contains(strings.ToLower(query), "nothing") {
		return nil, nil
	}
	live := wikimedia.StubImageURL()
	return []ImageResult{
		{
			Title:     "Kex Hostel, Reykjavik. Searched for: " + strings.TrimSpace(query),
			URL:       live,
			ThumbURL:  live,
			Width:     1200,
			Height:    800,
			SourceURL: "https://example.invalid/kex",
		},
		{
			Title:     "Visit Reykjavik - a picture that will not load",
			URL:       "https://example.invalid/visit/cover.jpg",
			ThumbURL:  "https://example.invalid/visit/thumb.jpg",
			SourceURL: "https://example.invalid/visit-reykjavik",
		},
	}, nil
}
