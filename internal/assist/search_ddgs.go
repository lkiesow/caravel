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

// DDGS (github.com/deedy5/ddgs), self-hosted.
//
// A metasearch library with a built-in FastAPI server: `pip install ddgs[api]`
// then `ddgs api`. That server is what this talks to, so Python runs as a
// service the operator starts rather than anything inside our binary -- the
// point on which todo.md's original rejection of ddgs was wrong.
//
// Despite the name it is no longer DuckDuckGo-only: text search aggregates
// bing, brave, duckduckgo, google, mojeek, startpage, yandex, yahoo and
// wikipedia, and `backend=auto` lets it fall back when one of them breaks. That
// fallback is the main thing making a scraper tolerable here -- but it is still
// a scraper, and the README says so.
//
// POST /search/text with {"query", "max_results", "backend"}, answering
// {"results": [{"title", "href", "body"}]}. Note `href` and `body` rather than
// the `url` and `content` the other providers use; the field names were taken
// from a live server rather than from documentation, since the two disagree
// less often than memory does.

type ddgsSearcher struct {
	url    string
	client *http.Client
}

func newDDGSSearcher(baseURL string) *ddgsSearcher {
	return &ddgsSearcher{
		// Trailing slash tolerated: CARAVEL_SEARCH_URL is the service root
		// ("http://localhost:8000"), and pasting it with a slash is not a
		// configuration error worth failing over.
		url:    strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/search/text",
		client: &http.Client{Timeout: searchTimeout},
	}
}

func (*ddgsSearcher) Name() string { return "ddgs" }

func (s *ddgsSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"query":       query,
		"max_results": searchMaxResults,
		// Let the server pick and fall back between engines. Pinning one would
		// mean a single site's markup change takes our search out entirely,
		// which is the failure mode this backend is otherwise good at
		// surviving.
		"backend": "auto",
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", assistUserAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		// This one is self-hosted, so "cannot reach it" usually means the
		// service is not running -- worth saying, because the operator can fix
		// that in one command.
		return nil, fmt.Errorf("the ddgs service at %s could not be reached (is it running?): %w", s.url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the ddgs service responded with status %d", resp.StatusCode)
	}

	var decoded struct {
		Results []struct {
			Title string `json:"title"`
			Href  string `json:"href"`
			Body  string `json:"body"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the ddgs service returned a response that could not be read: %w", err)
	}

	out := make([]SearchResult, 0, len(decoded.Results))
	for _, r := range decoded.Results {
		if strings.TrimSpace(r.Href) == "" {
			continue
		}
		out = append(out, SearchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.Href),
			Snippet: truncate(collapseWhitespace(r.Body), 600),
		})
	}
	return out, nil
}
