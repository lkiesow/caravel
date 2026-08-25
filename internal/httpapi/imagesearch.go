package httpapi

import (
	"net/http"
	"strings"
	"sync"

	"caravel/internal/assist"
	"caravel/internal/db"
	"caravel/internal/wikimedia"
)

// "Search for an image", the one image feature with no LLM in it.
//
// # Why this is a control and not a suggestion
//
// The assistant proposes a cover (Milestone 5 and 6) and it is good at it,
// because it picks the picture a page declares as its own. What it cannot do
// is *choose between* photographs: the model has no vision, so a second
// candidate would be judged by the words around it. Picking a picture out of a
// grid is the thing a person does in half a second and a blind agent does
// badly, so this is offered to the person -- the sibling of the address search
// two cards below, and reachable on an instance with no assistant at all.
//
// # Two sources, kept apart
//
// Wikipedia always, with nothing configured: it needs no key, and its images
// arrive with an author and a licence, which is the only kind of provenance
// worth storing. A web image search when the configured backend can do one:
// far better coverage of hotels and restaurants, and no licence information
// whatsoever -- neither Serper nor ddgs knows on what terms any of it may be
// used.
//
// They are returned as separate groups rather than one merged list precisely
// because of that difference. A merged list would have to either invent a
// licence field for results that have none, or drop the licence from the ones
// that do; labelling each group by where it came from lets the UI be honest
// about both.

// imageSearchMinQuery is short enough for "Kex" and long enough that a
// keystroke does not reach an upstream service. The control does not search
// per keystroke anyway -- this is the backstop, not the policy.
const imageSearchMinQuery = 2

// imageCandidate is one picture somebody could pick.
type imageCandidate struct {
	Title string `json:"title"`
	// URL is the full-size image: what gets fetched and stored on pick.
	URL string `json:"url"`
	// ThumbURL is the preview, hotlinked into the grid. May be empty, in
	// which case the client falls back to URL.
	ThumbURL string `json:"thumb_url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	// SourceURL, Credit and License are the provenance, forwarded verbatim to
	// POST /media/url when this candidate is chosen. Credit and License are
	// empty for a web search result, which is the honest answer.
	SourceURL string `json:"source_url"`
	Credit    string `json:"credit"`
	License   string `json:"license"`
}

// imageSearchGroup is one source's results, labelled so the UI can say where
// they came from.
type imageSearchGroup struct {
	// Source is "wikipedia" or the search backend's own name.
	Source  string           `json:"source"`
	Results []imageCandidate `json:"results"`
}

type imageSearchResponse struct {
	Groups []imageSearchGroup `json:"groups"`
}

// handleImageSearch handles GET /api/trips/{tripId}/image-search?q=…&lang=…
func (s *Server) handleImageSearch(w http.ResponseWriter, r *http.Request) {
	// RoleEditor rather than RoleViewer: this is a step in editing a location,
	// and it spends the instance owner's search quota. A viewer has no reason
	// to be able to do either.
	if _, _, ok := s.loadTrip(w, r, db.RoleEditor); !ok {
		return
	}
	if s.Wikimedia == nil && s.ImageSearch == nil {
		// 501 for the same reason as /geocode: the route exists and the
		// capability is off. /auth/me already told the client, so this is a
		// backstop rather than a path it should reach.
		writeError(w, http.StatusNotImplemented, "image search is not enabled on this server")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < imageSearchMinQuery {
		writeError(w, http.StatusBadRequest, "search term too short")
		return
	}
	lang := normaliseLocale(r.URL.Query().Get("lang"))

	// Both sources at once. They are independent, they are both somebody
	// else's service, and running them in sequence would mean a slow
	// Wikipedia holds up results that are already in hand.
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		groups    = make([]imageSearchGroup, 2)
		anyWorked bool
	)
	add := func(slot int, g imageSearchGroup, err error) {
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			// One source failing is not the request failing: an instance with
			// no network to Wikimedia and a working search key should still
			// get results. Logged rather than returned, because the error text
			// is upstream's and not ours to forward.
			return
		}
		anyWorked = true
		groups[slot] = g
	}

	if s.Wikimedia != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, err := s.Wikimedia.Search(r.Context(), lang, query, wikimediaImageLimit)
			add(0, imageSearchGroup{Source: "wikipedia", Results: fromWikimedia(found)}, err)
		}()
	}
	if s.ImageSearch != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, err := s.ImageSearch.SearchImages(r.Context(), query)
			name := "search"
			if named, ok := s.ImageSearch.(assist.Searcher); ok {
				name = named.Name()
			}
			add(1, imageSearchGroup{Source: name, Results: fromImageSearch(found)}, err)
		}()
	}
	wg.Wait()

	if !anyWorked {
		writeError(w, http.StatusBadGateway, "the image search services could not be reached")
		return
	}

	out := imageSearchResponse{Groups: make([]imageSearchGroup, 0, 2)}
	for _, g := range groups {
		if g.Source == "" || len(g.Results) == 0 {
			continue
		}
		out.Groups = append(out.Groups, g)
	}
	writeJSON(w, http.StatusOK, out)
}

// wikimediaImageLimit caps the Wikipedia group. Twelve is a grid a person can
// take in; more is a page to scroll rather than a choice to make.
const wikimediaImageLimit = 12

func fromWikimedia(found []wikimedia.Image) []imageCandidate {
	out := make([]imageCandidate, 0, len(found))
	for _, i := range found {
		// The credit links to the file page when there is one and to the
		// article otherwise -- both are pages that say where this came from,
		// and one of them always exists.
		source := i.DescriptionURL
		if source == "" {
			source = i.ArticleURL
		}
		out = append(out, imageCandidate{
			Title:     i.Title,
			URL:       i.URL,
			ThumbURL:  i.ThumbURL,
			Width:     i.Width,
			Height:    i.Height,
			SourceURL: source,
			Credit:    i.Credit,
			License:   i.Licence,
		})
	}
	return out
}

func fromImageSearch(found []assist.ImageResult) []imageCandidate {
	out := make([]imageCandidate, 0, len(found))
	for _, i := range found {
		out = append(out, imageCandidate{
			Title:    i.Title,
			URL:      i.URL,
			ThumbURL: i.ThumbURL,
			Width:    i.Width,
			Height:   i.Height,
			// No Credit and no License, deliberately: see the package note
			// above. Where it was found is the whole of what is known.
			SourceURL: i.SourceURL,
		})
	}
	return out
}

// imageSearchAvailable is what /auth/me reports. Either half alone is a
// working feature, which is the point of the Wikipedia half.
func (s *Server) imageSearchAvailable() bool {
	return s.Wikimedia != nil || s.ImageSearch != nil
}
