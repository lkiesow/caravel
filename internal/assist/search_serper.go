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
	url    string
	key    string
	client *http.Client
}

func newSerperSearcher(key, overrideURL string) *serperSearcher {
	endpoint := serperSearchURL
	if strings.TrimSpace(overrideURL) != "" {
		endpoint = strings.TrimSpace(overrideURL)
	}
	return &serperSearcher{
		url:    endpoint,
		key:    key,
		client: &http.Client{Timeout: searchTimeout},
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
