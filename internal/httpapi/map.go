package httpapi

import (
	"net/http"
	"strconv"

	"caravel/internal/db"
)

// The tile layer the browser draws the map with, and its shipped default:
// the standard OpenStreetMap tiles, which is what Caravel used when the URL
// was a literal in the frontend.
//
// Worth knowing before changing the default: these tiles label places in the
// local script, so Japan reads 東京 rather than Tokyo, and no parameter on
// them changes that -- the labels are pixels in a pre-rendered PNG. That is
// the reason this is configuration at all, and the alternatives (and what
// each is good for) are written up in docs/configuration/server.md.
//
// The default stays OSM because it adds no third party to a stock install and
// needs no key. Picking a nicer-labelled provider for everyone would send
// every user of every instance to a company that did not ask for the traffic.
const (
	DefaultTileURL         = "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
	DefaultTileAttribution = `&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors`
	DefaultTileMaxZoom     = 19
)

// TileSettings is the tile layer as the operator configured it. A zero field
// means unset and takes the default above -- the same convention the assist
// limits use.
type TileSettings struct {
	URL string
	// Attribution is HTML, rendered as markup by Leaflet and deliberately not
	// escaped: every provider's terms require a working link back, and this
	// value comes from the operator's environment rather than from a user.
	Attribution string
	MaxZoom     int
}

func (t TileSettings) withDefaults() TileSettings {
	if t.URL == "" {
		t.URL = DefaultTileURL
	}
	if t.Attribution == "" {
		t.Attribution = DefaultTileAttribution
	}
	if t.MaxZoom <= 0 {
		t.MaxZoom = DefaultTileMaxZoom
	}
	return t
}

type tileConfigResponse struct {
	TileURL         string `json:"tile_url"`
	TileAttribution string `json:"tile_attribution"`
	MaxZoom         int    `json:"max_zoom"`
}

// handleMapConfig tells the frontend where to fetch tiles from.
//
// Behind RequireAuth, unlike /auth/config: no page renders a map before
// anyone has a session, so there is nothing to gain by publishing the
// instance's tile provider to anonymous callers.
func (s *Server) handleMapConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tileConfigResponse{
		TileURL:         s.Tiles.URL,
		TileAttribution: s.Tiles.Attribution,
		MaxZoom:         s.Tiles.MaxZoom,
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

// googleMapsURL builds the outbound "View on Google Maps" link for a place.
//
// The twin of googleMapsUrl in web/js/url.js, and deliberately identical to it:
// the same place must produce the same URL whether the link is rendered from
// this payload or built in the browser. Before Stage 29 Milestone 1 they were
// not identical -- this side used %f, which is six decimals with the trailing
// zeros left on, and the two JS copies interpolated the raw number. FormatFloat
// with a precision of -1 is the shortest form that round-trips, which is what
// JS gives a number in a template literal, so the two now agree byte for byte.
//
// Duplicated across the language boundary rather than made the single source,
// because the single-marker map embed is driven entirely by its own attributes
// and has no server payload to read. See the note in web/js/url.js.
func googleMapsURL(lat, lng float64) string {
	return "https://www.google.com/maps/search/?api=1&query=" +
		strconv.FormatFloat(lat, 'f', -1, 64) + "," + strconv.FormatFloat(lng, 'f', -1, 64)
}

func mapItemToResponse(i db.MapItem) mapItemResponse {
	return mapItemResponse{
		ID:            i.ID,
		Title:         i.Title,
		Category:      i.Category,
		Lat:           i.Lat,
		Lng:           i.Lng,
		GoogleMapsURL: googleMapsURL(i.Lat, i.Lng),
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
