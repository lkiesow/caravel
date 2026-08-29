package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"caravel/internal/config"
)

func prefixes(t *testing.T, raw ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(raw))
	for _, r := range raw {
		p, err := netip.ParsePrefix(r)
		if err != nil {
			t.Fatalf("parse prefix %q: %v", r, err)
		}
		out = append(out, p)
	}
	return out
}

// TestClientIPResolution covers the whole decision table. The case that keeps
// the private-range default safe is "public peer, header present": a caller on
// the open internet must not get to name its own address.
func TestClientIPResolution(t *testing.T) {
	tests := []struct {
		name          string
		trusted       []netip.Prefix
		remoteAddr    string
		forwardedFor  string
		wantIP        string
		wantForwarded bool
	}{
		{
			name:         "public peer is not believed even with a header",
			trusted:      config.DefaultTrustedProxies,
			remoteAddr:   "203.0.113.7:5555",
			forwardedFor: "10.1.2.3",
			wantIP:       "203.0.113.7",
		},
		{
			name:          "trusted peer yields the forwarded client",
			trusted:       config.DefaultTrustedProxies,
			remoteAddr:    "127.0.0.1:5555",
			forwardedFor:  "203.0.113.9",
			wantIP:        "203.0.113.9",
			wantForwarded: true,
		},
		{
			name:          "a chain of trusted proxies yields the leftmost untrusted hop",
			trusted:       config.DefaultTrustedProxies,
			remoteAddr:    "10.0.0.1:5555",
			forwardedFor:  "203.0.113.9, 192.168.1.4, 10.0.0.2",
			wantIP:        "203.0.113.9",
			wantForwarded: true,
		},
		{
			name:         "no trusted proxies means the header is ignored",
			trusted:      nil,
			remoteAddr:   "127.0.0.1:5555",
			forwardedFor: "203.0.113.9",
			wantIP:       "127.0.0.1",
		},
		{
			name:       "no header falls back to the peer",
			trusted:    config.DefaultTrustedProxies,
			remoteAddr: "127.0.0.1:5555",
			wantIP:     "127.0.0.1",
		},
		{
			name:          "IPv6 peer with a port",
			trusted:       config.DefaultTrustedProxies,
			remoteAddr:    "[::1]:5555",
			forwardedFor:  "2001:db8::42",
			wantIP:        "2001:db8::42",
			wantForwarded: true,
		},
		{
			// The old LastIndex(":") split truncated this to "[::1".
			name:       "bare IPv6 peer without a port",
			trusted:    config.DefaultTrustedProxies,
			remoteAddr: "::1",
			wantIP:     "::1",
		},
		{
			name:         "a malformed hop stops the walk",
			trusted:      config.DefaultTrustedProxies,
			remoteAddr:   "10.0.0.1:5555",
			forwardedFor: "203.0.113.9, not-an-address",
			wantIP:       "10.0.0.1",
		},
		{
			name:         "an all-trusted chain falls back to the peer",
			trusted:      config.DefaultTrustedProxies,
			remoteAddr:   "10.0.0.1:5555",
			forwardedFor: "10.0.0.2, 10.0.0.3",
			wantIP:       "10.0.0.1",
		},
		{
			name:          "a narrowed set trusts only what it names",
			trusted:       prefixes(t, "10.9.0.0/16"),
			remoteAddr:    "10.9.1.1:5555",
			forwardedFor:  "203.0.113.9",
			wantIP:        "203.0.113.9",
			wantForwarded: true,
		},
		{
			name:         "a narrowed set does not trust loopback",
			trusted:      prefixes(t, "10.9.0.0/16"),
			remoteAddr:   "127.0.0.1:5555",
			forwardedFor: "203.0.113.9",
			wantIP:       "127.0.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{TrustedProxies: tc.trusted}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.forwardedFor != "" {
				r.Header.Set("X-Forwarded-For", tc.forwardedFor)
			}
			ip, forwarded := s.clientIP(r)
			if ip != tc.wantIP {
				t.Errorf("clientIP = %q, want %q", ip, tc.wantIP)
			}
			if forwarded != tc.wantForwarded {
				t.Errorf("forwarded = %v, want %v", forwarded, tc.wantForwarded)
			}
		})
	}
}

// TestLoginLimiterSeparatesForwardedClients is the bug this milestone exists
// for: behind a proxy, two people used to share one budget.
func TestLoginLimiterSeparatesForwardedClients(t *testing.T) {
	ts := newTestServerWithOptions(t, func(o *Options) {
		o.TrustedProxies = config.DefaultTrustedProxies
	})

	login := func(from string) int {
		body, err := json.Marshal(map[string]string{"username": "nobody", "password": "wrong-password"})
		if err != nil {
			t.Fatal(err)
		}
		w := ts.doFrom(http.MethodPost, "/api/auth/login", nil, string(body),
			"127.0.0.1:5555", map[string]string{"X-Forwarded-For": from})
		return w.Code
	}

	// Spend the first client's whole budget: 10/minute.
	for i := 0; i < 10; i++ {
		if code := login("203.0.113.9"); code == http.StatusTooManyRequests {
			t.Fatalf("first client rate limited after %d attempts, expected 10", i)
		}
	}
	if code := login("203.0.113.9"); code != http.StatusTooManyRequests {
		t.Fatalf("first client attempt 11 = %d, want 429", code)
	}

	// A different forwarded client behind the same proxy is unaffected.
	if code := login("203.0.113.10"); code == http.StatusTooManyRequests {
		t.Fatal("second client shares the first client's budget; the proxy address is still the key")
	}
}

// TestLoginLimiterExemptsDirectLoopback covers the exemption and the bypass it
// would open if it were keyed on the resolved address instead of the peer.
func TestLoginLimiterExemptsDirectLoopback(t *testing.T) {
	attempt := func(ts *testServer, remoteAddr string, headers map[string]string) int {
		body, err := json.Marshal(map[string]string{"username": "nobody", "password": "wrong-password"})
		if err != nil {
			t.Fatal(err)
		}
		return ts.doFrom(http.MethodPost, "/api/auth/login", nil, string(body), remoteAddr, headers).Code
	}

	t.Run("a direct loopback connection is not limited", func(t *testing.T) {
		ts := newTestServerWithOptions(t, func(o *Options) {
			o.TrustedProxies = config.DefaultTrustedProxies
		})
		for i := 0; i < 15; i++ {
			if code := attempt(ts, "127.0.0.1:5555", nil); code == http.StatusTooManyRequests {
				t.Fatalf("attempt %d was rate limited; the UI suite would fail here", i+1)
			}
		}
	})

	t.Run("a client behind a local proxy is still limited", func(t *testing.T) {
		ts := newTestServerWithOptions(t, func(o *Options) {
			o.TrustedProxies = config.DefaultTrustedProxies
		})
		hdr := map[string]string{"X-Forwarded-For": "203.0.113.9"}
		for i := 0; i < 10; i++ {
			if code := attempt(ts, "127.0.0.1:5555", hdr); code == http.StatusTooManyRequests {
				t.Fatalf("proxied client limited after %d attempts, expected 10", i)
			}
		}
		if code := attempt(ts, "127.0.0.1:5555", hdr); code != http.StatusTooManyRequests {
			t.Fatalf("proxied client attempt 11 = %d, want 429: the exemption leaked through the proxy", code)
		}
	})

	// The bypass the peer check exists to close. A caller already on loopback
	// gains nothing by claiming loopback -- it is exempt either way. The
	// dangerous case is someone elsewhere on a trusted network, who would take
	// the exemption with them if it were keyed on the resolved address.
	t.Run("a LAN peer forging a loopback header is still limited", func(t *testing.T) {
		ts := newTestServerWithOptions(t, func(o *Options) {
			o.TrustedProxies = config.DefaultTrustedProxies
		})
		hdr := map[string]string{"X-Forwarded-For": "127.0.0.1"}
		limited := false
		for i := 0; i < 12; i++ {
			if attempt(ts, "192.168.1.50:5555", hdr) == http.StatusTooManyRequests {
				limited = true
				break
			}
		}
		if !limited {
			t.Fatal("a forged X-Forwarded-For: 127.0.0.1 from the LAN switched the login limiter off")
		}
	})
}
