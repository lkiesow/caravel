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

// The places endpoint, against the two responses the live API actually sent
// when Stage 33 Milestone 3 asked it. Recorded verbatim rather than composed,
// because the whole reason this milestone made a live call first is that the
// documented shape and the real one have already differed once in this file.
//
// The two samples disagree with each other, which is the useful part: the
// first has `category`, `rating` and `ratingCount`; the second has none of
// those and has `phoneNumber` and `website` instead. Nothing may depend on a
// field being present.
const serperPlacesKexResponse = `{
  "searchParameters": {"q": "Kex Hostel Reykjavik", "type": "places"},
  "places": [
    {"position": 1, "title": "KEX Hostel and Hotel Reykjavik", "address": "Skúlagata 28",
     "latitude": 64.14547, "longitude": -21.919407, "rating": 4.3, "ratingCount": 2700,
     "category": "Hostel", "cid": "6391271468677959927"}
  ],
  "credits": 1
}`

const serperPlacesBraudResponse = `{
  "searchParameters": {"q": "Brauð & Co", "type": "places"},
  "places": [
    {"position": 1, "title": "Brauð & Co", "address": "Frakkastígur 16, 101 Reykjavík, Iceland",
     "latitude": 64.14408, "longitude": -21.925978, "phoneNumber": "+354 000 0000",
     "website": "https://example.invalid/", "cid": "1"}
  ],
  "credits": 1
}`

func TestSerperPlacesReadsBothRecordedResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		title    string
		address  string
		lat, lng float64
		category string
	}{
		{
			name: "with a category, and a bare street for an address",
			body: serperPlacesKexResponse, title: "KEX Hostel and Hotel Reykjavik",
			address: "Skúlagata 28", lat: 64.14547, lng: -21.919407, category: "Hostel",
		},
		{
			name: "with no category at all, and a full formatted address",
			body: serperPlacesBraudResponse, title: "Brauð & Co",
			address: "Frakkastígur 16, 101 Reykjavík, Iceland", lat: 64.14408, lng: -21.925978,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var asked map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&asked)
				if r.Header.Get("X-API-KEY") != "k" {
					t.Errorf("the API key did not reach the places endpoint")
				}
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			got, err := newSerperSearcher("k", srv.URL+"/search").SearchPlaces(context.Background(), "q")
			if err != nil {
				t.Fatalf("SearchPlaces: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("places = %d, want one", len(got))
			}
			if got[0].Title != tc.title || got[0].Address != tc.address {
				t.Errorf("title/address = %q / %q, want %q / %q", got[0].Title, got[0].Address, tc.title, tc.address)
			}
			if got[0].Lat != tc.lat || got[0].Lng != tc.lng {
				t.Errorf("position = %v,%v want %v,%v", got[0].Lat, got[0].Lng, tc.lat, tc.lng)
			}
			if got[0].Category != tc.category {
				t.Errorf("category = %q, want %q", got[0].Category, tc.category)
			}
		})
	}
}

// The endpoint is derived from the configured one, so an operator pointing
// CARAVEL_SEARCH_URL at a proxy gets all three from the single setting.
func TestSerperDerivesItsSiblingEndpoints(t *testing.T) {
	s := newSerperSearcher("k", "https://proxy.example/search")
	if s.imageURL != "https://proxy.example/images" {
		t.Errorf("imageURL = %q", s.imageURL)
	}
	if s.placesURL != "https://proxy.example/places" {
		t.Errorf("placesURL = %q", s.placesURL)
	}
	if hosted := newSerperSearcher("k", ""); hosted.placesURL != "https://google.serper.dev/places" {
		t.Errorf("the hosted default derived %q", hosted.placesURL)
	}
}

// A row with no coordinates is not a second opinion about anything, and 0,0 is
// a real point in the Gulf of Guinea -- so absence has to be distinguishable
// from zero, which is why the decode uses pointers.
func TestSerperPlacesSkipsRowsItCannotUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"places":[
		  {"position":1,"title":"No position","address":"Somewhere"},
		  {"position":2,"title":"","latitude":64.1,"longitude":-21.9},
		  {"position":3,"title":"Fine","address":"A street","latitude":64.1,"longitude":-21.9},
		  {"position":4,"title":"Null Island","address":"Nowhere","latitude":0,"longitude":0}
		]}`)
	}))
	defer srv.Close()

	got, err := newSerperSearcher("k", srv.URL+"/search").SearchPlaces(context.Background(), "q")
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("places = %d, want the usable two", len(got))
	}
	if got[0].Title != "Fine" {
		t.Errorf("first usable place = %q", got[0].Title)
	}
	// 0,0 is a position somebody could genuinely be told about; it is only
	// *absence* that is dropped.
	if got[1].Title != "Null Island" {
		t.Errorf("a row at 0,0 was dropped as if it had no position: %+v", got)
	}
}

// A Serper backend is a PlaceLocator and the others are not, which is what the
// resolver type-asserts on.
func TestOnlySerperOffersPlaces(t *testing.T) {
	if _, ok := any(newSerperSearcher("k", "")).(PlaceLocator); !ok {
		t.Error("serper should offer places")
	}
	if _, ok := any(&stubSearcher{}).(PlaceLocator); !ok {
		t.Error("the stub should offer places, or the browser suite cannot reach the two-source path")
	}
	if _, ok := any(newDDGSSearcher("http://example.invalid")).(PlaceLocator); ok {
		t.Error("ddgs has no places endpoint and should not claim one")
	}
	if _, ok := any(newOllamaSearcher("k", "")).(PlaceLocator); ok {
		t.Error("Ollama Cloud has no places endpoint and should not claim one")
	}
}
