package assist

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// The agent loop.
//
// Open-ended rather than a fixed sequence: the model decides what to search
// for, what to read and when it has enough, which is what produces a better
// answer than "search once, read the first result, summarise". The cost of
// that choice is that the loop has no natural end, so the guard rails below
// are a hard requirement rather than a nicety -- an unbounded loop against a
// metered API is a bug in any design.
//
// # Structure
//
// Two phases, deliberately separated. First a plain tool-calling conversation
// with no response format; then, once the model stops asking for tools, one
// final turn that asks for the structured answer.
//
// Combining the two -- offering tools *and* a strict json_schema on every turn
// -- is the obvious shortcut and a compatibility minefield: several
// OpenAI-compatible servers constrain all output to the schema when one is
// set, which makes tool calls impossible, and others differ in whether the
// schema applies to the tool arguments too. The separation costs one extra
// billed turn per run and buys behaviour that does not depend on which server
// the operator pointed us at.
//
// # What is enforced here rather than asked for
//
// The prompt asks the model for a lot of things. Three of them are checked
// afterwards, because asking is not a guarantee:
//
//   - Coordinates are never taken from the model. It supplies an address and a
//     place name; internal/geocode resolves them.
//   - Category is validated against the enum, and a wrong value is dropped
//     rather than corrected -- guessing which of three the model meant is how
//     a hotel becomes a ferry terminal.
//   - Every proposed link is fetched before it is shown, and dead ones are
//     dropped. Hallucinated URLs are the classic failure of this feature, and
//     a dead link is worse than no link because it looks authoritative until
//     someone clicks it.

// Limits are the guard rails on one run. Every field has a default (see
// DefaultLimits) and every one is settable from the environment, because these
// are the numbers an operator needs to turn *quickly*: a model that reasons
// more per turn, a search backend returning fatter extracts, or simply a
// bill that came in higher than expected are all reasons to change one of
// these today rather than at the next release.
//
// They are not independent. MaxTokens and AnswerReserve are checked against
// each other at construction, because a reserve larger than the budget means
// gathering never starts at all -- a configuration that looks conservative and
// silently disables the feature.
type Limits struct {
	// RunDuration bounds the gathering phase. Not the whole call: composing
	// the answer runs outside it, so a run that hits this still reports what
	// it found. The client can cancel sooner; this is the backstop for when
	// nobody does.
	RunDuration time.Duration
	// MaxTurns bounds the conversation. The backstop for a model that loops
	// cheaply, and the only rail that works at all against a server which
	// reports no token usage -- with usage absent, Tokens below never fires.
	MaxTurns int
	// MaxToolCalls bounds the work, since one turn may request several calls.
	MaxToolCalls int
	// MaxTokens is the cumulative budget for a run, summed from what each
	// response reports. Note this counts *billed* tokens, not context size:
	// every turn resends the conversation, so a long run costs
	// superlinearly, which is why this is spent faster than it looks.
	MaxTokens int
	// AnswerReserve is held back from MaxTokens so there is always enough
	// left to compose. Gathering stops at the difference. Without it a run can
	// spend everything researching and have nothing left to say what it found,
	// which is exactly what the first live run did.
	AnswerReserve int
	// AnswerTimeout bounds the composing turn, which runs outside RunDuration.
	AnswerTimeout time.Duration
}

// DefaultLimits are the shipped values, tuned in Milestone 5 against a real
// model and a real search backend. They are the single source of truth: config
// parses overrides and leaves anything unset as zero, and withDefaults fills
// the gaps.
func DefaultLimits() Limits {
	return Limits{
		// Gathering only, and lowered from two minutes in Milestone 8. Every
		// good live run has had what it needed inside half a minute; past
		// that the model is usually hunting a detail it will not find, and
		// each extra page read makes the composing turn below slower as well
		// as costlier, because the whole conversation is resent.
		RunDuration:  90 * time.Second,
		MaxTurns:     12,
		MaxToolCalls: 20,
		// Sized against what the tools actually cost: a search returns six
		// extracts and a page read is capped at fetchMaxTextBytes, so twenty
		// tool calls is most of this. Raised from 60000 in Milestone 5, where
		// the first live run spent it in 75 seconds with nothing to show.
		MaxTokens:     120000,
		AnswerReserve: 20000,
		// Raised from 60s in Milestone 8, where a Serper run gathered a long
		// conversation and then failed *composing* it -- the whole history is
		// resent on that turn, so it is the slowest single request of the run
		// by a wide margin, and the one whose failure wastes everything before
		// it. It is bounded separately from gathering precisely so it can be
		// generous without extending the research.
		AnswerTimeout: 2 * time.Minute,
	}
}

// withDefaults fills unset fields. Zero means "not configured" rather than
// "zero", which is the only sane reading: a run with a zero token budget or
// zero turns is not a configuration anybody wants, and treating it as one
// would turn a forgotten variable into a silently disabled feature.
func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.RunDuration <= 0 {
		l.RunDuration = d.RunDuration
	}
	if l.MaxTurns <= 0 {
		l.MaxTurns = d.MaxTurns
	}
	if l.MaxToolCalls <= 0 {
		l.MaxToolCalls = d.MaxToolCalls
	}
	if l.MaxTokens <= 0 {
		l.MaxTokens = d.MaxTokens
	}
	if l.AnswerReserve <= 0 {
		l.AnswerReserve = d.AnswerReserve
	}
	if l.AnswerTimeout <= 0 {
		l.AnswerTimeout = d.AnswerTimeout
	}
	return l
}

// validate rejects combinations that would disable the feature rather than
// constrain it.
func (l Limits) validate() error {
	if l.AnswerReserve >= l.MaxTokens {
		// Names both numbers and the variable to change, because the usual
		// way to arrive here is lowering the budget alone and colliding with
		// the *default* reserve -- at which point "the reserve is too big" is
		// baffling to someone who never set a reserve. Deliberately not
		// scaled down silently: a budget quietly reinterpreted is worse than
		// a startup error that says what to do.
		return fmt.Errorf("assist: the answer reserve (%d) must be smaller than the token budget (%d), or gathering never starts -- lower CARAVEL_ASSIST_ANSWER_RESERVE alongside CARAVEL_ASSIST_MAX_TOKENS", l.AnswerReserve, l.MaxTokens)
	}
	return nil
}

// String renders the effective limits for the startup log, so "what is this
// instance actually running with" is answerable without reading the
// environment of a running process.
func (l Limits) String() string {
	return fmt.Sprintf("tokens=%d (reserve %d) turns=%d tools=%d run=%s answer=%s",
		l.MaxTokens, l.AnswerReserve, l.MaxTurns, l.MaxToolCalls, l.RunDuration, l.AnswerTimeout)
}

const (
	// linkCheckTimeout bounds one liveness check. Short: these run in
	// parallel at the very end, and a slow server should not hold up a
	// proposal that is otherwise ready. Not configurable -- it is a property
	// of "is this URL alive", not a budget anyone needs to tune.
	linkCheckTimeout = 5 * time.Second
)

// Errors a caller may want to distinguish. The HTTP layer maps these to
// something the user can act on -- "it took too long, try again" reads very
// differently from "the model endpoint is misconfigured".
var (
	// ErrTimedOut means the caller's own context expired, leaving nothing to
	// compose with. Note that the agent's *internal* gathering deadline does
	// not produce this: hitting it ends the research and the run still
	// answers. See the note in the loop.
	ErrTimedOut = errors.New("assist: the run took too long and was stopped")
	// ErrBudgetExhausted is reserved for a budget so small that composing is
	// impossible. The ordinary case -- gathering spends the budget -- stops
	// the research and answers with what was found.
	ErrBudgetExhausted = errors.New("assist: the run reached its token budget and was stopped")
)

func (a *Agent) Propose(ctx context.Context, req Request, events func(Event)) (*Proposal, error) {
	if events == nil {
		events = func(Event) {}
	}
	if !req.Mode.Valid() {
		return nil, fmt.Errorf("assist: unknown mode %q", req.Mode)
	}

	// Two contexts, deliberately. runCtx carries the gathering deadline;
	// userCtx is the caller's own, which only ends when the user cancels.
	// The composing turn below uses the latter, so a run that ran out of time
	// still gets to say what it found.
	userCtx := ctx
	runCtx, cancel := context.WithTimeout(userCtx, a.limits.RunDuration)
	defer cancel()

	// The stub plays a fixed script, so a second run in the same process would
	// find it exhausted. Real providers are stateless between runs and need
	// nothing here.
	if s, ok := a.provider.(*stubProvider); ok {
		s.reset()
	}

	tools := newToolset(a.search, a.fetcher, a.geocoder, events)
	defs := tools.definitions()

	messages := []chatMessage{
		{Role: roleSystem, Content: systemPrompt(req)},
		{Role: roleUser, Content: userPrompt(req)},
	}

	events(Event{Key: "assist.progress.thinking"})

	var spent usage
	toolCalls := 0

	// Why the rails break rather than return an error.
	//
	// The first live run against a real model spent its whole budget hunting
	// for one detail it was never going to find, and returned nothing after
	// seventy-five seconds. But by then it had read the official site, the
	// city guide and Wikipedia -- it knew the address, the type and plenty for
	// the notes. Throwing that away because a ceiling was reached is the wrong
	// trade: the user has already waited and the tokens are already spent, and
	// a partial proposal they can accept field by field is worth far more than
	// an apology. So a ceiling ends the *gathering*, and the run still
	// composes. Only the user cancelling stops it outright.
	turn := 0
	for ; turn < a.limits.MaxTurns; turn++ {
		// The caller's own context, not ours. If it is already done there is
		// nothing to compose with -- every remaining call would fail too.
		if err := userCtx.Err(); err != nil {
			return nil, wrapRunError(err)
		}
		if runCtx.Err() != nil {
			events(Event{Key: "assist.progress.wrappingUp"})
			break
		}
		if spent.TotalTokens >= a.limits.MaxTokens-a.limits.AnswerReserve {
			events(Event{Key: "assist.progress.wrappingUp"})
			break
		}

		resp, err := a.provider.Complete(runCtx, chatRequest{Messages: messages, Tools: defs})
		if err != nil {
			// A provider call that failed because gathering ran out of time is
			// not a failure of the run: compose with what is already here.
			if errors.Is(err, context.DeadlineExceeded) && userCtx.Err() == nil {
				events(Event{Key: "assist.progress.wrappingUp"})
				break
			}
			return nil, wrapRunError(err)
		}
		spent = addUsage(spent, resp.Usage)

		// No tool calls means the model is done gathering. Break to the
		// structured turn rather than treating this text as the answer: it is
		// prose, and the answer has a shape.
		if len(resp.ToolCalls) == 0 {
			break
		}

		// The assistant turn must be echoed back with its tool_calls intact,
		// or the results below have nothing to attach to and most servers
		// reject the request.
		messages = append(messages, chatMessage{Role: roleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})

		for _, call := range resp.ToolCalls {
			if toolCalls >= a.limits.MaxToolCalls {
				// Answered rather than dropped: a tool call with no result
				// leaves the conversation malformed. Telling the model it is
				// out of budget lets it write the answer with what it has.
				messages = append(messages, chatMessage{
					Role:       roleTool,
					ToolCallID: call.ID,
					Content:    "No more tool calls are available for this run. Answer with what you already know.",
				})
				continue
			}
			messages = append(messages, chatMessage{
				Role:       roleTool,
				ToolCallID: call.ID,
				Content:    tools.dispatch(runCtx, call),
			})
			toolCalls++
		}
	}

	if err := userCtx.Err(); err != nil {
		return nil, wrapRunError(err)
	}

	// The turn and tool-call ceilings break out silently above; say so here,
	// so every reason the research stopped short reaches the user the same way.
	if turn >= a.limits.MaxTurns || toolCalls >= a.limits.MaxToolCalls {
		events(Event{Key: "assist.progress.wrappingUp"})
	}

	events(Event{Key: "assist.progress.composing"})

	messages = append(messages, chatMessage{Role: roleUser, Content: finalPrompt(req)})

	// Detached from the gathering deadline, still bound by the user cancelling
	// and by a timeout of its own. This is the turn that turns a run into an
	// answer, so it must not be the thing the run deadline kills.
	finalCtx, cancelFinal := context.WithTimeout(userCtx, a.limits.AnswerTimeout)
	defer cancelFinal()

	var raw modelProposal
	used, err := completeJSON(finalCtx, a.provider, chatRequest{Messages: messages, Format: proposalFormat()}, &raw)
	spent = addUsage(spent, used)
	if err != nil {
		return nil, wrapRunError(err)
	}

	return a.buildProposal(finalCtx, req, raw, tools.Sources(), events)
}

// wrapRunError catches the case where a provider call failed *because* the run
// ran out of time, which arrives as a transport error wrapping the deadline
// rather than as a bare context error.
func wrapRunError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimedOut
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return err
}

// buildProposal turns what the model said into what the user is shown,
// applying the three checks the prompt cannot guarantee.
func (a *Agent) buildProposal(ctx context.Context, req Request, raw modelProposal, sources []Source, events func(Event)) (*Proposal, error) {
	p := &Proposal{Sources: sources}

	// Category: validated, never corrected. A wrong value is dropped, because
	// guessing which of three the model meant is how a hotel becomes a ferry
	// terminal.
	category := strings.ToLower(strings.TrimSpace(raw.Category))
	if !slices.Contains(validCategories, category) {
		category = ""
	}

	// Title is left alone when enriching something already named. The user
	// chose that name; renaming it is not enrichment, and proposing a
	// near-identical title on every run is noise that trains people to click
	// past the review.
	title := strings.TrimSpace(raw.Title)
	if req.Mode == ModeEnrich && strings.TrimSpace(req.Current.Title) != "" {
		title = ""
	}

	for _, f := range []struct{ name, current, proposed string }{
		{"title", req.Current.Title, title},
		{"category", req.Current.Category, category},
		{"type", req.Current.Type, strings.TrimSpace(raw.Type)},
		{"notes", req.Current.Notes, strings.TrimSpace(raw.Notes)},
		{"address", req.Current.Address, strings.TrimSpace(raw.Address)},
	} {
		// Nothing to say, or nothing new to say. An empty proposal is not a
		// request to clear the field: the feature never offers to delete what
		// somebody wrote.
		if f.proposed == "" || f.proposed == strings.TrimSpace(f.current) {
			continue
		}
		p.Fields = append(p.Fields, Field{Name: f.name, Current: f.current, Proposed: f.proposed})
	}

	p.Links = a.checkLinks(ctx, req, raw.Links, events)

	// Coordinates last, and only from the geocoder. The address the model
	// proposed is tried first, then the place name, which is more forgiving of
	// a street address that Nominatim does not recognise.
	if a.geocoder != nil {
		for _, query := range []string{strings.TrimSpace(raw.Address), strings.TrimSpace(raw.PlaceName)} {
			if query == "" {
				continue
			}
			results, err := a.geocoder.Search(ctx, query)
			if err != nil || len(results) == 0 {
				continue
			}
			lat, lng := results[0].Lat, results[0].Lng
			p.Lat, p.Lng = &lat, &lng
			break
		}
	}

	return p, nil
}

// validCategories mirrors the CHECK constraint on items.category and the map
// in internal/httpapi/items.go. Duplicated rather than shared because this
// package must not import the HTTP layer; the two are pinned together by
// TestValidCategoriesMatchTheSchema.
var validCategories = []string{"site", "stay", "transport"}

// checkLinks fetches every proposed link and keeps the ones that answer.
//
// In parallel, because a run has already taken half a minute and doing six
// sequential requests at the end is a visible pause for no reason. Links
// already on the location are dropped as duplicates rather than checked --
// proposing what is already there wastes the user's attention.
func (a *Agent) checkLinks(ctx context.Context, req Request, proposed []modelLink, events func(Event)) []Link {
	if len(proposed) == 0 {
		return nil
	}

	existing := make(map[string]bool, len(req.Current.Links))
	for _, l := range req.Current.Links {
		existing[normaliseURL(l.URL)] = true
	}

	type candidate struct {
		link Link
		ok   bool
	}

	seen := make(map[string]bool, len(proposed))
	candidates := make([]candidate, 0, len(proposed))
	for _, l := range proposed {
		u := strings.TrimSpace(l.URL)
		key := normaliseURL(u)
		if u == "" || existing[key] || seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, candidate{link: Link{URL: u, Label: strings.TrimSpace(l.Label)}})
	}
	if len(candidates) == 0 {
		return nil
	}

	events(Event{Key: "assist.progress.checkingLinks"})

	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidates[i].ok = a.fetcher.LinkIsLive(ctx, candidates[i].link.URL)
		}(i)
	}
	wg.Wait()

	var out []Link
	for _, c := range candidates {
		if !c.ok {
			continue
		}
		if c.link.Label == "" {
			c.link.Label = hostOf(c.link.URL)
		}
		out = append(out, c.link)
	}
	return out
}

// normaliseURL is a deliberately shallow comparison key: enough to catch the
// same URL proposed twice or already present with a trailing slash, not an
// attempt at canonicalisation. Over-normalising would merge URLs that are
// genuinely different pages.
func normaliseURL(raw string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
}
