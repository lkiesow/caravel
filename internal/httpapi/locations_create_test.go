package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"caravel/internal/db"
)

// The point of the multipart create is that a location either exists complete
// or does not exist at all. Every failure test below therefore ends with the
// same assertion: the trip still has no locations.

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 20, G: 120, B: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// locationCreatePart describes one part of a multipart create body.
type locationCreatePart struct {
	field    string
	filename string
	value    string
	content  []byte
}

func (ts *testServer) createLocationMultipartReq(cookie *http.Cookie, tripID string, parts []locationCreatePart) *httptest.ResponseRecorder {
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

	r := httptest.NewRequest(http.MethodPost, "/api/trips/"+tripID+"/locations", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, r)
	return w
}

// locationCount is the assertion every failure case ends on.
func (ts *testServer) locationCount(cookie *http.Cookie, tripID string) int {
	ts.t.Helper()
	res := ts.do(http.MethodGet, "/api/trips/"+tripID+"/locations", cookie, "")
	if res.Code != http.StatusOK {
		ts.t.Fatalf("list items = %d: %s", res.Code, res.Body.String())
	}
	var locations []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &locations); err != nil {
		ts.t.Fatalf("decode locations: %v (%s)", err, res.Body.String())
	}
	return len(locations)
}

func locationJSON(title string) string {
	return `{"category":"site","tags":["museum"],"title":"` + title + `",` +
		`"geo":{"lat":52.5,"lng":13.4,"address":"Berlin"},` +
		`"links":[{"url":"https://example.com","label":"site"}],` +
		`"dates":[{"start_date":"2026-05-01","end_date":"2026-05-02"}]}`
}

// The happy path: one request carries the location, its nested rows, a cover
// and two files, and all of it lands.
func TestCreateLocationMultipartCommitsEverythingAtOnce(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
		{field: "file", filename: "ticket.txt", content: []byte("admit one")},
		{field: "file_note", value: "the ticket"},
		{field: "file_visibility", value: "personal"},
		{field: "file", filename: "map.txt", content: []byte("a map")},
		{field: "file_note", value: ""},
		{field: "file_visibility", value: "trip"},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %s", res.Code, res.Body.String())
	}

	var detail map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail["title"] != "Pergamon" {
		t.Fatalf("title = %v", detail["title"])
	}
	if detail["image_id"] == nil {
		t.Fatal("the cover did not land: image_id is null")
	}
	if detail["image_url"] == nil {
		t.Fatal("the cover has no url")
	}
	geo, _ := detail["geo"].(map[string]any)
	if geo == nil || geo["address"] != "Berlin" {
		t.Fatalf("nested geo did not land: %v", detail["geo"])
	}
	if links, _ := detail["links"].([]any); len(links) != 1 {
		t.Fatalf("links = %v, want 1", detail["links"])
	}
	if dates, _ := detail["dates"].([]any); len(dates) != 1 {
		t.Fatalf("dates = %v, want 1", detail["dates"])
	}

	locationID, _ := detail["id"].(string)
	filesRes := ts.do(http.MethodGet, "/api/locations/"+locationID+"/files", cookie, "")
	var files []map[string]any
	if err := json.Unmarshal(filesRes.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2: %s", len(files), filesRes.Body.String())
	}
	// The positional note and visibility have to line up with their file.
	byName := map[string]map[string]any{}
	for _, f := range files {
		byName[f["filename"].(string)] = f
	}
	if got := byName["ticket.txt"]["note"]; got != "the ticket" {
		t.Errorf("ticket note = %v, want %q", got, "the ticket")
	}
	if got := byName["ticket.txt"]["visibility"]; got != "personal" {
		t.Errorf("ticket visibility = %v, want personal", got)
	}
	if got := byName["map.txt"]["note"]; got != nil {
		t.Errorf("map note = %v, want null", got)
	}
	if got := byName["map.txt"]["visibility"]; got != "trip" {
		t.Errorf("map visibility = %v, want trip", got)
	}
}

// An undecodable cover must take the whole location down with it.
func TestCreateLocationMultipartRollsBackOnBadImage(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		{field: "image", filename: "cover.png", content: []byte("this is not a png")},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if n := ts.locationCount(cookie, trip); n != 0 {
		t.Fatalf("%d location(s) created despite the failure; the whole point is that none is", n)
	}
}

// The URL the browser could not be talked out of: unreachable host.
func TestCreateLocationMultipartRollsBackOnUnfetchableImageURL(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		// Port 0 is never listening, so this fails at dial without reaching
		// anyone else's server.
		{field: "image_url", value: "http://127.0.0.1:0/nope.png"},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if n := ts.locationCount(cookie, trip); n != 0 {
		t.Fatalf("%d location(s) created despite the failure", n)
	}
}

func TestCreateLocationMultipartRejectsBadLocationPart(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	cases := map[string]string{
		"no title":         `{"category":"site","tags":["museum"],"title":"  "}`,
		"bad category":     `{"category":"nonsense","tags":["museum"],"title":"X"}`,
		"unknown field":    `{"category":"site","tags":["museum"],"title":"X","colour":"red"}`,
		"not json at all":  `nonsense`,
		"link with no url": `{"category":"site","tags":["museum"],"title":"X","links":[{"label":"a"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
				{field: "location", value: body},
				{field: "image", filename: "cover.png", content: testPNG(t)},
			})
			if res.Code != http.StatusBadRequest {
				t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
			}
			if n := ts.locationCount(cookie, trip); n != 0 {
				t.Fatalf("%d location(s) created despite the failure", n)
			}
		})
	}
}

func TestCreateLocationMultipartRejectsMissingLocationPart(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "image", filename: "cover.png", content: testPNG(t)},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if n := ts.locationCount(cookie, trip); n != 0 {
		t.Fatalf("%d location(s) created despite the failure", n)
	}
}

// Sending both is a client bug, and guessing which one was meant would put
// the wrong picture on the location.
func TestCreateLocationMultipartRejectsBothImageAndURL(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
		{field: "image_url", value: "https://example.com/other.png"},
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", res.Code, res.Body.String())
	}
	if n := ts.locationCount(cookie, trip); n != 0 {
		t.Fatalf("%d location(s) created despite the failure", n)
	}
}

// A location with no cover and no files is still a perfectly good location,
// and the multipart path must not require one.
func TestCreateLocationMultipartWithoutImageOrFiles(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %s", res.Code, res.Body.String())
	}
	var detail map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &detail)
	if detail["image_id"] != nil {
		t.Fatalf("image_id = %v, want null", detail["image_id"])
	}
	if n := ts.locationCount(cookie, trip); n != 1 {
		t.Fatalf("locations = %d, want 1", n)
	}
}

// The JSON path is the one the assistant and the older clients use, and it
// must behave exactly as it did.
func TestCreateLocationJSONPathStillWorks(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.do(http.MethodPost, "/api/trips/"+trip+"/locations", cookie, locationJSON("Pergamon"))
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %s", res.Code, res.Body.String())
	}
	var detail map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &detail)
	if detail["title"] != "Pergamon" {
		t.Fatalf("title = %v", detail["title"])
	}
	geo, _ := detail["geo"].(map[string]any)
	if geo == nil || geo["address"] != "Berlin" {
		t.Fatalf("nested geo did not land: %v", detail["geo"])
	}
	// Still strict about unknown fields.
	bad := ts.do(http.MethodPost, "/api/trips/"+trip+"/locations", cookie,
		`{"category":"site","tags":["museum"],"title":"X","colour":"red"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", bad.Code)
	}
}

// A viewer may not create a location, whichever body shape they send.
func TestCreateLocationMultipartRequiresEditor(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	trip := ts.createTrip(owner, "Berlin")
	stranger := ts.login("stranger")

	res := ts.createLocationMultipartReq(stranger, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
	})
	if res.Code != http.StatusNotFound && res.Code != http.StatusForbidden {
		t.Fatalf("stranger create = %d, want 403/404: %s", res.Code, res.Body.String())
	}
	if n := ts.locationCount(owner, trip); n != 0 {
		t.Fatalf("%d location(s) created by a stranger", n)
	}
}

// A file too large for the per-part limit must not leave a location behind
// either -- the location is written last, so this exercises the ordering.
func TestCreateLocationMultipartRollsBackOnOversizeFile(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	// Comfortably over maxLocationCreateBytes, so the body cap trips first;
	// this is the shape of "somebody attached a video".
	huge := bytes.Repeat([]byte("x"), (maxLocationCreateBytes)+1024)
	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		{field: "file", filename: "huge.bin", content: huge},
	})
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("create = %d, want 413: %s", res.Code, res.Body.String())
	}
	if n := ts.locationCount(cookie, trip); n != 0 {
		t.Fatalf("%d location(s) created despite the failure", n)
	}
}

// The strongest atomicity claim: a failure on the *last* write in the
// transaction, after the location, its nested rows, the media asset and the
// image attachment have all been inserted. None of it may survive.
func TestCreateLocationMultipartRollsBackEveryWriteTogether(t *testing.T) {
	ts := newTestServerWithStore(t, func(s db.Store) db.Store {
		return failingStore{Store: s, failCreateFile: true}
	})
	cookie := ts.login("owner")
	trip := ts.createTrip(cookie, "Berlin")

	res := ts.createLocationMultipartReq(cookie, trip, []locationCreatePart{
		{field: "location", value: locationJSON("Pergamon")},
		{field: "image", filename: "cover.png", content: testPNG(t)},
		{field: "file", filename: "ticket.txt", content: []byte("admit one")},
	})
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("create = %d, want 500: %s", res.Code, res.Body.String())
	}

	if n := ts.locationCount(cookie, trip); n != 0 {
		t.Errorf("%d location(s) survived a failed create", n)
	}
	// And no orphan file row at the trip level either.
	filesRes := ts.do(http.MethodGet, "/api/trips/"+trip+"/files", cookie, "")
	var files []map[string]any
	if err := json.Unmarshal(filesRes.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode trip files: %v (%s)", err, filesRes.Body.String())
	}
	if len(files) != 0 {
		t.Errorf("%d file row(s) survived a failed create: %s", len(files), filesRes.Body.String())
	}
}
