package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"caravel/internal/db"
)

// The map the browser draws, as a pair of MapLibre style documents served
// from Caravel's own origin.
//
// Vector rather than raster, and that is the whole point of Stage 30: raster
// tiles are pre-rendered images, so their labels are baked in before anyone
// asks and an instance had one language for everybody. A vector style is drawn
// in the browser, so every reader gets place names in their own language --
// see localiseLabels in web/js/components/map-view.js.
//
// This replaced CARAVEL_TILE_URL and its two companions outright rather than
// joining them. That setting existed *because* raster could not do the above;
// with the reason gone, keeping a second rendering path would have meant two
// sets of documentation, two code paths and a per-reader light/dark setting
// that silently did nothing on half of them.
//
// Two URLs because light and dark are a per-browser preference
// (web/js/map-theme.js), so an instance has to be able to answer both.
const (
	DefaultMapStyleURL     = "/js/vendor/map-styles/positron.json"
	DefaultMapStyleDarkURL = "/js/vendor/map-styles/dark.json"
)

// MapStyleSettings is the map as the operator configured it. An empty field
// means unset and takes the default above -- the same convention the assist
// limits use.
//
// Paths by default, not URLs: the styles are vendored under web/js/vendor, so
// a stock instance serves its own map definition and reaches a third party
// only for the tiles the style names. An operator pointing these at their own
// tile server, or at a commercial provider, is the reason they are settings.
type MapStyleSettings struct {
	URL     string
	DarkURL string
}

func (m MapStyleSettings) withDefaults() MapStyleSettings {
	if m.URL == "" {
		m.URL = DefaultMapStyleURL
	}
	// Deliberately falls back to the light style rather than to the dark
	// default: an operator who named a style of their own but no dark
	// counterpart should get *their* map in both modes, not ours in one of
	// them.
	if m.DarkURL == "" {
		if m.URL != DefaultMapStyleURL {
			m.DarkURL = m.URL
		} else {
			m.DarkURL = DefaultMapStyleDarkURL
		}
	}
	return m
}

type mapConfigResponse struct {
	StyleURL     string `json:"style_url"`
	DarkStyleURL string `json:"dark_style_url"`
}

// handleMapConfig tells the frontend which map to draw.
//
// Behind RequireAuth, unlike /auth/config: no page renders a map before
// anyone has a session, so there is nothing to gain by publishing the
// instance's map provider to anonymous callers.
//
// No attribution field: a style carries its own credit, either inline on its
// sources or in the TileJSON they point at, and MapLibre renders it. The old
// CARAVEL_TILE_ATTRIBUTION existed because a bare XYZ template carries no
// provenance at all, and it was a standing trap -- change the URL, forget the
// credit, and the instance is out of compliance with a map that still looks
// right.
func (s *Server) handleMapConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mapConfigResponse{
		StyleURL:     s.MapStyle.URL,
		DarkStyleURL: s.MapStyle.DarkURL,
	})
}

type mapItemResponse struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Category      string  `json:"category"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
	GoogleMapsURL string  `json:"google_maps_url"`
}

// The zoom the coordinate bias is applied at. 17 is street level -- close
// enough that Google resolves the query against the right block, far enough
// that a slightly-off coordinate still contains the place.
const mapsBiasZoom = 17

// googleMapsURL builds the outbound "View on Google Maps" link for a place.
//
// The twin of googleMapsUrl in web/js/url.js, and deliberately identical to it:
// the same place must produce the same URL whether the link is rendered from
// this payload or built in the browser. Duplicated across the language boundary
// rather than made the single source, because the single-marker map embed is
// driven entirely by its own attributes and has no server payload to read. See
// the note in web/js/url.js.
//
// Two forms, and which one is used depends only on whether there is a name to
// search for.
//
// With a title, the link is a *text search biased by the coordinates*:
//
//	https://www.google.com/maps/search/<title>, <address>/@<lat>,<lng>,17z
//
// That lands on the place's own Google entry -- hours, reviews, photos -- with
// no API key and no place ID. The place ID this was long assumed to require
// turns out to be unnecessary: Google's documented query parameter takes "a
// place name, address, or comma-separated latitude/longitude coordinates", and
// a bare coordinate pair is *defined* to produce a dropped pin. Sending only
// coordinates, which is what Caravel did until Stage 29, asked for the pin.
//
// Three things measured during Stage 29 planning explain the exact shape, and
// none of them is guessable from the documentation:
//
//   - Coordinates inside the query parameter do not bias anything. They are
//     read as literal text: "Starbucks 48.8584,2.2945" returned results in San
//     Francisco. The bias has to be the /@lat,lng,z path segment.
//   - The bias is what makes this trustworthy for an ordinary place. A name
//     and address alone landed on a Starbucks several hundred metres from the
//     address given -- right chain, wrong branch.
//   - So this form is chosen over the documented ?api=1&query= one knowing the
//     trade: /@lat,lng,z is Google's own internal canonical form, stable for a
//     decade and promised by nobody. Landing on the wrong branch of a chain is
//     the worse failure, and if the form ever breaks this is one line.
//
// Without a usable title -- or with a title that is itself just a coordinate,
// which would make the search a tautology -- it falls back to the documented
// keyless coordinate form, which is what every link in Caravel used to be:
//
//	https://www.google.com/maps/search/?api=1&query=<lat>,<lng>
//
// Deliberately absent: the utm_source and utm_campaign parameters Google's
// documentation recommends. They are optional, and they tell Google which app
// sent the user.
func googleMapsURL(lat, lng float64, title string, address *string) string {
	coords := formatCoord(lat) + "," + formatCoord(lng)

	query := strings.TrimSpace(title)
	if query == "" || looksLikeCoordinate(query) {
		return "https://www.google.com/maps/search/?api=1&query=" + coords
	}
	if address != nil && strings.TrimSpace(*address) != "" {
		query += ", " + strings.TrimSpace(*address)
	}

	return "https://www.google.com/maps/search/" + escapeMapsQuery(query) +
		"/@" + coords + "," + strconv.Itoa(mapsBiasZoom) + "z"
}

// escapeMapsQuery encodes a search phrase for the path segment it sits in.
//
// Hand-rolled rather than url.PathEscape, for two reasons. The form verified in
// a browser during Stage 29 planning is the one Google itself emits -- spaces as
// "+" and commas left alone -- and PathEscape writes those as %20 and %2C.
// More importantly, PathEscape and the browser's encodeURIComponent disagree
// about several ordinary characters: an apostrophe is %27 to Go and untouched
// to JS, so "Bob's Cafe" would have produced two different URLs from the two
// twins. This escapes exactly the characters that would otherwise break the
// URL, and the JS twin does the same in the same order.
//
// Non-ASCII is left raw. Both twins leave it raw, which is what matters here,
// and browsers encode it on the way out.
func escapeMapsQuery(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// "%" first, so nothing written below is escaped twice.
		switch r {
		case '%':
			b.WriteString("%25")
		case '#':
			b.WriteString("%23")
		case '?':
			b.WriteString("%3F")
		case '&':
			b.WriteString("%26")
		case '+':
			b.WriteString("%2B")
		case '/':
			b.WriteString("%2F")
		case '\\':
			b.WriteString("%5C")
		case ' ':
			b.WriteByte('+')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatCoord renders a coordinate the way JS renders a number in a template
// literal: the shortest form that round-trips. Before Stage 29 this side used
// %f, which is six decimals with the trailing zeros left on, so the same place
// produced a different string here than in the browser.
func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// looksLikeCoordinate reports whether a title is really just a coordinate pair,
// in which case searching for it as text is a tautology and the coordinate form
// is the honest link. Cheap and deliberately loose: digits, separators and
// nothing else.
func looksLikeCoordinate(s string) bool {
	if !strings.ContainsRune(s, ',') {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return !strings.ContainsRune("0123456789+-., ", r)
	}) < 0
}

func mapItemToResponse(i db.MapItem) mapItemResponse {
	return mapItemResponse{
		ID:            i.ID,
		Title:         i.Title,
		Category:      i.Category,
		Lat:           i.Lat,
		Lng:           i.Lng,
		GoogleMapsURL: googleMapsURL(i.Lat, i.Lng, i.Title, i.Address),
	}
}

func (s *Server) handleGetTripMap(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}

	items, err := s.Store.ListMapItems(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load map")
		return
	}

	resp := make([]mapItemResponse, len(items))
	for i, it := range items {
		resp[i] = mapItemToResponse(it)
	}
	writeJSON(w, http.StatusOK, resp)
}
