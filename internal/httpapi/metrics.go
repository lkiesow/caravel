package httpapi

import (
	"crypto/subtle"
	"net/http"
)

// handleMetrics serves the Prometheus scrape endpoint.
//
// It sits outside session auth -- a scraper has no cookie -- and on the main
// port, so it is reachable through whatever publishes the app. The bearer
// token is therefore the whole of its protection; config refuses a short one.
// Compared in constant time over the full header, so neither the token nor its
// length leaks through how long a wrong guess takes.
//
// Off (no token configured) answers a plain 404, the same as a path that does
// not exist, rather than 401: an instance that is not exporting metrics has
// nothing to authenticate against.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.Metrics == nil || s.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	want := "Bearer " + s.MetricsToken
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.Metrics.Handler().ServeHTTP(w, r)
}
