package assist

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Image search, which is an *optional* capability rather than part of
// Searcher.
//
// The interesting cases are not "does it parse". They are: does the right
// endpoint get called, does the full-size URL end up in the right field, and
// does a backend that cannot do this stay out of the way.

func TestSerperAsksItsImagesEndpoint(t *testing.T) {
	var path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = w.Write([]byte(`{"images":[
			{"title":"Meiji Jingu","imageUrl":"https://site.example/full.jpg","imageWidth":380,"imageHeight":285,
			 "thumbnailUrl":"https://tbn.example/t.jpg","link":"https://site.example/about","domain":"site.example"},
			{"title":"no image url","link":"https://site.example/other"}
		]}`))
	}))
	defer srv.Close()

	// The override names the *text* endpoint; the images one is derived from
	// it, so an operator pointing at a proxy gets both from one setting.
	s := newSerperSearcher("k", srv.URL+"/search")
	got, err := s.SearchImages(context.Background(), "Meiji Shrine")
	if err != nil {
		t.Fatalf("SearchImages: %v", err)
	}
	if path != "/images" {
		t.Errorf("asked %q, want the images endpoint", path)
	}
	if !json.Valid([]byte(body)) {
		t.Errorf("request body is not JSON: %s", body)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want the one with an image URL", len(got))
	}
	// The pair easiest to get the wrong way round: imageUrl is the picture,
	// link is the page it sits on.
	if got[0].URL != "https://site.example/full.jpg" || got[0].SourceURL != "https://site.example/about" {
		t.Errorf("URL/SourceURL = %q / %q", got[0].URL, got[0].SourceURL)
	}
	if got[0].ThumbURL != "https://tbn.example/t.jpg" || got[0].Width != 380 {
		t.Errorf("thumbnail or size lost: %+v", got[0])
	}
}

// The one thing no documentation would have told us, and the reason the plan
// insisted on reading a live ddgs rather than a document: it sends the
// dimensions as *strings*. Decoding them into ints fails the whole response,
// so a working search would have returned nothing at all.
func TestDDGSDecodesTheDimensionsItReallySends(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/images" {
			t.Errorf("asked %q, want /search/images", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Meiji-jingu","image":"https://site.example/full.jpg","thumbnail":"https://tbn.example/t.jpg",
			 "url":"https://site.example/guide","height":"2930","width":"5207","source":""}
		]}`))
	}))
	defer srv.Close()

	got, err := newDDGSSearcher(srv.URL).SearchImages(context.Background(), "Meiji Shrine")
	if err != nil {
		t.Fatalf("SearchImages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results", len(got))
	}
	if got[0].Width != 5207 || got[0].Height != 2930 {
		t.Errorf("dimensions = %dx%d, want the quoted numbers read", got[0].Width, got[0].Height)
	}
	if got[0].URL != "https://site.example/full.jpg" || got[0].SourceURL != "https://site.example/guide" {
		t.Errorf("URL/SourceURL = %q / %q", got[0].URL, got[0].SourceURL)
	}
}

// A dimension that is neither a number nor a quoted number is "unknown", not a
// failed search: the picture is the point.
func TestDDGSSurvivesAMissingDimension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"image":"https://site.example/a.jpg","width":"","height":"lots"}]}`))
	}))
	defer srv.Close()

	got, err := newDDGSSearcher(srv.URL).SearchImages(context.Background(), "q")
	if err != nil || len(got) != 1 || got[0].Width != 0 || got[0].Height != 0 {
		t.Errorf("got %+v, %v; want the result kept with unknown dimensions", got, err)
	}
}

// Which backends can do this, asserted rather than left to a comment. Ollama
// Cloud has web_search and nothing else, and the type assertion is how the
// server finds that out.
func TestOnlySomeBackendsCanSearchForImages(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    Searcher
		want bool
	}{
		{"serper", newSerperSearcher("k", ""), true},
		{"ddgs", newDDGSSearcher("http://localhost:8000"), true},
		{"stub", &stubSearcher{}, true},
		{"ollama", newOllamaSearcher("k", ""), false},
	} {
		if _, ok := tc.s.(ImageSearcher); ok != tc.want {
			t.Errorf("%s implements ImageSearcher = %t, want %t", tc.name, ok, tc.want)
		}
	}
}

// NewSearcher exists so cmd/caravel can build one searcher for two consumers.
// The thing worth pinning is that it still works with no assistant in sight,
// which is the configuration Milestone 7 made legal.
func TestNewSearcherBuildsABackendWithoutAnAssistant(t *testing.T) {
	s, err := NewSearcher("serper", "k", "")
	if err != nil {
		t.Fatalf("NewSearcher: %v", err)
	}
	if s == nil || s.Name() != "serper" {
		t.Fatalf("NewSearcher returned %v", s)
	}
	if none, err := NewSearcher("", "", ""); none != nil || err != nil {
		t.Errorf("no provider = %v, %v; want nil, nil", none, err)
	}
	if _, err := NewSearcher("altavista", "", ""); err == nil {
		t.Error("an unknown provider was accepted")
	}
}

// And that the agent uses the one it is given rather than building a second.
func TestTheAgentUsesTheSearcherItIsHanded(t *testing.T) {
	mine := &stubSearcher{}
	a, err := New(Options{LLMURL: LLMStub, LLMModel: "m", Searcher: mine})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.(*Agent).search != Searcher(mine) {
		t.Error("the agent built its own searcher instead of using the shared one")
	}
}
