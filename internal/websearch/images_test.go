package websearch

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

// Brave keeps the picture under properties and the page at the top level, and
// its thumbnail is its own proxy. The response is cut down from a live one
// (Stage 46), including the HTML entity in a title.
func TestBraveAsksItsImagesEndpoint(t *testing.T) {
	var path, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		key = r.Header.Get("X-Subscription-Token")
		_, _ = w.Write([]byte(`{"type":"images","results":[
			{"type":"image_result","title":"KEX Hostel &amp; Hotel","url":"https://hostelgeeks.com/kex-hostel-reykjavik-iceland-review/",
			 "source":"hostelgeeks.com",
			 "thumbnail":{"src":"https://imgs.search.brave.com/QaSq/rs:fit:500:0:1:0/g:ce/aHR0","width":500,"height":313},
			 "properties":{"url":"https://hostelgeeks.com/wp-content/uploads/2019/08/KEX.jpg","placeholder":"https://imgs.search.brave.com/_dn","width":900,"height":563},
			 "confidence":"high"},
			{"type":"image_result","title":"no image url","url":"https://site.example/other","properties":{}}
		]}`))
	}))
	defer srv.Close()

	got, err := newBraveSearcher("k", srv.URL+"/res/v1/web/search").SearchImages(context.Background(), "Kex Hostel")
	if err != nil {
		t.Fatalf("SearchImages: %v", err)
	}
	if path != "/res/v1/images/search" {
		t.Errorf("asked %q, want the images endpoint", path)
	}
	if key != "k" {
		t.Errorf("X-Subscription-Token = %q", key)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want the one with an image URL", len(got))
	}
	r := got[0]
	if r.URL != "https://hostelgeeks.com/wp-content/uploads/2019/08/KEX.jpg" ||
		r.SourceURL != "https://hostelgeeks.com/kex-hostel-reykjavik-iceland-review/" {
		t.Errorf("URL/SourceURL = %q / %q", r.URL, r.SourceURL)
	}
	// The size of the picture, not of the 500px thumbnail.
	if r.ThumbURL != "https://imgs.search.brave.com/QaSq/rs:fit:500:0:1:0/g:ce/aHR0" || r.Width != 900 || r.Height != 563 {
		t.Errorf("thumbnail or size wrong: %+v", r)
	}
	if r.Title != "KEX Hostel & Hotel" {
		t.Errorf("title = %q", r.Title)
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
		{"brave", newBraveSearcher("k", ""), true},
		{"stub", &Stub{}, true},
		{"ollama", newOllamaSearcher("k", ""), false},
	} {
		if _, ok := tc.s.(ImageSearcher); ok != tc.want {
			t.Errorf("%s implements ImageSearcher = %t, want %t", tc.name, ok, tc.want)
		}
	}
}

// New exists so cmd/caravel can build one searcher for two consumers.
// The thing worth pinning is that it still works with no assistant in sight,
// which is the configuration Milestone 7 made legal.
func TestNewBuildsABackendWithoutAnAssistant(t *testing.T) {
	s, err := New("serper", "k", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil || s.Name() != "serper" {
		t.Fatalf("New returned %v", s)
	}
	if none, err := New("", "", ""); none != nil || err != nil {
		t.Errorf("no provider = %v, %v; want nil, nil", none, err)
	}
	if _, err := New("altavista", "", ""); err == nil {
		t.Error("an unknown provider was accepted")
	}
}
