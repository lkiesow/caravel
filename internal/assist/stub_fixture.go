package assist

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// The stub provider's fixture host.
//
// # Why this exists
//
// Everything about the assistant runs for real against the stub except the
// call to the model -- the loop, the tools, the validation, the link check and
// the SSE transport. Everything, that is, except the two parts that need a
// page to actually answer: fetch_page recording a source, and the liveness
// check keeping a proposed link. The stub's URLs used to point at
// example.invalid, so the fetch failed and the link was dropped, both
// correctly, and the browser suite consequently had no way to see either list
// at all. A bug in the sources list shipped because of exactly that.
//
// The obvious two fixes were considered in Stage 16 and rejected, rightly:
// giving CI a network dependency, and letting CARAVEL_LLM_URL=stub relax the
// fetcher's address policy, which is a configuration value weakening a
// security control.
//
// This is the third option. The stub starts its own server on loopback and the
// fetcher is given an allowlist holding exactly that one address. It is still
// a weakening of the SSRF guard and should be read as one -- but the address
// is chosen by the kernel when the process starts, no environment variable can
// name it, it exists only when the stub provider does, and everything else
// about the guard (the scheme check, the redirect re-check, the size and time
// caps, and the refusal of every other loopback address) still applies. See
// addressPolicy in fetch.go.
//
// # One per process
//
// A singleton, because newStubProvider is called by every test in this package
// as well as by the server, and a listener per call would leak dozens of them
// across a test run with nothing to close them. The server has no shutdown
// path for the same reason there is no Close on Assistant: the stub is a
// development and test sentinel, and one idle loopback listener for the life
// of the process is the whole cost.

// stubFixture is a started fixture host: the address it bound and the base URL
// pointing at it.
type stubFixture struct {
	addr string
	base string
}

var startStubFixture = sync.OnceValue(func() stubFixture {
	// Port 0: the kernel picks, which is what makes the allowlisted address
	// unnameable from configuration. 127.0.0.1 rather than localhost so there
	// is no name for a resolver to have opinions about -- the allowlist is
	// matched on the literal address in both the pre-flight check and the
	// dial-time check, and those two have to agree.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// Unreachable in any environment that can also run an HTTP server,
		// which this process is. Failing loudly beats a stub that silently
		// goes back to proposing dead links.
		panic("assist: starting the stub fixture host: " + err.Error())
	}

	mux := http.NewServeMux()
	for path, body := range stubFixturePages {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, body)
		})
	}

	srv := &http.Server{
		Handler: mux,
		// Small and fixed. Nothing here is slow, and a fixture that could hang
		// would turn a failing assertion into a timeout somewhere else.
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()

	addr := ln.Addr().String()
	return stubFixture{addr: addr, base: "http://" + addr}
})

// The pages. Deliberately plain HTML with a real <title> and a first paragraph
// worth extracting, because that is what the source list shows and what a
// human looking at a failing assertion has to recognise.
//
// The content matches the answer the stub scripts (see stub.go): a suggestion
// that cites a page saying something else would make the fixture harder to
// read than no fixture at all.
var stubFixturePages = map[string]string{
	"/kex": `<!doctype html><html><head><title>Kex Hostel — Reykjavik</title></head>
<body><h1>Kex Hostel</h1>
<p>A former biscuit factory on the harbour side of Reykjavik, now a hostel
with dorms and private rooms. Reception is open around the clock.</p>
<p>Skulagata 28, 101 Reykjavik, Iceland.</p>
<script>this should not be extracted</script>
</body></html>`,
	"/reykjavik": `<!doctype html><html><head><title>Visiting Reykjavik</title></head>
<body><h1>Visiting Reykjavik</h1>
<p>The harbour side of the city is walkable from the centre in about fifteen
minutes. The bar at Kex is open to non-guests and does food until late.</p>
</body></html>`,
}
