package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The documented response shape, from ollama.com/blog/web-search.
const ollamaResponse = `{
  "results": [
    {"title": "Kex Hostel", "url": "https://kexhostel.is/", "content": "A former biscuit factory on Skulagata."},
    {"title": "Visit Reykjavik", "url": "https://visitreykjavik.is/", "content": "City guide."}
  ]
}`

func TestOllamaSearcherMapsResults(t *testing.T) {
	var gotAuth, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		fmt.Fprint(w, ollamaResponse)
	}))
	defer srv.Close()

	s := newOllamaSearcher("test-key", srv.URL)
	got, err := s.Search(context.Background(), "Kex Hostel Reykjavik")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// content -> Snippet is the mapping most likely to be got wrong, since the
	// field is not called snippet at either end.
	if got[0].Title != "Kex Hostel" || got[0].URL != "https://kexhostel.is/" || !strings.Contains(got[0].Snippet, "biscuit factory") {
		t.Errorf("result[0] = %+v", got[0])
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody["query"] != "Kex Hostel Reykjavik" {
		t.Errorf("body query = %v", gotBody["query"])
	}
}

// A result with no URL is unusable: the model's next move is to read it, and
// there is nothing to read.
func TestOllamaSearcherSkipsResultsWithNoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"title":"No link","url":"","content":"x"},{"title":"Fine","url":"https://example.com/","content":"y"}]}`)
	}))
	defer srv.Close()

	got, err := newOllamaSearcher("k", srv.URL).Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Fine" {
		t.Errorf("results = %+v, want only the usable one", got)
	}
}

// This backend returns page extracts rather than one-line snippets. Six at
// full length is most of a context window spent before the model has read
// anything on purpose.
func TestOllamaSearcherTrimsLongContent(t *testing.T) {
	long := strings.Repeat("word ", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[{"title":"t","url":"https://example.com/","content":%q}]}`, long)
	}))
	defer srv.Close()

	got, err := newOllamaSearcher("k", srv.URL).Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got[0].Snippet) > 700 {
		t.Errorf("snippet is %d bytes, want it trimmed", len(got[0].Snippet))
	}
}

// "search returned 401" in a log is a puzzle for whoever set the instance up;
// naming the key is the difference between a five-minute fix and an hour.
func TestOllamaSearcherNamesAnAuthFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		_, err := newOllamaSearcher("bad", srv.URL).Search(context.Background(), "q")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "API key") {
			t.Errorf("status %d: error = %v, want the key named", status, err)
		}
	}
}

func TestOllamaSearcherReportsOtherFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newOllamaSearcher("k", srv.URL).Search(context.Background(), "q")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want the status reported", err)
	}
}

func TestOllamaSearcherDefaultsToTheHostedEndpoint(t *testing.T) {
	// The common case is a key and nothing else; CARAVEL_SEARCH_URL is only
	// for tests and for a compatible endpoint elsewhere.
	if got := newOllamaSearcher("k", "").url; got != ollamaSearchURL {
		t.Errorf("url = %q, want the hosted endpoint", got)
	}
	if got := newOllamaSearcher("k", "http://localhost:1234/search").url; got != "http://localhost:1234/search" {
		t.Errorf("url = %q, want the override respected", got)
	}
}

func TestNewSearcherRequiresAKeyForOllama(t *testing.T) {
	// Failing at startup beats a run that reaches the search tool and gets a
	// 401 thirty seconds in.
	if _, err := newSearcher(Options{SearchProvider: "ollama"}); err == nil {
		t.Error("newSearcher accepted the ollama provider with no key")
	}
	s, err := newSearcher(Options{SearchProvider: "ollama", SearchKey: "k"})
	if err != nil || s == nil {
		t.Fatalf("newSearcher = %v, %v", s, err)
	}
	if s.Name() != "ollama" {
		t.Errorf("Name() = %q", s.Name())
	}
}

// Every provider documents the base URL, so requiring the full path produces a
// 404 whose cause is invisible.
func TestCompletionsURLAcceptsBothForms(t *testing.T) {
	cases := map[string]string{
		"https://openrouter.ai/api/v1":                   "https://openrouter.ai/api/v1/chat/completions",
		"https://openrouter.ai/api/v1/":                  "https://openrouter.ai/api/v1/chat/completions",
		"https://api.openai.com/v1":                      "https://api.openai.com/v1/chat/completions",
		"http://localhost:11434/v1":                      "http://localhost:11434/v1/chat/completions",
		"https://openrouter.ai/api/v1/chat/completions":  "https://openrouter.ai/api/v1/chat/completions",
		"https://openrouter.ai/api/v1/chat/completions/": "https://openrouter.ai/api/v1/chat/completions",
		"https://gateway.example.com/weird/mount/point":  "https://gateway.example.com/weird/mount/point/chat/completions",
	}
	for in, want := range cases {
		if got := completionsURL(in); got != want {
			t.Errorf("completionsURL(%q) = %q, want %q", in, got, want)
		}
	}
}
