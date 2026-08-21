package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every test here points the server at its own httptest.Server. None of them
// may reach OpenStreetMap's public Nominatim - newTestServerWithStore leaves
// GeocoderURL empty precisely so that forgetting to set it fails loudly (501)
// rather than quietly sending a real request.

const nominatimTwoResults = `[
  {"display_name":"Reykjavík, Iceland","lat":"64.1466","lon":"-21.9426","place_id":1},
  {"display_name":"Reykjavík Airport, Iceland","lat":"64.1300","lon":"-21.9406","place_id":2}
]`

// stubGeocoder points ts at a fake upstream and returns the requests it saw.
func stubGeocoder(t *testing.T, ts *testServer, handler http.HandlerFunc) *[]*http.Request {
	t.Helper()
	var seen []*http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(r.Context()))
		handler(w, r)
	}))
	t.Cleanup(upstream.Close)
	ts.GeocoderURL = upstream.URL
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

	var got []geocodeResult
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
	var got []geocodeResult
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
			time.Sleep(geocodeTimeout + time.Second)
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
	ts := newTestServer(t) // GeocoderURL is "" here by default
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
			ts.GeocoderURL = tc.url
			cookie := ts.login("alice")

			rec := ts.do(http.MethodGet, "/api/auth/me", cookie, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got userResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Geocoding != tc.want {
				t.Errorf("geocoding = %v, want %v", got.Geocoding, tc.want)
			}
		})
	}
}
