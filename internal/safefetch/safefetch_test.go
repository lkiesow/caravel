package safefetch

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The SSRF guard, tested where it now lives.
//
// internal/assist keeps its own copies of these assertions against pageFetcher,
// deliberately: those prove the *assistant* still refuses what it always
// refused, which is what a move of security-critical code has to demonstrate.
// These prove the same policy directly, so a future caller has something to
// read that is not about page fetching.

func guard(t *testing.T, p Policy, rawURL string) error {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return p.Guard(u)
}

func TestGuardRefusesNonPublicTargets(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string // substring of the refusal reason
	}{
		{"loopback v4", "http://127.0.0.1/admin", "loopback"},
		{"loopback by name", "http://localhost:8080/", "loopback"},
		{"loopback v6", "http://[::1]:9000/", "loopback"},
		{"private 10/8", "http://10.0.0.1/", "private"},
		{"private 192.168/16", "http://192.168.1.1/", "private"},
		{"private 172.16/12", "http://172.16.0.1/", "private"},
		// The single most valuable SSRF target in any cloud deployment.
		{"cloud metadata", "http://169.254.169.254/latest/meta-data/", "link-local"},
		{"link-local v6", "http://[fe80::1]/", "link-local"},
		{"unique local v6", "http://[fd00::1]/", "unique-local"},
		{"unspecified", "http://0.0.0.0/", "unspecified"},
		{"file scheme", "file:///etc/passwd", "http and https"},
		{"gopher scheme", "gopher://example.com/", "http and https"},
		{"no host", "http:///nowhere", "no host"},
	}

	p := PublicOnly()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := guard(t, p, tc.url)
			if err == nil {
				t.Fatalf("Guard(%q) allowed it, want a refusal", tc.url)
			}
			var blocked ErrBlocked
			if !errors.As(err, &blocked) {
				t.Fatalf("error = %v (%T), want ErrBlocked", err, err)
			}
			if !strings.Contains(blocked.Reason, tc.want) {
				t.Errorf("reason = %q, want it to mention %q", blocked.Reason, tc.want)
			}
		})
	}
}

// The zero value must be the strict policy. A caller who forgets to build one
// should get the safe behaviour, not an open fetcher.
func TestZeroValuePolicyIsStrict(t *testing.T) {
	var p Policy
	if err := guard(t, p, "http://127.0.0.1/"); err == nil {
		t.Error("the zero-value Policy allowed loopback")
	}
	if err := p.CheckDialAddress("tcp", "127.0.0.1:80", nil); err == nil {
		t.Error("the zero-value Policy dialled loopback")
	}
}

// Neither exception may make a non-HTTP scheme fetchable: the scheme check runs
// before either of them is consulted.
func TestSchemeCheckSurvivesEveryException(t *testing.T) {
	for name, p := range map[string]Policy{
		"public only":   PublicOnly(),
		"allow list":    Allowing("127.0.0.1:80"),
		"allow private": AllowPrivateForTests(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := guard(t, p, "file:///etc/passwd"); err == nil {
				t.Error("file:// was allowed")
			}
		})
	}
}

func TestAllowingOpensExactlyOneAddress(t *testing.T) {
	p := Allowing("127.0.0.1:9999")

	if err := guard(t, p, "http://127.0.0.1:9999/fixture"); err != nil {
		t.Errorf("the allowed address was refused: %v", err)
	}
	if err := p.CheckDialAddress("tcp", "127.0.0.1:9999", nil); err != nil {
		t.Errorf("the allowed address was refused at dial time: %v", err)
	}
	// One address, not a class: the same host on another port is still
	// loopback, and so is its neighbour.
	if err := guard(t, p, "http://127.0.0.1:9998/"); err == nil {
		t.Error("a different port on the allowed host was permitted")
	}
	if err := p.CheckDialAddress("tcp", "127.0.0.2:9999", nil); err == nil {
		t.Error("a different loopback address was permitted")
	}
	// An entry with no port cannot match, because the entries are addresses
	// this process bound and those always carry one.
	if err := guard(t, Allowing(""), "http://127.0.0.1/"); err == nil {
		t.Error("an empty allowlist entry opened something")
	}
}

// The dial-time check is the belt to the pre-flight braces: it sees the
// resolved address, so a name that passes the lookup and then resolves
// differently is still refused.
func TestCheckDialAddress(t *testing.T) {
	p := PublicOnly()
	if err := p.CheckDialAddress("tcp", "127.0.0.1:80", nil); err == nil {
		t.Error("loopback was permitted at dial time")
	}
	if err := p.CheckDialAddress("tcp", "169.254.169.254:80", nil); err == nil {
		t.Error("the metadata endpoint was permitted at dial time")
	}
	if err := p.CheckDialAddress("tcp", "93.184.216.34:80", nil); err != nil {
		t.Errorf("a public address was refused: %v", err)
	}
	// Failing closed on something that cannot be classified.
	var blocked ErrBlocked
	if err := p.CheckDialAddress("tcp", "not-an-ip:80", nil); !errors.As(err, &blocked) {
		t.Errorf("error = %v, want a refusal for an unparseable address", err)
	}
}

// The standard bypass: a public hostname whose redirect lands somewhere
// private. Checking only the original URL catches none of it.
func TestClientRefusesRedirectIntoPrivateSpace(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "internal admin panel")
	}))
	defer private.Close()

	// The redirector is itself on loopback, so it is reached through a client
	// that may dial it -- what is under test is the CheckRedirect hook, which
	// is the half a pre-flight-only guard would miss. The policy the *redirect*
	// is checked against is the strict one.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/secrets", http.StatusFound)
	}))
	defer redirector.Close()

	client := PublicOnly().Client(Options{})
	_, err := client.Get(redirector.URL)
	if err == nil {
		t.Fatal("the redirect chain was followed into private space")
	}
	var blocked ErrBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("error = %v, want the guard to have refused", err)
	}
}

func TestClientStopsARedirectLoop(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
	}))
	defer srv.Close()

	// AllowPrivateForTests so the loop is reached at all; the redirect *count*
	// is what is under test, and it is not one of the exceptions.
	client := AllowPrivateForTests().Client(Options{MaxRedirects: 3})
	_, err := client.Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Errorf("err = %v, want the redirect cap to have stopped it", err)
	}
}

// A caller's own CheckRedirect runs in addition to the guard, never instead of
// it: the map-link resolver uses one to keep a chain on its host allowlist, and
// it must not be able to open the chain up by doing so.
func TestCallerCheckRedirectCannotReplaceTheGuard(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer private.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/", http.StatusFound)
	}))
	defer redirector.Close()

	called := false
	client := PublicOnly().Client(Options{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			called = true
			return nil // permissive on purpose
		},
	})
	_, err := client.Get(redirector.URL)

	var blocked ErrBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("error = %v, want the guard to refuse regardless of the caller hook", err)
	}
	if called {
		t.Error("the caller hook ran before the guard; it must not be able to pre-empt it")
	}
}
