package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ollama Cloud web search.
//
// The first real backend, chosen to be first because it needs no service stood
// up locally and because anyone already running Ollama for the model half uses
// one account and one key for both.
//
// POST https://ollama.com/api/web_search with a bearer key and {"query": ...},
// answering {"results": [{"title", "url", "content"}]}. A hosted API rather
// than a scraper, so unlike ddgs and SearXNG it does not break when someone
// else changes their markup.

const (
	ollamaSearchURL = "https://ollama.com/api/web_search"
	// searchTimeout bounds one search. Shorter than a page fetch: this is a
	// single API call, and a slow one is better abandoned than waited on
	// inside a run that is already minutes long.
	searchTimeout = 10 * time.Second
)

type ollamaSearcher struct {
	url    string
	key    string
	client *http.Client
}

func newOllamaSearcher(key, overrideURL string) *ollamaSearcher {
	endpoint := ollamaSearchURL
	// The override exists for tests and for anyone running a compatible
	// endpoint elsewhere; the hosted address is the default so the common
	// case needs only a key.
	if strings.TrimSpace(overrideURL) != "" {
		endpoint = strings.TrimSpace(overrideURL)
	}
	return &ollamaSearcher{
		url:    endpoint,
		key:    key,
		client: &http.Client{Timeout: searchTimeout},
	}
}

func (*ollamaSearcher) Name() string { return "ollama" }

func (s *ollamaSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"query": query,
		// Best-effort: the documented REST body only names `query`, but the
		// official SDKs expose a result count and pass it through here. An
		// endpoint that ignores it costs nothing, since the results are
		// truncated on our side anyway.
		"max_results": searchMaxResults,
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
	req.Header.Set("Authorization", "Bearer "+s.key)
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
		// The key is the usual cause and worth naming, because "search
		// returned 401" in a log is otherwise a puzzle for whoever set the
		// instance up. The key itself never appears.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("the search service rejected the API key (status %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("the search service responded with status %d", resp.StatusCode)
	}

	var decoded struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the search service returned a response that could not be read: %w", err)
	}

	out := make([]SearchResult, 0, len(decoded.Results))
	for _, r := range decoded.Results {
		// A result with no URL is unusable: the model's next move is to read
		// it, and there is nothing to read.
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		out = append(out, SearchResult{
			Title: strings.TrimSpace(r.Title),
			URL:   strings.TrimSpace(r.URL),
			// Trimmed hard. This backend returns substantial page extracts
			// rather than one-line snippets, and six of them at full length is
			// most of a context window spent before the model has read
			// anything on purpose.
			Snippet: truncate(collapseWhitespace(r.Content), 600),
		})
	}
	return out, nil
}
