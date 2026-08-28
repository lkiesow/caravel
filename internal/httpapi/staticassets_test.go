package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// staticFS is a stand-in for the embedded web tree: a shell, an asset in one
// of the asset directories, and one at the root.
func staticFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>Caravel</title>")},
		"js/app.js":  &fstest.MapFile{Data: []byte("export const hello = 1;\n")},
		"sw.js":      &fstest.MapFile{Data: []byte("// service worker\n")},
	}
}

func newStaticServer(t *testing.T, noCache bool) *testServer {
	t.Helper()
	return newTestServerWith(t, nil, func(o *Options) {
		o.WebFS = staticFS()
		o.NoCache = noCache
	})
}

// get issues an unauthenticated GET with optional extra headers. The static
// tree sits behind chi's NotFound, outside the session middleware, so no
// cookie is involved.
func getStatic(ts *testServer, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, r)
	return w
}

func TestStaticAssetETagAndRevalidation(t *testing.T) {
	ts := newStaticServer(t, false)

	res := getStatic(ts, "/js/app.js", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /js/app.js = %d, want 200", res.Code)
	}
	tag := res.Header().Get("ETag")
	if tag == "" {
		t.Fatal("GET /js/app.js carried no ETag")
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-cache")
	}

	// The whole point of the validator: a client that already has this copy
	// gets told so instead of being sent the bytes again.
	again := getStatic(ts, "/js/app.js", map[string]string{"If-None-Match": tag})
	if again.Code != http.StatusNotModified {
		t.Fatalf("conditional GET = %d, want 304", again.Code)
	}
	if body := again.Body.Len(); body != 0 {
		t.Fatalf("304 carried %d bytes of body, want none", body)
	}

	// A stale validator must not be honoured, or a new build would never
	// reach a client that had cached the old one.
	stale := getStatic(ts, "/js/app.js", map[string]string{"If-None-Match": `"0000000000000000"`})
	if stale.Code != http.StatusOK {
		t.Fatalf("GET with a stale ETag = %d, want 200", stale.Code)
	}
}

// The shell is reached as "/" far more often than as "/index.html", so it
// needs the validator under that name too.
func TestStaticShellHasETagAtRoot(t *testing.T) {
	ts := newStaticServer(t, false)

	root := getStatic(ts, "/", nil)
	if root.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", root.Code)
	}
	tag := root.Header().Get("ETag")
	if tag == "" {
		t.Fatal("GET / carried no ETag")
	}
	if named := getStatic(ts, "/index.html", nil).Header().Get("ETag"); named != tag {
		t.Fatalf("ETag differs by name: / = %q, /index.html = %q", tag, named)
	}
}

// A missing asset must 404 rather than fall back to the shell. Answering 200
// with index.html under a JS URL is what lets a service worker cache an HTML
// document as a module and stay broken until its cache is dropped.
func TestStaticMissingAssetIsNotFound(t *testing.T) {
	ts := newStaticServer(t, false)

	for _, path := range []string{
		"/js/gone.js",
		"/css/gone.css",
		"/locales/fr.json",
		"/icons/gone.svg",
		"/fonts/gone.woff2",
		"/gone.webmanifest",
		"/gone.png",
	} {
		res := getStatic(ts, path, nil)
		if res.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, res.Code)
		}
		if ct := res.Header().Get("Content-Type"); ct != "" && ct[:9] == "text/html" {
			t.Errorf("GET %s answered with HTML (%s); the fallback should not reach an asset path", path, ct)
		}
	}
}

// ...while a client-side route still does fall back, which is what a hard
// refresh on a deep link depends on.
func TestStaticRouteStillFallsBackToShell(t *testing.T) {
	ts := newStaticServer(t, false)

	for _, path := range []string{"/trips/abc", "/settings", "/trips/abc/locations/def/edit"} {
		res := getStatic(ts, path, nil)
		if res.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, res.Code)
		}
		if got := res.Body.String(); got != string(staticFS()["index.html"].Data) {
			t.Errorf("GET %s did not serve the shell, got %q", path, got)
		}
	}
}

// Dev serves from a live directory, so a startup hash would be wrong by the
// first edit. NoCache keeps its no-store header and grows no validator.
func TestStaticDevModeKeepsNoStoreAndNoETag(t *testing.T) {
	ts := newStaticServer(t, true)

	res := getStatic(ts, "/js/app.js", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /js/app.js = %d, want 200", res.Code)
	}
	if tag := res.Header().Get("ETag"); tag != "" {
		t.Fatalf("dev mode served an ETag (%q); the files change under the process", tag)
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("dev Cache-Control = %q, want the no-store header", got)
	}
}

// The ETag is content-derived, not version-derived: two files with different
// bytes must not share one, and the same bytes must produce the same tag
// across builds.
func TestAssetETagsFollowContent(t *testing.T) {
	tags := buildAssetETags(staticFS())

	if tags["/js/app.js"] == tags["/sw.js"] {
		t.Fatal("two different files share an ETag")
	}
	if tags["/js/app.js"] != buildAssetETags(staticFS())["/js/app.js"] {
		t.Fatal("the same content hashed to two different ETags")
	}
	changed := staticFS()
	changed["js/app.js"] = &fstest.MapFile{Data: []byte("export const hello = 2;\n")}
	if buildAssetETags(changed)["/js/app.js"] == tags["/js/app.js"] {
		t.Fatal("changed content kept its ETag; a new build would never reach a cached client")
	}
}
