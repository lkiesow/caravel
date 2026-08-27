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

	results, err := s.Geocoder.Search(r.Context(), query)
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

	result, err := s.Geocoder.Reverse(r.Context(), lat, lng)
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
