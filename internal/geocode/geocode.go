// Package geocode turns a place name or an address into coordinates, by
// calling a Nominatim-compatible search endpoint.
//
// Lifted out of internal/httpapi in Stage 16 Milestone 3, unchanged in
// behaviour. It moved because internal/assist needs it and cannot import the
// HTTP layer: the assistant resolves the address the model proposes rather
// than letting the model produce coordinates, which is the one hallucination
// with no visible tell -- a plausible lat/lng 40km from the real hotel looks
// entirely correct in the form and is wrong only on the map.
//
// Called from the server rather than from the browser, for three reasons that
// outlive Nominatim specifically:
//
//   - OSM's usage policy asks for an identifying User-Agent and no more than
//     one request a second. A browser can promise neither.
//   - A self-hosted app should not hand a user's typing to a third party the
//     moment a page loads. One place to see it, one place to switch it off.
//   - The upstream payload is large and provider-shaped. Mapping it down here
//     keeps the provider out of every caller, so swapping geocoders later is a
//     change to this file and nothing else.
package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"caravel/internal/buildinfo"
)

const (
	// Timeout bounds one search. Exported because callers time their own work
	// around it -- notably the agent, whose whole-run deadline has to be
	// larger than any single tool call it might make.
	Timeout = 6 * time.Second
	// MaxResults is what we ask upstream for. More than a handful is noise in
	// a picker nobody scrolls.
	MaxResults = 5
	// MinQueryLen keeps obviously-pointless queries from leaving the building.
	// Nominatim rejects them anyway; this saves the round trip.
	MinQueryLen = 2
	// maxResponseBytes bounds what a reply may be. Five candidates of address
	// text is a few kilobytes; anything approaching this is a misconfigured URL
	// answering with something that is not a geocoder, and reading all of it
	// into memory is the only harm it could do here.
	maxResponseBytes = 1 << 20
)

// Result is one candidate place. The JSON tags are part of Caravel's own API
// shape -- /api/geocode returns these verbatim -- so renaming a field here is
// a client-visible change.
type Result struct {
	DisplayName string  `json:"display_name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`

	// OSMType and OSMID identify the OpenStreetMap element this candidate came
	// from -- node, way or relation, plus its id. They are what makes a link to
	// openstreetmap.org/node/240109189 possible, which is a real feature page
	// with the hours and tag set somebody mapped, rather than a pin on a
	// coordinate (Stage 29).
	//
	// Empty for a result that did not come from an OSM element. Note in
	// particular that the map-link resolver (maplink.go) never sets them: it
	// resolves a Google URL to coordinates, and a coordinate has no OSM
	// identity. Callers must treat these as optional, not as "always present
	// on a search result".
	OSMType string `json:"osm_type,omitempty"`
	OSMID   string `json:"osm_id,omitempty"`

	// Class, Kind and AddressType say what *sort* of thing was matched, from
	// Nominatim jsonv2's `category`, `type` and `addresstype`. They were read
	// and discarded until Stage 33, which is the whole reason the assistant
	// could put a pin on the street outside a hotel and look entirely correct
	// doing it: the payload says "this is a road" and nothing looked.
	//
	// Kept as they arrive rather than reduced to a boolean here, because the
	// question "is this precise enough" has one answer for a pin and might
	// have another for something else later. Precise() is that answer for the
	// only caller there is today.
	//
	// Empty for a result that did not come from a search -- the map-link
	// resolver again, and any future provider that reports no such thing.
	Class       string `json:"class,omitempty"`
	Kind        string `json:"kind,omitempty"`
	AddressType string `json:"address_type,omitempty"`
}

// preciseClasses are the OSM top-level classes whose elements are the *thing*
// rather than the area or the line it sits on. A match in one of these is a
// building, a business, a monument -- a point somebody deliberately placed.
//
// Listed rather than inferred, because there is no rule in the data that
// separates them: OSM classes are a flat vocabulary and the split is editorial.
// These are the ones a trip is made of.
var preciseClasses = map[string]bool{
	"amenity":    true, // restaurants, bars, places of worship, pharmacies
	"tourism":    true, // hotels, hostels, museums, viewpoints, attractions
	"shop":       true,
	"leisure":    true, // parks, pools, sports centres
	"historic":   true, // monuments, ruins, memorials
	"office":     true,
	"craft":      true,
	"healthcare": true,
	"building":   true,
	"aeroway":    true, // terminals, aerodromes
	"railway":    true, // stations and halts
	"natural":    true, // a named waterfall or peak is a destination
	"man_made":   true, // lighthouses, towers, piers
}

// preciseAddressTypes catch what the class alone misses: a match on a specific
// building or house number is precise whatever class it carries. Nominatim
// reports this separately, and for an address search it is usually the more
// informative of the two.
var preciseAddressTypes = map[string]bool{
	"building":     true,
	"house":        true,
	"house_number": true,
}

// Precise reports whether this match is the place itself rather than something
// it is merely near.
//
// The distinction the assistant needs, and the one that makes an address
// search a bad way to position a named place: a postal address usually
// resolves to a `highway`/`road` way or an interpolated point along it, which
// is the correct answer to the question asked and 150m from the door. A search
// for the name usually resolves to the element somebody mapped.
//
// Deliberately conservative. An unknown class is *not* precise, so a provider
// that starts reporting something new errs towards "check this" rather than
// towards a confident wrong pin. That is the right way round: the cost of a
// false "coarse" is that a second source gets asked, and the cost of a false
// "precise" is the bug this stage exists to fix.
func (r Result) Precise() bool {
	if preciseAddressTypes[r.AddressType] {
		return true
	}
	return preciseClasses[r.Class]
}

// Client searches one upstream endpoint.
type Client struct {
	url  string
	http *http.Client
}

// New returns a client for the given endpoint, or nil if it is empty.
//
// Nil is the off switch, and it is a valid value: a nil *Client means address
// search is disabled, which callers check rather than carrying a separate
// bool. Same shape as assist.New. Search on a nil client returns
// ErrNotConfigured rather than panicking, so a missed check fails legibly.
func New(endpoint string) *Client {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	// The sentinel starts an in-process fixture geocoder and pins this client
	// to it. Everything below this line then behaves exactly as it does
	// against a real instance, including the reverse-endpoint derivation --
	// see stub.go for why that matters.
	if endpoint == StubURL {
		endpoint = startStubFixture()
	}
	return &Client{
		url:  endpoint,
		http: &http.Client{Timeout: Timeout},
	}
}

// ErrNotConfigured is returned by a nil Client.
var ErrNotConfigured = errNotConfigured{}

type errNotConfigured struct{}

func (errNotConfigured) Error() string { return "geocode: no geocoder is configured" }

// Search returns up to MaxResults candidates for query.
//
// A non-nil, possibly empty slice on success, so callers can tell "searched,
// found nothing" from "did not search".
//
// locale is the language to name places in, or empty for the provider's
// default. See withLocale.
func (c *Client) Search(ctx context.Context, query, locale string) ([]Result, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	body, err := c.get(ctx, c.url, withLocale(map[string]string{
		"q":      query,
		"format": "jsonv2",
		"limit":  strconv.Itoa(MaxResults),
	}, locale))
	if err != nil {
		return nil, err
	}

	var raw []nominatimResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(raw))
	for _, item := range raw {
		result, ok := item.toResult()
		// One unparseable row should not fail the whole search; skip it and
		// return the rest.
		if !ok {
			continue
		}
		out = append(out, result)
	}
	return out, nil
}

// ReverseURL derives the reverse endpoint from the configured search endpoint,
// and reports false when it cannot.
//
// Nominatim serves /search and /reverse as siblings, so swapping the last path
// segment is the whole derivation. The alternative was a second environment
// variable, which nobody wants to set and which would be wrong far more often
// than it was right.
//
// When the configured URL does not end in /search the answer is false rather
// than a guess: an operator pointing Caravel at something that is merely
// Nominatim-compatible on one path should get "reverse geocoding is not
// available" -- which the client can then not offer -- instead of a control
// that fails against a URL this package invented.
func (c *Client) ReverseURL() (string, bool) {
	if c == nil {
		return "", false
	}
	u, err := url.Parse(c.url)
	if err != nil {
		return "", false
	}
	trimmed := strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(trimmed, "/search") {
		return "", false
	}
	u.Path = strings.TrimSuffix(trimmed, "search") + "reverse"
	return u.String(), true
}

// ReverseAvailable reports whether Reverse can be called at all -- a configured
// client whose endpoint the derivation above understands. It is what the server
// capability flag on /auth/me is built from, so a client never offers the
// control on an instance where it could only fail.
func (c *Client) ReverseAvailable() bool {
	_, ok := c.ReverseURL()
	return ok
}

// Reverse turns a coordinate back into an address.
//
// The opposite direction from Search, and the asymmetry is real: a search has
// many candidates or none, a reverse lookup has exactly one answer or none. So
// this returns a single Result, and ErrNoResult when the upstream found nothing
// there -- the middle of an ocean being the honest example.
//
// The Result carries the *queried* coordinates rather than the ones upstream
// echoes back. Nominatim answers with the location of the thing it matched,
// which for a click in a car park is the building next door; the caller asked
// about a point they chose, and moving it under them is not this function's
// business. Only the address is news.
func (c *Client) Reverse(ctx context.Context, lat, lng float64, locale string) (Result, error) {
	if c == nil {
		return Result{}, ErrNotConfigured
	}
	endpoint, ok := c.ReverseURL()
	if !ok {
		return Result{}, ErrNoReverseEndpoint
	}
	body, err := c.get(ctx, endpoint, withLocale(map[string]string{
		"lat":    strconv.FormatFloat(lat, 'f', -1, 64),
		"lon":    strconv.FormatFloat(lng, 'f', -1, 64),
		"format": "jsonv2",
	}, locale))
	if err != nil {
		return Result{}, err
	}

	// A single object here, not an array -- and on a miss Nominatim answers 200
	// with {"error":...}, which decodes into a zero-valued struct rather than
	// failing. That is why the emptiness check below is the one that matters.
	var raw nominatimResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return Result{}, err
	}
	if raw.DisplayName == "" {
		return Result{}, ErrNoResult
	}
	return Result{DisplayName: raw.DisplayName, Lat: lat, Lng: lng}, nil
}

// withLocale adds the language to ask for names in, when there is one.
//
// Nominatim takes this as a query parameter rather than as an Accept-Language
// header -- both work, and the parameter is the documented one. Empty means
// "do not ask", which leaves the provider's default: names in whatever the
// local language is. That is the right answer for a caller with no user in
// front of it, which is why it is a value rather than a setting.
//
// The value reaching here has already been checked by the HTTP layer
// (normaliseLocale); this does not re-validate, but it also cannot inject
// anything, since url.Values escapes what it encodes.
func withLocale(params map[string]string, locale string) map[string]string {
	if locale != "" {
		params["accept-language"] = locale
	}
	return params
}

// get performs one upstream request and returns its body. Shared by Search and
// Reverse: the timeout, the identifying User-Agent and the non-200 handling are
// conditions of using the service rather than anything about which endpoint is
// being called, and having two copies of them was how one of them would come to
// omit the User-Agent.
func (c *Client) get(ctx context.Context, rawURL string, params map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	// Identifying ourselves is a condition of using the public instance, not
	// politeness: anonymous bulk traffic is what gets blocked.
	req.Header.Set("User-Agent", "Caravel/"+buildinfo.Version+" (self-hosted trip planner)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrUpstreamStatus{Code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
}

// The subset of Nominatim's response we use. Note lat/lon arrive as *strings*,
// which is why they are parsed rather than decoded straight into float64.
type nominatimResult struct {
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	// osm_id arrives as a *number*, unlike lat/lon. json.Number keeps it as
	// the digits that were on the wire rather than round-tripping it through a
	// float64, which for a large way id would be a silent precision loss.
	OSMType string      `json:"osm_type"`
	OSMID   json.Number `json:"osm_id"`
	// jsonv2 calls the OSM class `category`; the `json` format calls it
	// `class`. Both are read so that an operator pointing at a
	// Nominatim-compatible service that answers in the other shape still gets
	// a usable answer rather than a silently coarse one.
	Category    string `json:"category"`
	Class       string `json:"class"`
	Kind        string `json:"type"`
	AddressType string `json:"addresstype"`
}

// toResult parses one upstream row, reporting false for a row that cannot be
// used -- unparseable coordinates, or no name to show.
func (n nominatimResult) toResult() (Result, bool) {
	lat, latErr := strconv.ParseFloat(n.Lat, 64)
	lng, lngErr := strconv.ParseFloat(n.Lon, 64)
	if latErr != nil || lngErr != nil || n.DisplayName == "" {
		return Result{}, false
	}
	r := Result{
		DisplayName: n.DisplayName,
		Lat:         lat,
		Lng:         lng,
		Class:       firstNonEmpty(n.Category, n.Class),
		Kind:        n.Kind,
		AddressType: n.AddressType,
	}
	// Both or neither: an element type without an id, or the reverse, cannot
	// build a URL, and half an identity stored is worse than none.
	if isOSMType(n.OSMType) && isOSMID(n.OSMID.String()) {
		r.OSMType, r.OSMID = n.OSMType, n.OSMID.String()
	}
	return r, true
}

// firstNonEmpty returns the first of its arguments that is not empty. One line,
// but it names why there are two: see nominatimResult.Category.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// The three element types OpenStreetMap has. Anything else is not an OSM
// identity, whatever the upstream called it.
func isOSMType(s string) bool {
	return s == "node" || s == "way" || s == "relation"
}

// An OSM element id is a positive integer. Checked as digits rather than
// parsed, because it is stored and echoed as text and never used as a number.
func isOSMID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ErrNoResult means the lookup succeeded and there is nothing at that point.
// Distinct from an error, because "no address here" is an answer.
var ErrNoResult = errors.New("geocode: no result for that location")

// ErrNoReverseEndpoint means the configured endpoint is not one this package can
// derive a /reverse URL from. Callers should ask ReverseAvailable first; this is
// what happens when they do not.
var ErrNoReverseEndpoint = errors.New("geocode: cannot derive a reverse endpoint from the configured URL")

// ErrUpstreamStatus is a non-200 from the geocoder.
type ErrUpstreamStatus struct{ Code int }

func (e ErrUpstreamStatus) Error() string {
	return "geocode: upstream responded with status " + strconv.Itoa(e.Code)
}
