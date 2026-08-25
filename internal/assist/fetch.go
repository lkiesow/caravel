package assist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

// Fetching a web page on the model's instruction.
//
// This is the only place in Caravel where a URL chosen by something other than
// a person becomes an outbound request, which makes it the one real attack
// surface in this feature. The threat is server-side request forgery: the app
// runs inside a network the caller cannot reach, so "fetch this URL for me" is
// an invitation to read the cloud metadata endpoint, a database admin panel on
// localhost, or a router on the LAN. The model does not have to be malicious
// for this to matter -- it reads web pages, and a page can contain a link.
//
// The guard is therefore not "sanitise the URL" but "resolve it and refuse
// anything that is not a public address", applied again after every redirect,
// because a public hostname that redirects to 127.0.0.1 is the standard bypass
// and a check done only on the original URL catches none of it.

const (
	// fetchTimeout bounds one page. Shorter than the model's patience on
	// purpose: a slow page is not worth the run's deadline.
	fetchTimeout = 8 * time.Second
	// fetchMaxBytes caps what is read. Large enough for any real article,
	// small enough that a hostile server streaming forever cannot exhaust
	// memory. Applied to the *body*, not to the extracted text.
	fetchMaxBytes = 512 << 10
	// fetchMaxRedirects is generous for legitimate sites and finite, so a
	// redirect loop ends as an error rather than as a hang.
	fetchMaxRedirects = 5
	// fetchMaxTextBytes is what reaches the model, which is a different limit
	// from what is read: a 512KB page of navigation chrome is mostly tokens
	// nobody is paying for on purpose. Halved in Milestone 5 after watching a
	// live run spend its budget on page text -- the useful part of a page is
	// almost always near the top, and page reads are the dominant cost of a
	// run by a wide margin.
	fetchMaxTextBytes = 12 << 10
)

// errBlockedAddress means the URL resolved somewhere it is not allowed to go.
// Its own type so the tool can report it distinctly -- this is the one fetch
// failure that is interesting rather than routine.
type errBlockedAddress struct {
	host   string
	reason string
}

func (e errBlockedAddress) Error() string {
	return fmt.Sprintf("refusing to fetch %s: %s", e.host, e.reason)
}

// addressPolicy is the fetcher's exception list, and both of its fields are
// exceptions rather than settings.
//
// # Why there are any at all
//
// The guard refuses loopback by design, and everything reachable from a test
// is on loopback. Without an exception the page fetcher can only ever be
// tested against the addresses it must block, never against a server that
// actually answers -- and Milestone 5 of Stage 16 is the standing proof of
// what that costs: fetch_page had never worked against a real site, through
// two milestones of green tests, because httptest serves on an IP literal and
// only a real caller supplies a hostname.
//
// # Why they are two exceptions and not one
//
// allowPrivate switches the address check off wholesale. It is what the
// package's own tests use, and nothing else may: an unexported field with no
// path from an operator's environment.
//
// allowed is much narrower -- a set of exact host:port strings, each one an
// address this process itself just bound. The stub provider uses it for its
// fixture host (see stub_fixture.go), which is what lets the browser suite
// exercise a live link and a recorded source. It is still a weakening and
// should be read as one, but it opens one address rather than a class, the
// address is chosen by the kernel at start-up, and no configuration value can
// name it.
//
// Neither exception touches the rest of the policy: the scheme check, the
// redirect re-check and the size and time caps all still apply to both.
type addressPolicy struct {
	allowPrivate bool
	allowed      map[string]bool
}

// permits reports whether hostPort is on the exact allowlist. A URL with no
// port cannot match, which is intended: the entries are addresses this process
// bound, and those always carry one.
func (p addressPolicy) permits(hostPort string) bool {
	return p.allowed != nil && p.allowed[hostPort]
}

// pageFetcher retrieves pages, with the guard applied.
type pageFetcher struct {
	client *http.Client
	policy addressPolicy
}

func newPageFetcher() *pageFetcher { return newFetcherWithPolicy(addressPolicy{}) }

// newFetcherAllowing builds a fetcher that may reach exactly these host:port
// addresses in addition to the public internet. Everything else is refused as
// usual. See addressPolicy for why this exists and what it does not relax.
func newFetcherAllowing(addrs ...string) *pageFetcher {
	allowed := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if a != "" {
			allowed[a] = true
		}
	}
	return newFetcherWithPolicy(addressPolicy{allowed: allowed})
}

func newFetcherWithPolicy(policy addressPolicy) *pageFetcher {
	f := &pageFetcher{policy: policy}
	f.client = &http.Client{
		Timeout: fetchTimeout,
		// The redirect target is checked before it is followed. Returning an
		// error here aborts the chain, which is what turns "public host
		// redirects to 169.254.169.254" from a bypass into a refusal.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= fetchMaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return f.guard(req.URL)
		},
		Transport: &http.Transport{
			// The belt to guardURL's braces. Between the lookup in guardURL
			// and the connect, a hostile DNS server can answer differently --
			// the classic rebinding race -- so the address actually being
			// connected to is checked too.
			//
			// This must be Dialer.Control, not Transport.DialContext.
			// DialContext is handed the *hostname*; the dialer resolves it
			// afterwards, so a check there sees "example.com" and never an IP.
			// Control runs after resolution and once per candidate address,
			// with the resolved ip:port, which is the only hook that sees what
			// is really being connected to. Milestone 5 learned this the
			// expensive way: the first implementation checked in DialContext,
			// could not parse a hostname as an IP, failed closed, and refused
			// every fetch of every real site. Nothing caught it, because
			// httptest serves on an IP literal and only a live run uses names.
			DialContext: f.dialer().DialContext,
			// Modest: a page fetch is one-shot, and a pool of idle
			// connections to arbitrary hosts is not something to keep.
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}
	return f
}

// page is what a fetch yields: the readable text, and the document title when
// it has one.
type page struct {
	// Title is the <title>, trimmed. Empty when the page has none.
	Title string
	Text  string
}

// Fetch retrieves one page.
//
// Split into the guard and the retrieval below, rather than written as one
// function, for a testing reason worth stating: httptest servers listen on
// loopback, which the guard exists to refuse. Keeping the two apart lets the
// guard be tested against the addresses it must block and the retrieval be
// tested against a real server, instead of one of them going untested because
// the other is in the way. Nothing but Fetch and the tests call
// fetchUnguarded, and its name is the reminder.
func (f *pageFetcher) Fetch(ctx context.Context, rawURL string) (page, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return page{}, fmt.Errorf("not a usable URL: %w", err)
	}
	if err := f.guard(parsed); err != nil {
		return page{}, err
	}
	return f.fetchUnguarded(ctx, parsed.String())
}

// fetchUnguarded performs the request with the pre-flight check already done.
// The transport still applies the dial-time guard and the redirect guard, so
// this is not a way around the policy -- only a way past the first of its
// three checks.
func (f *pageFetcher) fetchUnguarded(ctx context.Context, target string) (page, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return page{}, err
	}
	req.Header.Set("User-Agent", assistUserAgent())
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.1")

	resp, err := f.client.Do(req)
	if err != nil {
		// A guard refusal arrives wrapped in *url.Error; unwrap so the caller
		// sees why rather than a generic transport failure.
		var blocked errBlockedAddress
		if errors.As(err, &blocked) {
			return page{}, blocked
		}
		return page{}, fmt.Errorf("could not fetch the page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return page{}, fmt.Errorf("the page responded with status %d", resp.StatusCode)
	}

	// Content-Type is advisory, so this is a cheap filter rather than a
	// guarantee -- a PDF or an image is many tokens of nothing useful.
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "html") && !strings.Contains(ct, "xml") {
		return page{}, fmt.Errorf("the page is %s, not text", strings.SplitN(ct, ";", 2)[0])
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes))
	if err != nil {
		return page{}, fmt.Errorf("could not read the page: %w", err)
	}

	title, text := extractText(string(body))
	if len(text) > fetchMaxTextBytes {
		text = text[:fetchMaxTextBytes] + "\n[truncated]"
	}
	if strings.TrimSpace(text) == "" {
		return page{}, fmt.Errorf("the page had no readable text")
	}
	return page{Title: title, Text: text}, nil
}

// guard applies this fetcher's address policy.
//
// The scheme check comes first and is never skipped, so neither exception can
// make file:// fetchable.
func (f *pageFetcher) guard(u *url.URL) error {
	if err := guardScheme(u); err != nil {
		return err
	}
	if f.policy.allowPrivate || f.policy.permits(u.Host) {
		return nil
	}
	return guardURL(u)
}

// dialer builds the connecting dialer, with the address policy applied in
// Control -- see the note on the Transport for why it has to be there.
func (f *pageFetcher) dialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   f.checkDialAddress,
	}
}

// checkDialAddress runs after DNS resolution and before the connect, once per
// candidate address. Its argument really is ip:port.
func (f *pageFetcher) checkDialAddress(_, address string, _ syscall.RawConn) error {
	// The allowlist is matched here on the resolved ip:port as well as on the
	// URL in guard, and the two agree because its entries are literal
	// addresses -- there is no name in between for the resolver to change its
	// mind about.
	if f.policy.allowPrivate || f.policy.permits(address) {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Control is documented to receive a resolved address, so this is
		// unreachable in practice -- but failing closed on something we could
		// not classify is the only safe direction.
		return errBlockedAddress{host: host, reason: "the connect address could not be read as an IP"}
	}
	return guardIP(host, ip)
}

// guardScheme is the half of the policy that always applies, however the
// address policy is set: file:// and gopher:// are never fetchable.
func guardScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return errBlockedAddress{host: u.Scheme + "://", reason: "only http and https are allowed"}
	}
	if u.Hostname() == "" {
		return errBlockedAddress{host: u.String(), reason: "no host"}
	}
	return nil
}

// guardURL rejects anything that is not a plain public HTTP(S) address.
//
// Scheme first, then the resolved addresses. Every A/AAAA record is checked,
// not just the first: a name that resolves to one public and one private
// address would otherwise pass here and dial either.
func guardURL(u *url.URL) error {
	if err := guardScheme(u); err != nil {
		return err
	}
	host := u.Hostname()

	// A literal address needs no lookup, and must not get one: resolving it
	// would be a no-op that only adds a failure mode.
	if ip := net.ParseIP(host); ip != nil {
		return guardIP(host, ip)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return errBlockedAddress{host: host, reason: "the host could not be resolved"}
	}
	if len(ips) == 0 {
		return errBlockedAddress{host: host, reason: "the host resolved to no addresses"}
	}
	for _, ip := range ips {
		if err := guardIP(host, ip); err != nil {
			return err
		}
	}
	return nil
}

// guardIP is the actual policy, in one place so the pre-flight check and the
// dial-time check cannot disagree.
func guardIP(host string, ip net.IP) error {
	switch {
	case ip.IsLoopback():
		return errBlockedAddress{host: host, reason: "it resolves to a loopback address"}
	// Before IsPrivate, which also covers IPv6 fc00::/7 and would otherwise
	// answer first with the vaguer reason. Both refuse; this one says which
	// kind of address it was, which is the difference between a useful log
	// line and a puzzling one.
	case isUniqueLocal(ip):
		return errBlockedAddress{host: host, reason: "it resolves to a unique-local address"}
	case ip.IsPrivate():
		return errBlockedAddress{host: host, reason: "it resolves to a private address"}
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// 169.254.169.254 lives here: the cloud metadata endpoint, and the
		// single most valuable target an SSRF can reach.
		return errBlockedAddress{host: host, reason: "it resolves to a link-local address"}
	case ip.IsUnspecified():
		return errBlockedAddress{host: host, reason: "it resolves to an unspecified address"}
	case ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return errBlockedAddress{host: host, reason: "it resolves to a multicast address"}
	}
	return nil
}

// isUniqueLocal covers IPv6 fc00::/7. net.IP.IsPrivate reports these too, so
// this is not the only thing standing between a ULA and a dial -- it runs
// first so the refusal names the actual kind of address, and it means the
// policy does not quietly depend on one stdlib helper's IPv6 behaviour.
func isUniqueLocal(ip net.IP) bool {
	v6 := ip.To16()
	return ip.To4() == nil && v6 != nil && v6[0]&0xfe == 0xfc
}

// extractText reduces HTML to the words a model can use, and picks up the
// document title on the way.
//
// Not a readability implementation: dropping script and style content and
// collapsing whitespace removes most of the tokens without pretending to know
// which div is the article. Anything cleverer is a maintenance burden for a
// gain the model mostly does not need.
//
// The title is taken here rather than guessed from the text, because the first
// line of a real page is very often furniture -- Milestone 8's first live run
// produced a source listed as "Skip to main content", which is an
// accessibility link every well-built site starts with.
func extractText(body string) (string, string) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		// Not HTML, or HTML too broken to parse. The raw text is still better
		// than nothing -- plain-text pages take this path deliberately.
		return "", collapseWhitespace(body)
	}

	title := ""
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				// Inside <head>, which is skipped below, so this is read
				// before the subtree is abandoned.
				if title == "" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					title = strings.TrimSpace(n.FirstChild.Data)
				}
				return
			case "script", "style", "noscript", "svg":
				return
			case "head":
				// Walked for its <title> only; nothing else in here is text a
				// reader would see.
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "title" {
						walk(c)
					}
				}
				return
			}
		}
		if n.Type == html.TextNode {
			if t := strings.TrimSpace(n.Data); t != "" {
				b.WriteString(t)
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		// A newline after block-level elements, so the model sees some
		// structure rather than one undifferentiated paragraph.
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "section", "article", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "br":
				b.WriteByte('\n')
			}
		}
	}
	walk(doc)
	return title, collapseWhitespace(b.String())
}

func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if f := strings.Join(strings.Fields(line), " "); f != "" {
			out = append(out, f)
		}
	}
	return strings.Join(out, "\n")
}

// LinkIsLive reports whether a proposed URL actually resolves to something.
//
// Hallucinated URLs are the classic failure of this feature, and a dead link
// is worse than no link: it looks authoritative until somebody clicks it. So
// every link the model proposes is checked before it is offered.
//
// HEAD first, because it is the cheap question and most servers answer it. A
// meaningful minority answer 405 or 501 to HEAD while serving the page
// perfectly well on GET, so those two specifically fall back rather than
// counting as dead -- treating them as dead would silently drop working links
// from a whole class of sites.
//
// The full guard applies: this is an outbound request driven by model output,
// exactly like a page fetch, and a link is as good an SSRF vector as anything
// else. Errs toward *dropping* a link when anything is unclear, since the cost
// of a false negative is one missing suggestion and the cost of a false
// positive is a broken link in the user's data.
func (f *pageFetcher) LinkIsLive(ctx context.Context, rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || f.guard(parsed) != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, linkCheckTimeout)
	defer cancel()

	status, err := f.probe(ctx, http.MethodHead, parsed.String())
	if err != nil {
		return false
	}
	if status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented {
		status, err = f.probe(ctx, http.MethodGet, parsed.String())
		if err != nil {
			return false
		}
	}
	// 2xx and 3xx both count: the client follows redirects, so a 3xx here
	// means the chain ended somewhere the guard allowed.
	return status >= 200 && status < 400
}

func (f *pageFetcher) probe(ctx context.Context, method, target string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", assistUserAgent())

	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	// Nothing here reads the body, but it must still be drained-and-closed or
	// the connection cannot be reused -- and with six of these in parallel
	// that is six sockets left hanging per run.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	return resp.StatusCode, nil
}
