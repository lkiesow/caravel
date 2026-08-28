package geocode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"caravel/internal/safefetch"
)

// Resolving a Google Maps link (Stage 22 Milestone 6).
//
// Two things are load-bearing here and get the most attention: the host
// allowlist, because this resolver follows redirects and must not become a
// way to make the app fetch anything; and the extraction order, because the
// viewport and the place marker are both in a Maps URL and only one of them is
// where you clicked.

func TestIsMapLinkHost(t *testing.T) {
	allowed := []string{
		"maps.app.goo.gl",
		"goo.gl",
		"google.com",
		"www.google.com",
		"maps.google.com",
		"google.de",
		"www.google.co.uk",
		"maps.google.co.jp",
		// Case and a trailing root dot are both legal ways to write a host.
		"MAPS.GOOGLE.COM",
		"www.google.com.",
	}
	for _, host := range allowed {
		if !isMapLinkHost(host) {
			t.Errorf("isMapLinkHost(%q) = false, want true", host)
		}
	}

	refused := []string{
		"",
		"example.com",
		"maps.example.com",
		// The lookalikes. Each of these contains "google" and is somebody
		// else's domain.
		"google.com.evil.example",
		"notgoogle.com",
		"google.evil.co.uk",
		"www.google.com.attacker.net",
		"goo.gl.evil.example",
		"evil-goo.gl",
		// Where an SSRF wants to go.
		"127.0.0.1",
		"169.254.169.254",
		"localhost",
	}
	for _, host := range refused {
		if isMapLinkHost(host) {
			t.Errorf("isMapLinkHost(%q) = true, want false", host)
		}
	}
}

// A URL that already carries the point is read without a request. That is not
// only an optimisation: it is most of the traffic this feature would otherwise
// generate, since a full Maps URL is what people paste most of the time.
func TestResolveMapLinkReadsAFullURLWithoutAnyRequest(t *testing.T) {
	cases := []struct {
		name string
		url  string
		lat  float64
		lng  float64
		want string // expected place name, "" for none
	}{
		{
			name: "the place marker in the data blob",
			url:  "https://www.google.com/maps/place/Hallgr%C3%ADmskirkja/@64.1417,-21.9266,17z/data=!3m1!4b1!4m6!3m5!1s0x0:0x0!8m2!3d64.1418!4d-21.9266",
			lat:  64.1418,
			lng:  -21.9266,
			want: "Hallgrímskirkja",
		},
		{
			name: "the viewport when there is no marker",
			url:  "https://www.google.com/maps/@64.1466,-21.9426,15z",
			lat:  64.1466,
			lng:  -21.9426,
		},
		{
			name: "a q= search pin, which is what Caravel's own links use",
			url:  "https://www.google.com/maps?q=64.1466,-21.9426",
			lat:  64.1466,
			lng:  -21.9426,
		},
		{
			name: "ll=",
			url:  "https://maps.google.com/?ll=48.8584,2.2945&z=17",
			lat:  48.8584,
			lng:  2.2945,
		},
		{
			name: "a country domain",
			url:  "https://www.google.de/maps/@52.5163,13.3777,17z",
			lat:  52.5163,
			lng:  13.3777,
		},
		{
			name: "a plus-encoded place name",
			url:  "https://www.google.com/maps/place/Blue+Lagoon/@63.8804,-22.4495,17z",
			lat:  63.8804,
			lng:  -22.4495,
			want: "Blue Lagoon",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No httptest server anywhere in this test: if the resolver made a
			// request for any of these, it would have to reach the real
			// google.com to succeed, and the assertions below would be the
			// least of it.
			got, err := ResolveMapLink(context.Background(), tc.url, "")
			if err != nil {
				t.Fatalf("ResolveMapLink: %v", err)
			}
			if got.Lat != tc.lat || got.Lng != tc.lng {
				t.Errorf("got %v,%v want %v,%v", got.Lat, got.Lng, tc.lat, tc.lng)
			}
			if got.DisplayName != tc.want {
				t.Errorf("display_name = %q, want %q", got.DisplayName, tc.want)
			}
		})
	}
}

// The place marker wins over the viewport: pan the map away from a pin and /@
// follows the screen while !3d stays on the place you clicked.
func TestResolveMapLinkPrefersTheMarkerOverTheViewport(t *testing.T) {
	got, err := ResolveMapLink(context.Background(),
		"https://www.google.com/maps/place/Somewhere/@10.0,10.0,17z/data=!4m6!3m5!8m2!3d64.1418!4d-21.9266", "")
	if err != nil {
		t.Fatalf("ResolveMapLink: %v", err)
	}
	if got.Lat != 64.1418 || got.Lng != -21.9266 {
		t.Errorf("got %v,%v, want the marker at 64.1418,-21.9266 rather than the viewport", got.Lat, got.Lng)
	}
}

func TestResolveMapLinkRefusesWhatItWillNotFollow(t *testing.T) {
	cases := map[string]string{
		"another host entirely": "https://example.com/maps/@64.1,-21.9,15z",
		"a lookalike host":      "https://google.com.evil.example/maps/@64.1,-21.9,15z",
		"loopback":              "http://127.0.0.1/maps/@64.1,-21.9,15z",
		"the metadata endpoint": "http://169.254.169.254/maps/@64.1,-21.9,15z",
		"a file URL":            "file:///etc/passwd",
		"not a URL at all":      "Hallgrímskirkja, Reykjavík",
		"empty":                 "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveMapLink(context.Background(), raw, "")
			if !errors.Is(err, ErrNotAMapLink) {
				t.Errorf("err = %v, want ErrNotAMapLink", err)
			}
		})
	}
}

// Coordinates that parse but cannot exist are not coordinates.
//
// Against coordinatesFrom rather than ResolveMapLink, deliberately: a URL with
// no *usable* point falls through to the shortener path, and asserting this
// through the front door would mean a test that makes a live request to
// google.com to prove a parsing rule.
func TestCoordinatesFromRejectsOutOfRangePairs(t *testing.T) {
	for _, raw := range []string{
		"https://www.google.com/maps/@999,999,15z",
		"https://www.google.com/maps?q=91,0",
		"https://www.google.com/maps?q=0,181",
		"https://www.google.com/maps?q=-91,0",
		"https://www.google.com/maps/place/Nowhere/data=!3d200!4d200",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got, ok := coordinatesFrom(u); ok {
			t.Errorf("coordinatesFrom(%q) = %v,%v, want a refusal rather than a bogus point", raw, got.Lat, got.Lng)
		}
	}
}

// The shortener path, against a stub. A resolver is built with both policies
// relaxed rather than the package ones being reached for: an httptest server is
// on loopback *and* is not google.com, and the alternative is a test that
// reaches maps.app.goo.gl for real -- the dependency this suite is careful not
// to take on anywhere else.
//
// allowPrivate here is what lets the request happen at all;
// TestResolveMapLinkStillRefusesPrivateAddresses below is the one that keeps
// the address guard honest, and it does not relax it.
func stubResolver(hosts ...string) *mapLinkResolver {
	allowed := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		allowed[h] = true
	}
	return &mapLinkResolver{
		policy:    safefetch.AllowPrivateForTests(),
		allowHost: func(host string) bool { return allowed[host] },
	}
}

func hostOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestResolveMapLinkFollowsAShortener(t *testing.T) {
	const expanded = "/maps/place/Kex+Hostel/@64.1466,-21.9426,17z/data=!3m1!4b1!4m5!3m4!8m2!3d64.1470!4d-21.9420"
	var seen []string
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		// Only the short path redirects. Redirecting everything, including the
		// destination, is a loop -- which is how the redirect cap earns its
		// keep, and not what this test is about.
		if strings.HasPrefix(r.URL.Path, "/maps/") {
			fmt.Fprint(w, "a page nobody reads")
			return
		}
		http.Redirect(w, r, expanded, http.StatusFound)
	}))
	defer final.Close()

	got, err := stubResolver(hostOf(t, final)).resolve(context.Background(), final.URL+"/abc123", "")
	if err != nil {
		t.Fatalf("ResolveMapLink: %v", err)
	}
	if got.Lat != 64.1470 || got.Lng != -21.9420 {
		t.Errorf("got %v,%v, want the marker from the expanded URL", got.Lat, got.Lng)
	}
	if got.DisplayName != "Kex Hostel" {
		t.Errorf("display_name = %q, want the place name from the expanded URL", got.DisplayName)
	}
	if len(seen) != 2 {
		t.Errorf("upstream saw %v, want the short link and then the expanded one", seen)
	}
}

// A short link that expands to somewhere off the allowlist is refused rather
// than followed: without this, a shortener is a way to make the app fetch
// anything at all.
func TestResolveMapLinkRefusesARedirectOffTheAllowlist(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the resolver followed a redirect off the allowlist")
	}))
	defer elsewhere.Close()

	shortener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/maps/@64.1,-21.9,15z", http.StatusFound)
	}))
	defer shortener.Close()

	// Only the shortener is allowed; the redirect target is not.
	_, err := stubResolver(hostOf(t, shortener)).resolve(context.Background(), shortener.URL+"/abc123", "")
	if !errors.Is(err, ErrNotAMapLink) {
		t.Errorf("err = %v, want the redirect to have been refused", err)
	}
}

// The guard is not bypassable by being on the allowlist: a host this resolver
// talks to that resolves somewhere private is still refused, by safefetch.
func TestResolveMapLinkStillRefusesPrivateAddresses(t *testing.T) {
	// The host allowlist is opened for a loopback address while the *address*
	// policy stays strict. The resolve must still fail: the two checks are
	// independent, and passing one is not passing the other.
	r := &mapLinkResolver{
		policy:    safefetch.PublicOnly(),
		allowHost: func(host string) bool { return host == "127.0.0.1:9" },
	}
	_, err := r.resolve(context.Background(), "http://127.0.0.1:9/abc", "")
	var blocked safefetch.ErrBlocked
	if !errors.As(err, &blocked) {
		t.Errorf("err = %v (%T), want safefetch.ErrBlocked -- the address guard is not the host allowlist", err, err)
	}
}

func TestResolveMapLinkReportsALinkWithNoCoordinates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A search results page: a perfectly good Maps link that names no
		// single place.
		if strings.HasPrefix(r.URL.Path, "/maps/") {
			fmt.Fprint(w, "results")
			return
		}
		http.Redirect(w, r, "/maps/search/coffee+near+me", http.StatusFound)
	}))
	defer srv.Close()

	_, err := stubResolver(hostOf(t, srv)).resolve(context.Background(), srv.URL+"/abc123", "")
	if !errors.Is(err, ErrNoCoordinates) {
		t.Errorf("err = %v, want ErrNoCoordinates", err)
	}
}

func TestResolveMapLinkReportsAnUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := stubResolver(hostOf(t, srv)).resolve(context.Background(), srv.URL+"/gone", "")
	var status ErrUpstreamStatus
	if !errors.As(err, &status) || status.Code != http.StatusNotFound {
		t.Errorf("err = %v, want ErrUpstreamStatus{404}", err)
	}
}

// The Accept-Language is sent, and travels the whole redirect chain.
//
// What it does *not* do -- measured against the real service, and recorded in
// expand() -- is change the name: that comes from the /maps/place/<name>/
// segment, which Google bakes into a short link when it is created. This test
// pins that the header goes out, not that Google honours it.
func TestResolveMapLinkSendsAcceptLanguage(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Accept-Language"))
		if strings.HasPrefix(r.URL.Path, "/maps/") {
			fmt.Fprint(w, "ok")
			return
		}
		http.Redirect(w, r, "/maps/place/Brandenburger+Tor/@52.5163,13.3777,17z/data=!3d52.5163!4d13.3777", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := stubResolver(hostOf(t, srv)).resolve(context.Background(), srv.URL+"/abc", "de"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) == 0 || got[0] != "de" {
		t.Errorf("Accept-Language = %v, want de on the first request", got)
	}
	// It has to survive the redirect too, since the page that carries the name
	// is the one at the end of the chain.
	for i, header := range got {
		if header != "de" {
			t.Errorf("request %d sent Accept-Language %q, want de all the way down the chain", i, header)
		}
	}

	got = nil
	if _, err := stubResolver(hostOf(t, srv)).resolve(context.Background(), srv.URL+"/abc", ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) == 0 || got[0] != "" {
		t.Errorf("Accept-Language = %v, want none when no locale was asked for", got)
	}
}
