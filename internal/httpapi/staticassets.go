package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"caravel/internal/buildinfo"
	"caravel/internal/webbundle"
)

// Static assets ship inside the binary, and until Stage 23 they shipped with
// nothing for a browser to cache against: no Cache-Control, no ETag, and no
// Last-Modified either, because embed.FS reports a zero modtime and
// http.ServeContent omits the header when it sees one. So the only thing
// deciding whether a client picked up a new build was the service worker,
// which is how an upgrade could stay invisible until somebody force-reloaded.
//
// The fix is a strong ETag computed from the content itself. It cannot be
// derived from the build version: a version-keyed tag would change on every
// release whether or not the file did, throwing away every cached asset each
// time. The content hash changes exactly when the bytes do.

// assetETag is the ETag map's value type - the quoted tag, ready to serve.
type assetETagMap map[string]string

// buildAssetETags hashes every file in the asset tree once, at startup.
//
// Eager rather than lazy: the tree is 71 files and 1.3MB, hashing it costs a
// few milliseconds, and doing it up front means the serving path is a map
// lookup with no locking. It is skipped entirely in dev, where the files
// change under the running process and NoCache already tells the browser and
// the service worker not to keep anything.
//
// A file that cannot be read is skipped rather than fatal: it then serves
// exactly as it did before this existed, with no validator. Refusing to start
// over an unreadable asset would be a worse trade.
func buildAssetETags(fsys fs.FS) assetETagMap {
	tags := make(assetETagMap)
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		f, err := fsys.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, f); err != nil {
			return nil
		}
		// 16 hex characters of SHA-256. A validator only has to distinguish
		// this build's copy of a file from another build's, and 64 bits is
		// far past what that needs.
		tags["/"+p] = `"` + hex.EncodeToString(sum.Sum(nil))[:16] + `"`
		return nil
	})
	// No alias for "/" here any more. The shell used to be served straight
	// off the FS under both names and so needed the same validator under
	// both; since the origin is substituted into it, handleShell computes its
	// own tag from the substituted bytes and never consults this map. The
	// /index.html entry stays because assetTreeFingerprint hashes the whole
	// map, and the shell changing is still a reason to rebuild the worker
	// cache.
	return tags
}

// assetDirs are the URL prefixes under which a miss is a *missing asset*
// rather than a client-side route.
var assetDirs = []string{"/v/", "/assets/", "/js/", "/css/", "/locales/", "/icons/", "/fonts/", "/brand/", "/vendor/"}

// assetExts catches the handful of asset files that sit at the root rather
// than in one of the directories above - sw.js, manifest.webmanifest, the
// favicons - so a request for one that no longer exists is answered honestly
// too.
var assetExts = []string{".js", ".css", ".json", ".svg", ".png", ".ico", ".woff2", ".webmanifest", ".map"}

// isAssetRequest reports whether a missing path should 404 instead of falling
// back to the shell.
//
// The SPA fallback exists so that a hard refresh on /trips/abc reaches the
// client-side router, and it has to stay. But it answers *any* unknown path
// with index.html and a 200, which for a stale client asking for a JS module
// this build no longer has is a lie with teeth: the service worker caches the
// HTML under the module URL and the app is then broken until the cache is
// dropped. A route never has a file extension and never lives under these
// prefixes, so the two cases are cleanly separable.
func isAssetRequest(p string) bool {
	for _, dir := range assetDirs {
		if strings.HasPrefix(p, dir) {
			return true
		}
	}
	ext := strings.ToLower(path.Ext(p))
	for _, e := range assetExts {
		if ext == e {
			return true
		}
	}
	return false
}

// serveStatic is the NotFound handler: the static asset tree, the SPA
// fallback, and the caching headers that make an upgrade visible.
func (s *Server) serveStatic(fileServer http.Handler, w http.ResponseWriter, r *http.Request) {
	if s.NoCache {
		// Dev serves from a live directory, so nothing may be kept at all -
		// this is what makes an edit visible on refresh with no rebuild, and
		// web/sw.js honours it too (see isCacheable there).
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}

	if rest, ok := strings.CutPrefix(r.URL.Path, versionedPrefix); ok {
		s.serveVersioned(fileServer, w, r, rest)
		return
	}

	// Built files live in memory, not in the tree, so they are looked up
	// before the miss below would answer them with a 404.
	if s.bundle != nil {
		if f, ok := s.bundle.Files[r.URL.Path]; ok {
			s.serveBundleFile(w, r, f)
			return
		}
	}

	status := http.StatusOK
	if f, err := s.WebFS.Open(r.URL.Path); err != nil {
		if isAssetRequest(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		// SPA fallback: serve index.html for any non-API path so client-side
		// routing (History API) works on a hard refresh/deep link. A path no
		// client route matches still gets the shell -- it renders the app's
		// own not-found page -- but with a 404, so the status does not claim
		// the page exists. Matched escaped, as location.pathname is.
		if !isClientRoute(r.URL.EscapedPath()) {
			status = http.StatusNotFound
		}
		r.URL.Path = "/"
	} else {
		f.Close()
	}

	// The shell is not a plain file either: the origin is substituted into it,
	// the same way the build fingerprint is substituted into sw.js. This is
	// reached for every deep link, since the fallback above rewrites the path
	// to "/" -- the explicit routes only catch "/" and "/index.html" asked for
	// by name.
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		s.handleShell(w, r, status)
		return
	}

	if tag, ok := s.assetETags[r.URL.Path]; ok {
		// no-cache means "keep it, but revalidate before using it" - not
		// "do not store". A repeat load is then a handful of 304s rather than
		// a refetch, and a new build is picked up on the first request
		// instead of whenever someone thinks to force-reload.
		w.Header().Set("ETag", tag)
		w.Header().Set("Cache-Control", "no-cache")
		// http.ServeContent reads the ETag back off the response header to
		// answer If-None-Match, so setting it here is all the 304 handling
		// this needs.
	}

	fileServer.ServeHTTP(w, r)
}

// buildBundle bundles the frontend for serveStatic, or returns nil to have it
// serve the source unbundled.
//
// A failure is logged, not fatal. Refusing to start would turn a broken import
// into an outage; serving the source keeps the app working, only slower, and
// TestRealTreeBundles is what keeps a broken bundle out of a release in the
// first place.
func buildBundle(fsys fs.FS, urls map[string]string) *webbundle.Bundle {
	start := time.Now()
	b, err := webbundle.Build(fsys, urls)
	if err != nil {
		slog.Error("frontend bundle failed; serving the source unbundled", "err", err)
		return nil
	}
	for _, w := range b.Warnings {
		slog.Warn("frontend bundle", "warning", w)
	}
	if len(b.Entries) == 0 {
		// A tree with no entry points: a test fixture. Nothing to say.
		return nil
	}
	slog.Info("frontend bundled", "files", len(b.Files), "duration", time.Since(start).Round(time.Millisecond))
	return b
}

// serveBundleFile serves one built file. Its name carries its content hash, so
// the bytes behind a URL can never change and the browser may keep it for a
// year without asking again -- that is the request-per-module, on every load,
// that Stage 45 exists to remove. The ETag is a courtesy for a client that
// does ask anyway.
func (s *Server) serveBundleFile(w http.ResponseWriter, r *http.Request, f webbundle.File) {
	sum := sha256.Sum256(f.Body)
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])[:16]+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.Body))
}

// pointShellAtBundle rewrites the shell's references to the entry points --
// src="/js/app.js", href="/css/base.css" -- to the built files, and every
// other quoted path that has a versioned URL (the font preload, the
// favicons) to that. The quotes are part of the match so a comment that
// mentions a path in prose is left alone; TestRealShellNamesTheBundleEntries
// holds index.html to that spelling.
//
// Versioned paths are rewritten only alongside the bundle. Without it the
// shell loads the source stylesheet, whose url()s name the plain font paths,
// and a preload of a different URL would be a second download of the same
// font rather than a head start on the first.
func (s *Server) pointShellAtBundle(shell string) string {
	if s.bundle == nil {
		return shell
	}
	for src, built := range s.bundle.Entries {
		shell = strings.ReplaceAll(shell, `"`+src+`"`, `"`+built+`"`)
	}
	for p, u := range s.assetURLs {
		shell = strings.ReplaceAll(shell, `"`+p+`"`, `"`+u+`"`)
	}
	return shell
}

// versionedPrefix starts a versioned URL: /v/<hash>/<path>, where <hash> is
// the hash of every file in <path>'s directory.
//
// The bundle is content-hashed by esbuild, but the files the app loads by
// path -- MapLibre, the locales, the map styles, the fonts, the icon sprite --
// cannot be renamed: MapLibre imports its siblings by literal name and derives
// its worker URL from its own location. Putting the version in a directory
// segment instead leaves every name alone, and hashing per *directory* rather
// than per file is what keeps MapLibre's relative URLs inside one prefix: its
// entry, shared chunk and worker all resolve under the same /v/<hash>/.
const versionedPrefix = "/v/"

// versionedDirs are the directories whose files get a versioned URL: the
// static files on the load path. The source modules are not here -- the
// bundle replaces them -- and neither is the root, whose sw.js and manifest
// must keep the names browsers know them by.
var versionedDirs = []string{"/brand", "/fonts", "/icons", "/locales", "/js/vendor/maplibre", "/js/vendor/map-styles"}

// buildAssetVersions derives, from the per-file ETags, a hash per directory
// and the versioned URL of each file in versionedDirs. A directory's hash
// changes when any file directly in it does, which over-invalidates a little
// (a new favicon re-fetches the sprite) and never under-invalidates.
func buildAssetVersions(tags assetETagMap) (dirs, urls map[string]string) {
	byDir := map[string][]string{}
	for p := range tags {
		d := path.Dir(p)
		byDir[d] = append(byDir[d], p)
	}
	dirs = make(map[string]string, len(byDir))
	for d, files := range byDir {
		sort.Strings(files)
		sum := sha256.New()
		for _, p := range files {
			_, _ = io.WriteString(sum, path.Base(p)+"="+tags[p]+"\n")
		}
		dirs[d] = hex.EncodeToString(sum.Sum(nil))[:12]
	}
	urls = map[string]string{}
	for _, d := range versionedDirs {
		for _, p := range byDir[d] {
			// Licences and READMEs sit next to the vendored files but are never
			// loaded by the app; leaving them out keeps the table in the bundle
			// to what it is for.
			if ext := path.Ext(p); ext == ".md" || ext == ".txt" {
				continue
			}
			urls[p] = versionedPrefix + dirs[d] + p
		}
	}
	return dirs, urls
}

// assetURL is the server's own assetURL(): the versioned URL of a static
// file, or the path unchanged when it has none (dev, or not a versioned file).
func (s *Server) assetURL(p string) string {
	if u, ok := s.assetURLs[p]; ok {
		return u
	}
	return p
}

// serveVersioned serves /v/<hash>/<path>.
//
// When <hash> is the current version of <path>'s directory the bytes cannot
// change under that URL, so it is immutable. When it is not -- a tab opened
// before a deploy, lazily loading the map afterwards -- the file is served
// anyway, but revalidating: a 404 would break the old tab's map, and an
// immutable answer would pin the new bytes under the old URL.
func (s *Server) serveVersioned(fileServer http.Handler, w http.ResponseWriter, r *http.Request, rest string) {
	hash, p, ok := strings.Cut(rest, "/")
	p = "/" + p
	tag, known := s.assetETags[p]
	if !ok || !known {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("ETag", tag)
	if hash == s.assetDirVersions[path.Dir(p)] {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	fileServer.ServeHTTP(w, r2)
}

// swVersionPlaceholder is what web/sw.js carries on disk, and what
// handleServiceWorker substitutes on the way out. It has to be inside a
// string literal in the file, because scripts/check_js.sh parses sw.js with
// node --check and a bare token would not be valid JavaScript.
const swVersionPlaceholder = "__CARAVEL_BUILD__"

// swAssetURLsPlaceholder is the worker's table of built URLs, quotes included:
// the whole string literal is replaced by another one holding the JSON, so
// whatever the table contains, the file stays a valid script.
const swAssetURLsPlaceholder = `"__CARAVEL_ASSET_URLS__"`

// builtURLs is the table web/sw.js precaches through: each entry point to its
// bundle and each versioned file to its URL. Empty without a bundle, for the
// reason pointShellAtBundle gives -- the source stylesheet loads plain paths.
func (s *Server) builtURLs() map[string]string {
	table := map[string]string{}
	if s.bundle == nil {
		return table
	}
	for p, u := range s.assetURLs {
		table[p] = u
	}
	for src, built := range s.bundle.Entries {
		table[src] = built
	}
	return table
}

// assetTreeFingerprint hashes the whole asset tree down to one short string:
// the ETags of every file, in path order, hashed again.
//
// This is what keys the service worker's cache, and it is a better key than
// the build version for the reason the version looked attractive: the version
// changes on every commit, including the many that touch only Go, and each
// change throws away every asset every client has cached. The fingerprint
// changes exactly when a served file does, which is precisely the condition
// under which a client must drop what it has.
func assetTreeFingerprint(tags assetETagMap) string {
	paths := make([]string, 0, len(tags))
	for p := range tags {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, p := range paths {
		_, _ = io.WriteString(sum, p)
		_, _ = io.WriteString(sum, "=")
		_, _ = io.WriteString(sum, tags[p])
		_, _ = io.WriteString(sum, "\n")
	}
	return hex.EncodeToString(sum.Sum(nil))[:12]
}

// serviceWorkerVersion is the cache key the worker runs with.
//
// Both halves are here on purpose. The fingerprint is what makes it correct;
// the build version is what makes a cache legible in DevTools, where
// "caravel-shell-a1b2c3d4e5f6" alone tells nobody which deploy it belongs to.
// In dev there is no fingerprint - the ETag map is not built, because the
// files change under the process - so the version stands alone, and nothing
// is cached there anyway.
func (s *Server) serviceWorkerVersion() string {
	if len(s.assetETags) == 0 {
		return buildinfo.Version
	}
	return buildinfo.Version + "-" + assetTreeFingerprint(s.assetETags)
}

// handleServiceWorker serves /sw.js with its cache key substituted in.
//
// The point is not that the worker can read a version; it is that the *bytes
// of this file change when a deploy changes the assets*. That is the only
// thing a browser watches to decide the worker has been updated, and it is
// what makes install-and-purge happen on its own instead of waiting for
// somebody to remember to edit a constant. CACHE_VERSION had been edited four
// times in the project's life, against a web/js directory that changes
// constantly.
func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	f, err := s.WebFS.Open("/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "could not read service worker", http.StatusInternalServerError)
		return
	}
	out := strings.ReplaceAll(string(body), swVersionPlaceholder, s.serviceWorkerVersion())
	table, err := json.Marshal(s.builtURLs())
	if err == nil {
		// Marshalled twice: once to JSON, then that JSON to a JSON *string*,
		// which is also a valid JavaScript string literal for JSON.parse.
		table, err = json.Marshal(string(table))
	}
	if err != nil {
		http.Error(w, "could not build service worker", http.StatusInternalServerError)
		return
	}
	out = strings.ReplaceAll(out, swAssetURLsPlaceholder, string(table))

	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	if s.NoCache {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	} else {
		// The worker script is the one file that must never be served stale
		// from the HTTP cache: it is what discovers every other update. A
		// browser caps this at 24h of its own accord; no-cache plus a
		// validator makes it a 304 rather than a refetch when nothing moved.
		sum := sha256.Sum256([]byte(out))
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])[:16]+`"`)
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "sw.js", time.Time{}, strings.NewReader(out))
}

// shellOriginPlaceholder is what web/index.html carries on disk wherever it
// needs the absolute origin, and what handleShell substitutes on the way out.
const shellOriginPlaceholder = "__CARAVEL_ORIGIN__"

// requestOrigin reports the absolute origin -- scheme and host, no trailing
// slash -- that this request reached the instance under.
//
// It exists because the Open Graph tags in the shell need an absolute image
// URL and a self-hosted server does not know its own public address. A
// relative og:image is not a workable substitute: Facebook, LinkedIn and
// Discord drop the image rather than resolving it against the page.
//
// CARAVEL_BASE_URL wins when set. Otherwise the Host header decides, with the
// scheme from isRequestSecure, which already handles both real TLS and the
// X-Forwarded-Proto a terminating proxy sends. Trusting Host here is a
// deliberately narrow decision: the only thing it can affect is which
// hostname a scraper is pointed at for a static PNG, and a request carrying a
// forged Host is one whose response goes back to whoever forged it.
func (s *Server) requestOrigin(r *http.Request) string {
	if s.BaseURL != "" {
		return s.BaseURL
	}
	if r.Host == "" {
		// No host to build one from. Substituting nothing leaves the tags
		// holding root-relative paths, which is what they were before this
		// existed: degraded, but not malformed.
		return ""
	}
	scheme := "http"
	if isRequestSecure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// handleShell serves web/index.html with the request origin substituted in.
//
// The ETag cannot be the file's content hash any more, which is why this does
// not go through serveStatic's map. The bytes now differ per origin, so a
// cache in front of two hostnames -- or one instance reached over both http
// and https -- could otherwise hand one host a card pointing at the other.
// The tag is computed from the substituted output, and Vary names the headers
// that decided it.
//
// A status other than 200 is the not-found shell: the same body, sent as is.
// It skips the ETag and http.ServeContent, whose conditional and range
// handling would answer 304 or 206 in place of the 404.
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request, status int) {
	f, err := s.WebFS.Open("/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "could not read application shell", http.StatusInternalServerError)
		return
	}
	out := strings.ReplaceAll(string(body), shellOriginPlaceholder, s.requestOrigin(r))
	out = s.pointShellAtBundle(out)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.BaseURL == "" {
		// Only worth saying when the request actually decided the answer. With
		// a pinned base URL every response is identical and naming these
		// headers would fragment caches for nothing.
		w.Header().Set("Vary", "Host, X-Forwarded-Proto")
	}
	if s.NoCache {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if status != http.StatusOK {
		w.Header().Set("Content-Length", strconv.Itoa(len(out)))
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, out)
		}
		return
	}
	if !s.NoCache {
		sum := sha256.Sum256([]byte(out))
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])[:16]+`"`)
	}
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(out))
}
