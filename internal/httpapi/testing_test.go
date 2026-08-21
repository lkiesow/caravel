package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"caravel/internal/auth"
	"caravel/internal/db"
	"caravel/internal/storagefs"
)

// Shared HTTP-level test harness for this package.
//
// These helpers were written for itinerary_test.go (Stage 07 Milestone 6, which
// needed a cross-user ownership check — a case no unit test can express) and were
// private to that one file. They're here so every handler can be tested the same
// way, which is what Stage 08 Milestone 7 is for.
//
// testServer is a real Server over a real (temporary, per-test) SQLite database —
// db.Open runs the migrations — so handlers, routing, the auth middleware and the
// schema's ON DELETE CASCADE are all exercised as they are in production. The
// static asset FS is an empty stand-in; the blob store is a real filesystem one
// rooted in the test's temp dir, so the file and media upload paths work.
type testServer struct {
	*Server
	t *testing.T
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	return newTestServerWithStore(t, nil)
}

// newTestServerWithStore is newTestServer with a hook to decorate the Store
// before the Server gets it — for tests that need a failure the real store
// won't produce on demand, such as proving a transaction rolls back (see
// failingStore in items_test.go). Pass nil for the plain store.
func newTestServerWithStore(t *testing.T, wrap func(db.Store) db.Store) *testServer {
	t.Helper()

	dir := t.TempDir()

	conn, err := db.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	store, err := db.NewStore("sqlite", conn)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if wrap != nil {
		store = wrap(store)
	}

	// A real blob store rather than nil: uploads are among the handlers with no
	// coverage, and a nil Blob panics rather than failing usefully.
	blob := storagefs.NewLocalFS(filepath.Join(dir, "uploads"))

	// Geocoding off by default, and deliberately so: with the production
	// default here instead, any test that reached /api/geocode would send a
	// real request to OpenStreetMap's public Nominatim. The geocode tests set
	// srv.GeocoderURL to their own httptest.Server.
	srv := NewServer(conn, store, auth.NewService(store), blob, fstest.MapFS{}, false, true, "")
	return &testServer{Server: srv, t: t}
}

// login registers a user and returns the session cookie for them.
func (ts *testServer) login(username string) *http.Cookie {
	ts.t.Helper()

	user, err := ts.Auth.Register(context.Background(), username, "password123", username)
	if err != nil {
		ts.t.Fatalf("register %s: %v", username, err)
	}
	token, _, err := ts.Auth.StartSession(context.Background(), user.ID, "test", "127.0.0.1")
	if err != nil {
		ts.t.Fatalf("start session for %s: %v", username, err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: token}
}

// do issues a request through the full router and returns the recorder.
// body may be empty for methods that carry none.
func (ts *testServer) do(method, path string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	ts.t.Helper()

	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, r)
	return w
}

// upload issues a multipart POST with a single "file" part, for the file and
// media handlers.
func (ts *testServer) upload(path string, cookie *http.Cookie, filename, contentType string, content []byte) *httptest.ResponseRecorder {
	ts.t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="file"; filename="` + filename + `"`}
	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}
	part, err := mw.CreatePart(header)
	if err != nil {
		ts.t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		ts.t.Fatalf("write multipart body: %v", err)
	}
	if err := mw.Close(); err != nil {
		ts.t.Fatalf("close multipart writer: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, r)
	return w
}

// decode unmarshals a JSON response body, failing the test if it doesn't parse.
func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

// mustCreate issues a request, asserts the status, and returns the response's
// "id" field — the shape almost every setup step in these tests needs.
func (ts *testServer) mustCreate(method, path string, cookie *http.Cookie, body string, wantStatus int) string {
	ts.t.Helper()

	w := ts.do(method, path, cookie, body)
	if w.Code != wantStatus {
		ts.t.Fatalf("%s %s: got %d, want %d, body %s", method, path, w.Code, wantStatus, w.Body.String())
	}
	id, ok := decode[map[string]any](ts.t, w)["id"].(string)
	if !ok {
		ts.t.Fatalf("%s %s: response has no string id: %s", method, path, w.Body.String())
	}
	return id
}

// createTrip makes a trip owned by the cookie's user and returns its ID.
func (ts *testServer) createTrip(cookie *http.Cookie, title string) string {
	ts.t.Helper()
	return ts.mustCreate(http.MethodPost, "/api/trips", cookie, `{"title":"`+title+`"}`, http.StatusCreated)
}

// createItem makes an item on a trip and returns its ID.
func (ts *testServer) createItem(cookie *http.Cookie, tripID, title string) string {
	ts.t.Helper()
	return ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/items", cookie,
		`{"title":"`+title+`","category":"site","type":"landmark"}`, http.StatusCreated,
	)
}
