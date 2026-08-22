package httpapi

import (
	"net/http"
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
