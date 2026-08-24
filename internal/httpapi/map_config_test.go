package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func decodeTileConfig(t *testing.T, body []byte) tileConfigResponse {
	t.Helper()
	var got tileConfigResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode /map/config: %v -- body %s", err, body)
	}
	return got
}

// An unconfigured instance has to answer with the tiles Caravel shipped with,
// because that answer is the frontend's only source for them: the URL is no
// longer a literal in leaflet-map.js.
func TestMapConfigDefaults(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("tiles-default")

	w := ts.do(http.MethodGet, "/api/map/config", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /map/config: got %d, want 200 -- body %s", w.Code, w.Body.String())
	}

	got := decodeTileConfig(t, w.Body.Bytes())
	if got.TileURL != DefaultTileURL {
		t.Errorf("tile_url = %q, want the shipped default %q", got.TileURL, DefaultTileURL)
	}
	if got.TileAttribution != DefaultTileAttribution {
		t.Errorf("tile_attribution = %q, want %q", got.TileAttribution, DefaultTileAttribution)
	}
	if got.MaxZoom != DefaultTileMaxZoom {
		t.Errorf("max_zoom = %d, want %d", got.MaxZoom, DefaultTileMaxZoom)
	}
}

// The point of the whole change: an operator who sets a provider gets that
// provider, markup and all. The attribution is checked byte for byte because
// escaping it would break the link every provider's terms require.
func TestMapConfigOverride(t *testing.T) {
	const (
		url         = "https://basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png"
		attribution = `&copy; <a href="https://carto.com/attributions">CARTO</a>`
	)

	ts := newTestServerWithOptions(t, func(o *Options) {
		o.Tiles = TileSettings{URL: url, Attribution: attribution, MaxZoom: 20}
	})
	cookie := ts.login("tiles-override")

	w := ts.do(http.MethodGet, "/api/map/config", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /map/config: got %d, want 200 -- body %s", w.Code, w.Body.String())
	}

	got := decodeTileConfig(t, w.Body.Bytes())
	if got.TileURL != url {
		t.Errorf("tile_url = %q, want %q", got.TileURL, url)
	}
	if got.TileAttribution != attribution {
		t.Errorf("tile_attribution = %q, want the configured markup unescaped %q", got.TileAttribution, attribution)
	}
	if got.MaxZoom != 20 {
		t.Errorf("max_zoom = %d, want 20", got.MaxZoom)
	}
}

// A half-set TileSettings takes the defaults field by field, so an operator
// who overrides only the URL does not silently lose the max zoom.
func TestMapConfigPartialOverrideKeepsOtherDefaults(t *testing.T) {
	const url = "https://tiles.example.invalid/{z}/{x}/{y}.png"

	ts := newTestServerWithOptions(t, func(o *Options) {
		o.Tiles = TileSettings{URL: url}
	})
	cookie := ts.login("tiles-partial")

	got := decodeTileConfig(t, ts.do(http.MethodGet, "/api/map/config", cookie, "").Body.Bytes())
	if got.TileURL != url {
		t.Errorf("tile_url = %q, want %q", got.TileURL, url)
	}
	if got.TileAttribution != DefaultTileAttribution {
		t.Errorf("tile_attribution = %q, want the default %q", got.TileAttribution, DefaultTileAttribution)
	}
	if got.MaxZoom != DefaultTileMaxZoom {
		t.Errorf("max_zoom = %d, want the default %d", got.MaxZoom, DefaultTileMaxZoom)
	}
}

// Unlike /auth/config, this one is behind the session: no page draws a map
// before anyone has logged in.
func TestMapConfigRequiresAuth(t *testing.T) {
	ts := newTestServer(t)

	w := ts.do(http.MethodGet, "/api/map/config", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /map/config: got %d, want 401 -- body %s", w.Code, w.Body.String())
	}
}
