// Package websearch is web search, behind an interface.
//
// Five backends are supported and they disagree about almost everything --
// auth, request shape, response shape, whether they are hosted or something you
// run yourself, and even what the three fields are called (`url`/`content`,
// `href`/`body`, `link`/`snippet`, `url`/`description`). What they agree on is
// what a result *is*, so that is the interface: a title, a URL and a snippet,
// which is the lowest common denominator every one of them returns and the
// most the model needs to decide what to read.
//
// The normalisation matters beyond tidiness. No caller ever sees
// provider-shaped JSON, so swapping providers is a change to one file and
// nothing else -- the same reasoning as geocode.Result versus the raw
// Nominatim payload. It also means a provider that disappears (these are
// scrapers and startups) costs a replacement implementation rather than a
// change to the assistant's prompt.
//
// Lifted out of internal/assist (where it started, as the assistant's research
// tool) once the image picker became a second consumer: cmd/caravel builds one
// searcher and hands it to both, and a package named for the assistant should
// not be imported for something with no LLM in it.
package websearch

import (
	"context"
	"fmt"
	"strings"

	"caravel/internal/buildinfo"
	"caravel/internal/wikimedia"
)

// Result is one hit, normalised.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Searcher is a web-search backend. Nil is valid and means no web search is
// configured: the agent then runs on OpenStreetMap and the model's own
// knowledge, which is a worse assistant but a working one.
type Searcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
	// Name is what appears in progress events and errors, so an operator can
	// tell which backend is misbehaving without reading the config.
	Name() string
}

// ImageResult is one image hit, normalised the same way Result is.
//
// Note what is *not* here: a licence. A web image search finds pictures on
// pages, and none of Serper, Brave and ddgs knows on what terms any of them
// may be used -- so an honest result carries where it was found and nothing more.
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
// Optional because the backends genuinely differ: Serper, Brave and ddgs all
// have an images endpoint, Ollama Cloud has web_search and nothing else, and the
// stub has no images at all. A backend that cannot do this simply does not
// implement it and the picker falls back to Wikipedia, which needs no
// configuration and is always there.
type ImageSearcher interface {
	SearchImages(ctx context.Context, query string) ([]ImageResult, error)
}

// PlaceResult is one place from a maps-style lookup: a business or a landmark
// with a position the provider is confident about.
//
// Deliberately narrower than what such an API returns. Serper's /places also
// carries a rating, a review count, a phone number, opening hours and a Google
// place id -- all real, none of them this feature's business. What is wanted
// here is a *second opinion on where something is*, and every extra field is
// something a caller could start depending on without anybody deciding it
// should.
type PlaceResult struct {
	// Title and Address are how the provider names the place. Shown to the
	// user as evidence about the pin, exactly as a geocoder display name is.
	Title   string
	Address string
	Lat     float64
	Lng     float64
	// Category is the provider's own word for what the place is -- "Hostel",
	// "Coffee shop". Not mapped onto Caravel's categories: it is a different
	// vocabulary with a different purpose, and guessing between them is how a
	// bar becomes a museum.
	Category string
}

// PlaceLocator is an *optional* capability a Searcher may also implement,
// discovered by type assertion rather than by a second provider registry --
// the same arrangement as ImageSearcher above, for the same reason.
//
// Only Serper has one today: /places is Google Maps data, which is why it is
// worth asking at all. OpenStreetMap is better than Google for landmarks,
// museums, churches and stations, and considerably thinner on the restaurants,
// cafes, bars, shops and hotels a trip is actually made of -- and for those,
// Google's pin is the business's own position rather than an address
// interpolation. ddgs and Ollama Cloud have no such endpoint, an unconfigured
// instance has no backend at all, and all of them keep working: the resolver
// falls back to OpenStreetMap alone.
//
// Note what implementing this costs the operator, because it is not nothing: a
// paid API call per location, up to six for one trip-suggestion run. That is a
// deliberate trade, made once in the config by choosing `serper`.
type PlaceLocator interface {
	SearchPlaces(ctx context.Context, query string) ([]PlaceResult, error)
}

// placeSearchMaxResults is what a places backend is asked for. Small: the
// resolver takes the first match and the rest exist only so that "it found
// several" is distinguishable from "it found one", which nothing uses yet.
const placeSearchMaxResults = 3

// imageSearchMaxResults is what an image-search backend is asked for. Larger
// than MaxResults because these are thumbnails in a grid being judged by
// eye, not text being read by a model.
const imageSearchMaxResults = 12

// MaxResults is what the assistant asks for and what providers are told to
// return. Enough to choose from, few enough that the list itself is not most
// of the prompt.
const MaxResults = 6

// New builds the configured backend, or nil if none is configured.
//
// An unknown name is a startup error rather than a silent fallback to "no
// search", because config.Load has already validated the value -- reaching here
// with something else means the two lists have drifted, which is a bug worth
// surfacing loudly.
func New(provider, key, searchURL string) (Searcher, error) {
	switch provider {
	case "":
		return nil, nil
	case "stub":
		return &Stub{}, nil
	case "ollama":
		if key == "" {
			return nil, fmt.Errorf("websearch: search provider %q needs CARAVEL_SEARCH_KEY", provider)
		}
		return newOllamaSearcher(key, searchURL), nil
	case "serper":
		if key == "" {
			return nil, fmt.Errorf("websearch: search provider %q needs CARAVEL_SEARCH_KEY", provider)
		}
		return newSerperSearcher(key, searchURL), nil
	case "brave":
		if key == "" {
			return nil, fmt.Errorf("websearch: search provider %q needs CARAVEL_SEARCH_KEY", provider)
		}
		return newBraveSearcher(key, searchURL), nil
	case "ddgs":
		// Self-hosted, so there is no address to fall back on. config.Load
		// already refuses this combination; the check is here too because this
		// constructor is reachable from tests that do not go through Load.
		if searchURL == "" {
			return nil, fmt.Errorf("websearch: search provider %q needs CARAVEL_SEARCH_URL", provider)
		}
		return newDDGSSearcher(searchURL), nil
	default:
		return nil, fmt.Errorf("websearch: unknown search provider %q", provider)
	}
}

// userAgent identifies our outbound requests to the search backends. The same
// string the assistant's page fetches send, so an operator reading a backend's
// logs sees one Caravel, not two.
func userAgent() string {
	return "Caravel/" + buildinfo.Version + " (self-hosted trip planner; +assistant)"
}

// truncate and collapseWhitespace are copies of the assistant's helpers of the
// same name, which its page fetcher still uses; a few lines each is cheaper
// than a shared package for them.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if f := strings.Join(strings.Fields(line), " "); f != "" {
			out = append(out, f)
		}
	}
	return strings.Join(out, "\n")
}

// Stub answers from a fixed table, selected by
// CARAVEL_SEARCH_PROVIDER=stub.
//
// Like the stub provider, it fakes exactly one thing -- the outbound HTTP call
// -- and leaves the dispatch, the agent loop and everything downstream real.
// The results deliberately point at example.invalid, which cannot resolve, so
// a test that accidentally follows one fails rather than reaching the network.
type Stub struct{}

func (*Stub) Name() string { return "stub" }

func (*Stub) Search(_ context.Context, query string) ([]Result, error) {
	// Echoing the query into the first result is not decoration: it makes a
	// Playwright assertion able to prove the search term actually reached the
	// backend, rather than that some fixture was rendered.
	return []Result{
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

// SearchPlaces makes the stub a PlaceLocator, so the two-source resolution
// path runs in `go test` and in the browser suite without a Serper key and
// without spending anybody's money.
//
// Three answers, each reachable by name, covering what the resolver has to
// tell apart:
//
//   - "Kex Hostel" -- a place OpenStreetMap also knows precisely. Google
//     agrees to within a few metres, which is the ordinary case and the one
//     where OSM should win, because only OSM carries a feature identity.
//   - "Braud and Co" -- a bakery the fixture geocoder does *not* know, which
//     is the case this whole milestone exists for: a small commercial place
//     that OSM misses and Google has.
//   - "Harpa" -- deliberately 3km from where the fixture geocoder puts it.
//     Two services disagreeing about a place is not a pin to show with
//     confidence, and Milestone 5 needs a way to reach that state.
func (*Stub) SearchPlaces(_ context.Context, query string) ([]PlaceResult, error) {
	switch key := strings.ToLower(strings.TrimSpace(query)); {
	case strings.Contains(key, "kex hostel"):
		// The same building the fixture geocoder returns, give or take the
		// few metres two surveys of one doorway differ by.
		return []PlaceResult{{
			Title:    "Kex Hostel",
			Address:  "Skulagata 28, 101 Reykjavik, Iceland",
			Lat:      64.146620,
			Lng:      -21.925280,
			Category: "Hostel",
		}}, nil
	case strings.Contains(key, "braud"):
		return []PlaceResult{{
			Title:    "Braud and Co",
			Address:  "Frakkastigur 16, 101 Reykjavik, Iceland",
			Lat:      64.143920,
			Lng:      -21.925610,
			Category: "Bakery",
		}}, nil
	case strings.Contains(key, "harpa"):
		// Roughly 3km east of the fixture geocoder's answer: past the
		// disagreement threshold, so the resolver has to treat it as a
		// question rather than a position.
		return []PlaceResult{{
			Title:    "Harpa Concert Hall",
			Address:  "Austurbakki 2, 101 Reykjavik, Iceland",
			Lat:      64.150500,
			Lng:      -21.862000,
			Category: "Concert hall",
		}}, nil
	}
	// Everything else: searched, found nothing. Non-nil so that "asked and got
	// none" is the same shape as a real backend's.
	return []PlaceResult{}, nil
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
func (*Stub) SearchImages(_ context.Context, query string) ([]ImageResult, error) {
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
