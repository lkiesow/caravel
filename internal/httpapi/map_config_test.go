package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func decodeMapConfig(t *testing.T, body []byte) mapConfigResponse {
	t.Helper()
	var got mapConfigResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode /map/config: %v -- body %s", err, body)
	}
	return got
}

// An unconfigured instance has to answer with the styles Caravel shipped with,
// because that answer is the frontend's only source for them: the map is no
// longer a literal in leaflet-map.js.
func TestMapConfigDefaults(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("style-default")

	w := ts.do(http.MethodGet, "/api/map/config", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /map/config: got %d, want 200 -- body %s", w.Code, w.Body.String())
	}

	got := decodeMapConfig(t, w.Body.Bytes())
	if got.StyleURL != DefaultMapStyleURL {
		t.Errorf("style_url = %q, want the shipped default %q", got.StyleURL, DefaultMapStyleURL)
	}
	if got.DarkStyleURL != DefaultMapStyleDarkURL {
		t.Errorf("dark_style_url = %q, want %q", got.DarkStyleURL, DefaultMapStyleDarkURL)
	}
}

// An operator who names their own styles gets them, both of them.
func TestMapConfigOverride(t *testing.T) {
	const (
		light = "https://tiles.example.invalid/styles/day"
		dark  = "https://tiles.example.invalid/styles/night"
	)

	ts := newTestServerWithOptions(t, func(o *Options) {
		o.MapStyle = MapStyleSettings{URL: light, DarkURL: dark}
	})
	cookie := ts.login("style-override")

	got := decodeMapConfig(t, ts.do(http.MethodGet, "/api/map/config", cookie, "").Body.Bytes())
	if got.StyleURL != light {
		t.Errorf("style_url = %q, want %q", got.StyleURL, light)
	}
	if got.DarkStyleURL != dark {
		t.Errorf("dark_style_url = %q, want %q", got.DarkStyleURL, dark)
	}
}

// The interesting half of withDefaults. An operator who names a style of their
// own but no dark counterpart gets *their* map in both modes -- not ours in
// one of them, which would mean a reader flipping to dark landed on a
// completely different instance's cartography.
func TestMapConfigCustomLightWithoutDarkRepeatsItself(t *testing.T) {
	const light = "https://tiles.example.invalid/styles/house"

	ts := newTestServerWithOptions(t, func(o *Options) {
		o.MapStyle = MapStyleSettings{URL: light}
	})
	cookie := ts.login("style-nodark")

	got := decodeMapConfig(t, ts.do(http.MethodGet, "/api/map/config", cookie, "").Body.Bytes())
	if got.StyleURL != light {
		t.Errorf("style_url = %q, want %q", got.StyleURL, light)
	}
	if got.DarkStyleURL != light {
		t.Errorf("dark_style_url = %q, want the operator's own style %q, not a shipped default", got.DarkStyleURL, light)
	}
}

// ...whereas leaving both unset must still give the shipped *pair*, so a stock
// instance has a real dark map rather than a light one twice.
func TestMapConfigDarkOnlyOverrideKeepsDefaultLight(t *testing.T) {
	const dark = "https://tiles.example.invalid/styles/night"

	ts := newTestServerWithOptions(t, func(o *Options) {
		o.MapStyle = MapStyleSettings{DarkURL: dark}
	})
	cookie := ts.login("style-darkonly")

	got := decodeMapConfig(t, ts.do(http.MethodGet, "/api/map/config", cookie, "").Body.Bytes())
	if got.StyleURL != DefaultMapStyleURL {
		t.Errorf("style_url = %q, want the shipped default %q", got.StyleURL, DefaultMapStyleURL)
	}
	if got.DarkStyleURL != dark {
		t.Errorf("dark_style_url = %q, want %q", got.DarkStyleURL, dark)
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
