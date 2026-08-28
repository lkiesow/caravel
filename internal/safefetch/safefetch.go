// Package safefetch makes outbound HTTP requests to URLs the app did not
// choose, without letting them reach anything private.
//
// The threat is server-side request forgery. Caravel runs inside a network its
// callers cannot reach, so "fetch this URL for me" is an invitation to read the
// cloud metadata endpoint, a database admin panel on localhost, or a router on
// the LAN. Two features now hand it such a URL: the assistant reads pages the
// model asks for (a page can contain a link), and the map-link resolver follows
// a redirect chain from a shortener.
//
// The guard is therefore not "sanitise the URL" but "resolve it and refuse
// anything that is not a public address", applied three times:
//
//  1. before the request, on the URL itself (Policy.Guard);
//  2. after DNS resolution and before the connect, on the resolved ip:port,
//     which is what closes the DNS-rebinding race;
//  3. on every redirect target before it is followed, because a public hostname
//     that redirects to 127.0.0.1 is the standard bypass and a check done only
//     on the original URL catches none of it.
//
// Lifted out of internal/assist in Stage 22 Milestone 6, unchanged in
// behaviour. It moved because the map-link resolver needs the same three checks
// and the alternative was a second copy of ~120 lines of security-critical
// code -- the kind of duplication where the copies drift and only one of them
// gets the fix.
//
// What stayed in internal/assist: the size caps, the timeouts it wants and all
// of the HTML extraction. This package has an opinion about *where* a request
// may go and none about what comes back.
package safefetch

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// ErrBlocked means the URL resolved somewhere it is not allowed to go.
//
// Its own type so callers can report it distinctly: for the assistant this is
// the one fetch failure that is interesting rather than routine, and for the
// map-link resolver it is the difference between "that link is not one we
// follow" and "that service is down".
type ErrBlocked struct {
	Host   string
	Reason string
}

func (e ErrBlocked) Error() string {
	return fmt.Sprintf("refusing to fetch %s: %s", e.Host, e.Reason)
}

// Policy is where a request may go. The zero value is the strict one -- public
// addresses only -- so a caller that forgets to build one gets the safe
// behaviour rather than an open fetcher.
//
// Both of its fields are exceptions rather than settings, and neither is
// reachable from configuration: there is no path from an operator's environment
// to either of them.
type Policy struct {
	allowPrivate bool
	allowed      map[string]bool
}

// PublicOnly is the policy every production caller uses.
func PublicOnly() Policy { return Policy{} }

// Allowing permits exactly these host:port addresses in addition to the public
// internet. Everything else is refused as usual, and the rest of the policy --
// the scheme check, the redirect re-check -- still applies.
//
// Much narrower than AllowPrivateForTests: each entry is one exact address this
// process itself just bound. The assistant's stub provider uses it for its
// fixture host, which is what lets the browser suite exercise a live link and a
// recorded source. It is still a weakening and should be read as one, but it
// opens one address rather than a class, the address is chosen by the kernel at
// start-up, and no configuration value can name it.
func Allowing(addrs ...string) Policy {
	allowed := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if a != "" {
			allowed[a] = true
		}
	}
	return Policy{allowed: allowed}
}

// AllowPrivateForTests switches the address check off wholesale.
//
// The name is the documentation: **no production path may call this**. It
// exists because the guard refuses loopback by design and everything reachable
// from a test is on loopback, so without it a fetcher can only ever be tested
// against the addresses it must block, never against a server that actually
// answers. Stage 16 Milestone 5 is the standing proof of what that costs --
// fetch_page had never worked against a real site, through two milestones of
// green tests, because httptest serves on an IP literal and only a real caller
// supplies a hostname.
//
// The scheme check still applies, so this cannot make file:// fetchable.
func AllowPrivateForTests() Policy { return Policy{allowPrivate: true} }

// permits reports whether hostPort is on the exact allowlist. A URL with no
// port cannot match, which is intended: the entries are addresses this process
// bound, and those always carry one.
func (p Policy) permits(hostPort string) bool {
	return p.allowed != nil && p.allowed[hostPort]
}

// Guard is the pre-flight check: may a request to this URL be made at all.
//
// The scheme check comes first and is never skipped, so neither exception can
// make file:// fetchable.
func (p Policy) Guard(u *url.URL) error {
	if err := guardScheme(u); err != nil {
		return err
	}
	if p.allowPrivate || p.permits(u.Host) {
		return nil
	}
	return guardURL(u)
}

// Options tune the client this package builds. Everything here is about the
// shape of the request rather than about the policy, which is why they are
// separate: two callers with the same policy legitimately want different
// timeouts.
type Options struct {
	// Timeout bounds the whole request including redirects. Zero means 30s,
	// which is a backstop rather than a recommendation -- callers should say.
	Timeout time.Duration
	// MaxRedirects is finite so a redirect loop ends as an error rather than as
	// a hang. Zero means 5.
	MaxRedirects int
	// CheckRedirect, when set, runs *in addition to* the guard on each redirect
	// target -- the guard is not replaceable. It is how the map-link resolver
	// keeps a chain from wandering off its host allowlist.
	CheckRedirect func(req *http.Request, via []*http.Request) error
}

// Client builds an http.Client with all three checks wired in.
//
// This is the only supported way to get one: a caller holding a Policy and its
// own http.Client would have the pre-flight check and neither of the other two,
// which is the shape of guard that looks present and stops nothing.
func (p Policy) Client(opts Options) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	maxRedirects := opts.MaxRedirects
	if maxRedirects == 0 {
		maxRedirects = 5
	}

	return &http.Client{
		Timeout: timeout,
		// The redirect target is checked before it is followed. Returning an
		// error here aborts the chain, which is what turns "public host
		// redirects to 169.254.169.254" from a bypass into a refusal.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			if err := p.Guard(req.URL); err != nil {
				return err
			}
			if opts.CheckRedirect != nil {
				return opts.CheckRedirect(req, via)
			}
			return nil
		},
		Transport: &http.Transport{
			// The belt to guardURL's braces. Between the lookup in guardURL and
			// the connect, a hostile DNS server can answer differently -- the
			// classic rebinding race -- so the address actually being connected
			// to is checked too.
			//
			// This must be Dialer.Control, not Transport.DialContext.
			// DialContext is handed the *hostname*; the dialer resolves it
			// afterwards, so a check there sees "example.com" and never an IP.
			// Control runs after resolution and once per candidate address,
			// with the resolved ip:port, which is the only hook that sees what
			// is really being connected to. Stage 16 Milestone 5 learned this
			// the expensive way: the first implementation checked in
			// DialContext, could not parse a hostname as an IP, failed closed,
			// and refused every fetch of every real site. Nothing caught it,
			// because httptest serves on an IP literal and only a live run uses
			// names.
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
				Control:   p.CheckDialAddress,
			}).DialContext,
			// Modest: these fetches are one-shot, and a pool of idle
			// connections to arbitrary hosts is not something to keep.
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}
}

// CheckDialAddress runs after DNS resolution and before the connect, once per
// candidate address. Its argument really is ip:port.
//
// Exported so it can be tested directly: it is the check that a test cannot
// reach through an ordinary request, because reaching it requires the very
// resolution it exists to second-guess.
func (p Policy) CheckDialAddress(_, address string, _ syscall.RawConn) error {
	// The allowlist is matched here on the resolved ip:port as well as on the
	// URL in Guard, and the two agree because its entries are literal addresses
	// -- there is no name in between for the resolver to change its mind about.
	if p.allowPrivate || p.permits(address) {
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
		return ErrBlocked{Host: host, Reason: "the connect address could not be read as an IP"}
	}
	return guardIP(host, ip)
}

// guardScheme is the half of the policy that always applies, however the
// address policy is set: file:// and gopher:// are never fetchable.
func guardScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrBlocked{Host: u.Scheme + "://", Reason: "only http and https are allowed"}
	}
	if u.Hostname() == "" {
		return ErrBlocked{Host: u.String(), Reason: "no host"}
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
		return ErrBlocked{Host: host, Reason: "the host could not be resolved"}
	}
	if len(ips) == 0 {
		return ErrBlocked{Host: host, Reason: "the host resolved to no addresses"}
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
		return ErrBlocked{Host: host, Reason: "it resolves to a loopback address"}
	// Before IsPrivate, which also covers IPv6 fc00::/7 and would otherwise
	// answer first with the vaguer reason. Both refuse; this one says which
	// kind of address it was, which is the difference between a useful log
	// line and a puzzling one.
	case isUniqueLocal(ip):
		return ErrBlocked{Host: host, Reason: "it resolves to a unique-local address"}
	case ip.IsPrivate():
		return ErrBlocked{Host: host, Reason: "it resolves to a private address"}
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// 169.254.169.254 lives here: the cloud metadata endpoint, and the
		// single most valuable target an SSRF can reach.
		return ErrBlocked{Host: host, Reason: "it resolves to a link-local address"}
	case ip.IsUnspecified():
		return ErrBlocked{Host: host, Reason: "it resolves to an unspecified address"}
	case ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return ErrBlocked{Host: host, Reason: "it resolves to a multicast address"}
	}
	return nil
}

// isUniqueLocal covers IPv6 fc00::/7. net.IP.IsPrivate reports these too, so
// this is not the only thing standing between a ULA and a dial -- it runs first
// so the refusal names the actual kind of address, and it means the policy does
// not quietly depend on one stdlib helper's IPv6 behaviour.
func isUniqueLocal(ip net.IP) bool {
	v6 := ip.To16()
	return ip.To4() == nil && v6 != nil && v6[0]&0xfe == 0xfc
}
