package httpapi

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// isRequestSecure reports whether the request arrived over a secure
// connection, honoring X-Forwarded-Proto so the "Secure" cookie attribute is
// still set correctly when TLS is terminated by a reverse proxy in front of
// the app (a common self-hosted deployment shape). Trusting this header only
// ever *adds* Secure=true, never removes it (r.TLS is checked too), so a
// spoofed header can't downgrade cookie security — at worst a browser
// refuses to store a Secure cookie set over plain HTTP.
func isRequestSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// securityHeaders adds baseline defensive headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// loginLimiter is a small in-memory fixed-window rate limiter keyed by
// client IP, guarding the login endpoint against brute-force credential
// guessing. This is a single-process app (SQLite default, one Go binary),
// so an in-memory limiter is sufficient — no need for a shared store.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{attempts: make(map[string][]time.Time), max: max, window: window}
}

// Allow reports whether another attempt from key is permitted right now,
// and records the attempt if so.
func (l *loginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, now)
	return true
}

// sweep drops keys with no attempts left in the window, bounding memory use
// for a long-running process. Call periodically from a background goroutine.
func (l *loginLimiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-l.window)
	for key, times := range l.attempts {
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(l.attempts, key)
		} else {
			l.attempts[key] = kept
		}
	}
}

func (s *Server) rateLimitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.LoginLimiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}
