package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
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
	// The shell is reached as "/" far more often than as "/index.html" - it is
	// what every deep link falls back to - and the file server resolves the
	// directory to the same bytes, so it needs the same validator.
	if tag, ok := tags["/index.html"]; ok {
		tags["/"] = tag
	}
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
