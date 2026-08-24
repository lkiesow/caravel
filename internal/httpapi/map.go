package httpapi

import (
	"fmt"
	"net/http"

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

func mapItemToResponse(i db.MapItem) mapItemResponse {
	return mapItemResponse{
		ID:            i.ID,
		Title:         i.Title,
		Category:      i.Category,
		Lat:           i.Lat,
		Lng:           i.Lng,
		GoogleMapsURL: fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%f,%f", i.Lat, i.Lng),
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
