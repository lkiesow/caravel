package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// staticFS is a stand-in for the embedded web tree: a shell, an asset in one
// of the asset directories, and one at the root.
func staticFS() fstest.MapFS {
	return fstest.MapFS{
		// The placeholder is what handleShell substitutes; the fixture carries
		// it in the same shape web/index.html does, twice, because the real
		// shell uses it for both og:url and og:image.
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html><title>Caravel</title>` +
			`<meta property="og:url" content="__CARAVEL_ORIGIN__/" />` +
			`<meta property="og:image" content="__CARAVEL_ORIGIN__/brand/og-card.png" />`)},
		"js/app.js": &fstest.MapFile{Data: []byte("export const hello = 1;\n")},
		"sw.js":     &fstest.MapFile{Data: []byte("// service worker\n")},
		// A second module and a nested directory, so a directory listing would
		// actually have something to leak and is recognisable when it happens.
		"js/components/dialog.js": &fstest.MapFile{Data: []byte("export const dialog = 1;\n")},
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

// A directory under the asset tree must never be answered with its contents.
//
// The Stage 24 backlog carried this as a bug -- "Open succeeds for a directory,
// so /js/ reaches http.FileServer and gets an index of every module" -- and it
// is not one: http.FS trims the leading slash, leaving "js/", which
// fs.ValidPath rejects, so Open fails and the asset branch already 404s. That
// was verified against the real web/ tree, not just the map below. This test
// exists to keep it that way, because the protection is a side effect of path
// validation rather than anything deliberate, and nothing else pins it.
func TestStaticDirectoryIsNotListed(t *testing.T) {
	ts := newStaticServer(t, false)

	for _, path := range []string{"/js/", "/js/components/", "/icons/"} {
		res := getStatic(ts, path, nil)
		if res.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, res.Code)
		}
	}

	// Without the trailing slash the path does not match an asset directory,
	// so it is an ordinary unknown route and takes the SPA fallback. That is
	// pre-existing behaviour and fine; what matters is that neither shape ever
	// answers with the contents of the directory.
	for _, path := range []string{"/js/", "/js", "/js/components/", "/icons/"} {
		body := getStatic(ts, path, nil).Body.String()
		if strings.Contains(body, "app.js") || strings.Contains(body, "dialog.js") {
			t.Errorf("GET %s listed the directory contents:\n%s", path, body)
		}
	}
}

// The root is a directory too, and must keep working: it is not an asset
// request, so it takes the SPA fallback and resolves to index.html.
func TestStaticRootStillServesTheShell(t *testing.T) {
	ts := newStaticServer(t, false)

	res := getStatic(ts, "/", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", res.Code)
	}
	if !strings.Contains(res.Body.String(), "<title>Caravel</title>") {
		t.Errorf("GET / did not serve the shell:\n%s", res.Body.String())
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
		// Compared by a marker rather than byte-for-byte: the shell is no
		// longer served verbatim, since handleShell substitutes the origin
		// into it. TestShellDeepLinkGetsOrigin covers that substitution.
		if got := res.Body.String(); !strings.Contains(got, "<title>Caravel</title>") {
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

// swFS is staticFS plus a worker carrying the placeholder, which is what the
// real web/sw.js looks like on disk.
func swFS() fstest.MapFS {
	fsys := staticFS()
	fsys["sw.js"] = &fstest.MapFile{
		Data: []byte(`const CACHE_VERSION = "caravel-shell-` + swVersionPlaceholder + `";` + "\n"),
	}
	return fsys
}

func newSWServer(t *testing.T, noCache bool) *testServer {
	t.Helper()
	return newTestServerWith(t, nil, func(o *Options) {
		o.WebFS = swFS()
		o.NoCache = noCache
	})
}

// The whole mechanism: the worker the browser receives must never still carry
// the placeholder, because a constant that does not change is exactly the
// manual step this replaces.
func TestServiceWorkerVersionIsSubstituted(t *testing.T) {
	ts := newSWServer(t, false)

	res := getStatic(ts, "/sw.js", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /sw.js = %d, want 200", res.Code)
	}
	body := res.Body.String()
	if strings.Contains(body, swVersionPlaceholder) {
		t.Fatalf("the placeholder reached the browser: %q", body)
	}
	if want := ts.Server.serviceWorkerVersion(); !strings.Contains(body, want) {
		t.Fatalf("body %q does not carry the version %q", body, want)
	}
	if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("Content-Type = %q, want text/javascript", ct)
	}
	// The worker script is what discovers every other update, so it must not
	// be served stale from the HTTP cache either.
	if got := res.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if res.Header().Get("ETag") == "" {
		t.Fatal("/sw.js carried no ETag")
	}
}

// The key must change when a served asset changes, and hold still when
// nothing does — the two halves of "a deploy invalidates the cache, and only
// a deploy does".
func TestServiceWorkerVersionTracksTheAssetTree(t *testing.T) {
	base := swFS()
	ts := newTestServerWith(t, nil, func(o *Options) { o.WebFS = base })
	first := ts.Server.serviceWorkerVersion()

	same := newTestServerWith(t, nil, func(o *Options) { o.WebFS = swFS() })
	if got := same.Server.serviceWorkerVersion(); got != first {
		t.Fatalf("an unchanged tree produced two keys: %q then %q", first, got)
	}

	changed := swFS()
	changed["js/app.js"] = &fstest.MapFile{Data: []byte("export const hello = 99;\n")}
	moved := newTestServerWith(t, nil, func(o *Options) { o.WebFS = changed })
	if got := moved.Server.serviceWorkerVersion(); got == first {
		t.Fatalf("a changed asset kept the cache key %q; clients would never drop the old files", got)
	}
}

// In dev there is no fingerprint, because the ETag map is not built: the
// files change under the running process. The worker still gets a key and
// still must not carry the placeholder.
func TestServiceWorkerInDevMode(t *testing.T) {
	ts := newSWServer(t, true)

	res := getStatic(ts, "/sw.js", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /sw.js = %d, want 200", res.Code)
	}
	if strings.Contains(res.Body.String(), swVersionPlaceholder) {
		t.Fatal("dev mode served the placeholder unsubstituted")
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("dev Cache-Control = %q, want the no-store header", got)
	}
}

// The real web/sw.js has to carry the placeholder the handler looks for. A
// rename on either side would otherwise ship a worker whose cache key is a
// literal that never changes, silently restoring the bug.
func TestRealServiceWorkerCarriesThePlaceholder(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "sw.js"))
	if err != nil {
		t.Fatalf("read web/sw.js: %v", err)
	}
	if !strings.Contains(string(body), swVersionPlaceholder) {
		t.Fatalf("web/sw.js does not contain %s; the substitution would be a no-op", swVersionPlaceholder)
	}
}

// getShell asks for the shell under a given Host, which is what decides the
// origin when no base URL is pinned. httptest.NewRequest fills Host from the
// target, so it has to be overridden rather than set as a header.
func getShell(ts *testServer, path, host string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if host != "" {
		r.Host = host
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	ts.ServeHTTP(w, r)
	return w
}

// The social tags need an absolute URL or the strict scrapers drop the image,
// and the instance only learns its own address from the request.
func TestShellOriginFromRequest(t *testing.T) {
	ts := newStaticServer(t, false)

	for _, tc := range []struct {
		name    string
		host    string
		headers map[string]string
		want    string
	}{
		{name: "plain http", host: "caravel.example", want: "http://caravel.example"},
		{
			name:    "TLS terminated by a proxy",
			host:    "caravel.example",
			headers: map[string]string{"X-Forwarded-Proto": "https"},
			want:    "https://caravel.example",
		},
		{name: "a port is part of the origin", host: "caravel.example:8080", want: "http://caravel.example:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := getShell(ts, "/", tc.host, tc.headers).Body.String()
			for _, want := range []string{
				`content="` + tc.want + `/"`,
				`content="` + tc.want + `/brand/og-card.png"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("shell missing %s\ngot: %s", want, body)
				}
			}
			if strings.Contains(body, shellOriginPlaceholder) {
				t.Errorf("placeholder survived into the response: %s", body)
			}
		})
	}
}

// A pinned base URL is for the deployment where something in front rewrites
// Host, so it has to beat whatever the request says.
func TestShellOriginBaseURLWins(t *testing.T) {
	ts := newTestServerWith(t, nil, func(o *Options) {
		o.WebFS = staticFS()
		o.BaseURL = "https://caravel.example"
	})

	body := getShell(ts, "/", "internal.lan:8080", map[string]string{"X-Forwarded-Proto": "http"}).Body.String()
	if !strings.Contains(body, `content="https://caravel.example/brand/og-card.png"`) {
		t.Errorf("base URL did not win over the request Host\ngot: %s", body)
	}
	// Nothing about the response depends on the request, so it must not claim
	// otherwise -- that would fragment every cache in front of it for nothing.
	if v := getShell(ts, "/", "internal.lan:8080", nil).Header().Get("Vary"); v != "" {
		t.Errorf("Vary = %q with a pinned base URL, want none", v)
	}
}

// The whole point of computing the ETag from the substituted bytes: a shared
// cache fronting two hostnames must not be able to hand one host the other
// host card.
func TestShellETagVariesByOrigin(t *testing.T) {
	ts := newStaticServer(t, false)

	a := getShell(ts, "/", "one.example", nil)
	b := getShell(ts, "/", "two.example", nil)
	if a.Header().Get("ETag") == "" {
		t.Fatal("shell carried no ETag")
	}
	if a.Header().Get("ETag") == b.Header().Get("ETag") {
		t.Errorf("same ETag %q for two origins", a.Header().Get("ETag"))
	}
	if v := a.Header().Get("Vary"); !strings.Contains(v, "Host") {
		t.Errorf("Vary = %q, want it to name Host", v)
	}
	// A repeat request under the same origin still has to revalidate cheaply.
	again := getShell(ts, "/", "one.example", map[string]string{"If-None-Match": a.Header().Get("ETag")})
	if again.Code != http.StatusNotModified {
		t.Errorf("revalidation = %d, want 304", again.Code)
	}
}

// A deep link falls through to the shell, and that copy needs the tags
// substituted too -- it is a perfectly ordinary URL for someone to share.
func TestShellDeepLinkGetsOrigin(t *testing.T) {
	ts := newStaticServer(t, false)

	body := getShell(ts, "/trips/abc", "caravel.example", nil).Body.String()
	if !strings.Contains(body, `content="http://caravel.example/brand/og-card.png"`) {
		t.Errorf("deep-link shell was not substituted\ngot: %s", body)
	}
}

// bundleFS is staticFS with both entry points and a shell that loads them, so
// the server has something to bundle. Kept separate so the tests above keep
// exercising a tree with no bundle at all.
func bundleFS() fstest.MapFS {
	fsys := staticFS()
	fsys["index.html"] = &fstest.MapFile{Data: []byte(`<!doctype html><title>Caravel</title>` +
		`<link rel="stylesheet" href="/css/base.css" />` +
		`<!-- /js/app.js is named here in prose, and must stay as written -->` +
		`<script type="module" src="/js/app.js"></script>`)}
	fsys["js/app.js"] = &fstest.MapFile{Data: []byte(`import { dialog } from "./components/dialog.js"; console.log(dialog);`)}
	fsys["css/base.css"] = &fstest.MapFile{Data: []byte(`body { color: red; }`)}
	return fsys
}

func newBundleServer(t *testing.T, noCache bool) *testServer {
	t.Helper()
	return newTestServerWith(t, nil, func(o *Options) {
		o.WebFS = bundleFS()
		o.NoCache = noCache
	})
}

var bundleRef = regexp.MustCompile(`"(/assets/[a-z]+-[A-Z0-9]+\.(?:js|css))"`)

// The shell points at the built files, and they are served immutable: the
// whole point is that a reload asks about nothing but the shell.
func TestShellLoadsTheBundle(t *testing.T) {
	ts := newBundleServer(t, false)

	shell := getStatic(ts, "/", nil).Body.String()
	refs := bundleRef.FindAllStringSubmatch(shell, -1)
	if len(refs) != 2 {
		t.Fatalf("shell names %d bundle files, want 2:\n%s", len(refs), shell)
	}
	if strings.Contains(shell, `"/js/app.js"`) || strings.Contains(shell, `"/css/base.css"`) {
		t.Errorf("shell still loads the source:\n%s", shell)
	}
	if !strings.Contains(shell, "<!-- /js/app.js is named here in prose") {
		t.Error("the substitution rewrote a path mentioned in a comment")
	}

	for _, ref := range refs {
		res := getStatic(ts, ref[1], nil)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", ref[1], res.Code)
		}
		if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("%s Cache-Control = %q, want immutable", ref[1], got)
		}
		want := "text/javascript; charset=utf-8"
		if strings.HasSuffix(ref[1], ".css") {
			want = "text/css; charset=utf-8"
		}
		if got := res.Header().Get("Content-Type"); got != want {
			t.Errorf("%s Content-Type = %q, want %q", ref[1], got, want)
		}
		if res.Header().Get("ETag") == "" {
			t.Errorf("%s has no ETag", ref[1])
		}
		if m := getStatic(ts, ref[1]+".map", nil); m.Code != http.StatusOK {
			t.Errorf("GET %s.map = %d, want 200", ref[1], m.Code)
		}
	}
}

// A built file this server does not have -- an old tab asking for the
// previous deploy's bundle -- is a 404, not the shell: the service worker
// would otherwise cache HTML under a script URL (see isAssetRequest).
func TestUnknownBundleFileIsNotFound(t *testing.T) {
	ts := newBundleServer(t, false)
	if res := getStatic(ts, "/assets/app-NOTAHASH.js", nil); res.Code != http.StatusNotFound {
		t.Fatalf("GET an unknown bundle file = %d, want 404", res.Code)
	}
}

// The source is still served as it was, for a tab that loaded before the
// deploy, with the revalidating headers rather than the bundle's.
func TestSourceStillServedBesideTheBundle(t *testing.T) {
	ts := newBundleServer(t, false)
	res := getStatic(ts, "/js/app.js", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /js/app.js = %d, want 200", res.Code)
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("source Cache-Control = %q, want no-cache", got)
	}
}

// Dev serves the source live, so it must not bundle: a snapshot taken at
// startup would hide every edit after it.
func TestDevModeDoesNotBundle(t *testing.T) {
	ts := newBundleServer(t, true)
	shell := getStatic(ts, "/", nil).Body.String()
	if !strings.Contains(shell, `src="/js/app.js"`) {
		t.Errorf("dev shell does not load the source:\n%s", shell)
	}
	if bundleRef.MatchString(shell) {
		t.Errorf("dev shell names a bundle:\n%s", shell)
	}
}

// pointShellAtBundle matches the entry paths *with their quotes*. If
// index.html ever spells them differently -- single quotes, a query string --
// the substitution silently does nothing and production loads the source
// again. This holds the real shell to the spelling.
func TestRealShellNamesTheBundleEntries(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "index.html"))
	if err != nil {
		t.Fatalf("read web/index.html: %v", err)
	}
	for _, want := range []string{`src="/js/app.js"`, `href="/css/base.css"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("web/index.html does not contain %s; the bundle substitution would be a no-op", want)
		}
	}
}

// versionedFS is bundleFS with files in versioned directories, a stylesheet
// that names one by url(), and a shell that preloads it.
func versionedFS() fstest.MapFS {
	fsys := bundleFS()
	fsys["index.html"] = &fstest.MapFile{Data: []byte(`<!doctype html><title>Caravel</title>` +
		`<link rel="preload" href="/fonts/x.woff2" as="font" />` +
		`<link rel="stylesheet" href="/css/base.css" />` +
		`<script type="module" src="/js/app.js"></script>`)}
	fsys["css/base.css"] = &fstest.MapFile{Data: []byte(`@font-face { font-family: X; src: url("/fonts/x.woff2"); }`)}
	fsys["fonts/x.woff2"] = &fstest.MapFile{Data: []byte("font bytes")}
	fsys["locales/en.json"] = &fstest.MapFile{Data: []byte(`{"hello":"Hello"}`)}
	return fsys
}

var versionedFont = regexp.MustCompile(`/v/[0-9a-f]{12}/fonts/x\.woff2`)

// The preload in the shell and the url() in the built stylesheet name the
// same versioned URL -- if they differed the font would download twice -- and
// that URL is immutable.
func TestVersionedAssetIsImmutable(t *testing.T) {
	ts := newTestServerWith(t, nil, func(o *Options) { o.WebFS = versionedFS() })

	shell := getStatic(ts, "/", nil).Body.String()
	preload := versionedFont.FindString(shell)
	if preload == "" {
		t.Fatalf("shell does not preload a versioned font:\n%s", shell)
	}
	css := bundleRef.FindAllStringSubmatch(shell, -1)
	var cssURL string
	for _, m := range css {
		if strings.HasSuffix(m[1], ".css") {
			cssURL = m[1]
		}
	}
	if got := versionedFont.FindString(getStatic(ts, cssURL, nil).Body.String()); got != preload {
		t.Errorf("stylesheet loads %q but the shell preloads %q", got, preload)
	}

	res := getStatic(ts, preload, nil)
	if res.Code != http.StatusOK || res.Body.String() != "font bytes" {
		t.Fatalf("GET %s = %d %q", preload, res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("current version Cache-Control = %q, want immutable", got)
	}
	if got := res.Header().Get("Content-Type"); got != "font/woff2" {
		t.Errorf("Content-Type = %q, want font/woff2", got)
	}
}

// A version that is not the current one is what a tab opened before a deploy
// asks for. It gets the current file -- a 404 would break that tab -- but
// revalidating, so the new bytes are never pinned under the old URL.
func TestStaleVersionIsServedRevalidating(t *testing.T) {
	ts := newTestServerWith(t, nil, func(o *Options) { o.WebFS = versionedFS() })

	res := getStatic(ts, "/v/000000000000/fonts/x.woff2", nil)
	if res.Code != http.StatusOK || res.Body.String() != "font bytes" {
		t.Fatalf("stale version = %d %q, want the current file", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("stale version Cache-Control = %q, want no-cache", got)
	}

	for _, p := range []string{"/v/000000000000/fonts/missing.woff2", "/v/", "/v/nohash"} {
		if res := getStatic(ts, p, nil); res.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, res.Code)
		}
	}
}

// A directory's version follows the files directly in it and nothing else.
func TestAssetVersionsFollowTheirDirectory(t *testing.T) {
	tags := assetETagMap{"/fonts/a.woff2": `"1"`, "/locales/en.json": `"2"`, "/js/app.js": `"3"`}
	dirs, urls := buildAssetVersions(tags)
	tags["/fonts/a.woff2"] = `"changed"`
	dirs2, _ := buildAssetVersions(tags)

	if dirs["/fonts"] == dirs2["/fonts"] {
		t.Error("changing a font kept the /fonts version")
	}
	if dirs["/locales"] != dirs2["/locales"] {
		t.Error("changing a font changed the /locales version")
	}
	if urls["/fonts/a.woff2"] != "/v/"+dirs["/fonts"]+"/fonts/a.woff2" {
		t.Errorf("font URL = %q", urls["/fonts/a.woff2"])
	}
	// Source modules are the bundle's business, not this table's.
	if _, ok := urls["/js/app.js"]; ok {
		t.Error("a source module was given a versioned URL")
	}
}

// Every path the real source passes to assetURL() has to be in the table, or
// it loads unversioned and revalidates on every load -- silently, since the
// fallback is to pass the path through. A literal is checked directly; a
// template (`/locales/${locale}.json`) by its fixed prefix.
func TestRealAssetURLCallSitesAreVersioned(t *testing.T) {
	web := os.DirFS(filepath.Join("..", "..", "web"))
	_, urls := buildAssetVersions(buildAssetETags(web))

	call := regexp.MustCompile("assetURL\\(([\"`])(/[^\"`$]*)(\\$\\{)?")
	found := 0
	err := fs.WalkDir(web, "js", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(p, "/vendor/") || !strings.HasSuffix(p, ".js") {
			return err
		}
		src, err := fs.ReadFile(web, p)
		if err != nil {
			return err
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			found++
			if m[3] == "" {
				if _, ok := urls[m[2]]; !ok {
					t.Errorf("%s: assetURL(%q) has no versioned URL", p, m[2])
				}
				continue
			}
			matched := false
			for k := range urls {
				matched = matched || strings.HasPrefix(k, m[2])
			}
			if !matched {
				t.Errorf("%s: assetURL(`%s${...}`) matches no versioned URL", p, m[2])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found < 5 {
		t.Fatalf("found only %d assetURL() calls; is the pattern still right?", found)
	}
}

var swTable = regexp.MustCompile(`JSON\.parse\(("(?:[^"\\]|\\.)*")\)`)

// swBuiltURLs fetches /sw.js and decodes the table substituted into it, the
// way the worker's JSON.parse will.
func swBuiltURLs(t *testing.T, ts *testServer) map[string]string {
	t.Helper()
	body := getStatic(ts, "/sw.js", nil).Body.String()
	m := swTable.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no JSON.parse(...) literal in the served worker:\n%s", body)
	}
	var raw string
	if err := json.Unmarshal([]byte(m[1]), &raw); err != nil {
		t.Fatalf("the substituted literal is not a string: %v\n%s", err, m[1])
	}
	table := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &table); err != nil {
		t.Fatalf("the string does not hold a JSON table: %v\n%s", err, raw)
	}
	return table
}

func workerFS() fstest.MapFS {
	fsys := versionedFS()
	fsys["sw.js"] = &fstest.MapFile{Data: []byte(`const BUILT_URLS = JSON.parse("__CARAVEL_ASSET_URLS__");`)}
	return fsys
}

// The worker precaches what the pages of this build load: the bundle for each
// entry point and the versioned URL of each static file.
func TestServiceWorkerCarriesTheBuiltURLs(t *testing.T) {
	ts := newTestServerWith(t, nil, func(o *Options) { o.WebFS = workerFS() })
	table := swBuiltURLs(t, ts)

	if !bundleRef.MatchString(`"` + table["/js/app.js"] + `"`) {
		t.Errorf("/js/app.js -> %q, want its bundle", table["/js/app.js"])
	}
	if !bundleRef.MatchString(`"` + table["/css/base.css"] + `"`) {
		t.Errorf("/css/base.css -> %q, want its bundle", table["/css/base.css"])
	}
	if !versionedFont.MatchString(table["/fonts/x.woff2"]) {
		t.Errorf("/fonts/x.woff2 -> %q, want its versioned URL", table["/fonts/x.woff2"])
	}
	// The same URLs the shell names, or the precache warms files no page asks for.
	shell := getStatic(ts, "/", nil).Body.String()
	for _, p := range []string{"/js/app.js", "/css/base.css", "/fonts/x.woff2"} {
		if !strings.Contains(shell, `"`+table[p]+`"`) {
			t.Errorf("the worker precaches %s but the shell does not load it", table[p])
		}
	}
}

// Dev bundles nothing, so the table is empty and the worker keeps every path.
func TestServiceWorkerTableIsEmptyInDev(t *testing.T) {
	ts := newTestServerWith(t, nil, func(o *Options) {
		o.WebFS = workerFS()
		o.NoCache = true
	})
	if table := swBuiltURLs(t, ts); len(table) != 0 {
		t.Errorf("dev worker table = %v, want empty", table)
	}
}

func TestRealServiceWorkerCarriesTheURLTablePlaceholder(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "sw.js"))
	if err != nil {
		t.Fatalf("read web/sw.js: %v", err)
	}
	if !strings.Contains(string(body), "JSON.parse("+swAssetURLsPlaceholder+")") {
		t.Fatalf("web/sw.js does not parse %s; the precache would never see the bundle", swAssetURLsPlaceholder)
	}
}
