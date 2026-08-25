package wikimedia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A fake Wikipedia. Both calls the client makes -- the page lookup and the
// file lookup -- are answered from the same handler, keyed on the `prop`
// parameter, which is how the real API distinguishes them too.
func fakeWiki(t *testing.T, page, file string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Query().Get("prop"), "imageinfo") {
			fmt.Fprint(w, file)
			return
		}
		fmt.Fprint(w, page)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

const goodPage = `{"query":{"pages":[{"title":"Hallgrimskirkja",
  "original":{"source":"https://upload.wikimedia.org/wikipedia/commons/a/ab/Hallgrimskirkja.jpg","width":2000,"height":1500},
  "thumbnail":{"source":"https://upload.wikimedia.org/thumb/Hallgrimskirkja.jpg"},
  "fullurl":"https://en.wikipedia.org/wiki/Hallgrimskirkja"}]}}`

const goodFile = `{"query":{"pages":[{"imageinfo":[{
  "descriptionurl":"https://commons.wikimedia.org/wiki/File:Hallgrimskirkja.jpg",
  "extmetadata":{
    "LicenseShortName":{"value":"CC BY-SA 4.0"},
    "Artist":{"value":"<a href=\"https://commons.wikimedia.org/wiki/User:Someone\" title=\"User:Someone\">Someone</a>"}}}]}]}}`

func TestLeadImage(t *testing.T) {
	c := fakeWiki(t, goodPage, goodFile)
	got, err := c.LeadImage(context.Background(), "en", "Hallgrimskirkja")
	if err != nil {
		t.Fatalf("LeadImage: %v", err)
	}
	if !strings.HasSuffix(got.URL, "Hallgrimskirkja.jpg") {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Width != 2000 || got.Height != 1500 {
		t.Errorf("dimensions = %dx%d", got.Width, got.Height)
	}
	if got.Licence != "CC BY-SA 4.0" {
		t.Errorf("Licence = %q", got.Licence)
	}
	// The markup has to go somewhere, and dropping it here means no caller can
	// render a third party's HTML by accident.
	if got.Credit != "Someone" {
		t.Errorf("Credit = %q, want the author as plain text", got.Credit)
	}
	if strings.ContainsAny(got.Credit, "<>") {
		t.Errorf("Credit = %q still carries markup", got.Credit)
	}
	if got.DescriptionURL == "" {
		t.Error("no description URL; a credit has nothing to link to")
	}
}

// None of these is an error. "No article", "no image" and "an image too small
// to be a photograph of anything" all mean the same thing to the caller --
// nothing to offer -- and giving it three branches with identical behaviour
// behind them would be worse than one.
func TestLeadImageAnswersEmptyRatherThanFailing(t *testing.T) {
	cases := []struct {
		name string
		page string
	}{
		{"no such article", `{"query":{"pages":[{"title":"Nope","missing":true}]}}`},
		{"no pages at all", `{"query":{"pages":[]}}`},
		{"an article with no lead image", `{"query":{"pages":[{"title":"Kex Hostel"}]}}`},
		{
			// Icons, flags and logos share a page with its photographs.
			"an image too small to be a photograph",
			`{"query":{"pages":[{"title":"X","original":{"source":"https://upload.wikimedia.org/icon.png","width":48,"height":48}}]}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fakeWiki(t, tc.page, goodFile).LeadImage(context.Background(), "en", "X")
			if err != nil {
				t.Fatalf("LeadImage returned an error rather than nothing: %v", err)
			}
			if got.URL != "" {
				t.Errorf("URL = %q, want nothing", got.URL)
			}
		})
	}
}

// An image with no credit is still an image. Refusing to offer it because the
// metadata call failed would trade a working feature for a missing line of
// attribution.
func TestAFileLookupFailureStillYieldsTheImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Query().Get("prop"), "imageinfo") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, goodPage)
	}))
	defer srv.Close()

	got, err := New(srv.URL).LeadImage(context.Background(), "en", "Hallgrimskirkja")
	if err != nil {
		t.Fatalf("LeadImage: %v", err)
	}
	if got.URL == "" {
		t.Fatal("the image was dropped because its licence could not be read")
	}
	if got.Licence != "" || got.Credit != "" {
		t.Errorf("credit = %q / %q, want both empty", got.Credit, got.Licence)
	}
}

// An error the API reports in a 200 body, which is how MediaWiki reports most
// of them.
func TestAnAPIErrorIsReported(t *testing.T) {
	c := fakeWiki(t, `{"error":{"code":"badvalue","info":"Unrecognized value"}}`, goodFile)
	if _, err := c.LeadImage(context.Background(), "en", "X"); err == nil {
		t.Fatal("an API error was not reported")
	}
}

func TestAnEmptyTitleMakesNoRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	if _, err := New(srv.URL).LeadImage(context.Background(), "en", "   "); err != nil {
		t.Fatalf("LeadImage: %v", err)
	}
	if called {
		t.Error("an empty title still reached the API")
	}
}

// Nil is not a valid client, and a missed check should say so rather than
// panic -- the same contract geocode.Client has.
func TestANilClientReportsItself(t *testing.T) {
	var c *Client
	if _, err := c.LeadImage(context.Background(), "en", "X"); err != ErrNotConfigured {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

// No configuration needed, which is the whole reason this is the fallback
// rather than a paid image search -- and the edition follows the language the
// model was asked to answer in.
//
// The edition matters: article titles are not translations of each other
// ("Brandenburger Tor" vs "Brandenburg Gate"), and smaller places often have
// an article in one language and none in another, so a client pinned to en
// would find nothing for a whole class of places.
func TestTheEditionFollowsTheLanguage(t *testing.T) {
	c := New("")
	for lang, want := range map[string]string{
		"en":    "https://en.wikipedia.org/w/api.php",
		"de":    "https://de.wikipedia.org/w/api.php",
		"de-AT": "https://de.wikipedia.org/w/api.php",
		"de_DE": "https://de.wikipedia.org/w/api.php",
		"DE":    "https://de.wikipedia.org/w/api.php",
		// Anything that is not plainly a language code falls back, because
		// this string ends up in a hostname.
		"":                "https://en.wikipedia.org/w/api.php",
		"  ":              "https://en.wikipedia.org/w/api.php",
		"english":         "https://en.wikipedia.org/w/api.php",
		"e":               "https://en.wikipedia.org/w/api.php",
		"e../../evil":     "https://en.wikipedia.org/w/api.php",
		"de.evil.example": "https://en.wikipedia.org/w/api.php",
	} {
		if got := c.endpointFor(lang); got != want {
			t.Errorf("endpointFor(%q) = %q, want %q", lang, got, want)
		}
	}

	// An explicit endpoint pins every lookup, for a mirror or a test.
	pinned := New("https://mirror.example/w/api.php")
	if got := pinned.endpointFor("de"); got != "https://mirror.example/w/api.php" {
		t.Errorf("an explicit endpoint was overridden: %q", got)
	}
}

func TestFileNameOf(t *testing.T) {
	cases := map[string]string{
		"https://upload.wikimedia.org/wikipedia/commons/a/ab/Kex_Hostel.jpg": "Kex_Hostel.jpg",
		// Percent-encoded in the path, decoded for the API.
		"https://upload.wikimedia.org/wikipedia/commons/1/12/Hallgr%C3%ADmskirkja.jpg": "Hallgrímskirkja.jpg",
		"not a url at all": "not a url at all",
	}
	for in, want := range cases {
		if got := fileNameOf(in); got != want {
			t.Errorf("fileNameOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlattenHTML(t *testing.T) {
	cases := map[string]string{
		`<a href="/x">Someone</a>`:                  "Someone",
		`<a href="/x">A</a> and <a href="/y">B</a>`: "A and B",
		`Jane &amp; Co.`:                            "Jane & Co.",
		`  spaced   out  `:                          "spaced out",
		``:                                          "",
		`<span class="a">Nested <b>markup</b></span>`: "Nested markup",
	}
	for in, want := range cases {
		if got := flattenHTML(in); got != want {
			t.Errorf("flattenHTML(%q) = %q, want %q", in, got, want)
		}
	}
	// Long credits are truncated by rune, not by byte: cutting mid-rune
	// produces replacement characters.
	long := flattenHTML(strings.Repeat("é", 400))
	if n := len([]rune(long)); n > 301 {
		t.Errorf("a long credit came back %d runes", n)
	}
	if strings.Contains(long, "�") {
		t.Error("truncation cut a rune in half")
	}
}

// The request the client actually sends. formatversion=2 is what makes pages
// an array rather than a map keyed by an unpredictable page id, and
// redirects=1 is what makes "Eiffel tower" find "Eiffel Tower".
func TestTheRequestCarriesTheParametersTheAPINeeds(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got == nil {
			got = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"query":{"pages":[]}}`)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).LeadImage(context.Background(), "en", "Eiffel tower"); err != nil {
		t.Fatalf("LeadImage: %v", err)
	}
	for k, want := range map[string]string{
		"format":        "json",
		"formatversion": "2",
		"redirects":     "1",
		"titles":        "Eiffel tower",
	} {
		if got.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want)
		}
	}
	if !strings.Contains(got.Get("prop"), "pageimages") {
		t.Errorf("prop = %q", got.Get("prop"))
	}
}

// A body larger than the cap is refused rather than read into memory.
func TestAnEnormousResponseIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(map[string]string{"padding": strings.Repeat("x", maxBody+1024)})
	}))
	defer srv.Close()
	if _, err := New(srv.URL).LeadImage(context.Background(), "en", "X"); err == nil {
		t.Error("an oversized response was accepted")
	}
}
