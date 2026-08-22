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

const (
	// maxRunDuration bounds the whole run. Generous because the run is
	// genuinely slow -- several searches and page reads -- and because the
	// user is watching a progress line rather than a frozen spinner. The
	// client can cancel sooner; this is the backstop for when nobody does.
	maxRunDuration = 2 * time.Minute
	// maxTurns bounds the conversation. The backstop for a model that loops
	// cheaply: a server reporting no token usage makes the budget below
	// useless, and this still ends the run.
	maxTurns = 12
	// maxToolCalls bounds the work, since one turn may request several calls.
	maxToolCalls = 20
	// maxTokens is the budget per run, checked between turns. Enough for a
	// dozen turns carrying a few pages of context; small enough that a
	// pathological run costs cents rather than dollars.
	maxTokens = 60000
	// linkCheckTimeout bounds one liveness check. Short: these run in
	// parallel at the very end, and a slow server should not hold up a
	// proposal that is otherwise ready.
	linkCheckTimeout = 5 * time.Second
)

// Errors a caller may want to distinguish. The HTTP layer maps these to
// something the user can act on -- "it took too long, try again" reads very
// differently from "the model endpoint is misconfigured".
var (
	ErrTimedOut        = errors.New("assist: the run took too long and was stopped")
	ErrBudgetExhausted = errors.New("assist: the run reached its token budget and was stopped")
)

func (a *Agent) Propose(ctx context.Context, req Request, events func(Event)) (*Proposal, error) {
	if events == nil {
		events = func(Event) {}
	}
	if !req.Mode.Valid() {
		return nil, fmt.Errorf("assist: unknown mode %q", req.Mode)
	}

	ctx, cancel := context.WithTimeout(ctx, maxRunDuration)
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

	for turn := 0; turn < maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return nil, runContextError(err)
		}
		if spent.TotalTokens >= maxTokens {
			return nil, ErrBudgetExhausted
		}

		resp, err := a.provider.Complete(ctx, chatRequest{Messages: messages, Tools: defs})
		if err != nil {
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
			if toolCalls >= maxToolCalls {
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
				Content:    tools.dispatch(ctx, call),
			})
			toolCalls++
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, runContextError(err)
	}

	events(Event{Key: "assist.progress.composing"})

	messages = append(messages, chatMessage{Role: roleUser, Content: finalPrompt(req)})

	var raw modelProposal
	used, err := completeJSON(ctx, a.provider, chatRequest{Messages: messages, Format: proposalFormat()}, &raw)
	spent = addUsage(spent, used)
	if err != nil {
		return nil, wrapRunError(err)
	}

	return a.buildProposal(ctx, req, raw, tools.Sources(), events)
}

// runContextError turns a cancelled or expired context into something a caller
// can tell apart. context.Canceled is passed through unchanged, because
// "the user pressed Cancel" is not an error condition to report.
func runContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimedOut
	}
	return err
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
