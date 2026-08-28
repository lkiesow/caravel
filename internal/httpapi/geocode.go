package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"caravel/internal/geocode"
)

// Address search, proxied rather than called from the browser. The proxying
// itself, the User-Agent and the response mapping all live in
// internal/geocode; what is left here is the HTTP shape -- who may ask, what
// counts as too short, and which status a failure earns.
//
// The package moved out in Stage 16 Milestone 3 so internal/assist could reach
// it. See its doc comment for why the browser does not call Nominatim itself.

// requestLocale is the language the caller wants places named in.
//
// It comes from the client rather than from the Accept-Language header, for the
// reason the assistant's own locale field gives: the app's language is a
// setting in the browser (localStorage), and it is routinely *not* what the
// browser advertises -- somebody running a German UI on an English system is
// the ordinary case, not the odd one.
//
// Empty when absent or malformed, which means "do not ask" and leaves the
// provider's default: names in the local language of the place. normaliseLocale
// is the same check the assistant applies before a locale reaches a third
// party.
func requestLocale(r *http.Request) string {
	return normaliseLocale(r.URL.Query().Get("locale"))
}

func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	if s.Geocoder == nil {
		// 501 rather than 404: the route exists, the capability is switched
		// off. The client already knows from /auth/me and should not be
		// asking, so this is a backstop, not a path anyone should hit.
		writeError(w, http.StatusNotImplemented, "address search is not enabled on this server")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < geocode.MinQueryLen {
		writeError(w, http.StatusBadRequest, "search term too short")
		return
	}

	results, err := s.Geocoder.Search(r.Context(), query, requestLocale(r))
	if err != nil {
		// Deliberately not 500 and deliberately not the upstream's own words:
		// this is somebody else's service being slow or unhappy, which is a
		// bad gateway, and its error text is not ours to forward to a user.
		writeError(w, http.StatusBadGateway, "the address search service could not be reached")
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// handleReverseGeocode turns a coordinate back into an address, so clicking the
// map can answer "what is here" instead of only "where is this".
//
// It answers with a candidate the client *offers* rather than one it applies:
// see the note in location-editor-page.js. That is a client decision, but it is
// the reason this endpoint returns one result and nothing else -- there is no
// "confidence" for the client to threshold on and no list to choose from.
func (s *Server) handleReverseGeocode(w http.ResponseWriter, r *http.Request) {
	if s.Geocoder == nil {
		writeError(w, http.StatusNotImplemented, "address search is not enabled on this server")
		return
	}
	// Separate from the check above, and a different sentence: the geocoder can
	// be configured while its endpoint is not one a reverse URL can be derived
	// from (see geocode.ReverseURL). The client knows from /auth/me and should
	// not be asking, so this is a backstop like the one above.
	if !s.Geocoder.ReverseAvailable() {
		writeError(w, http.StatusNotImplemented, "reverse geocoding is not available on this server")
		return
	}

	lat, lng, ok := parseLatLng(w, r.URL.Query().Get("lat"), r.URL.Query().Get("lng"))
	if !ok {
		return
	}

	result, err := s.Geocoder.Reverse(r.Context(), lat, lng, requestLocale(r))
	if err != nil {
		// Nothing there is not a failure -- the middle of an ocean has no
		// address. 404 rather than an empty 200, so a client cannot mistake it
		// for a blank address worth accepting.
		if errors.Is(err, geocode.ErrNoResult) {
			writeError(w, http.StatusNotFound, "no address found for that location")
			return
		}
		// Same reasoning as the search handler: somebody else's service being
		// unhappy is a bad gateway, and its words are not ours to forward.
		writeError(w, http.StatusBadGateway, "the address search service could not be reached")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// parseLatLng validates a coordinate pair from the query string, answering the
// request itself on failure.
//
// Range-checked, not merely parseable: a latitude of 500 is a client bug, and
// forwarding it to a volunteer-run service to be rejected is rude in a way that
// scales with how often the bug fires. NaN and the infinities parse happily as
// floats and are refused here for the same reason.
func parseLatLng(w http.ResponseWriter, rawLat, rawLng string) (float64, float64, bool) {
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(rawLat), 64)
	lng, lngErr := strconv.ParseFloat(strings.TrimSpace(rawLng), 64)
	if latErr != nil || lngErr != nil {
		writeError(w, http.StatusBadRequest, "lat and lng are required and must be numbers")
		return 0, 0, false
	}
	if math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) ||
		lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		writeError(w, http.StatusBadRequest, "lat must be between -90 and 90, lng between -180 and 180")
		return 0, 0, false
	}
	return lat, lng, true
}

// handleResolveMapLink turns a pasted Google Maps link into coordinates.
//
// Unlike the two handlers above this needs no geocoder configured: it reads a
// URL and, if it has to, follows a redirect. So there is no 501 branch and no
// capability flag -- it works on every instance.
//
// It lives under /geocode all the same, because what it produces is a
// coordinate and because it should spend the same rate-limit budget: both are
// this app making an outbound request on a user keystroke. Worth knowing: the
// *control* that reaches it is inside the address-search panel, which is hidden
// unless `geocoding` is on, so an instance with no geocoder has the endpoint
// and no way to press it. That is a UI coupling rather than a rule, and if
// somebody wants link resolution without address search it is the panel that
// needs splitting, not this.
func (s *Server) handleResolveMapLink(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	// A cap before anything is parsed. A URL is a query parameter, and there is
	// no reason for a real Maps link to be longer than this.
	if len(raw) > 2048 {
		writeError(w, http.StatusBadRequest, "that URL is too long")
		return
	}

	result, err := geocode.ResolveMapLink(r.Context(), raw, requestLocale(r))
	if err != nil {
		switch {
		case errors.Is(err, geocode.ErrNotAMapLink):
			// 400 and not 502: nothing was tried. The caller sent something
			// this endpoint does not follow, which is a fact about the request.
			writeError(w, http.StatusBadRequest, "that is not a Google Maps link")
		case errors.Is(err, geocode.ErrNoCoordinates):
			// The link was followed and names no single place -- a search
			// results page, say. Distinct from the above so the client can say
			// which of the two happened.
			writeError(w, http.StatusNotFound, "that link does not point at a single place")
		default:
			writeError(w, http.StatusBadGateway, "that link could not be resolved")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}
