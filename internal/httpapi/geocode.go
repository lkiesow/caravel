package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"caravel/internal/buildinfo"
)

// Address search, proxied rather than called from the browser.
//
// Three reasons, all of which outlive Nominatim specifically:
//   - OSM's usage policy asks for an identifying User-Agent and no more than
//     one request a second. A browser can promise neither.
//   - A self-hosted app should not hand a user's typing to a third party the
//     moment a page loads. Going through here means one place to see it and
//     one place to switch it off (CARAVEL_GEOCODER_URL="").
//   - The upstream payload is large and provider-shaped. Mapping it down here
//     keeps the provider out of the client, so swapping geocoders later is a
//     change to this file and nothing else.
const (
	geocodeTimeout    = 6 * time.Second
	geocodeMaxResults = 5
	// Nominatim rejects queries with no useful content anyway; this keeps
	// the obviously-pointless ones from ever leaving the building.
	geocodeMinQueryLen = 2
)

type geocodeResult struct {
	DisplayName string  `json:"display_name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// The subset of Nominatim's response we use. Note lat/lon arrive as *strings*,
// which is why they are parsed rather than decoded straight into float64.
type nominatimResult struct {
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
}

func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	if s.GeocoderURL == "" {
		// 501 rather than 404: the route exists, the capability is switched
		// off. The client already knows from /auth/me and should not be
		// asking, so this is a backstop, not a path anyone should hit.
		writeError(w, http.StatusNotImplemented, "address search is not enabled on this server")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < geocodeMinQueryLen {
		writeError(w, http.StatusBadRequest, "search term too short")
		return
	}

	results, err := s.geocode(r.Context(), query)
	if err != nil {
		// Deliberately not 500 and deliberately not the upstream's own words:
		// this is somebody else's service being slow or unhappy, which is a
		// bad gateway, and its error text is not ours to forward to a user.
		writeError(w, http.StatusBadGateway, "the address search service could not be reached")
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) geocode(ctx context.Context, query string) ([]geocodeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, geocodeTimeout)
	defer cancel()

	endpoint, err := url.Parse(s.GeocoderURL)
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("q", query)
	q.Set("format", "jsonv2")
	q.Set("limit", strconv.Itoa(geocodeMaxResults))
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	// Identifying ourselves is a condition of using the public instance, not
	// politeness: anonymous bulk traffic is what gets blocked.
	req.Header.Set("User-Agent", "Caravel/"+buildinfo.Version+" (self-hosted trip planner)")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errUpstreamStatus{code: resp.StatusCode}
	}

	var raw []nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	// Non-nil even when empty, so the client gets [] rather than null and can
	// tell "searched, found nothing" from "did not search".
	out := make([]geocodeResult, 0, len(raw))
	for _, item := range raw {
		lat, latErr := strconv.ParseFloat(item.Lat, 64)
		lng, lngErr := strconv.ParseFloat(item.Lon, 64)
		// One unparseable row should not fail the whole search; skip it and
		// return the rest.
		if latErr != nil || lngErr != nil || item.DisplayName == "" {
			continue
		}
		out = append(out, geocodeResult{DisplayName: item.DisplayName, Lat: lat, Lng: lng})
	}
	return out, nil
}

type errUpstreamStatus struct{ code int }

func (e errUpstreamStatus) Error() string {
	return "geocoder responded with status " + strconv.Itoa(e.code)
}
