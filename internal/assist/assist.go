// Package assist proposes metadata for a location by asking an LLM, which may
// call a small set of read-only tools (web search, page fetch, OpenStreetMap)
// while it works.
//
// # Off by default
//
// The whole package is optional. New returns a nil Assistant when no endpoint
// is configured, and a nil Assistant is the single off switch: the HTTP layer
// answers 501, /auth/me reports the capability as absent, and the client never
// renders the control. This mirrors CARAVEL_GEOCODER_URL, which disables
// address search the same way and for the same reason -- a control that can
// only report "not enabled on this server" is worse than no control.
//
// Configuration is env vars only, never the database. An API key in
// app_settings is an API key in every backup.
//
// # What this package promises the rest of the app
//
// Propose returns a *proposal*, never a mutation. Nothing here writes to the
// database, and no tool the model can reach has a side effect. The caller
// shows the proposal to a person who accepts or rejects it field by field, and
// only then does the ordinary item-update path run. That is what bounds the
// blast radius of the obvious risk: the agent reads web pages, and a page can
// carry text shaped like instructions. A prompt injection can therefore make
// the model propose nonsense -- it cannot make it *do* anything, because there
// is nothing to do.
//
// Two further guarantees the agent enforces rather than trusting the model to
// respect, both in agent.go:
//
//   - Coordinates never come from the model. It returns a place name and an
//     address string, and internal/geocode resolves them. A hallucinated
//     lat/lng 40km from the real hotel looks entirely plausible in the form and
//     is only wrong on the map, which makes it the one failure mode with no
//     visible tell.
//   - Category is validated against the enum rather than accepted.
package assist

import (
	"context"

	"caravel/internal/buildinfo"
	"caravel/internal/geocode"
)

// Assistant proposes location metadata. One implementation today (*Agent);
// the interface exists so the HTTP layer can hold nil for "not configured"
// and so tests can substitute a fake without an HTTP endpoint.
type Assistant interface {
	// Propose runs the agent to completion and returns a validated proposal.
	//
	// events is called synchronously as the run progresses, for the SSE
	// stream -- a run can take 30 seconds or more, and without progress it
	// reads as a hung spinner. It may be nil if the caller does not care.
	//
	// The run honors ctx: cancelling it stops the agent at the next turn
	// boundary rather than leaving it spending tokens for a client that has
	// gone away.
	Propose(ctx context.Context, req Request, events func(Event)) (*Proposal, error)
}

// Options are New's dependencies, mapped from config.Config by the caller.
//
// A struct of its own rather than taking config.Config directly, so this
// package does not depend on the app's whole configuration surface -- the same
// shape httpapi.Options uses.
type Options struct {
	// LLMURL is an OpenAI-compatible chat-completions endpoint, or the
	// sentinel "stub" for the in-process fake. Empty disables the assistant.
	LLMURL   string
	LLMKey   string
	LLMModel string

	// SearchProvider is empty, "stub", or the name of a real backend. Empty
	// means the agent runs without web search: a worse assistant, but a
	// working one.
	SearchProvider string
	SearchKey      string
	SearchURL      string

	// Geocoder resolves a proposed address to coordinates. Nil means the
	// agent proposes an address with no coordinates rather than guessing
	// them -- which is the right failure, since the alternative is a
	// plausible position that is wrong only on the map.
	Geocoder *geocode.Client

	// Limits are the guard rails on a run. Any field left zero takes its
	// default from DefaultLimits, so a caller that does not care passes the
	// zero value and gets the shipped behaviour.
	Limits Limits
}

// LLMStub is the CARAVEL_LLM_URL sentinel selecting the in-process fake
// provider rather than a real endpoint. Duplicated from config rather than
// imported, so this package does not depend on the app's configuration
// package for one string.
const LLMStub = "stub"

// New builds the assistant, or returns nil if it is not configured.
//
// A nil Assistant and a nil error is a success: it means the operator did not
// turn this on. Callers must check for nil rather than assuming a non-nil
// result, which is why the doc comment says so twice.
func New(opts Options) (Assistant, error) {
	if opts.LLMURL == "" {
		return nil, nil
	}

	var p provider
	// The stub answers with URLs on a loopback fixture host it starts itself,
	// so the fetcher it is paired with has to be allowed to reach exactly that
	// one address -- and nothing else, including every other loopback address.
	// See stub_fixture.go for what this buys and what it costs.
	fetcher := newPageFetcher()
	if opts.LLMURL == LLMStub {
		p = newStubProvider()
		fetcher = newFetcherAllowing(startStubFixture().addr)
	} else {
		p = newHTTPProvider(opts.LLMURL, opts.LLMKey, opts.LLMModel)
	}

	// An unknown provider name is a startup error rather than a silent
	// downgrade to "no search": config.Load has already validated it, so
	// reaching here with something else means the two lists have drifted.
	search, err := newSearcher(opts)
	if err != nil {
		return nil, err
	}

	// Defaults filled and the combination checked here rather than at first
	// use, so a nonsensical setting is a startup failure naming the problem
	// instead of a run that behaves strangely once somebody presses the
	// button.
	limits := opts.Limits.withDefaults()
	if err := limits.validate(); err != nil {
		return nil, err
	}

	return &Agent{
		opts:     opts,
		provider: p,
		search:   search,
		fetcher:  fetcher,
		geocoder: opts.Geocoder,
		limits:   limits,
	}, nil
}

// Agent is the open-ended tool-calling loop. Milestone 4 fills in Propose.
type Agent struct {
	opts     Options
	provider provider
	search   Searcher
	fetcher  *pageFetcher
	geocoder *geocode.Client
	limits   Limits
}

// Limits reports the effective guard rails, for the startup log.
func (a *Agent) Limits() Limits { return a.limits }

// assistUserAgent identifies our outbound requests to the sites the agent
// reads. Naming ourselves is the polite half of scraping and the useful half
// of being blocked: an operator who sees this in their logs can tell what it
// is and complain to the right project.
func assistUserAgent() string {
	return "Caravel/" + buildinfo.Version + " (self-hosted trip planner; +assistant)"
}
