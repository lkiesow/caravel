package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"caravel/internal/geocode"
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
	a.fetcher = newFetcherWithPolicy(true)

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
	a.fetcher = newFetcherWithPolicy(true)

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
	a.fetcher = newFetcherWithPolicy(true)

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
	if got := counting.n.Load(); got > maxToolCalls {
		t.Errorf("%d tool calls ran, want at most %d", got, maxToolCalls)
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
	a := &Agent{provider: &greedyProvider{inner: expensive}, fetcher: newPageFetcher(), search: &stubSearcher{}}

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
	a := &Agent{provider: &expiringProvider{inner: slow}, fetcher: newPageFetcher(), search: &stubSearcher{}}

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
	resp.Usage = usage{TotalTokens: maxTokens + 1}
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
		if !strings.HasPrefix(k, "assist.progress.") {
			t.Errorf("event key %q is not an i18n key", k)
		}
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

	// And again, to prove the script rewinds: a second enrichment in the same
	// process must not find an exhausted stub.
	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("second Propose: %v", err)
	}
}
