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
	"net/http"
	"net/url"
	"strconv"
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
)

// Result is one candidate place. The JSON tags are part of Caravel's own API
// shape -- /api/geocode returns these verbatim -- so renaming a field here is
// a client-visible change.
type Result struct {
	DisplayName string  `json:"display_name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
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
	if endpoint == "" {
		return nil
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
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	endpoint, err := url.Parse(c.url)
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("q", query)
	q.Set("format", "jsonv2")
	q.Set("limit", strconv.Itoa(MaxResults))
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

	var raw []nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(raw))
	for _, item := range raw {
		lat, latErr := strconv.ParseFloat(item.Lat, 64)
		lng, lngErr := strconv.ParseFloat(item.Lon, 64)
		// One unparseable row should not fail the whole search; skip it and
		// return the rest.
		if latErr != nil || lngErr != nil || item.DisplayName == "" {
			continue
		}
		out = append(out, Result{DisplayName: item.DisplayName, Lat: lat, Lng: lng})
	}
	return out, nil
}

// The subset of Nominatim's response we use. Note lat/lon arrive as *strings*,
// which is why they are parsed rather than decoded straight into float64.
type nominatimResult struct {
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
}

// ErrUpstreamStatus is a non-200 from the geocoder.
type ErrUpstreamStatus struct{ Code int }

func (e ErrUpstreamStatus) Error() string {
	return "geocode: upstream responded with status " + strconv.Itoa(e.Code)
}
