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
// way to know a location had a cover at all.

type mapLocationPayload struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	ImageURL *string `json:"image_url"`
}

func (ts *testServer) tripMap(cookie *http.Cookie, tripID string) []mapLocationPayload {
	ts.t.Helper()
	res := ts.do(http.MethodGet, "/api/trips/"+tripID+"/map", cookie, "")
	if res.Code != http.StatusOK {
		ts.t.Fatalf("get map = %d: %s", res.Code, res.Body.String())
	}
	var locations []mapLocationPayload
	if err := json.Unmarshal(res.Body.Bytes(), &locations); err != nil {
		ts.t.Fatalf("decode map: %v (%s)", err, res.Body.String())
	}
	return locations
}

func TestTripMapCarriesLocationImageURL(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	// One located place with a cover, one without: image_url has to be the
	// per-location answer, not a property of the payload.
	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create with cover = %d: %s", res.Code, res.Body.String())
	}
	res = ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Fernsehturm")},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create without cover = %d: %s", res.Code, res.Body.String())
	}

	locations := ts.tripMap(cookie, trip)
	if len(locations) != 2 {
		t.Fatalf("got %d map locations, want 2: %+v", len(locations), locations)
	}

	byTitle := map[string]mapLocationPayload{}
	for _, it := range locations {
		byTitle[it.Title] = it
	}

	withCover, ok := byTitle["Pergamon"]
	if !ok {
		t.Fatalf("Pergamon missing from map: %+v", locations)
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
		t.Fatalf("Fernsehturm missing from map: %+v", locations)
	}
	// Explicitly null rather than an empty string: the popup renders no image
	// element at all in that case, with no placeholder box.
	if withoutCover.ImageURL != nil {
		t.Errorf("image_url = %q for a location with no cover, want null", *withoutCover.ImageURL)
	}
}
