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
	// nobody is paying for on purpose.
	fetchMaxTextBytes = 24 << 10
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

// pageFetcher retrieves pages, with the guard applied.
type pageFetcher struct {
	client *http.Client
}

func newPageFetcher() *pageFetcher {
	f := &pageFetcher{}
	f.client = &http.Client{
		Timeout: fetchTimeout,
		// The redirect target is checked before it is followed. Returning an
		// error here aborts the chain, which is what turns "public host
		// redirects to 169.254.169.254" from a bypass into a refusal.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= fetchMaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return guardURL(req.URL)
		},
		Transport: &http.Transport{
			// DialContext re-checks the address the dialer actually got. The
			// belt to guardURL's braces: between the lookup in guardURL and
			// the dial, a hostile DNS server can answer differently -- the
			// classic DNS-rebinding race. Checking the address being connected
			// to closes it, because this hook sees the resolved IP rather than
			// the name.
			DialContext: guardedDialContext,
			// Modest: a page fetch is one-shot, and a pool of idle
			// connections to arbitrary hosts is not something to keep.
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}
	return f
}

// Fetch retrieves one page and returns its text.
//
// Split into the guard and the retrieval below, rather than written as one
// function, for a testing reason worth stating: httptest servers listen on
// loopback, which the guard exists to refuse. Keeping the two apart lets the
// guard be tested against the addresses it must block and the retrieval be
// tested against a real server, instead of one of them going untested because
// the other is in the way. Nothing but Fetch and the tests call
// fetchUnguarded, and its name is the reminder.
func (f *pageFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("not a usable URL: %w", err)
	}
	if err := guardURL(parsed); err != nil {
		return "", err
	}
	return f.fetchUnguarded(ctx, parsed.String())
}

// fetchUnguarded performs the request with the pre-flight check already done.
// The transport still applies the dial-time guard and the redirect guard, so
// this is not a way around the policy -- only a way past the first of its
// three checks.
func (f *pageFetcher) fetchUnguarded(ctx context.Context, target string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", assistUserAgent())
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.1")

	resp, err := f.client.Do(req)
	if err != nil {
		// A guard refusal arrives wrapped in *url.Error; unwrap so the caller
		// sees why rather than a generic transport failure.
		var blocked errBlockedAddress
		if errors.As(err, &blocked) {
			return "", blocked
		}
		return "", fmt.Errorf("could not fetch the page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the page responded with status %d", resp.StatusCode)
	}

	// Content-Type is advisory, so this is a cheap filter rather than a
	// guarantee -- a PDF or an image is many tokens of nothing useful.
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "html") && !strings.Contains(ct, "xml") {
		return "", fmt.Errorf("the page is %s, not text", strings.SplitN(ct, ";", 2)[0])
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes))
	if err != nil {
		return "", fmt.Errorf("could not read the page: %w", err)
	}

	text := extractText(string(body))
	if len(text) > fetchMaxTextBytes {
		text = text[:fetchMaxTextBytes] + "\n[truncated]"
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("the page had no readable text")
	}
	return text, nil
}

// guardURL rejects anything that is not a plain public HTTP(S) address.
//
// Scheme first, then the resolved addresses. Every A/AAAA record is checked,
// not just the first: a name that resolves to one public and one private
// address would otherwise pass here and dial either.
func guardURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return errBlockedAddress{host: u.Scheme + "://", reason: "only http and https are allowed"}
	}
	host := u.Hostname()
	if host == "" {
		return errBlockedAddress{host: u.String(), reason: "no host"}
	}

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

// guardedDialContext re-checks the address at dial time, closing the
// DNS-rebinding race described on the Transport above.
func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// The dialer is given an address, not a name, so this should be
		// unreachable. Failing closed rather than dialing something we could
		// not classify.
		return nil, errBlockedAddress{host: host, reason: "the dial address is not an IP"}
	}
	if err := guardIP(host, ip); err != nil {
		return nil, err
	}
	return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(host, port))
}

// extractText reduces HTML to the words a model can use.
//
// Not a readability implementation: dropping script and style content and
// collapsing whitespace removes most of the tokens without pretending to know
// which div is the article. Anything cleverer is a maintenance burden for a
// gain the model mostly does not need.
func extractText(body string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		// Not HTML, or HTML too broken to parse. The raw text is still better
		// than nothing -- plain-text pages take this path deliberately.
		return collapseWhitespace(body)
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "head":
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
	return collapseWhitespace(b.String())
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
