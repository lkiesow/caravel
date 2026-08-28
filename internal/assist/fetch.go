package assist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"

	"caravel/internal/safefetch"
)

// Fetching a web page on the model's instruction.
//
// A URL chosen by something other than a person becoming an outbound request is
// the one real attack surface in this feature -- the model does not have to be
// malicious for it to matter, since it reads web pages and a page can contain a
// link. The refusals that make that safe are **not in this file**: they are
// internal/safefetch, whose package comment is where the threat and the three
// checks are explained. It stopped being only this feature's problem in Stage
// 22, when the map-link resolver needed the same guard.
//
// What is here is everything else a page fetch needs: how long to wait, how
// much to read, how much of what was read to hand a model, and how to get
// text out of HTML.

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

// pageFetcher retrieves pages, with the guard applied.
//
// The guard itself lives in internal/safefetch since Stage 22 Milestone 6 --
// the map-link resolver needs the same three checks, and a second copy of them
// is the kind of duplication where one copy gets the fix. What is left here is
// this package's own business: the size caps, the timeout, the User-Agent and
// the HTML extraction.
type pageFetcher struct {
	client *http.Client
	policy safefetch.Policy
}

func newPageFetcher() *pageFetcher { return newFetcherWithPolicy(safefetch.PublicOnly()) }

// newFetcherAllowing builds a fetcher that may reach exactly these host:port
// addresses in addition to the public internet. Everything else is refused as
// usual. See safefetch.Allowing for why this exists and what it does not relax.
func newFetcherAllowing(addrs ...string) *pageFetcher {
	return newFetcherWithPolicy(safefetch.Allowing(addrs...))
}

func newFetcherWithPolicy(policy safefetch.Policy) *pageFetcher {
	return &pageFetcher{
		policy: policy,
		client: policy.Client(safefetch.Options{
			Timeout:      fetchTimeout,
			MaxRedirects: fetchMaxRedirects,
		}),
	}
}

// page is what a fetch yields: the readable text, the document title when it
// has one, and the page's own headline image when it advertises one.
type page struct {
	// Title is the <title>, trimmed. Empty when the page has none.
	Title string
	Text  string
	// Image is the absolute URL from og:image, or empty.
	//
	// Harvested here rather than looked for separately because the agent has
	// already fetched and parsed this page: it costs no extra request, no
	// search backend and no key. And it is the best-provenance photograph
	// available to this feature -- the venue's own picture of itself, from the
	// page being proposed as its official link. A generic image search would
	// mean the model choosing a photograph by the text around it, which it
	// cannot see, and a wrong-but-plausible picture of somewhere you have
	// never been is the same failure mode with no tell that made Stage 16
	// refuse to take coordinates from the model.
	Image string
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
		var blocked safefetch.ErrBlocked
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

	title, image, text := extractText(string(body))
	// Relative and protocol-relative og:image values are common enough to be
	// the rule rather than the exception, and resolving needs the page URL,
	// which extractText has no business knowing. A value that will not parse
	// is dropped rather than passed on: a broken image URL in a suggestion is
	// worse than no suggestion.
	if image != "" {
		if base, err := url.Parse(target); err == nil {
			if ref, err := url.Parse(image); err == nil {
				resolved := base.ResolveReference(ref)
				if resolved.Scheme == "http" || resolved.Scheme == "https" {
					image = resolved.String()
				} else {
					image = ""
				}
			} else {
				image = ""
			}
		}
	}
	if len(text) > fetchMaxTextBytes {
		text = text[:fetchMaxTextBytes] + "\n[truncated]"
	}
	if strings.TrimSpace(text) == "" {
		return page{}, fmt.Errorf("the page had no readable text")
	}
	return page{Title: title, Text: text, Image: image}, nil
}

// guard is the pre-flight check, delegated whole to the policy.
func (f *pageFetcher) guard(u *url.URL) error { return f.policy.Guard(u) }

// checkDialAddress is kept as a method so this package's tests can reach the
// dial-time check the way they always have -- it is the check an ordinary
// request cannot exercise, because reaching it needs the resolution it exists
// to second-guess.
func (f *pageFetcher) checkDialAddress(network, address string, c syscall.RawConn) error {
	return f.policy.CheckDialAddress(network, address, c)
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
func extractText(body string) (title, image, text string) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		// Not HTML, or HTML too broken to parse. The raw text is still better
		// than nothing -- plain-text pages take this path deliberately.
		return "", "", collapseWhitespace(body)
	}

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
				// Walked for its <title> and its og:image only; nothing else
				// in here is text a reader would see.
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.ElementNode {
						continue
					}
					switch c.Data {
					case "title":
						walk(c)
					case "meta":
						if v := metaImage(c); v != "" && image == "" {
							image = v
						}
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
	return title, image, collapseWhitespace(b.String())
}

// metaImage reads a <meta> element and returns its content when it is one of
// the tags that names a page's headline image.
//
// Both `property` and `name` are accepted. Open Graph specifies `property`,
// but a large minority of real sites emit `name` instead -- some through a CMS
// that does not know the difference -- and refusing those would drop a working
// image for a spelling nobody outside a validator notices. twitter:image is
// the last resort: it means the same thing and is present on pages that have
// no Open Graph tags at all.
func metaImage(n *html.Node) string {
	var key, content string
	for _, a := range n.Attr {
		switch strings.ToLower(a.Key) {
		case "property", "name":
			// A page carrying both `property` and `name` is malformed; taking
			// the first non-empty is as good a rule as any.
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(a.Val))
			}
		case "content":
			content = strings.TrimSpace(a.Val)
		}
	}
	if content == "" {
		return ""
	}
	switch key {
	case "og:image", "og:image:url", "og:image:secure_url", "twitter:image", "twitter:image:src":
		return content
	}
	return ""
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
