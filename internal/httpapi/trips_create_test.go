package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTripMultipartReq builds a multipart POST /api/trips, reusing the part
// description the location create tests already use.
func (ts *testServer) createTripMultipartReq(cookie *http.Cookie, parts []itemCreatePart) *httptest.ResponseRecorder {
	ts.t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.filename == "" {
			if err := mw.WriteField(p.field, p.value); err != nil {
				ts.t.Fatalf("write field %q: %v", p.field, err)
			}
			continue
		}
		part, err := mw.CreateFormFile(p.field, p.filename)
		if err != nil {
			ts.t.Fatalf("create part %q: %v", p.field, err)
		}
		if _, err := part.Write(p.content); err != nil {
			ts.t.Fatalf("write part %q: %v", p.field, err)
		}
	}
	if err := mw.Close(); err != nil {
		ts.t.Fatalf("close multipart: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/trips", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, r)
	return w
}

// tripCount is the assertion every failure case ends on: nothing was created.
func (ts *testServer) tripCount(cookie *http.Cookie) int {
	ts.t.Helper()
	res := ts.do(http.MethodGet, "/api/trips", cookie, "")
	if res.Code != http.StatusOK {
		ts.t.Fatalf("list trips = %d: %s", res.Code, res.Body.String())
	}
	var trips []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &trips); err != nil {
		ts.t.Fatalf("decode trips: %v (%s)", err, res.Body.String())
	}
	return len(trips)
}

func tripJSON(title string) string {
	return `{"title":"` + title + `","start_date":"2026-05-01","end_date":"2026-05-10",` +
		`"subtitle":"a test","currency":"EUR"}`
}

// The happy path: one request carries the trip and its cover, and both land
// with the trip pointing at the asset.
func TestCreateTripMultipartCommitsEverythingAtOnce(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")

	res := ts.createTripMultipartReq(cookie, []itemCreatePart{
		{field: "trip", value: tripJSON("Iceland")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", res.Code, res.Body.String())
	}

	var trip map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &trip); err != nil {
		t.Fatalf("decode trip: %v", err)
	}
	if trip["title"] != "Iceland" {
		t.Errorf("title = %v", trip["title"])
	}
	if trip["preview_image_id"] == nil {
		t.Error("preview_image_id is nil; the cover did not reach the trip")
	}
	if trip["currency"] != "EUR" {
		t.Errorf("currency = %v", trip["currency"])
	}
}

// The point of the milestone: an unfetchable cover URL leaves no trip behind.
// The old flow created the trip first and reported the failure afterwards.
func TestCreateTripMultipartRollsBackOnUnfetchableImageURL(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	before := ts.tripCount(cookie)

	res := ts.createTripMultipartReq(cookie, []itemCreatePart{
		{field: "trip", value: tripJSON("Nowhere")},
		// A port nothing is listening on: the fetch fails, before any write.
		{field: "image_url", value: "http://127.0.0.1:1/cover.png"},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if after := ts.tripCount(cookie); after != before {
		t.Errorf("trip count %d -> %d; a failed cover still created the trip", before, after)
	}
}

func TestCreateTripMultipartRollsBackOnBadImage(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	before := ts.tripCount(cookie)

	res := ts.createTripMultipartReq(cookie, []itemCreatePart{
		{field: "trip", value: tripJSON("Nowhere")},
		{field: "image", filename: "cover.png", content: []byte("not an image")},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if after := ts.tripCount(cookie); after != before {
		t.Errorf("trip count %d -> %d; an undecodable cover still created the trip", before, after)
	}
}

func TestCreateTripMultipartRejectsBothImageAndURL(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	before := ts.tripCount(cookie)

	res := ts.createTripMultipartReq(cookie, []itemCreatePart{
		{field: "trip", value: tripJSON("Ambiguous")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
		{field: "image_url", value: "https://example.com/cover.png"},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if after := ts.tripCount(cookie); after != before {
		t.Errorf("trip count %d -> %d", before, after)
	}
}

func TestCreateTripMultipartRejectsBadTripPart(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	before := ts.tripCount(cookie)

	for _, tc := range []struct{ name, value string }{
		// DisallowUnknownFields, as the JSON path does.
		{"unknown field", `{"title":"X","description":"not a field"}`},
		{"no title", `{"start_date":"2026-05-01"}`},
		{"end before start", `{"title":"X","start_date":"2026-05-10","end_date":"2026-05-01"}`},
		{"malformed json", `{"title":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := ts.createTripMultipartReq(cookie, []itemCreatePart{
				{field: "trip", value: tc.value},
			})
			if res.Code != http.StatusBadRequest {
				t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
			}
		})
	}

	t.Run("missing trip part", func(t *testing.T) {
		res := ts.createTripMultipartReq(cookie, []itemCreatePart{
			{field: "image", filename: "cover.png", content: testPNG(t)},
		})
		if res.Code != http.StatusBadRequest {
			t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
		}
	})

	if after := ts.tripCount(cookie); after != before {
		t.Errorf("trip count %d -> %d; a rejected create left something behind", before, after)
	}
}

// A trip with no cover at all is still a valid multipart create.
func TestCreateTripMultipartWithoutImage(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")

	res := ts.createTripMultipartReq(cookie, []itemCreatePart{
		{field: "trip", value: tripJSON("Bare")},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", res.Code, res.Body.String())
	}
	var trip map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &trip); err != nil {
		t.Fatalf("decode trip: %v", err)
	}
	if trip["preview_image_id"] != nil {
		t.Errorf("preview_image_id = %v, want nil", trip["preview_image_id"])
	}
}

// The JSON path is untouched: it is what every existing client and test uses.
func TestCreateTripJSONPathStillWorks(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")

	res := ts.do(http.MethodPost, "/api/trips", cookie, tripJSON("Plain JSON"))
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", res.Code, res.Body.String())
	}
	var trip map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &trip); err != nil {
		t.Fatalf("decode trip: %v", err)
	}
	if trip["title"] != "Plain JSON" {
		t.Errorf("title = %v", trip["title"])
	}
}
