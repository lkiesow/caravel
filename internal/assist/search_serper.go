package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Serper (serper.dev), hosted.
//
// Real Google results through an API rather than by scraping, which makes it
// the one backend here that is neither a scraper nor dependent on a service
// the operator has to run. It costs money per query, which is the trade.
//
// POST https://google.serper.dev/search with an X-API-KEY header and
// {"q", "num"}, answering an object whose `organic` array carries
// {"title", "link", "snippet"} -- a third set of field names for the same
// three things, which is the whole argument for normalising in Searcher.

const serperSearchURL = "https://google.serper.dev/search"

type serperSearcher struct {
	url string
	// imageURL and placesURL are the /images and /places siblings of url.
	// Derived rather than configured, so an operator pointing
	// CARAVEL_SEARCH_URL at a proxy gets all three endpoints from the one
	// setting.
	imageURL  string
	placesURL string
	key       string
	client    *http.Client
}

func newSerperSearcher(key, overrideURL string) *serperSearcher {
	endpoint := serperSearchURL
	if strings.TrimSpace(overrideURL) != "" {
		endpoint = strings.TrimSpace(overrideURL)
	}
	base := strings.TrimSuffix(endpoint, "/search")
	return &serperSearcher{
		url:       endpoint,
		imageURL:  base + "/images",
		placesURL: base + "/places",
		key:       key,
		client:    &http.Client{Timeout: searchTimeout},
	}
}

func (*serperSearcher) Name() string { return "serper" }

func (s *serperSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{"q": query, "num": searchMaxResults})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-KEY", s.key)
	req.Header.Set("User-Agent", assistUserAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the search service could not be reached: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// Serper answers 403 for a bad key and 402 when the credit runs out.
		// The second is worth naming on its own: "the key was refused" would
		// send an operator to check a key that is perfectly fine.
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Errorf("the search service rejected the API key (status %d)", resp.StatusCode)
		case http.StatusPaymentRequired:
			return nil, fmt.Errorf("the search service reports the account is out of credit (status 402)")
		}
		return nil, fmt.Errorf("the search service responded with status %d", resp.StatusCode)
	}

	var decoded struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the search service returned a response that could not be read: %w", err)
	}

	out := make([]SearchResult, 0, len(decoded.Organic))
	for _, r := range decoded.Organic {
		if strings.TrimSpace(r.Link) == "" {
			continue
		}
		out = append(out, SearchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.Link),
			Snippet: truncate(collapseWhitespace(r.Snippet), 600),
		})
	}
	return out, nil
}

// SearchImages implements ImageSearcher.
//
// POST /images, same key and same shape as the text endpoint, answering an
// `images` array of {title, imageUrl, imageWidth, imageHeight, thumbnailUrl,
// link, domain}. Taken from a live response rather than from documentation:
// `imageUrl` is the picture and `link` is the page it sits on, which is the
// pair this feature needs and the pair easiest to get the wrong way round.
func (s *serperSearcher) SearchImages(ctx context.Context, query string) ([]ImageResult, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{"q": query, "num": imageSearchMaxResults})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.imageURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-KEY", s.key)
	req.Header.Set("User-Agent", assistUserAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the image search service could not be reached: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Errorf("the image search service rejected the API key (status %d)", resp.StatusCode)
		case http.StatusPaymentRequired:
			return nil, fmt.Errorf("the image search service reports the account is out of credit (status 402)")
		}
		return nil, fmt.Errorf("the image search service responded with status %d", resp.StatusCode)
	}

	var decoded struct {
		Images []struct {
			Title        string `json:"title"`
			ImageURL     string `json:"imageUrl"`
			ImageWidth   int    `json:"imageWidth"`
			ImageHeight  int    `json:"imageHeight"`
			ThumbnailURL string `json:"thumbnailUrl"`
			Link         string `json:"link"`
		} `json:"images"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the image search service returned a response that could not be read: %w", err)
	}

	out := make([]ImageResult, 0, len(decoded.Images))
	for _, r := range decoded.Images {
		if strings.TrimSpace(r.ImageURL) == "" {
			continue
		}
		out = append(out, ImageResult{
			Title:     truncate(collapseWhitespace(r.Title), 200),
			URL:       strings.TrimSpace(r.ImageURL),
			ThumbURL:  strings.TrimSpace(r.ThumbnailURL),
			Width:     r.ImageWidth,
			Height:    r.ImageHeight,
			SourceURL: strings.TrimSpace(r.Link),
		})
	}
	return out, nil
}

// SearchPlaces implements PlaceLocator.
//
// POST /places, same key and same shape as the other two endpoints, answering
// a `places` array. Taken from live responses rather than from documentation,
// exactly as the image endpoint above was, and the two samples disagreed with
// each other in ways worth writing down:
//
//	{"position":1,"title":"KEX Hostel and Hotel Reykjavik","address":"Skúlagata 28",
//	 "latitude":64.14547,"longitude":-21.919407,"rating":4.3,"ratingCount":2700,
//	 "category":"Hostel","cid":"6391271468677959927"}
//
//	{"position":1,"title":"Brauð & Co","address":"Frakkastígur 16, 101 Reykjavík, Iceland",
//	 "latitude":64.14408,"longitude":-21.925978,"phoneNumber":"...","website":"...","cid":"..."}
//
// Three things that shape the code below:
//
//   - The coordinates are JSON *numbers*, unlike Nominatim, which sends them
//     as strings. They are decoded into pointers all the same, so that a row
//     with no position is distinguishable from one at 0,0 -- which is a real
//     point in the Gulf of Guinea and the classic way this kind of bug hides.
//   - `category` is not always present. The first sample has it and the second
//     has no such key at all, so nothing may depend on it.
//   - `address` is sometimes the whole formatted address and sometimes a bare
//     street and number. It is shown to the user as evidence, never parsed.
//
// The response also carries `credits`, which is 1 per call. That is the price
// of the second opinion, and it is why this is only reached when the operator
// has chosen `serper`.
func (s *serperSearcher) SearchPlaces(ctx context.Context, query string) ([]PlaceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{"q": query, "num": placeSearchMaxResults})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.placesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-KEY", s.key)
	req.Header.Set("User-Agent", assistUserAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the places service could not be reached: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Errorf("the places service rejected the API key (status %d)", resp.StatusCode)
		case http.StatusPaymentRequired:
			return nil, fmt.Errorf("the places service reports the account is out of credit (status 402)")
		}
		return nil, fmt.Errorf("the places service responded with status %d", resp.StatusCode)
	}

	var decoded struct {
		Places []struct {
			Title    string   `json:"title"`
			Address  string   `json:"address"`
			Lat      *float64 `json:"latitude"`
			Lng      *float64 `json:"longitude"`
			Category string   `json:"category"`
		} `json:"places"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the places service returned a response that could not be read: %w", err)
	}

	out := make([]PlaceResult, 0, len(decoded.Places))
	for _, r := range decoded.Places {
		// A row with no position is not a second opinion about anything, and
		// a row with no name cannot be shown as evidence for one. Skipped
		// rather than failing the lookup: one unusable row should not cost the
		// others, which is how the geocoder treats the same case.
		if r.Lat == nil || r.Lng == nil || strings.TrimSpace(r.Title) == "" {
			continue
		}
		out = append(out, PlaceResult{
			Title:    collapseWhitespace(r.Title),
			Address:  collapseWhitespace(r.Address),
			Lat:      *r.Lat,
			Lng:      *r.Lng,
			Category: collapseWhitespace(r.Category),
		})
	}
	return out, nil
}
