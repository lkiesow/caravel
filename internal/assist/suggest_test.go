package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"caravel/internal/geocode"
)

// The trip-level run. Its loop is Propose's loop -- agent_test.go covers that
// at length and none of it is repeated here. What is new is the shape of the
// answer and what buildCandidates does with it: the cap, the dedup, and the
// fact that every guard rail applied to one place is applied to each of these.

func suggestionsJSON(t *testing.T, places ...modelProposal) string {
	t.Helper()
	b, err := json.Marshal(modelSuggestions{Suggestions: places})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func suggestRequest() SuggestRequest {
	return SuggestRequest{Prompt: "things to do in Reykjavik", Locale: "en"}
}

// A geocoder answering a fixed position for everything, so a test about dedup
// is not also a test about Nominatim.
func geocoderAt(t *testing.T, lat, lng float64) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"display_name":"somewhere","lat":"%f","lon":"%f"}]`, lat, lng)
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

func TestSuggestReturnsSeveralCandidates(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "Hallgrimskirkja", Category: "site", Tags: "church", Notes: "A church."},
			modelProposal{Title: "Kex Hostel", Category: "stay", Notes: "A hostel."},
		)},
	)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(out.Candidates))
	}
	first := out.Candidates[0].Place
	if first.Title != "Hallgrimskirkja" || first.Category != "site" || first.Tags != "church" {
		t.Errorf("first candidate = %+v, want the model's first place intact", first)
	}
	if out.Dropped != 0 {
		t.Errorf("dropped = %d, want 0", out.Dropped)
	}
}

// A run with no prompt is refused before a single token is spent. Unlike an
// enrichment there is nothing else to work from, so this would be a paid run
// asking the model to guess what the user wanted.
func TestSuggestRefusesAnEmptyPrompt(t *testing.T) {
	a := agentWith(stubTurn{Content: "unreachable"})

	if _, err := a.Suggest(context.Background(), SuggestRequest{Prompt: "   "}, nil); err == nil {
		t.Fatal("an empty prompt was accepted")
	}
}

// maxItems in the schema is a request, not a guarantee: the json_object
// fallback enforces no schema at all. The cap is enforced here as well, and
// truncating beats failing -- six good places and a seventh too many is not a
// broken run.
func TestSuggestCapsTheList(t *testing.T) {
	var many []modelProposal
	for i := range maxSuggestions + 3 {
		many = append(many, modelProposal{Title: fmt.Sprintf("Place %d", i), Category: "site"})
	}

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t, many...)},
	)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != maxSuggestions {
		t.Errorf("candidates = %d, want the cap of %d", len(out.Candidates), maxSuggestions)
	}
	// Truncated from the end, so the places the model thought of first are the
	// ones that survive.
	if out.Candidates[0].Place.Title != "Place 0" {
		t.Errorf("first candidate = %q, want the model's first", out.Candidates[0].Place.Title)
	}
}

// The prompt names what the trip already has and asks for none of it, and this
// is what happens when the model does it anyway.
func TestSuggestDropsAPlaceTheTripAlreadyHas(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "  hallgrímskirkja  ", Category: "site"},
			modelProposal{Title: "Braud and Co", Category: "site"},
		)},
	)

	req := suggestRequest()
	req.Existing = []ExistingPlace{{Title: "Hallgrímskirkja"}}

	out, err := a.Suggest(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Place.Title != "Braud and Co" {
		t.Fatalf("candidates = %+v, want only the place the trip lacks", out.Candidates)
	}
	// Counted rather than silently discarded: "it found two and one was
	// already yours" reads differently from "it only found one".
	if out.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", out.Dropped)
	}
}

// The same answer twice in one list, which a model asked for distinct places
// still manages.
func TestSuggestDropsADuplicateWithinOneAnswer(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "Kex Hostel", Category: "stay"},
			modelProposal{Title: "KEX HOSTEL!", Category: "stay"},
		)},
	)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 1 {
		t.Errorf("candidates = %d, want 1 after the duplicate was dropped", len(out.Candidates))
	}
	if out.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", out.Dropped)
	}
}

// The duplicate a name comparison cannot catch: the same place under a second
// spelling, or in the other language. Both candidates geocode to the same
// point, so the second is the first.
func TestSuggestDropsAPlaceAtTheSamePosition(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "Hallgrimskirkja", Category: "site", Address: "Hallgrimstorg 1"},
			modelProposal{Title: "Church of Hallgrimur", Category: "site", Address: "Hallgrimstorg 1, Reykjavik"},
		)},
	)
	a.geocoder = geocode.New(geocoderAt(t, 64.1417, -21.9266))

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1: both are the same church", len(out.Candidates))
	}
	if out.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", out.Dropped)
	}
	if out.Candidates[0].Lat == nil || *out.Candidates[0].Lat != 64.1417 {
		t.Errorf("coordinates = %v, want the geocoder's", out.Candidates[0].Lat)
	}
}

// Coordinates come from the geocoder for a candidate exactly as they do for a
// proposal: the model supplies the words and never the position.
func TestSuggestResolvesEachCandidateThroughTheGeocoder(t *testing.T) {
	var asked []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		asked = append(asked, q)
		if strings.Contains(q, "Nowhere") {
			fmt.Fprint(w, `[]`)
			return
		}
		fmt.Fprint(w, `[{"display_name":"x","lat":"64.1","lon":"-21.9"}]`)
	}))
	defer upstream.Close()

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "One", Category: "site", Address: "12 Nowhere Street", PlaceName: "One, Reykjavik"},
			modelProposal{Title: "Two", Category: "site"},
		)},
	)
	a.geocoder = geocode.New(upstream.URL)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(out.Candidates))
	}
	if out.Candidates[0].Lat == nil {
		t.Error("the first candidate did not resolve through the place-name fallback")
	}
	// The second names no address and no place, so it must not have cost a
	// request at all.
	if out.Candidates[1].Lat != nil {
		t.Error("the second candidate has coordinates it could not have got honestly")
	}
	if len(asked) != 2 || asked[0] != "12 Nowhere Street" || asked[1] != "One, Reykjavik" {
		t.Errorf("geocoder queries = %v, want the address then the place name, and nothing for the second candidate", asked)
	}
}

// One bad category does not spoil the answer: it is dropped from that
// candidate and the rest of the list is untouched. The consequence differs
// from an enrichment, where an unusable category simply proposes nothing --
// here it produces a candidate the user has to categorise themselves.
func TestSuggestDropsOneCandidatesInvalidCategory(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "One", Category: "restaurant"},
			modelProposal{Title: "Two", Category: "stay"},
		)},
	)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2: a bad category drops the value, not the place", len(out.Candidates))
	}
	if out.Candidates[0].Place.Category != "" {
		t.Errorf("category = %q, want empty rather than guessed", out.Candidates[0].Place.Category)
	}
	if out.Candidates[1].Place.Category != "stay" {
		t.Errorf("the valid category was disturbed: %q", out.Candidates[1].Place.Category)
	}
}

// A candidate with no name cannot be reviewed and cannot be saved, so it is
// not offered. Distinct from an enrichment, where an empty title is the normal
// way of saying "this place is already named".
func TestSuggestSkipsACandidateWithNoTitle(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: suggestionsJSON(t,
			modelProposal{Title: "   ", Category: "site", Notes: "Something."},
			modelProposal{Title: "Two", Category: "site"},
		)},
	)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Place.Title != "Two" {
		t.Fatalf("candidates = %+v, want only the named one", out.Candidates)
	}
	// Not a duplicate, so not counted as one.
	if out.Dropped != 0 {
		t.Errorf("dropped = %d, want 0: a nameless candidate is not a duplicate", out.Dropped)
	}
}

// The whole point of the milestone before this one: the trip-level run is the
// same loop, so it answers a propose call the same way rather than needing the
// two-phase composing turn.
func TestSuggestAnswersFromAProposeCall(t *testing.T) {
	a := agentWith(
		turnCalling(toolPropose, suggestionsJSON(t, modelProposal{Title: "Kex Hostel", Category: "stay"})),
	)

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(out.Candidates))
	}
}

// The stub the browser suite and a manual run both drive.
func TestTheStubHasASuggestScript(t *testing.T) {
	a := agentWith()
	a.provider = newStubProvider()

	out, err := a.Suggest(context.Background(), suggestRequest(), nil)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(out.Candidates) != 3 {
		t.Fatalf("candidates = %d, want the scripted 3", len(out.Candidates))
	}
	// A second run in the same process must find the script rewound, and an
	// enrichment afterwards must find its own script rather than this one.
	if _, err := a.Suggest(context.Background(), suggestRequest(), nil); err != nil {
		t.Fatalf("the second Suggest run: %v", err)
	}
	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose after Suggest: %v", err)
	}
	if len(p.Fields) == 0 {
		t.Error("the location script did not run after a suggest run")
	}
}

// The schema is built by wrapping the proposal schema rather than restating
// it, which is only worth doing if the result is actually the same shape.
func TestSuggestionsSchemaWrapsTheProposalSchema(t *testing.T) {
	var parsed struct {
		Properties struct {
			Suggestions struct {
				MaxItems int             `json:"maxItems"`
				Items    json.RawMessage `json:"items"`
			} `json:"suggestions"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(suggestionsSchema, &parsed); err != nil {
		t.Fatalf("the suggestions schema is not valid JSON: %v", err)
	}
	if parsed.Properties.Suggestions.MaxItems != maxSuggestions {
		t.Errorf("maxItems = %d, want %d", parsed.Properties.Suggestions.MaxItems, maxSuggestions)
	}
	if !json.Valid(parsed.Properties.Suggestions.Items) {
		t.Fatal("the element schema is not valid JSON")
	}
	var a, b any
	if err := json.Unmarshal(parsed.Properties.Suggestions.Items, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(proposalSchema, &b); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Error("the element schema is not the proposal schema")
	}
}
