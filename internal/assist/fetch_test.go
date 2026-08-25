package assist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The SSRF guard. This is the one part of the feature where being wrong is a
// security bug rather than a bad suggestion, so the table is deliberately
// broad and includes the bypasses rather than only the obvious cases.

// newRelaxedFetcher is the fetcher these tests use to reach an httptest
// server, which listens on loopback -- the thing the guard exists to refuse.
// It switches off the address check wholesale, which no production path can
// do; see addressPolicy for why the exception exists and what it leaves in
// place.
func newRelaxedFetcher() *pageFetcher {
	return newFetcherWithPolicy(addressPolicy{allowPrivate: true})
}

func TestGuardURLRejectsNonPublicTargets(t *testing.T) {
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
		// A UNC-ish or schemeless string must not be treated as fetchable.
		{"no host", "http:///nowhere", "no host"},
	}

	f := newPageFetcher()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.Fetch(context.Background(), tc.url)
			if err == nil {
				t.Fatalf("Fetch(%q) succeeded, want a refusal", tc.url)
			}
			var blocked errBlockedAddress
			if !errors.As(err, &blocked) {
				t.Fatalf("error = %v (%T), want errBlockedAddress", err, err)
			}
			if !strings.Contains(blocked.reason, tc.want) {
				t.Errorf("reason = %q, want it to mention %q", blocked.reason, tc.want)
			}
		})
	}
}

// The standard bypass: a public hostname whose redirect lands somewhere
// private. Checking only the original URL catches none of it.
func TestFetchRefusesRedirectIntoPrivateSpace(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body>internal admin panel</body></html>")
	}))
	defer private.Close()

	// The redirector is itself on loopback in this test, so it is reached
	// through the client directly rather than through Fetch's pre-flight
	// check -- what is being asserted is the CheckRedirect hook, which is the
	// half that a pre-flight-only guard would miss.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/secrets", http.StatusFound)
	}))
	defer redirector.Close()

	f := newPageFetcher()
	req, err := http.NewRequest(http.MethodGet, redirector.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.client.Do(req)
	if err == nil {
		t.Fatal("the redirect chain was followed into private space")
	}
	var blocked errBlockedAddress
	if !errors.As(err, &blocked) {
		t.Fatalf("error = %v, want the guard to have refused", err)
	}
}

// The dial-time check is the belt to guardURL's braces: it sees the resolved
// address, so a name that passes the pre-flight lookup and then resolves
// differently is still refused.
func TestGuardedDialRefusesPrivateAddresses(t *testing.T) {
	// Control is handed a resolved ip:port, which is what makes it the right
	// hook -- see the note on the Transport.
	err := newPageFetcher().checkDialAddress("tcp", "127.0.0.1:80", nil)
	var blocked errBlockedAddress
	if !errors.As(err, &blocked) {
		t.Fatalf("error = %v, want the dial-time guard to refuse", err)
	}
	if err := newPageFetcher().checkDialAddress("tcp", "93.184.216.34:80", nil); err != nil {
		t.Errorf("a public address was refused: %v", err)
	}
}

// The regression test for the bug that shipped through Milestone 3 and 4
// unnoticed: the dial-time check was in Transport.DialContext, which receives
// the *hostname*, so it could never parse an IP, failed closed, and refused
// every fetch of every real site.
//
// It survived two milestones of tests because every one of them used
// httptest's URL, which is an IP literal -- the single way the test
// environment differed from every real caller. So this test insists on
// reaching the same server through a *name*.
func TestFetchWorksWhenTheHostIsANameNotAnIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><p>Reached by name.</p></body></html>")
	}))
	defer srv.Close()

	// "localhost" is a name that resolves to loopback, so the relaxed policy
	// is needed to get past the pre-flight check -- but the dialer still runs
	// exactly as it does in production, which is the half under test.
	byName := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if byName == srv.URL {
		t.Skipf("test server URL %q is not the expected loopback form", srv.URL)
	}

	got, err := newRelaxedFetcher().Fetch(context.Background(), byName)
	if err != nil {
		t.Fatalf("Fetch(%q) = %v; a hostname must reach the dialer intact", byName, err)
	}
	if !strings.Contains(got.Text, "Reached by name") {
		t.Errorf("text = %q", got.Text)
	}
}

// The same for the liveness probe, which takes its own path through the
// client and would have been just as broken.
func TestLinkIsLiveWorksWhenTheHostIsAName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	byName := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if !newRelaxedFetcher().LinkIsLive(context.Background(), byName) {
		t.Errorf("LinkIsLive(%q) = false; a hostname must reach the dialer intact", byName)
	}
}

func TestFetchReadsAPublicPage(t *testing.T) {
	// httptest listens on loopback, which the guard refuses by design, so this
	// fetcher relaxes the address policy only. Everything else -- the scheme
	// check, redirects, status handling, content type, the size cap and text
	// extraction -- runs exactly as in production. Testing the guard and
	// testing what happens past it are separate jobs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Kex</title><style>body{color:red}</style></head>
		  <body><script>alert(1)</script><h1>Kex Hostel</h1><p>Skulagata 28.</p></body></html>`)
	}))
	defer srv.Close()

	f := newRelaxedFetcher()

	got, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(got.Text, "Kex Hostel") || !strings.Contains(got.Text, "Skulagata 28") {
		t.Errorf("text = %q, want the page content", got.Text)
	}
	// Script and style content is tokens the model pays for and cannot use.
	if strings.Contains(got.Text, "alert(1)") || strings.Contains(got.Text, "color:red") {
		t.Errorf("text = %q, want script and style stripped", got.Text)
	}
	// The <title> is taken from <head>, which is otherwise skipped.
	if got.Title != "Kex" {
		t.Errorf("Title = %q, want the document title", got.Title)
	}
}

func TestFetchRejectsNonTextContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		fmt.Fprint(w, "\x89PNG\r\n\x1a\n")
	}))
	defer srv.Close()

	f := newRelaxedFetcher()
	_, err := f.Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "not text") {
		t.Errorf("error = %v, want a complaint about the content type", err)
	}
}

func TestFetchCapsTheBodyItReads(t *testing.T) {
	// A hostile server that streams forever must not exhaust memory.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		chunk := strings.Repeat("a", 4096)
		for range 1000 { // ~4MB, well past the 512KB cap
			if _, err := fmt.Fprint(w, chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	f := newRelaxedFetcher()
	got, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// What reaches the model is capped tighter than what is read.
	if len(got.Text) > fetchMaxTextBytes+64 {
		t.Errorf("text is %d bytes, want it capped near %d", len(got.Text), fetchMaxTextBytes)
	}
}

func TestFetchRejectsAnEmptyPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><script>var x=1</script></body></html>")
	}))
	defer srv.Close()

	f := newRelaxedFetcher()
	_, err := f.Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "no readable text") {
		t.Errorf("error = %v, want a complaint that there was nothing to read", err)
	}
}

func TestFetchSurfacesAnErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	f := newRelaxedFetcher()
	_, err := f.Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want the status reported", err)
	}
}

// The relaxed policy is address-only. A test fetcher must still refuse
// file:// and a URL with no host, or the tests would be exercising a
// different guard from the one that ships.
func TestRelaxedPolicyStillEnforcesTheScheme(t *testing.T) {
	f := newRelaxedFetcher()
	for _, bad := range []string{"file:///etc/passwd", "gopher://example.com/", "http:///nowhere"} {
		if _, err := f.Fetch(context.Background(), bad); err == nil {
			t.Errorf("Fetch(%q) succeeded under the relaxed policy", bad)
		}
	}
}

func TestExtractTextHandlesNonHTML(t *testing.T) {
	// A plain-text page takes the parse path too; it must not come back empty,
	// and it has no title to find.
	title, text := extractText("Kex Hostel\n\nSkulagata 28, Reykjavik")
	if !strings.Contains(text, "Kex Hostel") || !strings.Contains(text, "Skulagata 28") {
		t.Errorf("extractText text = %q", text)
	}
	if title != "" {
		t.Errorf("extractText title = %q, want empty", title)
	}
}

// The reason the title is read at all: a real page's first line is very often
// furniture. A live run in Milestone 8 listed a source as "Skip to main
// content", which is the accessibility link every well-built site opens with.
func TestExtractTextPrefersTheDocumentTitleOverSkipLinks(t *testing.T) {
	title, text := extractText(`<html><head><title>Hallgrimskirkja Church</title></head>
	  <body><a href="#main">Skip to main content</a><h1>Hallgrimskirkja</h1></body></html>`)
	if title != "Hallgrimskirkja Church" {
		t.Errorf("title = %q", title)
	}
	// The skip link is still in the text; it is only the *title* that must not
	// be taken from it.
	if !strings.Contains(text, "Skip to main content") {
		t.Errorf("text = %q, want the body left alone", text)
	}
}

// The allowlist exception, in both directions.
//
// It exists so the stub's fixture host is reachable (see stub_fixture.go), and
// the thing that makes it acceptable is how narrow it is: one exact address,
// and nothing else -- not even another loopback address one port along, which
// is what an SSRF against a developer machine would actually be aiming at.
func TestAllowlistPolicyPermitsOnlyTheNamedAddress(t *testing.T) {
	page := `<!doctype html><html><head><title>Allowed</title></head><body><p>Reachable.</p></body></html>`
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer allowed.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer other.Close()

	f := newFetcherAllowing(strings.TrimPrefix(allowed.URL, "http://"))

	got, err := f.Fetch(context.Background(), allowed.URL+"/kex")
	if err != nil {
		t.Fatalf("the allowlisted address was refused: %v", err)
	}
	if got.Title != "Allowed" {
		t.Errorf("Title = %q, want the fixture's", got.Title)
	}

	// A different loopback address is still loopback.
	if _, err := f.Fetch(context.Background(), other.URL+"/kex"); err == nil {
		t.Error("a loopback address that is not on the list was fetched")
	} else {
		var blocked errBlockedAddress
		if !errors.As(err, &blocked) {
			t.Errorf("refusal = %v, want a blocked-address error", err)
		}
	}

	// And the rest of the policy is untouched: an allowlist is an address
	// exception, not a licence to leave the web.
	for _, target := range []string{
		"file:///etc/passwd",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
	} {
		if _, err := f.Fetch(context.Background(), target); err == nil {
			t.Errorf("Fetch(%q) was allowed", target)
		}
	}
}

// The same narrowness, at the dial-time check rather than the pre-flight one.
// The two have to agree, or a URL that passes the first is refused by the
// second and the fixture is unusable in a way that looks like a network fault.
func TestAllowlistPolicyAppliesAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>Dialled</title></head><body><p>Text.</p></body></html>`)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	f := newFetcherAllowing(addr)
	if err := f.checkDialAddress("tcp", addr, nil); err != nil {
		t.Errorf("checkDialAddress(%q) = %v, want it permitted", addr, err)
	}
	if err := f.checkDialAddress("tcp", "127.0.0.1:1", nil); err == nil {
		t.Error("checkDialAddress permitted a loopback address that is not on the list")
	}
	if !f.LinkIsLive(context.Background(), srv.URL) {
		t.Error("LinkIsLive refused the allowlisted address")
	}
}
