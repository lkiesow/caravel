package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"caravel/internal/buildinfo"
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
var assetDirs = []string{"/js/", "/css/", "/locales/", "/icons/", "/fonts/", "/brand/", "/vendor/"}

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

	if f, err := s.WebFS.Open(r.URL.Path); err != nil {
		if isAssetRequest(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		// SPA fallback: serve index.html for any non-API path so client-side
		// routing (History API) works on a hard refresh/deep link.
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
		s.handleShell(w, r)
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

// swVersionPlaceholder is what web/sw.js carries on disk, and what
// handleServiceWorker substitutes on the way out. It has to be inside a
// string literal in the file, because scripts/check_js.sh parses sw.js with
// node --check and a bare token would not be valid JavaScript.
const swVersionPlaceholder = "__CARAVEL_BUILD__"

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
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
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
		sum := sha256.Sum256([]byte(out))
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])[:16]+`"`)
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(out))
}
