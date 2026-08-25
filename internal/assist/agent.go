package assist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// runCounter labels each run so the records of two concurrent ones can be told
// apart in a log that interleaves them. A counter rather than a timestamp or a
// random value: it is short, it sorts, and it says how many runs this process
// has served, which is itself worth knowing.
var runCounter atomic.Uint64

func (a *Agent) Propose(ctx context.Context, req Request, events func(Event)) (*Proposal, error) {
	if events == nil {
		events = func(Event) {}
	}
	if !req.Mode.Valid() {
		return nil, fmt.Errorf("assist: unknown mode %q", req.Mode)
	}

	// a.logger is set by New; tests that build an Agent literal leave it nil,
	// and a run must not panic on the way to its first log line.
	base := a.logger
	if base == nil {
		base = slog.Default()
	}
	log := base.With("run", runCounter.Add(1))
	started := time.Now()
	searchName := "none"
	if a.search != nil {
		searchName = a.search.Name()
	}
	log.Debug("assist: run started",
		"mode", string(req.Mode),
		"model", a.opts.LLMModel,
		"search", searchName,
		"geocoder", a.geocoder != nil,
		"trip_context", req.Trip.Sent(),
		"locale", req.Locale,
		"type_vocabulary", len(req.TypeVocabulary),
		"limits", a.limits.String())

	var spent usage
	toolCalls := 0
	turnsUsed := 0
	steps := 0

	// Every event that is a step is counted here rather than at each emission
	// site, so the total in the summary cannot drift from the list the client
	// actually received.
	emit := func(e Event) {
		if e.Kind == EventStep {
			steps++
		}
		events(e)
	}

	// The summary closes the run *however it ends*, which is why it is
	// deferred rather than written before the return. A run that timed out or
	// failed is exactly when somebody wants to see what it managed to do, and
	// an account that only appears on success is an account of the runs that
	// did not need explaining.
	defer func() {
		events(Event{
			Kind: EventSummary,
			Totals: Totals{
				DurationMS: time.Since(started).Milliseconds(),
				Steps:      steps,
				Turns:      turnsUsed,
				ToolCalls:  toolCalls,
				Tokens:     spent.TotalTokens,
			},
		})
	}()

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

	tools := newToolset(a.search, a.fetcher, a.geocoder, emit, log)
	defs := tools.definitions()

	messages := []chatMessage{
		{Role: roleSystem, Content: systemPrompt(req)},
		{Role: roleUser, Content: userPrompt(req)},
	}

	events(Event{Key: "assist.progress.thinking"})

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
	// Why the gathering stopped, for the one record at the end of the loop.
	// Set at each exit rather than reconstructed afterwards, because two of
	// these are indistinguishable once the loop has left: a run that used
	// eleven of twelve turns and a run that used twelve both have a turn count
	// and only one of them hit a ceiling.
	stop := "answered"

	// proposed is set when the model ended the run with a propose call, which
	// makes the composing request below unnecessary. Nil means the run ended
	// some other way -- no tool calls, a ceiling, a deadline -- and the
	// two-phase path still answers it.
	var proposed *modelProposal

	// turnsUsed (declared above, with the summary that reports it) counts turns
	// the provider actually answered, which is not the same as the loop
	// counter: a loop that breaks *inside* an iteration -- which the ordinary
	// "no more tool calls" exit does -- leaves the counter one behind the
	// number of turns that were billed. A trace whose job is accuracy must not
	// undercount the thing it is counting.
	turn := 0
	for ; turn < a.limits.MaxTurns; turn++ {
		// The caller's own context, not ours. If it is already done there is
		// nothing to compose with -- every remaining call would fail too.
		if err := userCtx.Err(); err != nil {
			log.Debug("assist: run abandoned", "reason", "cancelled", "turns", turnsUsed, "ms", time.Since(started).Milliseconds())
			return nil, wrapRunError(err)
		}
		if runCtx.Err() != nil {
			stop = "deadline"
			events(Event{Key: "assist.progress.wrappingUp"})
			break
		}
		if spent.TotalTokens >= a.limits.MaxTokens-a.limits.AnswerReserve {
			stop = "budget"
			events(Event{Key: "assist.progress.wrappingUp"})
			break
		}

		turnStarted := time.Now()
		emit(Event{Key: "assist.progress.thinking"})
		resp, err := a.provider.Complete(runCtx, chatRequest{Messages: messages, Tools: defs})
		if err != nil {
			// A provider call that failed because gathering ran out of time is
			// not a failure of the run: compose with what is already here.
			if errors.Is(err, context.DeadlineExceeded) && userCtx.Err() == nil {
				stop = "deadline"
				log.Debug("assist: turn timed out",
					"turn", turn+1, "ms", time.Since(turnStarted).Milliseconds())
				events(Event{Key: "assist.progress.wrappingUp"})
				break
			}
			// Not logged here: the HTTP layer records the provider error at
			// error level, and doing it twice would put the same failure in
			// the log under two different messages.
			return nil, wrapRunError(err)
		}
		spent = addUsage(spent, resp.Usage)
		turnsUsed++
		emit(Event{
			Kind:       EventStep,
			Key:        "assist.step.thinking",
			DurationMS: time.Since(turnStarted).Milliseconds(),
		})

		log.Debug("assist: turn",
			"turn", turnsUsed,
			"ms", time.Since(turnStarted).Milliseconds(),
			"finish", resp.FinishReason,
			"prompt_tokens", resp.Usage.PromptTokens,
			"completion_tokens", resp.Usage.CompletionTokens,
			"spent_tokens", spent.TotalTokens,
			"tool_calls", len(resp.ToolCalls))

		// No tool calls means the model is done gathering. Break to the
		// structured turn rather than treating this text as the answer: it is
		// prose, and the answer has a shape.
		if len(resp.ToolCalls) == 0 {
			break
		}

		// A propose call ends the run: its arguments *are* the answer, so
		// there is no second request to compose one. Checked before the
		// dispatch loop because everything else in this turn is moot -- the
		// model has said it is finished.
		//
		// A call whose arguments do not decode is answered rather than fatal,
		// the same way every other tool failure is: the model is told what was
		// wrong and gets to try again, bounded by the turn and tool-call
		// ceilings. That path is rare and costs exactly what the old flow cost
		// every single time.
		if call, ok := findCall(resp.ToolCalls, toolPropose); ok {
			var raw modelProposal
			if err := decodeJSONAnswer(call.Function.Arguments, &raw); err == nil {
				log.Debug("assist: proposed by tool call",
					"turn", turnsUsed,
					"ms", time.Since(started).Milliseconds(),
					"spent_tokens", spent.TotalTokens)
				emit(Event{
					Kind:       EventStep,
					Key:        "assist.step.composing",
					DurationMS: time.Since(turnStarted).Milliseconds(),
				})
				proposed = &raw
				stop = "proposed"
				break
			} else {
				log.Debug("assist: propose call rejected", "turn", turnsUsed, "err", err)
				messages = append(messages, chatMessage{Role: roleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})
				messages = append(messages, chatMessage{
					Role:       roleTool,
					ToolCallID: call.ID,
					Content: "That did not work: " + err.Error() +
						". Call propose again with arguments matching the schema.",
				})
				toolCalls++
				continue
			}
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
		log.Debug("assist: run abandoned", "reason", "cancelled", "turns", turnsUsed, "ms", time.Since(started).Milliseconds())
		return nil, wrapRunError(err)
	}

	// The turn and tool-call ceilings break out silently above; say so here,
	// so every reason the research stopped short reaches the user the same way.
	if turn >= a.limits.MaxTurns || toolCalls >= a.limits.MaxToolCalls {
		if stop == "answered" {
			if turn >= a.limits.MaxTurns {
				stop = "turn_ceiling"
			} else {
				stop = "tool_call_ceiling"
			}
		}
		events(Event{Key: "assist.progress.wrappingUp"})
	}

	log.Debug("assist: gathering finished",
		"reason", stop,
		"turns", turnsUsed,
		"tool_calls", toolCalls,
		"spent_tokens", spent.TotalTokens,
		"ms", time.Since(started).Milliseconds())

	raw, err := a.answer(userCtx, req, messages, proposed, &spent, emit, log)
	if err != nil {
		return nil, err
	}

	p, err := a.buildProposal(userCtx, req, *raw, tools.Sources(), emit, log)
	if err != nil {
		return nil, err
	}

	log.Debug("assist: run finished",
		"ms", time.Since(started).Milliseconds(),
		"turns", turnsUsed,
		"tool_calls", toolCalls,
		"spent_tokens", spent.TotalTokens,
		"answered_by", map[bool]string{true: "propose", false: "composing"}[proposed != nil],
		"fields", len(p.Fields),
		"links", len(p.Links),
		"sources", len(p.Sources),
		"coordinates", p.Lat != nil,
		"cover", coverFrom(p.Cover))
	return p, nil
}

// wikipediaArticle recognises an article URL and pulls the language edition
// and the title out of it.
//
// Two callers' worth of value from one function: it keeps Wikipedia out of the
// og:image path, where the licence would be lost, and it recovers an article
// title from a link the model proposed when the model did not name one
// separately. The second is worth having because a model that finds the
// article well enough to link it has already done the hard part.
func wikipediaArticle(rawURL string) (lang, title string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".wikipedia.org") && host != "wikipedia.org" {
		return "", "", false
	}
	// /wiki/Article_Title, which is the only article form worth recognising --
	// index.php?title= exists but is not what anything links to.
	rest, found := strings.CutPrefix(u.EscapedPath(), "/wiki/")
	if !found || rest == "" {
		return "", "", false
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		return "", "", false
	}
	// Wikipedia writes spaces as underscores in a URL and the API wants them
	// either way, but the space form is what a person recognises in a log.
	decoded = strings.ReplaceAll(decoded, "_", " ")
	lang, _, _ = strings.Cut(host, ".")
	return lang, decoded, true
}

// coverFrom names the route a cover came from, for the log. "none" rather than
// an empty string, so a reader is not left wondering whether the field failed
// to render.
func coverFrom(c *Cover) string {
	if c == nil {
		return "none"
	}
	return c.From
}

// findCall returns the first call to the named tool.
func findCall(calls []toolCall, name string) (toolCall, bool) {
	for _, c := range calls {
		if c.Function.Name == name {
			return c, true
		}
	}
	return toolCall{}, false
}

// answer produces the structured result.
//
// When the model ended the run with a propose call there is nothing to do: the
// arguments were already decoded and this is a pass-through. Otherwise the
// original two-phase path runs -- a final request carrying the whole
// conversation plus an instruction to answer in the schema.
//
// Keeping the second path rather than replacing it matters: a model that never
// calls propose, or a server that mishandles a tool schema, still gets an
// answer by the route that has worked since Stage 16.
func (a *Agent) answer(userCtx context.Context, req Request, messages []chatMessage, proposed *modelProposal, spent *usage, emit func(Event), log *slog.Logger) (*modelProposal, error) {
	if proposed != nil {
		return proposed, nil
	}

	emit(Event{Key: "assist.progress.composing"})

	messages = append(messages, chatMessage{Role: roleUser, Content: finalPrompt(req)})

	// Detached from the gathering deadline, still bound by the user cancelling
	// and by a timeout of its own. This is the turn that turns a run into an
	// answer, so it must not be the thing the run deadline kills.
	finalCtx, cancelFinal := context.WithTimeout(userCtx, a.limits.AnswerTimeout)
	defer cancelFinal()

	var raw modelProposal
	composeStarted := time.Now()
	used, attempts, err := completeJSON(finalCtx, a.provider, chatRequest{Messages: messages, Format: proposalFormat()}, &raw)
	*spent = addUsage(*spent, used)
	// Logged either way. The composing turn resends the whole conversation, so
	// it is the slowest single request of a run by a wide margin and the one
	// whose failure wastes everything before it -- which makes "how long did
	// composing take" the first question to ask of a slow run.
	log.Debug("assist: composed",
		"ms", time.Since(composeStarted).Milliseconds(),
		"messages", len(messages),
		// 2 means the first answer did not decode and was sent back for
		// reshaping. Without this, a slow composing phase and a retried one
		// are indistinguishable -- and they want opposite fixes.
		"calls", attempts,
		"tokens", used.TotalTokens,
		"spent_tokens", spent.TotalTokens,
		"ok", err == nil)
	emit(Event{
		Kind:       EventStep,
		Key:        "assist.step.composing",
		DurationMS: time.Since(composeStarted).Milliseconds(),
		Failed:     err != nil,
	})
	if err != nil {
		return nil, wrapRunError(err)
	}
	return &raw, nil
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
func (a *Agent) buildProposal(ctx context.Context, req Request, raw modelProposal, sources []Source, events func(Event), log *slog.Logger) (*Proposal, error) {
	p := &Proposal{Sources: sources}

	// Category: validated, never corrected. A wrong value is dropped, because
	// guessing which of three the model meant is how a hotel becomes a ferry
	// terminal.
	category := strings.ToLower(strings.TrimSpace(raw.Category))
	if !slices.Contains(validCategories, category) {
		if category != "" {
			// Worth its own line: this is the model failing to follow an
			// instruction the prompt states plainly, and a run of these is a
			// prompt problem rather than a bad day.
			log.Debug("assist: category dropped", "proposed", category, "allowed", validCategories)
		}
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
			// Sizes rather than values: a run that proposes nothing and a run
			// that proposes what is already there look identical from the
			// outside, and the difference is the whole question.
			log.Debug("assist: field not proposed",
				"field", f.name,
				"reason", map[bool]string{true: "empty", false: "unchanged"}[f.proposed == ""])
			continue
		}
		log.Debug("assist: field proposed",
			"field", f.name, "overwrites", strings.TrimSpace(f.current) != "", "chars", len([]rune(f.proposed)))
		p.Fields = append(p.Fields, Field{Name: f.name, Current: f.current, Proposed: f.proposed})
	}

	p.Links = a.checkLinks(ctx, req, raw.Links, events, log)

	// Coordinates last, and only from the geocoder. The address the model
	// proposed is tried first, then the place name, which is more forgiving of
	// a street address that Nominatim does not recognise.
	if a.geocoder != nil {
		for _, from := range []struct{ source, query string }{
			{"address", strings.TrimSpace(raw.Address)},
			{"place_name", strings.TrimSpace(raw.PlaceName)},
		} {
			if from.query == "" {
				continue
			}
			results, err := a.geocoder.Search(ctx, from.query)
			if err != nil || len(results) == 0 {
				log.Debug("assist: geocode missed", "from", from.source, "query", from.query, "err", err)
				continue
			}
			lat, lng := results[0].Lat, results[0].Lng
			p.Lat, p.Lng = &lat, &lng
			// Which of the two answered matters: the address answering means
			// the model got the street right, and falling through to the place
			// name means it did not.
			log.Debug("assist: coordinates resolved", "from", from.source, "query", from.query, "matches", len(results))
			break
		}
	} else {
		log.Debug("assist: no coordinates", "reason", "no geocoder configured")
	}

	p.Cover = a.chooseCover(ctx, req, raw, p.Links, sources, log)

	return p, nil
}

// chooseCover picks a cover photograph, preferring the one with the best claim
// to being a picture of this place.
//
// The order is not arbitrary. An og:image from the page the agent proposed as
// the official link is the venue's own photograph of itself, chosen by the
// venue, on a page a person can go and look at. A Wikipedia lead image is
// curated to be a picture of the article's subject, which is nearly as good
// and covers the landmarks that have no useful site of their own. Nothing else
// is offered: a generic image search would mean the model choosing a
// photograph by the text around it, which it cannot see.
func (a *Agent) chooseCover(ctx context.Context, req Request, raw modelProposal, links []Link, sources []Source, log *slog.Logger) *Cover {
	// First choice: a page the run both read *and* proposed as a link. The
	// second condition matters -- an og:image from some aggregator the model
	// happened to open is not this place's photograph of itself.
	proposed := make(map[string]bool, len(links))
	for _, l := range links {
		proposed[normaliseURL(l.URL)] = true
	}
	for _, src := range sources {
		if src.Image == "" || !proposed[normaliseURL(src.URL)] {
			continue
		}
		// A Wikipedia article is not taken through this path, even though it
		// has a perfectly good og:image. That image is a Wikimedia photograph,
		// and Wikimedia photographs are freely licensed rather than
		// unencumbered -- nearly all of them require attribution, which an
		// og:image tag does not carry. Taking it here would store a picture
		// that needs a credit with no record of whose it is, which is the
		// exact failure the provenance columns exist to prevent.
		//
		// Found by a live run: a German landmark came back with an image from
		// de.wikipedia and both credit and licence empty. The article goes
		// through the API below instead, which knows the licence.
		if lang, title, ok := wikipediaArticle(src.URL); ok {
			log.Debug("assist: og:image skipped, it is a wikipedia article", "page", src.URL, "title", title)
			if strings.TrimSpace(raw.WikipediaTitle) == "" {
				raw.WikipediaTitle = title
				if strings.TrimSpace(req.Locale) == "" {
					req.Locale = lang
				}
			}
			continue
		}
		log.Debug("assist: cover from og:image", "image", src.Image, "page", src.URL)
		return &Cover{URL: src.Image, SourceURL: src.URL, From: "og"}
	}

	title := strings.TrimSpace(raw.WikipediaTitle)
	if title == "" {
		// The ordinary case for a hotel or a restaurant, and the prompt asks
		// for silence rather than a guess: a wrong article title produces a
		// good photograph of the wrong place, which looks entirely correct.
		log.Debug("assist: no cover", "reason", "no wikipedia title proposed")
		return nil
	}
	if a.wikimedia == nil {
		// Only reachable from a test that built an Agent literal: New always
		// sets one, because wikimedia.New falls back to a working default.
		log.Debug("assist: no cover", "reason", "no wikimedia client")
		return nil
	}

	// The user's own language, because the model was asked for a title from
	// that edition -- article titles are not translations of each other, so
	// looking a German title up in the English encyclopaedia simply misses.
	img, err := a.wikimedia.LeadImage(ctx, req.Locale, title)
	if err != nil {
		// Not fatal, and not worth telling the user about: a proposal without
		// a picture is the ordinary case anyway.
		log.Debug("assist: wikipedia lookup failed", "title", title, "lang", req.Locale, "err", err)
		return nil
	}
	if img.URL == "" {
		log.Debug("assist: no cover", "reason", "no lead image", "title", title, "lang", req.Locale)
		return nil
	}

	// The article page, not the file page, when both exist: it is the thing a
	// reader recognises, and the file page is one click from it.
	source := img.ArticleURL
	if source == "" {
		source = img.DescriptionURL
	}
	log.Debug("assist: cover from wikipedia",
		"title", title, "lang", req.Locale, "image", img.URL, "licence", img.Licence, "credited", img.Credit != "")
	return &Cover{
		URL:       img.URL,
		ThumbURL:  img.ThumbURL,
		SourceURL: source,
		Credit:    img.Credit,
		Licence:   img.Licence,
		From:      "wikipedia",
	}
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
func (a *Agent) checkLinks(ctx context.Context, req Request, proposed []modelLink, events func(Event), log *slog.Logger) []Link {
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
			if u != "" {
				log.Debug("assist: link skipped", "url", u, "reason", map[bool]string{true: "already on the location", false: "duplicate"}[existing[key]])
			}
			continue
		}
		seen[key] = true
		candidates = append(candidates, candidate{link: Link{URL: u, Label: strings.TrimSpace(l.Label)}})
	}
	if len(candidates) == 0 {
		return nil
	}

	events(Event{Key: "assist.progress.checkingLinks"})
	checkStarted := time.Now()

	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidates[i].ok = a.fetcher.LinkIsLive(ctx, candidates[i].link.URL)
		}(i)
	}
	wg.Wait()

	events(Event{
		Kind:       EventStep,
		Key:        "assist.step.checkingLinks",
		Params:     map[string]string{"count": strconv.Itoa(len(candidates))},
		DurationMS: time.Since(checkStarted).Milliseconds(),
	})

	var out []Link
	for _, c := range candidates {
		if !c.ok {
			// The classic failure of this feature is a hallucinated URL that
			// looks authoritative until somebody clicks it. Recording the drop
			// is how you find out that is happening rather than assuming the
			// model simply proposed no links.
			log.Debug("assist: link dropped", "url", c.link.URL, "reason", "did not answer")
			continue
		}
		log.Debug("assist: link kept", "url", c.link.URL)
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
