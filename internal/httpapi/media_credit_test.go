package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Storing where an image came from, and handing it back so it can be credited.
//
// The point of the columns: a freely licensed photograph is not an
// unencumbered one, and an image saved with no record of whose it is cannot be
// credited afterwards. Caravel is already multi-user and the backlog carries
// public share links, so this is not hypothetical.

// pngServer serves one small PNG, which is what the image pipeline needs to
// see for a fetch-by-URL to succeed at all.
func pngServer(t *testing.T) *httptest.Server {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		for y := range 8 {
			img.Set(x, y, color.RGBA{R: 20, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMediaByURLStoresProvenance(t *testing.T) {
	ts := newTestServer(t)
	img := pngServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	itemID := ts.createItem(cookie, tripID, "Heger Tor")

	body := fmt.Sprintf(`{"url":%q,"source_url":"https://de.wikipedia.org/wiki/Waterloo-Tor",
	  "credit":"MrsMyer","license":"CC BY-SA 3.0"}`, img.URL+"/a.png")
	rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/media/url", cookie, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var asset struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &asset); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Attach it, because the item is where the credit is rendered and
	// therefore where it has to arrive.
	rec = ts.do(http.MethodPut, "/api/items/"+itemID+"/image", cookie, `{"media_asset_id":"`+asset.ID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = ts.do(http.MethodGet, "/api/items/"+itemID, cookie, "")
	var item struct {
		ImageURL    *string `json:"image_url"`
		ImageCredit *struct {
			Text      string `json:"text"`
			License   string `json:"license"`
			SourceURL string `json:"source_url"`
		} `json:"image_credit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item.ImageURL == nil {
		t.Fatal("the item has no image")
	}
	if item.ImageCredit == nil {
		t.Fatal("the item carries no credit; storing one was pointless")
	}
	if item.ImageCredit.Text != "MrsMyer" || item.ImageCredit.License != "CC BY-SA 3.0" {
		t.Errorf("credit = %+v", item.ImageCredit)
	}
	if item.ImageCredit.SourceURL != "https://de.wikipedia.org/wiki/Waterloo-Tor" {
		t.Errorf("source = %q, want the page it came from", item.ImageCredit.SourceURL)
	}
}

// The ordinary case, and it must stay the ordinary case: an image somebody
// pasted or uploaded has no credit, and the client must be able to tell that
// apart from one that does.
func TestAnImageWithNoProvenanceHasNoCredit(t *testing.T) {
	ts := newTestServer(t)
	img := pngServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	itemID := ts.createItem(cookie, tripID, "A hotel")

	rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/media/url", cookie,
		fmt.Sprintf(`{"url":%q}`, img.URL+"/a.png"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var asset struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &asset)
	ts.do(http.MethodPut, "/api/items/"+itemID+"/image", cookie, `{"media_asset_id":"`+asset.ID+`"}`)

	rec = ts.do(http.MethodGet, "/api/items/"+itemID, cookie, "")
	if !strings.Contains(rec.Body.String(), `"image_credit":null`) {
		t.Errorf("body = %s, want a null credit", rec.Body.String())
	}
}

// A malformed provenance field costs the value, never the image. Somebody
// losing a picture because a credit URL was mistyped would be a worse outcome
// than an uncredited picture.
func TestBadProvenanceIsDroppedRatherThanRefused(t *testing.T) {
	ts := newTestServer(t)
	img := pngServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	itemID := ts.createItem(cookie, tripID, "A hotel")

	body := fmt.Sprintf(`{"url":%q,"source_url":"javascript:alert(1)","credit":%q}`,
		img.URL+"/a.png", strings.Repeat("A", maxCreditBytes*2))
	rec := ts.do(http.MethodPost, "/api/trips/"+tripID+"/media/url", cookie, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var asset struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &asset)
	ts.do(http.MethodPut, "/api/items/"+itemID+"/image", cookie, `{"media_asset_id":"`+asset.ID+`"}`)

	rec = ts.do(http.MethodGet, "/api/items/"+itemID, cookie, "")
	// A credit with nowhere to point is not a credit, so the whole object is
	// absent rather than half-filled.
	if !strings.Contains(rec.Body.String(), `"image_credit":null`) {
		t.Errorf("body = %s, want the unusable source URL to have dropped the credit", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "javascript:") {
		t.Error("a javascript: URL was stored as a source")
	}
}

func TestTruncateBytesCutsOnARuneBoundary(t *testing.T) {
	// Cutting mid-rune produces replacement characters in something that gets
	// rendered on a page.
	got := truncateBytes(strings.Repeat("é", 400), 100)
	if len(got) > 100 {
		t.Errorf("len = %d, want at most 100", len(got))
	}
	if strings.Contains(got, "�") {
		t.Error("truncation cut a rune in half")
	}
	if s := "short"; truncateBytes(s, 100) != s {
		t.Error("a short string was altered")
	}
}
