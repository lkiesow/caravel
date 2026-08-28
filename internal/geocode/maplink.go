package geocode

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"caravel/internal/buildinfo"
	"caravel/internal/safefetch"
)

// Resolving a Google Maps link into coordinates.
//
// Somebody sends you https://maps.app.goo.gl/xfB9TzpFos2N4oAW8 and that is
// where you are staying. Today that is a dead end: the short form carries
// nothing parseable, so the only way into Caravel is to open it, read the
// coordinates off the screen and type them in.
//
// Two halves, and the order matters because the first one costs nothing:
//
//  1. If the URL already carries coordinates -- a full /maps/@... or
//     ?q=lat,lng link -- they are read straight out of it. No request leaves
//     the building.
//  2. Otherwise, if it is a shortener, the redirect is followed and the
//     *expanded* URL is parsed the same way. The body is never read: the
//     answer is in the URL, and a Maps page is a megabyte of JavaScript.
//
// This is one direction of the "better Google Maps interoperability" backlog
// item. The other -- linking out to a place's own Google entry rather than to a
// dropped pin -- needs a place ID, which OSM cannot give us, and stays blocked.

// The same identification the geocoder sends. A var rather than a const
// because the version is stamped in at link time.
var mapLinkUserAgent = "Caravel/" + buildinfo.Version + " (self-hosted trip planner)"

// ErrNotAMapLink means the URL is not one this resolver will follow. Its own
// error because it is a refusal rather than a failure: nothing was tried.
var ErrNotAMapLink = errors.New("geocode: not a Google Maps link")

// ErrNoCoordinates means the link was followed and carries no coordinates.
// Distinct from ErrNotAMapLink so the client can say "that link does not point
// at a place" rather than "that is not a Maps link", which would be wrong and
// confusing for a perfectly good link to a search results page.
var ErrNoCoordinates = errors.New("geocode: that link carries no coordinates")

// Coordinates in the path: /maps/@64.1466,-21.9426,15z -- what the URL bar
// holds when you pan the map. This is the *viewport*, so it is tried after the
// place marker below.
var atPattern = regexp.MustCompile(`/@(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)`)

// The place marker, inside the opaque `data=` blob: !3d<lat>!4d<lng>. Google
// puts the thing you actually clicked here, which is why it wins over the
// viewport -- pan away from a pin and /@ follows the screen while !3d stays on
// the place.
var dataPattern = regexp.MustCompile(`!3d(-?\d+(?:\.\d+)?)!4d(-?\d+(?:\.\d+)?)`)

// A bare "lat,lng" pair, as carried by ?q= / ?ll= / ?center= / ?destination=.
var pairPattern = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*$`)

// The place name, when the URL spells one out: /maps/place/Hallgrimskirkja/@...
var placePattern = regexp.MustCompile(`/place/([^/@]+)`)

// isMapLinkHost reports whether a host is one this resolver will talk to.
//
// Deliberately a small, explicit set rather than a pattern like "anything
// google": the resolver follows redirects, and the allowlist is what keeps a
// chain from wandering somewhere this app has no business being. It is checked
// before the first request and again on every redirect.
//
// Google serves Maps from a per-country domain (google.de, google.co.uk), so
// the check is structural rather than a list of every TLD: the label before the
// public suffix must be "google", or the host must be one of the two
// shorteners.
func isMapLinkHost(host string) bool {
	// Takes url.Host rather than url.Hostname() so the same predicate can be
	// used on a redirect target, where a port may be present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "goo.gl" || host == "maps.app.goo.gl" {
		return true
	}
	labels := strings.Split(host, ".")
	// google.com -> [google com]; www.google.co.uk -> [www google co uk].
	// Two labels of suffix is the most any Google country domain uses.
	for i, label := range labels {
		if label != "google" {
			continue
		}
		rest := len(labels) - i - 1
		if rest >= 1 && rest <= 2 {
			return true
		}
	}
	return false
}

// mapLinkResolver is the resolver with its two policies made explicit: where it
// may connect (safefetch) and which hosts it will talk to at all.
//
// A struct rather than package-level functions reading a test-only global. The
// tests need both policies relaxed -- an httptest server is on loopback *and*
// is not google.com -- and a pair of mutable package variables that production
// code reads is a worse answer than a value the tests construct for themselves.
// Nothing outside this package can build one, and ResolveMapLink is the only
// door in.
type mapLinkResolver struct {
	policy    safefetch.Policy
	allowHost func(string) bool
}

// ResolveMapLink turns a Google Maps link into a coordinate, following a
// shortener if it has to.
//
// The returned Result carries the place name when the URL spells one out --
// which the client uses as the location's title -- and Lat/Lng are the point
// the link identifies. locale is the language to ask Google to name it in, or
// empty for its default, which is English.
func ResolveMapLink(ctx context.Context, rawURL, locale string) (Result, error) {
	r := &mapLinkResolver{policy: safefetch.PublicOnly(), allowHost: isMapLinkHost}
	return r.resolve(ctx, rawURL, locale)
}

func (r *mapLinkResolver) resolve(ctx context.Context, rawURL, locale string) (Result, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Result{}, ErrNotAMapLink
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || !r.allowHost(parsed.Host) {
		return Result{}, ErrNotAMapLink
	}

	// The free answer first: a full link already carries the point, and
	// following it would be a request whose reply we already have.
	if result, ok := coordinatesFrom(parsed); ok {
		return result, nil
	}

	expanded, err := r.expand(ctx, parsed, locale)
	if err != nil {
		return Result{}, err
	}
	if result, ok := coordinatesFrom(expanded); ok {
		return result, nil
	}
	return Result{}, ErrNoCoordinates
}

// expand follows the redirect chain and returns the URL it ended on.
//
// The guard from internal/safefetch applies at every step -- a shortener that
// redirects to 127.0.0.1 is refused like any other -- and the host check runs
// again on each redirect, so a short link cannot be used to make this app fetch
// some third party. The two are independent: being on the host allowlist does
// not get past the address guard, which is what
// TestResolveMapLinkStillRefusesPrivateAddresses pins.
func (r *mapLinkResolver) expand(ctx context.Context, target *url.URL, locale string) (*url.URL, error) {
	client := r.policy.Client(safefetch.Options{
		Timeout:      Timeout,
		MaxRedirects: 5,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !r.allowHost(req.URL.Host) {
				return ErrNotAMapLink
			}
			return nil
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	// Not a condition of use here the way it is for Nominatim, but being
	// nameable is the polite half of automated traffic either way.
	req.Header.Set("User-Agent", mapLinkUserAgent)
	// The language to name the place in, asked for and -- measured -- **not
	// granted**, at least for a short link.
	//
	// The name this resolver returns comes from the `/maps/place/<name>/`
	// segment of the expanded URL, and Google bakes that into a short link's
	// canonical URL when the link is created. Neither `Accept-Language: de` nor
	// an `hl=de` parameter changes it: the Brandenburger Tor comes back as
	// "Brandenburg Gate" whatever is asked for, because that is the name in the
	// URL. Only whoever created the link could have made it German.
	//
	// Sent anyway, and deliberately: it costs one header, it is the correct
	// thing to ask, and the reason it does not help is Google's rather than
	// ours -- a link whose canonical URL was made in a German session already
	// carries a German name. A header rather than hl= because this is a
	// redirect chain, and a header travels it.
	//
	// The consequence is a known limitation rather than a bug: a title
	// suggested from a link may be in the language of whoever made the link.
	// The address, which comes from Nominatim, does follow the locale.
	if locale != "" {
		req.Header.Set("Accept-Language", locale)
	}

	resp, err := client.Do(req)
	if err != nil {
		// A refusal is not a service failure, and the two answer differently:
		// unwrap it out of the *url.Error the client wraps it in.
		if errors.Is(err, ErrNotAMapLink) {
			return nil, ErrNotAMapLink
		}
		var blocked safefetch.ErrBlocked
		if errors.As(err, &blocked) {
			return nil, blocked
		}
		return nil, err
	}
	defer resp.Body.Close()
	// The body is deliberately never read. Everything wanted is in the URL the
	// chain ended on, and a Maps page is a megabyte of JavaScript.

	if resp.StatusCode >= 400 {
		return nil, ErrUpstreamStatus{Code: resp.StatusCode}
	}
	// resp.Request is the *last* request made, so its URL is the expanded one.
	return resp.Request.URL, nil
}

// coordinatesFrom pulls a point out of a Maps URL, trying the places Google
// puts one in order of how specific they are.
func coordinatesFrom(u *url.URL) (Result, bool) {
	full := u.String()

	// The place marker first: it is the thing that was clicked. /@ is the
	// viewport, which follows the screen when the map is panned, so it is a
	// fallback rather than the answer.
	for _, pattern := range []*regexp.Regexp{dataPattern, atPattern} {
		if m := pattern.FindStringSubmatch(full); m != nil {
			if lat, lng, ok := parsePair(m[1], m[2]); ok {
				return Result{DisplayName: placeName(u), Lat: lat, Lng: lng}, true
			}
		}
	}

	// Then the query parameters that carry a bare pair. `q` is what Caravel's
	// own outbound links use, so this also makes a link the app produced
	// readable by the app.
	query := u.Query()
	for _, key := range []string{"q", "query", "ll", "center", "destination", "daddr"} {
		if m := pairPattern.FindStringSubmatch(query.Get(key)); m != nil {
			if lat, lng, ok := parsePair(m[1], m[2]); ok {
				return Result{DisplayName: placeName(u), Lat: lat, Lng: lng}, true
			}
		}
	}
	return Result{}, false
}

// parsePair parses and range-checks one candidate. A pattern match is not
// enough: "/@999,999" is a match and not a place.
func parsePair(rawLat, rawLng string) (float64, float64, bool) {
	lat, latErr := strconv.ParseFloat(rawLat, 64)
	lng, lngErr := strconv.ParseFloat(rawLng, 64)
	if latErr != nil || lngErr != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return 0, 0, false
	}
	return lat, lng, true
}

// placeName reads the name out of a /place/<name>/ segment, or returns empty.
//
// Google percent-encodes it and uses + for spaces, so it needs decoding twice
// over -- and a name that will not decode is dropped rather than shown raw: an
// offered address with %C3%B6 in it is worse than no offered address.
func placeName(u *url.URL) string {
	m := placePattern.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return ""
	}
	name, err := url.QueryUnescape(m[1])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(name, "+", " "))
}
