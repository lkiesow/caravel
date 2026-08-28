package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"caravel/internal/geocode"
)

// Every test here points the server at its own httptest.Server. None of them
// may reach OpenStreetMap's public Nominatim - newTestServerWithStore leaves
// Geocoder nil precisely so that forgetting to set it fails loudly (501)
// rather than quietly sending a real request.

const nominatimTwoResults = `[
  {"display_name":"Reykjavík, Iceland","lat":"64.1466","lon":"-21.9426","place_id":1},
  {"display_name":"Reykjavík Airport, Iceland","lat":"64.1300","lon":"-21.9406","place_id":2}
]`

// stubGeocoder points ts at a fake upstream and returns the requests it saw.
//
// The configured URL ends in /search, matching a real deployment: since Stage 22
// the reverse endpoint is *derived* from it by swapping that last segment, so a
// bare host here would leave every reverse test testing the "cannot derive one"
// path instead of the lookup. The handler sees which path was asked for.
func stubGeocoder(t *testing.T, ts *testServer, handler http.HandlerFunc) *[]*http.Request {
	t.Helper()
	var seen []*http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(r.Context()))
		handler(w, r)
	}))
	t.Cleanup(upstream.Close)
	ts.Geocoder = geocode.New(upstream.URL + "/search")
	return &seen
}

func TestGeocodeMapsUpstreamResults(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	seen := stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, nominatimTwoResults)
	})

	rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var got []geocode.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].DisplayName != "Reykjavík, Iceland" {
		t.Errorf("display_name = %q", got[0].DisplayName)
	}
	// Nominatim sends lat/lon as strings; the client must get numbers.
	if got[0].Lat != 64.1466 || got[0].Lng != -21.9426 {
		t.Errorf("coordinates = %v,%v want 64.1466,-21.9426", got[0].Lat, got[0].Lng)
	}

	// The proxy's own request is part of the contract: identifying ourselves
	// is a condition of using the public instance, not politeness.
	if len(*seen) != 1 {
		t.Fatalf("upstream saw %d requests, want 1", len(*seen))
	}
	req := (*seen)[0]
	if ua := req.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Caravel/") {
		t.Errorf("upstream User-Agent = %q, want it to identify Caravel", ua)
	}
	if q := req.URL.Query().Get("q"); q != "Reykjavik" {
		t.Errorf("upstream q = %q", q)
	}
	if limit := req.URL.Query().Get("limit"); limit != "5" {
		t.Errorf("upstream limit = %q, want 5", limit)
	}
}

func TestGeocodeSkipsUnparseableRowsRatherThanFailing(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
		  {"display_name":"Fine","lat":"1.5","lon":"2.5"},
		  {"display_name":"No coordinates","lat":"","lon":""},
		  {"display_name":"","lat":"3.5","lon":"4.5"}
		]`)
	})

	rec := ts.do(http.MethodGet, "/api/geocode?q=anything", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []geocode.Result
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 1 || got[0].DisplayName != "Fine" {
		t.Fatalf("got %+v, want only the one usable row", got)
	}
}

func TestGeocodeEmptyResultIsAnArrayNotNull(t *testing.T) {
	// "searched, found nothing" has to be distinguishable from "did not
	// search" on the client, which means [] and not null.
	ts := newTestServer(t)
	cookie := ts.login("alice")
	stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})

	rec := ts.do(http.MethodGet, "/api/geocode?q=zzzzzz", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}

func TestGeocodeUpstreamFailureIsABadGatewayNotA500(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"upstream error status", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "go away", http.StatusForbidden)
		}},
		{"upstream nonsense body", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<html>not json</html>")
		}},
		{"upstream too slow", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(geocode.Timeout + time.Second)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if testing.Short() && tc.name == "upstream too slow" {
				t.Skip("takes the full geocode timeout")
			}
			ts := newTestServer(t)
			cookie := ts.login("alice")
			stubGeocoder(t, ts, tc.handler)

			rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", cookie, "")
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502", rec.Code)
			}
			// Someone else's service being unhappy is not our internal error,
			// and its words are not ours to forward to a user.
			if strings.Contains(rec.Body.String(), "go away") {
				t.Errorf("upstream error text leaked to the client: %s", rec.Body.String())
			}
		})
	}
}

func TestGeocodeRejectsShortAndMissingQueries(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	seen := stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, nominatimTwoResults)
	})

	for _, path := range []string{"/api/geocode", "/api/geocode?q=", "/api/geocode?q=a", "/api/geocode?q=%20%20"} {
		rec := ts.do(http.MethodGet, path, cookie, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", path, rec.Code)
		}
	}
	// The point of the check: none of those cost the upstream anything.
	if len(*seen) != 0 {
		t.Errorf("upstream saw %d requests, want 0", len(*seen))
	}
}

func TestGeocodeRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	seen := stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, nominatimTwoResults)
	})

	rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(*seen) != 0 {
		t.Errorf("an anonymous caller reached the upstream %d time(s)", len(*seen))
	}
}

func TestGeocodeDisabledWhenNoGeocoderConfigured(t *testing.T) {
	ts := newTestServer(t) // Geocoder is nil here by default
	cookie := ts.login("alice")

	rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", cookie, "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestGeocodeIsRateLimitedSeparatelyFromLogin(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	})

	// The limiter is 20/minute/IP. Spend them, then expect the 21st refused.
	var lastOK int
	for i := 0; i < 20; i++ {
		rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", cookie, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, rec.Code)
		}
		lastOK = i + 1
	}
	rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", cookie, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: status = %d, want 429", lastOK+1, rec.Code)
	}

	// Separate budgets: exhausting geocoding must not lock anyone out of
	// logging in, which is the reason it is its own limiter and not login's.
	if !ts.LoginLimiter.Allow("192.0.2.1") {
		t.Error("the login limiter was spent by geocode requests")
	}
}

func TestAuthMeReportsGeocodingCapability(t *testing.T) {
	// The client hides the search control rather than offering one that
	// cannot work, so this flag is what makes the 501 above a backstop.
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{"configured", "http://example.invalid/search", true},
		{"disabled", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.Geocoder = geocode.New(tc.url)
			cookie := ts.login("alice")

			rec := ts.do(http.MethodGet, "/api/auth/me", cookie, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got userResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Capabilities.Geocoding != tc.want {
				t.Errorf("capabilities.geocoding = %v, want %v", got.Capabilities.Geocoding, tc.want)
			}
		})
	}
}

// Reverse geocoding: a coordinate to an address (Stage 22 Milestone 5).

func TestReverseGeocodeReturnsAnAddress(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	seen := stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"display_name":"Laugavegur 1, Reykjavík","lat":"64.147","lon":"-21.933"}`)
	})

	rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=64.1466&lng=-21.9426", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var got geocode.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.DisplayName != "Laugavegur 1, Reykjavík" {
		t.Errorf("display_name = %q", got.DisplayName)
	}
	// The coordinates asked about come back, not the ones upstream matched: the
	// client is going to keep the point the user chose and take only the
	// address, so the payload must not invite it to move the marker.
	if got.Lat != 64.1466 || got.Lng != -21.9426 {
		t.Errorf("got %v,%v, want the queried 64.1466,-21.9426", got.Lat, got.Lng)
	}

	if len(*seen) != 1 {
		t.Fatalf("upstream saw %d requests, want 1", len(*seen))
	}
	// The derived endpoint, not the configured one.
	if path := (*seen)[0].URL.Path; path != "/reverse" {
		t.Errorf("upstream path = %q, want /reverse", path)
	}
}

func TestReverseGeocodeIs501WhenNotConfigured(t *testing.T) {
	t.Run("no geocoder at all", func(t *testing.T) {
		ts := newTestServer(t)
		cookie := ts.login("alice")
		rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=64.1&lng=-21.9", cookie, "")
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", rec.Code)
		}
	})

	// The case that makes reverse_geocoding a separate capability: address
	// search works, and no reverse endpoint can be derived from its URL.
	t.Run("a geocoder whose URL has no derivable reverse endpoint", func(t *testing.T) {
		ts := newTestServer(t)
		cookie := ts.login("alice")
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, nominatimTwoResults)
		}))
		t.Cleanup(upstream.Close)
		ts.Geocoder = geocode.New(upstream.URL + "/lookup")

		if rec := ts.do(http.MethodGet, "/api/geocode?q=Reykjavik", cookie, ""); rec.Code != http.StatusOK {
			t.Fatalf("forward search status = %d, want it still working", rec.Code)
		}
		rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=64.1&lng=-21.9", cookie, "")
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("reverse status = %d, want 501", rec.Code)
		}
	})
}

// Nothing at that point is a 404, not an empty 200: a client must not be able
// to mistake it for a blank address worth accepting.
func TestReverseGeocodeIs404WhenThereIsNothingThere(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":"Unable to geocode"}`)
	})

	rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=0&lng=0", cookie, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReverseGeocodeIs502WhenUpstreamFails(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=64.1&lng=-21.9", cookie, "")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// Range-checked rather than merely parseable, and refused here: forwarding a
// latitude of 500 to a volunteer-run service to be rejected is rude in a way
// that scales with how often the client bug fires.
func TestReverseGeocodeRejectsBadCoordinates(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	seen := stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"display_name":"Somewhere","lat":"1","lon":"1"}`)
	})

	for _, query := range []string{
		"",
		"lat=64.1",
		"lng=-21.9",
		"lat=&lng=",
		"lat=north&lng=west",
		"lat=91&lng=0",
		"lat=-91&lng=0",
		"lat=0&lng=181",
		"lat=0&lng=-181",
		"lat=NaN&lng=0",
		"lat=Inf&lng=0",
	} {
		t.Run(query, func(t *testing.T) {
			rec := ts.do(http.MethodGet, "/api/geocode/reverse?"+query, cookie, "")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}

	if len(*seen) != 0 {
		t.Errorf("upstream saw %d requests; a refused coordinate must not leave the building", len(*seen))
	}
}

func TestReverseGeocodeNeedsASession(t *testing.T) {
	ts := newTestServer(t)
	stubGeocoder(t, ts, func(w http.ResponseWriter, r *http.Request) {})
	rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=64.1&lng=-21.9", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// The capability flag and the endpoint must agree: a client that trusts
// /auth/me and finds the control fails anyway is worse off than one with no
// control at all.
func TestReverseGeocodingCapabilityMatchesTheEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint func(base string) string
		want     bool
	}{
		{"a search endpoint", func(base string) string { return base + "/search" }, true},
		{"an endpoint with no derivable reverse", func(base string) string { return base + "/lookup" }, false},
		{"no geocoder", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			cookie := ts.login("alice")
			if tc.endpoint != nil {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
				t.Cleanup(upstream.Close)
				ts.Geocoder = geocode.New(tc.endpoint(upstream.URL))
			}

			if got := ts.capability(cookie, "reverse_geocoding"); got != tc.want {
				t.Errorf("capabilities.reverse_geocoding = %v, want %v", got, tc.want)
			}
			rec := ts.do(http.MethodGet, "/api/geocode/reverse?lat=64.1&lng=-21.9", cookie, "")
			if reachable := rec.Code != http.StatusNotImplemented; reachable != tc.want {
				t.Errorf("the endpoint answered %d while the capability said %v", rec.Code, tc.want)
			}
		})
	}
}

// Resolving a pasted Google Maps link (Stage 22 Milestone 6).
//
// The resolver itself is covered in internal/geocode/maplink_test.go, including
// the host allowlist and the redirect refusals. What is here is the HTTP shape:
// which status each kind of answer earns, and that the endpoint needs neither a
// session-less caller nor a configured geocoder to behave.

func TestResolveMapLinkEndpoint(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	// A full Maps URL is resolved without any outbound request at all, which is
	// what lets this be an end-to-end test of the endpoint with no stub.
	rec := ts.do(http.MethodGet,
		"/api/geocode/link?url="+url.QueryEscape("https://www.google.com/maps/place/Blue+Lagoon/@63.8804,-22.4495,17z"),
		cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got geocode.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Lat != 63.8804 || got.Lng != -22.4495 {
		t.Errorf("got %v,%v, want 63.8804,-22.4495", got.Lat, got.Lng)
	}
	// The place name rides along so the client can offer it for an empty
	// address field, the way a search result is offered.
	if got.DisplayName != "Blue Lagoon" {
		t.Errorf("display_name = %q, want Blue Lagoon", got.DisplayName)
	}
}

// No geocoder configured, and it still works: this endpoint reads a URL, it
// does not ask Nominatim anything. newTestServer leaves Geocoder nil.
func TestResolveMapLinkNeedsNoGeocoder(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	if ts.Geocoder != nil {
		t.Fatal("this test is meaningless with a geocoder configured")
	}

	rec := ts.do(http.MethodGet,
		"/api/geocode/link?url="+url.QueryEscape("https://www.google.com/maps/@64.1466,-21.9426,15z"),
		cookie, "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 -- link resolution needs no geocoder", rec.Code)
	}
}

func TestResolveMapLinkEndpointRefusals(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")

	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"missing", "", http.StatusBadRequest},
		{"blank", "   ", http.StatusBadRequest},
		{"not a maps link", "https://example.com/somewhere", http.StatusBadRequest},
		{"a lookalike host", "https://google.com.evil.example/maps/@1,2,15z", http.StatusBadRequest},
		{"loopback", "http://127.0.0.1/maps/@1,2,15z", http.StatusBadRequest},
		{"the metadata endpoint", "http://169.254.169.254/maps/@1,2,15z", http.StatusBadRequest},
		{"a file URL", "file:///etc/passwd", http.StatusBadRequest},
		{"absurdly long", "https://www.google.com/maps/@1,2,15z?x=" + strings.Repeat("a", 2100), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := ts.do(http.MethodGet, "/api/geocode/link?url="+url.QueryEscape(tc.raw), cookie, "")
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestResolveMapLinkEndpointNeedsASession(t *testing.T) {
	ts := newTestServer(t)
	rec := ts.do(http.MethodGet,
		"/api/geocode/link?url="+url.QueryEscape("https://www.google.com/maps/@1,2,15z"), nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
