package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The map payload carries each place's photo, so a marker popup can show the
// same picture the location page shows (Stage 34). Before that it carried only
// title, coordinates and the outbound Google Maps link, and the popup had no
// way to know an item had a cover at all.

type mapItemPayload struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	ImageURL *string `json:"image_url"`
}

func (ts *testServer) tripMap(cookie *http.Cookie, tripID string) []mapItemPayload {
	ts.t.Helper()
	res := ts.do(http.MethodGet, "/api/trips/"+tripID+"/map", cookie, "")
	if res.Code != http.StatusOK {
		ts.t.Fatalf("get map = %d: %s", res.Code, res.Body.String())
	}
	var items []mapItemPayload
	if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
		ts.t.Fatalf("decode map: %v (%s)", err, res.Body.String())
	}
	return items
}

func TestTripMapCarriesItemImageURL(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	// One located place with a cover, one without: image_url has to be the
	// per-item answer, not a property of the payload.
	res := ts.createItemMultipartReq(cookie, trip, []itemCreatePart{
		{field: "item", value: itemJSON("Pergamon")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create with cover = %d: %s", res.Code, res.Body.String())
	}
	res = ts.createItemMultipartReq(cookie, trip, []itemCreatePart{
		{field: "item", value: itemJSON("Fernsehturm")},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create without cover = %d: %s", res.Code, res.Body.String())
	}

	items := ts.tripMap(cookie, trip)
	if len(items) != 2 {
		t.Fatalf("got %d map items, want 2: %+v", len(items), items)
	}

	byTitle := map[string]mapItemPayload{}
	for _, it := range items {
		byTitle[it.Title] = it
	}

	withCover, ok := byTitle["Pergamon"]
	if !ok {
		t.Fatalf("Pergamon missing from map: %+v", items)
	}
	if withCover.ImageURL == nil {
		t.Fatalf("Pergamon has no image_url")
	}
	// The same locally-served media URL the location page renders, not the
	// media id and not the original upload.
	if !strings.HasPrefix(*withCover.ImageURL, "/api/media/") || !strings.HasSuffix(*withCover.ImageURL, "/file") {
		t.Errorf("image_url = %q, want /api/media/{id}/file", *withCover.ImageURL)
	}

	withoutCover, ok := byTitle["Fernsehturm"]
	if !ok {
		t.Fatalf("Fernsehturm missing from map: %+v", items)
	}
	// Explicitly null rather than an empty string: the popup renders no image
	// element at all in that case, with no placeholder box.
	if withoutCover.ImageURL != nil {
		t.Errorf("image_url = %q for an item with no cover, want null", *withoutCover.ImageURL)
	}
}
