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

// The two Milestone 8 backends. Both response shapes were copied from a live
// server rather than from documentation -- the field names are the thing most
// likely to be wrong from memory, and all three providers spell the same three
// things differently.

// Captured verbatim from a running `ddgs api` on localhost:8000. Note `href`
// and `body`, not `url` and `content`.
const ddgsResponse = `{
  "results": [
    {"title": "KEX Hostel & Hotel", "href": "https://www.kexrvk.is/", "body": "Housed in an old biscuit factory, we offer vintage vibes."},
    {"title": "Kex Hostel, Reykjavík", "href": "https://www.booking.com/hotel/is/kex.html", "body": "Nur 250 m von der Einkaufsstraße entfernt."}
  ]
}`

// Serper's shape: the results live under `organic`, as link/snippet.
const serperResponse = `{
  "searchParameters": {"q": "kex hostel", "type": "search"},
  "organic": [
    {"title": "KEX Hostel", "link": "https://www.kexrvk.is/", "snippet": "A hostel in an old biscuit factory.", "position": 1},
    {"title": "Kex on Booking", "link": "https://www.booking.com/hotel/is/kex.html", "snippet": "Rooms and dorms.", "position": 2}
  ],
  "credits": 1
}`

func TestDDGSSearcherMapsResults(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		fmt.Fprint(w, ddgsResponse)
	}))
	defer srv.Close()

	got, err := newDDGSSearcher(srv.URL).Search(context.Background(), "Kex Hostel Reykjavik")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// href -> URL and body -> Snippet are the mappings a from-memory
	// implementation gets wrong, because no other provider uses these names.
	if got[0].Title != "KEX Hostel & Hotel" || got[0].URL != "https://www.kexrvk.is/" {
		t.Errorf("result[0] = %+v", got[0])
	}
	if !strings.Contains(got[0].Snippet, "biscuit factory") {
		t.Errorf("snippet = %q, want the body text", got[0].Snippet)
	}
	if gotPath != "/search/text" {
		t.Errorf("path = %q, want /search/text", gotPath)
	}
	if gotBody["query"] != "Kex Hostel Reykjavik" {
		t.Errorf("body query = %v", gotBody["query"])
	}
	// Pinning one engine would let a single site's markup change take the
	// search out entirely, which is the failure this backend is good at
	// surviving.
	if gotBody["backend"] != "auto" {
		t.Errorf("backend = %v, want auto", gotBody["backend"])
	}
}

func TestDDGSSearcherToleratesATrailingSlash(t *testing.T) {
	// CARAVEL_SEARCH_URL is a service root, and pasting it with a slash is not
	// a configuration error worth failing over.
	for _, base := range []string{"http://localhost:8000", "http://localhost:8000/"} {
		if got := newDDGSSearcher(base).url; got != "http://localhost:8000/search/text" {
			t.Errorf("newDDGSSearcher(%q).url = %q", base, got)
		}
	}
}

// Self-hosted, so "cannot reach it" almost always means the service is not
// running -- worth saying, because that is a one-command fix.
func TestDDGSSearcherSaysWhenTheServiceIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := newDDGSSearcher(url).Search(context.Background(), "q")
	if err == nil {
		t.Fatal("Search succeeded against a closed server")
	}
	if !strings.Contains(err.Error(), "is it running") {
		t.Errorf("error = %v, want it to suggest the service is down", err)
	}
}

func TestSerperSearcherMapsResults(t *testing.T) {
	var gotKey string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-KEY")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		fmt.Fprint(w, serperResponse)
	}))
	defer srv.Close()

	got, err := newSerperSearcher("test-key", srv.URL).Search(context.Background(), "kex hostel")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].Title != "KEX Hostel" || got[0].URL != "https://www.kexrvk.is/" {
		t.Errorf("result[0] = %+v", got[0])
	}
	if got[0].Snippet != "A hostel in an old biscuit factory." {
		t.Errorf("snippet = %q", got[0].Snippet)
	}
	// A header, not a bearer token: getting this wrong is a 403 that looks
	// like a bad key.
	if gotKey != "test-key" {
		t.Errorf("X-API-KEY = %q", gotKey)
	}
	if gotBody["q"] != "kex hostel" {
		t.Errorf("body q = %v", gotBody["q"])
	}
}

// Out of credit is a different problem from a bad key, and saying "the key was
// refused" would send an operator to check a key that is perfectly fine.
func TestSerperSearcherDistinguishesCreditFromAuth(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:        "API key",
		http.StatusForbidden:           "API key",
		http.StatusPaymentRequired:     "out of credit",
		http.StatusInternalServerError: "500",
	}
	for status, want := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		_, err := newSerperSearcher("k", srv.URL).Search(context.Background(), "q")
		srv.Close()
		if err == nil {
			t.Errorf("status %d: Search succeeded", status)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("status %d: error = %v, want it to mention %q", status, err, want)
		}
	}
}

func TestSerperSearcherDefaultsToTheHostedEndpoint(t *testing.T) {
	if got := newSerperSearcher("k", "").url; got != serperSearchURL {
		t.Errorf("url = %q, want the hosted endpoint", got)
	}
}

// Every backend drops a result with no URL, for the same reason: the model's
// next move is to read it, and there is nothing to read.
func TestAllBackendsSkipResultsWithNoURL(t *testing.T) {
	t.Run("ddgs", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"results":[{"title":"No link","href":"","body":"x"},{"title":"Fine","href":"https://example.com/","body":"y"}]}`)
		}))
		defer srv.Close()
		got, err := newDDGSSearcher(srv.URL).Search(context.Background(), "q")
		if err != nil || len(got) != 1 || got[0].Title != "Fine" {
			t.Errorf("got %+v, err %v", got, err)
		}
	})
	t.Run("serper", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"organic":[{"title":"No link","link":"","snippet":"x"},{"title":"Fine","link":"https://example.com/","snippet":"y"}]}`)
		}))
		defer srv.Close()
		got, err := newSerperSearcher("k", srv.URL).Search(context.Background(), "q")
		if err != nil || len(got) != 1 || got[0].Title != "Fine" {
			t.Errorf("got %+v, err %v", got, err)
		}
	})
}

// The point of the interface: four implementations, no changes anywhere else.
func TestNewSearcherKnowsEveryProvider(t *testing.T) {
	cases := []struct {
		provider, key, url, wantName string
		wantErr                      bool
	}{
		{provider: "stub", wantName: "stub"},
		{provider: "ollama", key: "k", wantName: "ollama"},
		{provider: "serper", key: "k", wantName: "serper"},
		{provider: "ddgs", url: "http://localhost:8000", wantName: "ddgs"},
		{provider: "ollama", wantErr: true},                   // needs a key
		{provider: "serper", wantErr: true},                   // needs a key
		{provider: "ddgs", wantErr: true},                     // needs an address
		{provider: "searxng", url: "http://x", wantErr: true}, // deferred, not supported
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+tc.wantName, func(t *testing.T) {
			s, err := newSearcher(Options{SearchProvider: tc.provider, SearchKey: tc.key, SearchURL: tc.url})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("newSearcher(%q) succeeded, want an error", tc.provider)
				}
				return
			}
			if err != nil {
				t.Fatalf("newSearcher(%q) = %v", tc.provider, err)
			}
			if s.Name() != tc.wantName {
				t.Errorf("Name() = %q, want %q", s.Name(), tc.wantName)
			}
		})
	}
}
