package geocode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The behaviour that arrived with the lift: nil is a valid Client meaning
// "disabled", and calling it fails legibly rather than panicking. The mapping
// itself is covered end to end through the handler in
// internal/httpapi/geocode_test.go, which is where it was tested before the
// move and stayed green through it.

func TestNewReturnsNilForAnEmptyEndpoint(t *testing.T) {
	if c := New(""); c != nil {
		t.Errorf("New(\"\") = %v, want nil — nil is the off switch", c)
	}
	if c := New("http://example.invalid/search"); c == nil {
		t.Error("New(url) = nil, want a client")
	}
}

func TestSearchOnNilClientIsAnErrorNotAPanic(t *testing.T) {
	var c *Client
	// A missed nil check should fail where it happens, not three frames later
	// in a nil dereference.
	_, err := c.Search(context.Background(), "Reykjavik", "")
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestSearchSkipsUnparseableRowsRatherThanFailing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
		  {"display_name":"Good","lat":"64.1","lon":"-21.9"},
		  {"display_name":"Bad coords","lat":"north","lon":"-21.9"},
		  {"display_name":"","lat":"64.2","lon":"-21.8"},
		  {"display_name":"Also good","lat":"64.3","lon":"-21.7"}
		]`)
	}))
	defer srv.Close()

	got, err := New(srv.URL).Search(context.Background(), "Reykjavik", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want the 2 usable ones: %+v", len(got), got)
	}
	if got[0].DisplayName != "Good" || got[1].DisplayName != "Also good" {
		t.Errorf("results = %+v", got)
	}
}

func TestSearchReturnsEmptyNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	// "searched, found nothing" must be distinguishable from "did not search",
	// and the handler serialises this straight to JSON where nil becomes null.
	got, err := New(srv.URL).Search(context.Background(), "zzzz", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == nil {
		t.Error("Search returned nil, want an empty slice")
	}
}

func TestSearchSendsAnIdentifyingUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	// A condition of using OSM's public instance, not politeness: anonymous
	// bulk traffic is what gets blocked.
	if _, err := New(srv.URL).Search(context.Background(), "Reykjavik", ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if ua == "" || ua == "Go-http-client/1.1" {
		t.Errorf("User-Agent = %q, want an identifying one", ua)
	}
}

// Reverse geocoding (Stage 22 Milestone 5).
//
// The derivation is the part worth pinning hardest: the reverse endpoint is
// *computed* from the configured search URL rather than configured separately,
// so a wrong derivation would send a user's coordinates to a URL this package
// invented.

func TestReverseURLDerivation(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     string
		wantOK   bool
	}{
		{
			name:     "the public instance",
			endpoint: "https://nominatim.openstreetmap.org/search",
			want:     "https://nominatim.openstreetmap.org/reverse",
			wantOK:   true,
		},
		{
			name:     "a self-hosted instance under a path prefix",
			endpoint: "https://maps.example.org/nominatim/search",
			want:     "https://maps.example.org/nominatim/reverse",
			wantOK:   true,
		},
		{
			name:     "a trailing slash",
			endpoint: "https://nominatim.example.org/search/",
			want:     "https://nominatim.example.org/reverse",
			wantOK:   true,
		},
		{
			name:     "a query string is preserved",
			endpoint: "https://maps.example.org/search?key=abc",
			want:     "https://maps.example.org/reverse?key=abc",
			wantOK:   true,
		},
		// The honest refusals. An endpoint this package does not recognise gets
		// "unavailable" rather than a URL guessed from it.
		{name: "not a search endpoint", endpoint: "https://maps.example.org/geocode", wantOK: false},
		{name: "no path at all", endpoint: "https://maps.example.org", wantOK: false},
		{name: "search is only a prefix", endpoint: "https://maps.example.org/searching", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.endpoint)
			got, ok := c.ReverseURL()
			if ok != tc.wantOK {
				t.Fatalf("ReverseURL() ok = %v, want %v (got %q)", ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("ReverseURL() = %q, want %q", got, tc.want)
			}
			if c.ReverseAvailable() != tc.wantOK {
				t.Errorf("ReverseAvailable() = %v, want %v", c.ReverseAvailable(), tc.wantOK)
			}
		})
	}
}

func TestReverseAvailableIsFalseWhenDisabled(t *testing.T) {
	// nil is the off switch for the whole package, so it must not claim a
	// capability either.
	if New("").ReverseAvailable() {
		t.Error("ReverseAvailable() = true on a disabled client")
	}
	var c *Client
	if _, err := c.Reverse(context.Background(), 64.1, -21.9, ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestReverseReturnsTheAddressAndTheQueriedPoint(t *testing.T) {
	var gotPath, gotQuery string
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotUA = r.URL.Path, r.URL.RawQuery, r.Header.Get("User-Agent")
		// Nominatim answers a reverse lookup with one object, not an array, and
		// echoes the coordinates of whatever it matched -- here deliberately
		// different from the ones asked about.
		fmt.Fprint(w, `{"display_name":"Laugavegur 1, Reykjavik","lat":"64.147","lon":"-21.933"}`)
	}))
	defer srv.Close()

	c := New(srv.URL + "/search")
	got, err := c.Reverse(context.Background(), 64.1466, -21.9426, "")
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	if gotPath != "/reverse" {
		t.Errorf("upstream path = %q, want /reverse", gotPath)
	}
	// lon, not lng: the wire name is the provider's, and getting it wrong
	// returns an address for the equator.
	for _, want := range []string{"lat=64.1466", "lon=-21.9426", "format=jsonv2"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q is missing %q", gotQuery, want)
		}
	}
	if !strings.HasPrefix(gotUA, "Caravel/") {
		t.Errorf("User-Agent = %q, want the identifying one -- it is a condition of using the public instance", gotUA)
	}

	if got.DisplayName != "Laugavegur 1, Reykjavik" {
		t.Errorf("DisplayName = %q", got.DisplayName)
	}
	// The point asked about, not the one upstream matched: the caller chose
	// those coordinates and this function does not move them.
	if got.Lat != 64.1466 || got.Lng != -21.9426 {
		t.Errorf("got %v,%v back, want the queried 64.1466,-21.9426", got.Lat, got.Lng)
	}
}

func TestReverseTreatsAnEmptyAnswerAsNoResult(t *testing.T) {
	// The middle of an ocean: Nominatim answers 200 with an error object, which
	// decodes cleanly into a zero-valued struct. Without the emptiness check
	// that would look like a successful lookup of a nameless place.
	for name, body := range map[string]string{
		"nominatim error object": `{"error":"Unable to geocode"}`,
		"empty object":           `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer srv.Close()

			_, err := New(srv.URL+"/search").Reverse(context.Background(), 0, 0, "")
			if !errors.Is(err, ErrNoResult) {
				t.Errorf("err = %v, want ErrNoResult", err)
			}
		})
	}
}

func TestReverseReportsAnUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := New(srv.URL+"/search").Reverse(context.Background(), 64.1, -21.9, "")
	var status ErrUpstreamStatus
	if !errors.As(err, &status) || status.Code != http.StatusServiceUnavailable {
		t.Errorf("err = %v, want ErrUpstreamStatus{503}", err)
	}
}

func TestReverseOnAnUnderivableEndpoint(t *testing.T) {
	_, err := New("https://maps.example.org/geocode").Reverse(context.Background(), 64.1, -21.9, "")
	if !errors.Is(err, ErrNoReverseEndpoint) {
		t.Errorf("err = %v, want ErrNoReverseEndpoint", err)
	}
}

// The language to name places in (Stage 22 Milestone 6, second follow-up).
//
// Empty means "do not ask", which leaves the provider's default -- names in the
// local language of the place. That is the right answer for a caller with no
// user in front of it, which is what both assist call sites are.
func TestLocaleReachesTheUpstream(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if strings.HasSuffix(r.URL.Path, "/reverse") {
			fmt.Fprint(w, `{"display_name":"Somewhere","lat":"1","lon":"2"}`)
			return
		}
		fmt.Fprint(w, `[{"display_name":"Somewhere","lat":"1","lon":"2"}]`)
	}))
	defer srv.Close()
	c := New(srv.URL + "/search")

	cases := []struct {
		name   string
		call   func()
		locale string
		want   bool
	}{
		{"search with a locale", func() { _, _ = c.Search(context.Background(), "x", "de") }, "de", true},
		{"search without one", func() { _, _ = c.Search(context.Background(), "x", "") }, "", false},
		{"reverse with a locale", func() { _, _ = c.Reverse(context.Background(), 1, 2, "de") }, "de", true},
		{"reverse without one", func() { _, _ = c.Reverse(context.Background(), 1, 2, "") }, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQuery = ""
			tc.call()
			has := strings.Contains(gotQuery, "accept-language="+tc.locale) && tc.locale != ""
			if has != tc.want {
				t.Errorf("query = %q, want accept-language %v", gotQuery, tc.want)
			}
			if !tc.want && strings.Contains(gotQuery, "accept-language") {
				t.Errorf("query = %q, want no accept-language at all", gotQuery)
			}
		})
	}
}

// Stage 29 Milestone 3. Nominatim reports osm_type and osm_id on every result
// and this package used to discard both, which is what kept Caravel from
// linking to a real OpenStreetMap feature page.
func TestSearchCapturesOSMIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// osm_id is a JSON *number* on the wire, unlike lat/lon which are
		// strings. The way id below is deliberately large: decoded through a
		// float64 it would still be exact, but the point is that it is kept as
		// the digits that arrived.
		_, _ = w.Write([]byte(`[
			{"display_name":"Hallgrimskirkja, Reykjavik","lat":"64.1417951","lon":"-21.9267103","osm_type":"way","osm_id":1234567890123},
			{"display_name":"A node","lat":"1.0","lon":"2.0","osm_type":"node","osm_id":240109189},
			{"display_name":"No identity at all","lat":"3.0","lon":"4.0"},
			{"display_name":"Type Caravel does not know","lat":"5.0","lon":"6.0","osm_type":"nonsense","osm_id":7},
			{"display_name":"Half an identity","lat":"7.0","lon":"8.0","osm_type":"node"}
		]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).Search(context.Background(), "anything", "en")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d results, want 5", len(got))
	}

	want := []struct{ osmType, osmID string }{
		{"way", "1234567890123"},
		{"node", "240109189"},
		// A row with no identity, an unrecognised element type, or only half of
		// one leaves both fields empty: half an identity cannot build a URL,
		// and storing half invites a render site to interpolate an empty
		// string into the path.
		{"", ""},
		{"", ""},
		{"", ""},
	}
	for i, w := range want {
		if got[i].OSMType != w.osmType || got[i].OSMID != w.osmID {
			t.Errorf("result %d (%s): got %q/%q, want %q/%q",
				i, got[i].DisplayName, got[i].OSMType, got[i].OSMID, w.osmType, w.osmID)
		}
	}
}
