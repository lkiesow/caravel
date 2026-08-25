package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"caravel/internal/geocode"
	"caravel/internal/wikimedia"
)

// answerJSON builds a final-turn answer, so each test can vary the one field
// it is about without restating the whole shape.
func answerJSON(t *testing.T, mp modelProposal) string {
	t.Helper()
	b, err := json.Marshal(mp)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// agentWith wires an Agent around a scripted provider, with no search, no
// geocoder and no reachable network unless the test adds one.
func agentWith(turns ...stubTurn) *Agent {
	return &Agent{
		provider: newScriptedProvider(turns...),
		fetcher:  newPageFetcher(),
		limits:   DefaultLimits(),
	}
}

func enrichRequest() Request {
	return Request{
		Mode:    ModeEnrich,
		Current: Location{Title: "Kex Hostel"},
		Locale:  "en",
	}
}

// The two-phase shape: the loop gathers until a turn arrives with no tool
// calls, then asks for the answer as a separate structured turn.
func TestProposeRunsTheToolLoopThenAsksForTheAnswer(t *testing.T) {
	a := agentWith(
		turnCalling(toolWebSearch, `{"query":"Kex Hostel"}`),
		stubTurn{Content: "done gathering"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Type: "hostel", Notes: "A hostel."})},
	)
	a.search = &stubSearcher{}

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(p.Fields) == 0 {
		t.Fatal("no fields proposed")
	}
	names := fieldNames(p)
	if !names["category"] || !names["type"] || !names["notes"] {
		t.Errorf("fields = %v, want category, type and notes", names)
	}
}

func fieldNames(p *Proposal) map[string]bool {
	out := map[string]bool{}
	for _, f := range p.Fields {
		out[f.Name] = true
	}
	return out
}

func fieldNamed(p *Proposal, name string) (Field, bool) {
	for _, f := range p.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// The single most important guarantee in the feature. A plausible lat/lng 40km
// from the real hotel looks entirely correct in the form and is wrong only on
// the map, so the model is never allowed to supply one.
func TestCoordinatesComeFromTheGeocoderNotTheModel(t *testing.T) {
	var asked []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Query().Get("q"))
		fmt.Fprint(w, `[{"display_name":"Skulagata 28, Reykjavik","lat":"64.1466","lon":"-21.9426"}]`)
	}))
	defer upstream.Close()

	// The model tries to smuggle coordinates in through fields that have no
	// business carrying them. The schema has no lat/lng, so this is the shape
	// such an attempt would actually take.
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{
			Category:  "stay",
			Address:   "Skulagata 28, 101 Reykjavik",
			PlaceName: "Kex Hostel, Reykjavik",
		})},
	)
	a.geocoder = geocode.New(upstream.URL)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Lat == nil || p.Lng == nil {
		t.Fatal("no coordinates resolved")
	}
	if *p.Lat != 64.1466 || *p.Lng != -21.9426 {
		t.Errorf("coordinates = %v,%v want the geocoder's", *p.Lat, *p.Lng)
	}
	if len(asked) == 0 || asked[0] != "Skulagata 28, 101 Reykjavik" {
		t.Errorf("geocoder queries = %v, want the proposed address first", asked)
	}
}

// The address is tried first and the place name is the fallback, because a
// street address Nominatim does not recognise is common and a named place is
// more forgiving.
func TestCoordinatesFallBackToThePlaceName(t *testing.T) {
	var asked []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		asked = append(asked, q)
		if strings.Contains(q, "Nowhere") {
			fmt.Fprint(w, `[]`)
			return
		}
		fmt.Fprint(w, `[{"display_name":"Kex Hostel","lat":"64.1","lon":"-21.9"}]`)
	}))
	defer upstream.Close()

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Address: "12 Nowhere Street", PlaceName: "Kex Hostel, Reykjavik"})},
	)
	a.geocoder = geocode.New(upstream.URL)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Lat == nil {
		t.Fatal("no coordinates after the fallback")
	}
	if len(asked) != 2 {
		t.Errorf("geocoder queries = %v, want the address then the place name", asked)
	}
}

func TestNoCoordinatesWithoutAGeocoder(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Address: "Skulagata 28"})},
	)
	// Proposing an address with no position is the right failure. The
	// alternative is a plausible position that is wrong only on the map.
	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Lat != nil || p.Lng != nil {
		t.Error("coordinates appeared with no geocoder configured")
	}
	if _, ok := fieldNamed(p, "address"); !ok {
		t.Error("the address should still be proposed")
	}
}

// Dropped rather than corrected: guessing which of three the model meant is
// how a hotel becomes a ferry terminal.
func TestInvalidCategoryIsDroppedNotGuessed(t *testing.T) {
	for _, bad := range []string{"hotel", "Accommodation", "", "restaurant"} {
		t.Run("category "+bad, func(t *testing.T) {
			a := agentWith(
				stubTurn{Content: "done"},
				stubTurn{Content: answerJSON(t, modelProposal{Category: bad, Type: "hostel"})},
			)
			p, err := a.Propose(context.Background(), enrichRequest(), nil)
			if err != nil {
				t.Fatalf("Propose: %v", err)
			}
			if _, ok := fieldNamed(p, "category"); ok {
				t.Errorf("category %q was proposed, want it dropped", bad)
			}
			// The rest of the answer survives: one bad field is not a reason
			// to throw away a good run.
			if _, ok := fieldNamed(p, "type"); !ok {
				t.Error("the valid fields were dropped along with the invalid one")
			}
		})
	}
}

func TestValidCategoryIsAcceptedCaseInsensitively(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "STAY"})},
	)
	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	f, ok := fieldNamed(p, "category")
	if !ok || f.Proposed != "stay" {
		t.Errorf("category = %+v, want normalised to stay", f)
	}
}

// Pins the duplicated list against the schema's CHECK constraint and the map
// in internal/httpapi/items.go, which this package cannot import.
func TestValidCategoriesMatchTheSchema(t *testing.T) {
	want := []string{"site", "stay", "transport"}
	if len(validCategories) != len(want) {
		t.Fatalf("validCategories = %v, want %v", validCategories, want)
	}
	for i := range want {
		if validCategories[i] != want[i] {
			t.Errorf("validCategories = %v, want %v", validCategories, want)
		}
	}
}

// Hallucinated URLs are the classic failure, and a dead link is worse than no
// link because it looks authoritative until someone clicks it.
func TestDeadLinksAreDropped(t *testing.T) {
	var live, dead string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "gone") {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	live, dead = srv.URL+"/real", srv.URL+"/gone"

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Links: []modelLink{
			{URL: live, Label: "Official site"},
			{URL: dead, Label: "Hallucinated"},
		}})},
	)
	// Loopback again: the address policy is relaxed so the liveness check
	// itself is what gets tested. The guard has its own tests.
	a.fetcher = newRelaxedFetcher()

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(p.Links) != 1 {
		t.Fatalf("links = %+v, want only the live one", p.Links)
	}
	if p.Links[0].Label != "Official site" {
		t.Errorf("link = %+v", p.Links[0])
	}
}

// A meaningful minority of servers answer 405 to HEAD and serve the page fine
// on GET. Treating those as dead would silently drop working links from a
// whole class of sites.
func TestLinksAnsweringOnlyToGETSurvive(t *testing.T) {
	var sawGet atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		sawGet.Store(true)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Links: []modelLink{{URL: srv.URL + "/x"}}})},
	)
	a.fetcher = newRelaxedFetcher()

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(p.Links) != 1 {
		t.Fatalf("links = %+v, want the GET-only link kept", p.Links)
	}
	if !sawGet.Load() {
		t.Error("no GET fallback was attempted after the 405")
	}
	// No label from the model, so the host stands in rather than showing a
	// bare URL as the link text.
	if p.Links[0].Label == "" {
		t.Error("the link has no label")
	}
}

func TestLinksAlreadyPresentAreNotProposedAgain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	req := enrichRequest()
	// Trailing slash on one side: the comparison is shallow but has to catch
	// at least this, which is how the same URL usually differs.
	req.Current.Links = []Link{{URL: srv.URL + "/same/"}}

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Links: []modelLink{
			{URL: srv.URL + "/same"},
			{URL: srv.URL + "/new"},
		}})},
	)
	a.fetcher = newRelaxedFetcher()

	p, err := a.Propose(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(p.Links) != 1 || !strings.HasSuffix(p.Links[0].URL, "/new") {
		t.Errorf("links = %+v, want only the one not already present", p.Links)
	}
}

// The feature never offers to delete what somebody wrote, so an empty proposal
// is silence rather than a request to clear the field.
func TestEmptyAndUnchangedFieldsAreNotProposed(t *testing.T) {
	req := enrichRequest()
	req.Current.Type = "hostel"
	req.Current.Notes = "Already written by hand."

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{
			Category: "stay",
			Type:     "hostel", // identical to what is there
			Notes:    "",       // nothing found
		})},
	)
	p, err := a.Propose(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, ok := fieldNamed(p, "type"); ok {
		t.Error("an unchanged value was proposed")
	}
	if _, ok := fieldNamed(p, "notes"); ok {
		t.Error("an empty proposal was offered as a change; it would clear the field")
	}
}

// A field with existing content is the case the UI must show as a
// before/after, so the current value has to survive into the proposal.
func TestOverwritingFieldsCarryTheirCurrentValue(t *testing.T) {
	req := enrichRequest()
	req.Current.Notes = "My own notes."

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Notes: "Something the model found."})},
	)
	p, err := a.Propose(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	f, ok := fieldNamed(p, "notes")
	if !ok {
		t.Fatal("notes were not proposed")
	}
	if f.Current != "My own notes." {
		t.Errorf("Current = %q, want the existing text for the before/after", f.Current)
	}
	if !f.Overwrites() {
		t.Error("Overwrites() = false on a field with existing content")
	}
}

// The user chose the name; renaming is not enrichment, and proposing a
// near-identical title every run trains people to click past the review.
func TestTitleIsLeftAloneWhenEnrichingANamedLocation(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Title: "KEX Hostel Reykjavík", Category: "stay"})},
	)
	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, ok := fieldNamed(p, "title"); ok {
		t.Error("a title was proposed for a location that already has one")
	}
}

func TestTitleIsProposedInPromptMode(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Title: "Kex Hostel", Category: "stay"})},
	)
	p, err := a.Propose(context.Background(), Request{Mode: ModePrompt, Prompt: "a cheap hostel in Reykjavik"}, nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	f, ok := fieldNamed(p, "title")
	if !ok || f.Proposed != "Kex Hostel" {
		t.Errorf("title = %+v, want it proposed when building from scratch", f)
	}
}

// --- Guard rails ---

func TestRunStopsAtTheTurnCeiling(t *testing.T) {
	// A model that calls a tool forever. Without the ceiling this never ends.
	turns := make([]stubTurn, 40)
	for i := range turns {
		turns[i] = turnCalling(toolWebSearch, `{"query":"again"}`)
	}
	a := agentWith(turns...)
	a.search = &stubSearcher{}

	// It reaches the structured turn and fails there, having stopped looping.
	// What matters is that it stops at all, and quickly.
	done := make(chan struct{})
	go func() {
		_, _ = a.Propose(context.Background(), enrichRequest(), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the loop did not stop at the turn ceiling")
	}
}

func TestRunStopsAtTheToolCallCeiling(t *testing.T) {
	// One turn asking for far more calls than the budget allows.
	var calls []toolCall
	for i := range 40 {
		c := callTo(toolWebSearch, `{"query":"x"}`)
		c.ID = fmt.Sprintf("call_%d", i)
		calls = append(calls, c)
	}
	counting := &countingSearcher{}
	a := agentWith(
		stubTurn{ToolCalls: calls},
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})},
	)
	a.search = counting

	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if got := counting.n.Load(); got > int32(DefaultLimits().MaxToolCalls) {
		t.Errorf("%d tool calls ran, want at most %d", got, DefaultLimits().MaxToolCalls)
	}
	if counting.n.Load() == 0 {
		t.Error("no tool calls ran at all")
	}
}

type countingSearcher struct{ n atomic.Int32 }

func (*countingSearcher) Name() string { return "counting" }
func (c *countingSearcher) Search(context.Context, string) ([]SearchResult, error) {
	c.n.Add(1)
	return []SearchResult{{Title: "t", URL: "https://example.invalid/x", Snippet: "s"}}, nil
}

// Spending the budget ends the research, not the run. The first live run
// against a real model burned its whole budget hunting one detail and returned
// nothing after 75 seconds, having already read the official site and
// Wikipedia. The tokens are spent and the user has waited either way, so a
// partial proposal beats an apology.
func TestSpendingTheBudgetStillProducesAProposal(t *testing.T) {
	expensive := newScriptedProvider(
		stubTurn{ToolCalls: []toolCall{callTo(toolWebSearch, `{"query":"x"}`)}},
		// Never reached: the budget check fires before the second turn, and
		// the run jumps to composing.
		stubTurn{ToolCalls: []toolCall{callTo(toolWebSearch, `{"query":"never"}`)}},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Type: "hostel"})},
	)
	a := &Agent{provider: &greedyProvider{inner: expensive}, fetcher: newPageFetcher(), search: &stubSearcher{}, limits: DefaultLimits()}

	var keys []string
	p, err := a.Propose(context.Background(), enrichRequest(), func(e Event) { keys = append(keys, e.Key) })
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, ok := fieldNamed(p, "type"); !ok {
		t.Error("no proposal came back from a budget-limited run")
	}
	// The user should be told the run is cutting its research short rather
	// than left wondering why the answer is thin.
	if !slices.Contains(keys, "assist.progress.wrappingUp") {
		t.Errorf("events = %v, want a wrapping-up event", keys)
	}
}

// The gathering deadline behaves the same way: research stops, the run still
// answers.
func TestGatheringDeadlineStillProducesAProposal(t *testing.T) {
	slow := newScriptedProvider(
		stubTurn{ToolCalls: []toolCall{callTo(toolWebSearch, `{"query":"x"}`)}},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Type: "hostel"})},
	)
	a := &Agent{provider: &expiringProvider{inner: slow}, fetcher: newPageFetcher(), search: &stubSearcher{}, limits: DefaultLimits()}

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, ok := fieldNamed(p, "type"); !ok {
		t.Error("no proposal came back from a time-limited run")
	}
}

// expiringProvider reports a gathering-deadline failure once, the way a real
// provider call does when the run clock runs out mid-request.
type expiringProvider struct {
	inner provider
	done  bool
}

func (e *expiringProvider) Complete(ctx context.Context, req chatRequest) (*chatResponse, error) {
	if !e.done {
		e.done = true
		return nil, fmt.Errorf("post: %w", context.DeadlineExceeded)
	}
	return e.inner.Complete(ctx, req)
}

// greedyProvider reports the entire budget spent on its first turn.
type greedyProvider struct{ inner provider }

func (g *greedyProvider) Complete(ctx context.Context, req chatRequest) (*chatResponse, error) {
	resp, err := g.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Usage = usage{TotalTokens: DefaultLimits().MaxTokens + 1}
	return resp, nil
}

func TestRunHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := agentWith(
		stubTurn{ToolCalls: []toolCall{callTo(toolWebSearch, `{"query":"x"}`)}},
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})},
	)
	a.search = &cancellingSearcher{cancel: cancel}

	_, err := a.Propose(ctx, enrichRequest(), nil)
	// The user pressing Cancel is not an error to dress up; it is passed
	// through as context.Canceled so the transport can tell the two apart.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

type cancellingSearcher struct{ cancel context.CancelFunc }

func (*cancellingSearcher) Name() string { return "cancelling" }
func (c *cancellingSearcher) Search(context.Context, string) ([]SearchResult, error) {
	c.cancel()
	return nil, nil
}

// The caller's *own* expired context is different from the agent's internal
// gathering deadline: there is nothing left to compose with, because every
// remaining call would fail too.
func TestTheCallersExpiredDeadlineIsReportedAsATimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	a := agentWith(stubTurn{Content: "done"}, stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})})
	_, err := a.Propose(ctx, enrichRequest(), nil)
	if !errors.Is(err, ErrTimedOut) {
		t.Fatalf("err = %v, want ErrTimedOut", err)
	}
}

func TestUnknownModeIsRejected(t *testing.T) {
	a := agentWith(stubTurn{Content: "{}"})
	if _, err := a.Propose(context.Background(), Request{Mode: "improvise"}, nil); err == nil {
		t.Error("Propose accepted an unknown mode")
	}
}

// --- Progress and sources ---

func TestProgressEventsAreEmittedThroughTheRun(t *testing.T) {
	var keys []string
	a := agentWith(
		turnCalling(toolWebSearch, `{"query":"Kex"}`),
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})},
	)
	a.search = &stubSearcher{}

	if _, err := a.Propose(context.Background(), enrichRequest(), func(e Event) {
		keys = append(keys, e.Key)
	}); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	// Without these the UI is a frozen spinner for half a minute.
	if len(keys) < 2 {
		t.Fatalf("events = %v, want several", keys)
	}
	for _, k := range keys {
		// The summary carries no key; everything else is one, and never a
		// sentence.
		if k == "" {
			continue
		}
		if !strings.HasPrefix(k, "assist.progress.") && !strings.HasPrefix(k, "assist.step.") {
			t.Errorf("event key %q is not an i18n key", k)
		}
	}
}

// The trace the editor renders is built entirely from these events, so their
// shape is a contract: a step for each thing the run did, and exactly one
// summary, last.
func TestStepAndSummaryEventsCloseOutARun(t *testing.T) {
	var events []Event
	a := agentWith(
		turnCalling(toolWebSearch, `{"query":"Kex"}`),
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})},
	)
	a.search = &stubSearcher{}

	if _, err := a.Propose(context.Background(), enrichRequest(), func(e Event) {
		events = append(events, e)
	}); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	var steps []Event
	var summaries []Event
	for _, e := range events {
		switch e.Kind {
		case EventStep:
			steps = append(steps, e)
		case EventSummary:
			summaries = append(summaries, e)
		}
	}

	if len(summaries) != 1 {
		t.Fatalf("got %d summary events, want exactly one", len(summaries))
	}
	if events[len(events)-1].Kind != EventSummary {
		t.Errorf("last event is %+v, want the summary", events[len(events)-1])
	}

	// Two model turns and one search, at least.
	if len(steps) < 3 {
		t.Errorf("steps = %+v, want one per turn and per tool call", steps)
	}
	// The count in the heading has to match the list under it, or the trace
	// contradicts itself on its own first line.
	if got := summaries[0].Totals.Steps; got != len(steps) {
		t.Errorf("summary counts %d steps, %d were sent", got, len(steps))
	}
	if summaries[0].Totals.Turns != 2 {
		t.Errorf("turns = %d, want 2", summaries[0].Totals.Turns)
	}
	if summaries[0].Totals.ToolCalls != 1 {
		t.Errorf("tool calls = %d, want 1", summaries[0].Totals.ToolCalls)
	}
	if summaries[0].Totals.Tokens == 0 {
		t.Error("the summary reports no tokens; the stub reports usage")
	}
}

// A run that failed is exactly when somebody wants to see what it managed to
// do, so the summary is deferred rather than written on the success path.
func TestASummaryClosesAFailedRunToo(t *testing.T) {
	var events []Event
	a := agentWith(stubTurn{Err: errors.New("the provider fell over")})

	if _, err := a.Propose(context.Background(), enrichRequest(), func(e Event) {
		events = append(events, e)
	}); err == nil {
		t.Fatal("Propose succeeded; this run is meant to fail")
	}

	if len(events) == 0 || events[len(events)-1].Kind != EventSummary {
		t.Fatalf("events = %+v, want a summary last", events)
	}
}

func TestNilEventCallbackIsAllowed(t *testing.T) {
	// The interface says events may be nil; a nil-check bug here would panic
	// in exactly the callers that do not care about progress.
	a := agentWith(stubTurn{Content: "done"}, stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})})
	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
}

func TestTheDefaultStubScriptRunsEndToEnd(t *testing.T) {
	// The exact path CI and the Playwright suite take. If this breaks, the
	// UI milestone has nothing to develop against.
	a, err := New(Options{LLMURL: LLMStub, LLMModel: "stub", SearchProvider: "stub"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(p.Fields) == 0 {
		t.Error("the stub run proposed nothing")
	}

	// The two lists the browser suite could never see before the stub grew a
	// fixture host: a link that survived the liveness check, and the pages the
	// run actually read. Both were empty against example.invalid -- correctly,
	// which is what made the gap awkward -- and a bug in the sources list
	// shipped because of it.
	if len(p.Links) == 0 {
		t.Error("the stub run proposed no live link; the fixture host is what makes one possible")
	}
	if len(p.Sources) < 2 {
		t.Errorf("the stub run recorded %d source(s), want the two pages it reads", len(p.Sources))
	}
	for _, src := range p.Sources {
		if src.Title == "" || src.Title == "(untitled)" {
			t.Errorf("source %q has no usable title; the fixture pages carry one", src.URL)
		}
	}

	// And again, to prove the script rewinds: a second enrichment in the same
	// process must not find an exhausted stub.
	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("second Propose: %v", err)
	}
}

// --- Configurable limits ---

// Zero means "not configured", not "zero". A run with no turns or no token
// budget is not a configuration anybody wants, and reading it as one would
// turn a forgotten variable into a silently disabled feature.
func TestLimitsFillFromDefaults(t *testing.T) {
	got := Limits{}.withDefaults()
	if got != DefaultLimits() {
		t.Errorf("withDefaults() on the zero value = %+v, want the defaults", got)
	}

	// A set field survives, and only that field.
	partial := Limits{MaxTokens: 5000}.withDefaults()
	if partial.MaxTokens != 5000 {
		t.Errorf("MaxTokens = %d, want the override kept", partial.MaxTokens)
	}
	if partial.MaxTurns != DefaultLimits().MaxTurns {
		t.Errorf("MaxTurns = %d, want the default", partial.MaxTurns)
	}
}

// A reserve at or above the budget means gathering never starts: the feature
// looks configured and does nothing. That is a startup error, not a run that
// behaves oddly once somebody presses the button.
func TestLimitsRejectAReserveThatSwallowsTheBudget(t *testing.T) {
	for _, l := range []Limits{
		{MaxTokens: 1000, AnswerReserve: 1000},
		{MaxTokens: 1000, AnswerReserve: 5000},
	} {
		if err := l.withDefaults().validate(); err == nil {
			t.Errorf("validate() accepted %+v", l)
		}
	}
	if err := (Limits{MaxTokens: 5000, AnswerReserve: 1000}).withDefaults().validate(); err != nil {
		t.Errorf("validate() rejected a sane pair: %v", err)
	}
}

func TestNewRejectsIncoherentLimits(t *testing.T) {
	_, err := New(Options{
		LLMURL: LLMStub, LLMModel: "stub",
		Limits: Limits{MaxTokens: 1000, AnswerReserve: 2000},
	})
	if err == nil {
		t.Fatal("New accepted a reserve larger than the budget")
	}
	if !strings.Contains(err.Error(), "reserve") {
		t.Errorf("error = %v, want it to name the reserve", err)
	}
}

// The rails have to actually follow the configured values, not the constants
// they replaced.
func TestConfiguredLimitsAreTheOnesEnforced(t *testing.T) {
	// Two turns allowed, and a script that would loop for ten.
	// Exactly the two turns the limit allows, then the answer. A third tool
	// turn here would be consumed by the composing request instead, which is
	// how this test first failed -- worth stating, since the script position
	// of the answer depends on the limit being tested.
	turns := make([]stubTurn, 2)
	for i := range turns {
		turns[i] = turnCalling(toolWebSearch, `{"query":"again"}`)
	}
	counting := &countingSearcher{}
	a := &Agent{
		provider: newScriptedProvider(append(turns, stubTurn{Content: answerJSON(t, modelProposal{Category: "stay"})})...),
		fetcher:  newPageFetcher(),
		search:   counting,
		limits:   Limits{MaxTurns: 2, MaxToolCalls: 3}.withDefaults(),
	}

	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	// Two turns of one call each, so the turn ceiling binds first.
	if got := counting.n.Load(); got != 2 {
		t.Errorf("%d tool calls ran, want 2 from a two-turn limit", got)
	}
}

// Lowering the budget alone collides with the default reserve, and the error
// has to name the variable to change -- "the reserve is too big" is baffling
// to someone who never set a reserve.
func TestLoweringTheBudgetAloneIsRefusedHelpfully(t *testing.T) {
	_, err := New(Options{LLMURL: LLMStub, LLMModel: "stub", Limits: Limits{MaxTokens: 7777}})
	if err == nil {
		t.Fatal("New accepted a budget smaller than the default reserve")
	}
	if !strings.Contains(err.Error(), "CARAVEL_ASSIST_ANSWER_RESERVE") {
		t.Errorf("error = %v, want it to name the variable to change", err)
	}
}

func TestAgentReportsItsEffectiveLimits(t *testing.T) {
	a, err := New(Options{LLMURL: LLMStub, LLMModel: "stub", Limits: Limits{MaxTokens: 7777, AnswerReserve: 1000}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := a.(*Agent).Limits()
	if got.MaxTokens != 7777 || got.AnswerReserve != 1000 {
		t.Errorf("limits = %+v, want the overrides", got)
	}
	// The startup log prints this; it should name the numbers an operator
	// would want to check.
	if s := got.String(); !strings.Contains(s, "7777") || !strings.Contains(s, "turns=") {
		t.Errorf("String() = %q", s)
	}
}

// --- The answer as a tool call (Milestone 4a) ---

// proposeCall scripts a turn that ends the run by calling propose.
func proposeCall(t *testing.T, p modelProposal) stubTurn {
	t.Helper()
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("encode propose arguments: %v", err)
	}
	return turnCalling(toolPropose, string(encoded))
}

// The point of the whole milestone: a run that ends with propose makes no
// composing request at all. Before this, the model spent a round trip saying
// "I have enough" and the answer took a second one.
func TestAProposeCallAnswersWithoutASecondRequest(t *testing.T) {
	a := agentWith(
		turnCalling(toolWebSearch, `{"query":"Kex"}`),
		proposeCall(t, modelProposal{Category: "stay", Type: "hostel", Notes: "A hostel."}),
		// Deliberately scripted but unreachable: if the loop still asks for a
		// composing turn, this answers it and the test below catches the extra
		// request rather than mysteriously passing.
		stubTurn{Content: answerJSON(t, modelProposal{Category: "site", Notes: "SHOULD NOT BE USED"})},
	)
	a.search = &stubSearcher{}

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	// The third scripted turn must be untouched.
	stub := a.provider.(*stubProvider)
	if stub.n != 2 {
		t.Errorf("the provider was called %d times, want 2 -- a composing request was still made", stub.n)
	}
	for _, f := range p.Fields {
		if strings.Contains(f.Proposed, "SHOULD NOT BE USED") {
			t.Fatalf("the answer came from the composing turn, not the propose call: %+v", p.Fields)
		}
	}
	if len(p.Fields) == 0 {
		t.Error("the propose call produced no fields")
	}
}

// The fallback has to keep working, or a model that ignores propose -- or a
// server that mishandles a tool schema -- loses the feature entirely.
func TestARunWithNoProposeCallStillComposes(t *testing.T) {
	a := agentWith(
		turnCalling(toolWebSearch, `{"query":"Kex"}`),
		stubTurn{Content: "I have enough."},
		stubTurn{Content: answerJSON(t, modelProposal{Category: "stay", Notes: "From the composing turn."})},
	)
	a.search = &stubSearcher{}

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	var notes string
	for _, f := range p.Fields {
		if f.Name == "notes" {
			notes = f.Proposed
		}
	}
	if notes != "From the composing turn." {
		t.Errorf("notes = %q, want the two-phase path to have answered", notes)
	}
}

// Arguments that do not decode are answered rather than fatal, exactly as
// every other tool failure is. The model is told what was wrong and gets to
// try again -- a rare path that costs what the old flow cost every time.
func TestAMalformedProposeCallIsAnsweredAndRecovers(t *testing.T) {
	a := agentWith(
		turnCalling(toolPropose, `{"category": "stay", "notes": `), // truncated JSON
		proposeCall(t, modelProposal{Category: "stay", Notes: "Second attempt."}),
	)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	var notes string
	for _, f := range p.Fields {
		if f.Name == "notes" {
			notes = f.Proposed
		}
	}
	if notes != "Second attempt." {
		t.Errorf("notes = %q, want the retried propose call", notes)
	}
}

// A propose call is the end of the run, so anything else in the same turn is
// moot -- the model has said it is finished, and dispatching a page read whose
// result nobody will ever see is a request paid for and thrown away.
func TestProposeEndsTheTurnEvenAlongsideOtherCalls(t *testing.T) {
	var fetched bool
	a := agentWith(stubTurn{ToolCalls: []toolCall{
		callTo(toolFetchPage, `{"url":"https://example.invalid/never"}`),
		callTo(toolPropose, answerJSON(t, modelProposal{Category: "stay", Notes: "Done."})),
	}})
	a.fetcher = newRelaxedFetcher()
	a.search = &recordingSearcher{called: &fetched}

	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if fetched {
		t.Error("a tool call alongside propose was dispatched; the run was already over")
	}
}

type recordingSearcher struct{ called *bool }

func (*recordingSearcher) Name() string { return "recording" }
func (r *recordingSearcher) Search(context.Context, string) ([]SearchResult, error) {
	*r.called = true
	return nil, nil
}

// --- The cover image (Milestone 5) ---

// The preferred source, and the reason for the "and proposed as a link"
// condition: an og:image from some aggregator the model happened to open is
// not this place's photograph of itself.
func TestTheCoverPrefersTheOgImageOfAProposedLink(t *testing.T) {
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Kex Hostel</title>
		  <meta property="og:image" content="/photo.jpg"></head><body><p>A hostel.</p></body></html>`)
	}))
	defer official.Close()
	aggregator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Booking aggregator</title>
		  <meta property="og:image" content="/stock.jpg"></head><body><p>Book now.</p></body></html>`)
	}))
	defer aggregator.Close()

	// The run reads both pages but proposes only the official one as a link.
	a := agentWith(
		stubTurn{ToolCalls: []toolCall{
			callTo(toolFetchPage, `{"url":"`+aggregator.URL+`/x"}`),
			callTo(toolFetchPage, `{"url":"`+official.URL+`/x"}`),
		}},
		proposeCall(t, modelProposal{
			Category: "stay",
			Notes:    "A hostel.",
			Links:    []modelLink{{URL: official.URL + "/x", Label: "Official site"}},
		}),
	)
	a.fetcher = newFetcherAllowing(
		strings.TrimPrefix(official.URL, "http://"),
		strings.TrimPrefix(aggregator.URL, "http://"),
	)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Cover == nil {
		t.Fatal("no cover was proposed")
	}
	if p.Cover.From != "og" {
		t.Errorf("From = %q, want og", p.Cover.From)
	}
	if p.Cover.URL != official.URL+"/photo.jpg" {
		t.Errorf("URL = %q, want the official site's own image", p.Cover.URL)
	}
	if p.Cover.SourceURL != official.URL+"/x" {
		t.Errorf("SourceURL = %q, want the page it came from", p.Cover.SourceURL)
	}
	// og:image carries no licence metadata, and inventing one would be worse
	// than admitting there is none.
	if p.Cover.Credit != "" || p.Cover.Licence != "" {
		t.Errorf("credit = %q / %q, want both empty for an og:image", p.Cover.Credit, p.Cover.Licence)
	}
}

// A page the run read but did not propose is not this place's own photograph.
func TestAnOgImageFromAnUnproposedPageIsNotUsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Somewhere</title>
		  <meta property="og:image" content="/stock.jpg"></head><body><p>Text.</p></body></html>`)
	}))
	defer srv.Close()

	a := agentWith(
		turnCalling(toolFetchPage, `{"url":"`+srv.URL+`/x"}`),
		proposeCall(t, modelProposal{Category: "stay", Notes: "A hostel."}),
	)
	a.fetcher = newFetcherAllowing(strings.TrimPrefix(srv.URL, "http://"))

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Cover != nil {
		t.Errorf("cover = %+v, want none: the page was read but never proposed", p.Cover)
	}
}

// The fallback, for the landmarks with a good article and no useful site.
func TestTheCoverFallsBackToWikipedia(t *testing.T) {
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Query().Get("prop"), "imageinfo") {
			fmt.Fprint(w, `{"query":{"pages":[{"imageinfo":[{
			  "descriptionurl":"https://commons.example/File:H.jpg",
			  "extmetadata":{"LicenseShortName":{"value":"CC BY-SA 4.0"},
			                 "Artist":{"value":"<a href=\"/u\">Someone</a>"}}}]}]}}`)
			return
		}
		fmt.Fprint(w, `{"query":{"pages":[{"title":"Hallgrimskirkja",
		  "original":{"source":"https://upload.example/H.jpg","width":2000,"height":1500},
		  "thumbnail":{"source":"https://upload.example/thumb/H.jpg"},
		  "fullurl":"https://en.example/wiki/Hallgrimskirkja"}]}}`)
	}))
	defer wiki.Close()

	a := agentWith(proposeCall(t, modelProposal{
		Category:       "site",
		Notes:          "A church.",
		WikipediaTitle: "Hallgrimskirkja",
	}))
	a.wikimedia = wikimedia.New(wiki.URL)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Cover == nil {
		t.Fatal("no cover was proposed")
	}
	if p.Cover.From != "wikipedia" {
		t.Errorf("From = %q, want wikipedia", p.Cover.From)
	}
	if p.Cover.Licence != "CC BY-SA 4.0" || p.Cover.Credit != "Someone" {
		t.Errorf("credit = %q / %q, want both carried through", p.Cover.Credit, p.Cover.Licence)
	}
	// The article, not the file page: it is the thing a reader recognises,
	// and the file page is one click from it.
	if p.Cover.SourceURL != "https://en.example/wiki/Hallgrimskirkja" {
		t.Errorf("SourceURL = %q, want the article", p.Cover.SourceURL)
	}
}

// Every way this can come to nothing, and none of them is an error: a
// proposal without a picture is the ordinary case.
func TestNoCoverIsNotAFailure(t *testing.T) {
	cases := []struct {
		name    string
		raw     modelProposal
		handler http.HandlerFunc
	}{
		{
			name: "the model named no article",
			raw:  modelProposal{Category: "stay", Notes: "A hostel."},
		},
		{
			name: "the article has no lead image",
			raw:  modelProposal{Category: "site", Notes: "X.", WikipediaTitle: "Nowhere"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"query":{"pages":[{"title":"Nowhere","missing":true}]}}`)
			},
		},
		{
			name: "the encyclopaedia is down",
			raw:  modelProposal{Category: "site", Notes: "X.", WikipediaTitle: "Hallgrimskirkja"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusBadGateway)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := agentWith(proposeCall(t, tc.raw))
			if tc.handler != nil {
				srv := httptest.NewServer(tc.handler)
				defer srv.Close()
				a.wikimedia = wikimedia.New(srv.URL)
			}
			p, err := a.Propose(context.Background(), enrichRequest(), nil)
			if err != nil {
				t.Fatalf("Propose failed rather than proposing no cover: %v", err)
			}
			if p.Cover != nil {
				t.Errorf("cover = %+v, want none", p.Cover)
			}
			// The rest of the proposal is unaffected: one missing picture does
			// not discard a good run.
			if len(p.Fields) == 0 {
				t.Error("the proposal lost its fields along with its cover")
			}
		})
	}
}

// A Wikipedia article has a perfectly good og:image, and taking it would lose
// the licence: Wikimedia photographs are freely licensed, not unencumbered,
// and nearly all of them need a credit an og:image tag does not carry.
//
// Found by a live run, which came back with a German landmark's picture and
// both credit and licence empty.
//
// chooseCover is exercised directly rather than through a run, because the
// recogniser keys on the wikipedia.org hostname and an httptest server cannot
// have one.
func TestAWikipediaArticleGoesThroughTheAPINotItsOgImage(t *testing.T) {
	wiki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Query().Get("prop"), "imageinfo") {
			fmt.Fprint(w, `{"query":{"pages":[{"imageinfo":[{
			  "descriptionurl":"https://commons.example/File:W.jpg",
			  "extmetadata":{"LicenseShortName":{"value":"CC BY-SA 3.0"},
			                 "Artist":{"value":"A Photographer"}}}]}]}}`)
			return
		}
		fmt.Fprint(w, `{"query":{"pages":[{"title":"Waterloo-Tor",
		  "original":{"source":"https://upload.example/clean.jpg","width":1280,"height":960},
		  "fullurl":"https://de.wikipedia.org/wiki/Waterloo-Tor"}]}}`)
	}))
	defer wiki.Close()

	a := agentWith()
	a.wikimedia = wikimedia.New(wiki.URL)

	article := "https://de.wikipedia.org/wiki/Waterloo-Tor"
	cover := a.chooseCover(
		context.Background(),
		Request{Mode: ModeEnrich, Locale: "de"},
		// The model named no article title: it has to be recovered from the
		// link, which is worth doing because a model that finds the article
		// well enough to link it has already done the hard part.
		modelProposal{},
		[]Link{{URL: article, Label: "Wikipedia"}},
		[]Source{{Title: "Waterloo-Tor", URL: article, Image: "https://upload.example/tracked.jpg?utm_source=de.wikipedia.org"}},
		slog.Default(),
	)

	if cover == nil {
		t.Fatal("no cover was chosen")
	}
	if cover.From != "wikipedia" {
		t.Fatalf("From = %q, want the API route so the licence comes with it", cover.From)
	}
	if cover.Licence != "CC BY-SA 3.0" || cover.Credit != "A Photographer" {
		t.Errorf("credit = %q / %q, want both from the API", cover.Credit, cover.Licence)
	}
	if strings.Contains(cover.URL, "utm_source") {
		t.Errorf("URL = %q, want the API's clean upload URL rather than the tagged og:image", cover.URL)
	}
}

func TestWikipediaArticleRecognisesArticleURLs(t *testing.T) {
	cases := []struct {
		url        string
		lang, want string
		ok         bool
	}{
		{"https://de.wikipedia.org/wiki/Waterloo-Tor", "de", "Waterloo-Tor", true},
		{"https://en.wikipedia.org/wiki/Brandenburg_Gate", "en", "Brandenburg Gate", true},
		{"https://en.m.wikipedia.org/wiki/Kex_Hostel", "en", "Kex Hostel", true},
		// Percent-encoded titles are the norm for anything non-ASCII.
		{"https://de.wikipedia.org/wiki/Hallgr%C3%ADmskirkja", "de", "Hallgrímskirkja", true},
		{"https://de.wikipedia.org/", "", "", false},
		{"https://de.wikipedia.org/wiki/", "", "", false},
		// Not Wikipedia, including a lookalike host.
		{"https://commons.wikimedia.org/wiki/File:X.jpg", "", "", false},
		{"https://www.kexrvk.is/", "", "", false},
		{"https://notwikipedia.org/wiki/X", "", "", false},
		{"https://evil.example/wiki/X?x=.wikipedia.org", "", "", false},
		{"not a url", "", "", false},
	}
	for _, tc := range cases {
		lang, title, ok := wikipediaArticle(tc.url)
		if ok != tc.ok || title != tc.want {
			t.Errorf("wikipediaArticle(%q) = %q, %q, %t; want %q, %q, %t", tc.url, lang, title, ok, tc.lang, tc.want, tc.ok)
		}
		if ok && lang != tc.lang {
			t.Errorf("wikipediaArticle(%q) language = %q, want %q", tc.url, lang, tc.lang)
		}
	}
}
