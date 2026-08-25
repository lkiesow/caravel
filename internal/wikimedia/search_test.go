package wikimedia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Searching for pictures, and the filter that makes the second call worth
// making at all.
//
// The plan was explicit that this might not be worth having: generator=images
// lists every file on an article, and an article's files are icons, flags,
// maps, logos and Commons chrome as well as photographs. Offering six pictures
// of which four are icons is worse than offering one good one -- so the filter
// is the feature, and this is the test that says so.

// noisyArticle answers both calls Search makes, with an image list shaped like
// a real one: a handful of photographs among the page furniture.
func noisyArticle(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		switch q.Get("generator") {
		case "search":
			_ = enc.Encode(map[string]any{"query": map[string]any{"pages": []any{
				// Deliberately out of rank order: the API returns pages in
				// whatever order it likes and puts the rank in `index`, so a
				// caller that trusts the array order gets the wrong "best
				// match" -- and reads the wrong article in the second call.
				map[string]any{
					"index": 2, "title": "Runner Up",
					"original": map[string]any{"source": "https://img.example/runner.jpg", "width": 900, "height": 600},
					"fullurl":  "https://wiki.example/Runner_Up",
				},
				map[string]any{
					"index": 1, "title": "Best Match",
					"original":  map[string]any{"source": "https://img.example/best.jpg", "width": 1200, "height": 800},
					"thumbnail": map[string]any{"source": "https://img.example/best-320.jpg"},
					"fullurl":   "https://wiki.example/Best_Match",
				},
			}}})
		case "images":
			if got := q.Get("titles"); got != "Best Match" {
				t.Errorf("the image list was read from %q, want the best-ranked article", got)
			}
			file := func(name, mime string, w, h int) any {
				return map[string]any{
					"title": "File:" + name,
					"imageinfo": []any{map[string]any{
						"url": "https://img.example/" + name, "mime": mime,
						"width": w, "height": h,
						"extmetadata": map[string]any{
							"LicenseShortName": map[string]any{"value": "CC BY-SA 4.0"},
							"Artist":           map[string]any{"value": `<a href="/u">A. Photographer</a>`},
						},
					}},
				}
			}
			_ = enc.Encode(map[string]any{"query": map[string]any{"pages": []any{
				file("Commons-logo.svg", "image/svg+xml", 1024, 1376),
				file("Icon of Shinto.svg", "image/svg+xml", 128, 128),
				file("Location map.svg", "image/svg+xml", 413, 373),
				file("OOjs UI icon edit.svg", "image/svg+xml", 20, 20),
				file("Tiny thumbnail.jpg", "image/jpeg", 80, 60),
				file("A video.ogv", "application/ogg", 1830, 1080),
				file("Real photograph.jpg", "image/jpeg", 2048, 1536),
				file("Another photograph.png", "image/png", 1600, 1200),
			}}})
		default:
			// The batched licence lookup for the lead images.
			_ = enc.Encode(map[string]any{"query": map[string]any{"pages": []any{
				map[string]any{
					"title": "File:best.jpg",
					"imageinfo": []any{map[string]any{
						"descriptionurl": "https://wiki.example/File:best.jpg",
						"extmetadata": map[string]any{
							"LicenseShortName": map[string]any{"value": "CC BY 3.0"},
							"Artist":           map[string]any{"value": "B. Photographer"},
						},
					}},
				},
			}}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchKeepsThePhotographsAndDropsThePageFurniture(t *testing.T) {
	got, err := New(noisyArticle(t).URL).Search(context.Background(), "en", "best match", 12)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var titles []string
	for _, i := range got {
		titles = append(titles, i.Title)
	}
	// Two lead images and two photographs off the top article. Named rather
	// than counted: a count of four would also pass on the wrong four.
	want := []string{"Best Match", "Runner Up", "Real photograph", "Another photograph"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("Search returned %v, want %v", titles, want)
	}

	// The four SVGs, the 80x60 thumbnail and the video are what the gate
	// exists for, and each is a shape that really does turn up.
	for _, i := range got {
		if strings.Contains(i.Title, "logo") || strings.Contains(i.Title, "Icon") ||
			strings.Contains(i.Title, "map") || strings.Contains(i.Title, "Tiny") || strings.Contains(i.Title, "video") {
			t.Errorf("page furniture reached the results: %q", i.Title)
		}
	}
}

func TestSearchRanksByTheSearchRankNotTheArrayOrder(t *testing.T) {
	got, err := New(noisyArticle(t).URL).Search(context.Background(), "en", "best match", 12)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 || got[0].Title != "Best Match" {
		t.Fatalf("first result is %+v, want the index=1 article", got)
	}
}

func TestSearchFillsInTheLicenceOfALeadImage(t *testing.T) {
	got, err := New(noisyArticle(t).URL).Search(context.Background(), "en", "best match", 12)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// pageimages says what the picture is, not who took it: without the third
	// call a lead image would be offered with no attribution at all.
	if got[0].Credit != "B. Photographer" || got[0].Licence != "CC BY 3.0" {
		t.Errorf("lead image credit = %q / %q, want the batched lookup to have filled it in", got[0].Credit, got[0].Licence)
	}
	if got[0].DescriptionURL != "https://wiki.example/File:best.jpg" {
		t.Errorf("lead image has no file page to credit: %q", got[0].DescriptionURL)
	}
	// And the markup Wikimedia wraps an author in is gone: this is rendered as
	// text and stored as text.
	if strings.Contains(got[2].Credit, "<") {
		t.Errorf("credit still carries markup: %q", got[2].Credit)
	}
}

func TestSearchLimitIsAHardCap(t *testing.T) {
	got, err := New(noisyArticle(t).URL).Search(context.Background(), "en", "best match", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("Search returned %d results for a limit of 3", len(got))
	}
}

func TestSearchAnswersNothingRatherThanFailing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// What the API really sends for a search that matched no article: the
		// query object with no pages in it at all.
		_, _ = w.Write([]byte(`{"batchcomplete":true,"query":{}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).Search(context.Background(), "en", "no such place anywhere", 12)
	if err != nil {
		t.Fatalf("a search that matches nothing is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Search returned %d results for a query that matched nothing", len(got))
	}
}

func TestSearchNeedsAQueryAndAClient(t *testing.T) {
	if _, err := (*Client)(nil).Search(context.Background(), "en", "anything", 12); err == nil {
		t.Error("a nil client answered a search")
	}
	got, err := New("https://unused.invalid").Search(context.Background(), "en", "   ", 12)
	if err != nil || got != nil {
		t.Errorf("an empty query = %v, %v; want nothing, and no request made", got, err)
	}
}

// The fixture the browser suite runs against, checked here rather than only
// from Playwright: a broken stub fails as a confusing UI test otherwise.
func TestTheStubServesAGridWorthRendering(t *testing.T) {
	got, err := New(StubURL).Search(context.Background(), "en", "stub", 12)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("the stub offered %d images, want three (one of them dead)", len(got))
	}
	for _, i := range got {
		if i.URL == "" || i.Licence == "" || i.Credit == "" {
			t.Errorf("stub image %+v is missing something the grid renders", i)
		}
	}
	// The SVG in its image list is there to be dropped.
	for _, i := range got {
		if strings.Contains(i.Title, "Commons") {
			t.Errorf("the stub's icon reached the results: %q", i.Title)
		}
	}
}
