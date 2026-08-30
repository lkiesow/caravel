package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// The in-process fake provider, selected by CARAVEL_LLM_URL=stub.
//
// It is not a mock in the usual sense: it fakes exactly one thing, the HTTP
// call to the model, and nothing else. The agent loop, the tool dispatch, the
// validation, the link checking, the geocoding and the SSE transport all run
// for real against it. That is deliberate -- a stub that short-circuited to a
// finished Proposal would make the Playwright suite prove only that a fixture
// can be rendered, which is worth close to nothing.
//
// It exists because every other part of this feature is untestable without it:
// CI has no API key, no network budget and no tolerance for a model that
// answers differently on Tuesday.

type stubTurn struct {
	// ToolCalls, when set, makes this turn a request for tools rather than an
	// answer.
	ToolCalls []toolCall
	// Content is the assistant's text, and on the final turn the JSON answer.
	Content string
	// Err makes the turn fail, for testing the error paths.
	Err error
}

type stubProvider struct {
	mu    sync.Mutex
	turns []stubTurn
	n     int
	// byMode holds one script per kind of run. The default stub has two --
	// enriching one place and suggesting several -- and the agent selects
	// between them at the start of a run, because the two answers have
	// different shapes and one script cannot serve both. Nil for a provider
	// built by newScriptedProvider, which plays its one script whatever it is
	// asked.
	byMode map[Mode][]stubTurn
}

// newStubProvider returns the default script: search, read two pages, answer
// with a propose call.
//
// Four turns rather than one because the number of turns is the interesting
// part. A single-turn script would never exercise the loop, the tool
// dispatcher or the history-echoing rule that most servers enforce, so the
// first real provider would be the first time any of it ran.
//
// The URLs point at the fixture host rather than at example.invalid, which is
// what lets a run against the stub produce a live link and a recorded source.
// See stub_fixture.go for why that host exists and what it costs.
func newStubProvider() *stubProvider {
	fixture := startStubFixture()

	answer, err := json.Marshal(modelProposal{
		Title:     "Kex Hostel",
		Category:  "stay",
		Tags:      "hostel, harbour, city centre",
		Notes:     "A former biscuit factory on the harbour side of Reykjavik, now a hostel with dorms and private rooms. The bar is open to non-guests and does food until late.",
		Address:   "Skulagata 28, 101 Reykjavik, Iceland",
		PlaceName: "Kex Hostel, Reykjavik",
		Links: []modelLink{
			{URL: fixture.base + "/kex", Label: "Official site"},
		},
	})
	if err != nil {
		// Unreachable: the value is a literal above. Panicking rather than
		// returning an error keeps the constructor signature honest for the
		// dozens of call sites that cannot fail.
		panic("assist: encoding the stub answer: " + err.Error())
	}

	location := []stubTurn{
		turnCalling(toolWebSearch, `{"query":"Kex Hostel Reykjavik"}`),
		// Two page reads rather than one, so the sources list has more than a
		// single entry in it. A list of one renders the same whether it is
		// built correctly or by accident.
		turnCalling(toolFetchPage, fetchArgs(fixture.base+"/kex")),
		turnCalling(toolFetchPage, fetchArgs(fixture.base+"/reykjavik")),
		// The run ends with a propose call carrying the answer, which is what
		// a well-behaved model does since Milestone 4a -- so the browser suite
		// exercises the path people actually get rather than the fallback.
		// The two-phase route is covered by agent_test.go instead.
		turnCalling(toolPropose, string(answer)),
	}

	suggestions, err := json.Marshal(modelSuggestions{Suggestions: []modelProposal{
		{
			Title:     "Hallgrimskirkja",
			Category:  "site",
			Tags:      "church, landmark, city centre",
			Notes:     "The tall concrete church above the old town. The tower is worth the lift fare for the view over the coloured roofs.",
			Address:   "Hallgrimstorg 1, 101 Reykjavik, Iceland",
			PlaceName: "Hallgrimskirkja, Reykjavik",
			Links:     []modelLink{{URL: fixture.base + "/reykjavik", Label: "About Reykjavik"}},
		},
		{
			Title:     "Kex Hostel",
			Category:  "stay",
			Tags:      "hostel, harbour",
			Notes:     "A former biscuit factory on the harbour side, now a hostel. The bar does food until late.",
			Address:   "Skulagata 28, 101 Reykjavik, Iceland",
			PlaceName: "Kex Hostel, Reykjavik",
			Links:     []modelLink{{URL: fixture.base + "/kex", Label: "Official site"}},
		},
		{
			// Deliberately thin: no address, no link, nothing to geocode. A
			// script where every candidate is complete would never show the
			// review screen what a sparse one looks like, and sparse ones are
			// the common case.
			Title:    "Braud and Co",
			Category: "site",
			Tags:     "bakery",
			Notes:    "A small bakery known for cinnamon buns. Sells out by the middle of the morning.",
		},
	}})
	if err != nil {
		panic("assist: encoding the stub suggestions: " + err.Error())
	}

	suggest := []stubTurn{
		turnCalling(toolWebSearch, `{"query":"things to do in Reykjavik"}`),
		turnCalling(toolFetchPage, fetchArgs(fixture.base+"/reykjavik")),
		turnCalling(toolFetchPage, fetchArgs(fixture.base+"/kex")),
		turnCalling(toolPropose, string(suggestions)),
	}

	return &stubProvider{
		turns: location,
		byMode: map[Mode][]stubTurn{
			ModeEnrich:  location,
			ModePrompt:  location,
			ModeSuggest: suggest,
		},
	}
}

// newScriptedProvider builds a stub that plays the given turns in order. Used
// by tests that need a specific sequence -- a tool loop, a malformed answer, a
// mid-run failure.
func newScriptedProvider(turns ...stubTurn) *stubProvider {
	return &stubProvider{turns: turns}
}

// fetchArgs encodes a fetch_page argument object. Built rather than formatted,
// because the fixture URL carries a port the script cannot know in advance and
// a hand-written JSON string is one missed quote from a turn that silently
// does nothing.
func fetchArgs(target string) string {
	encoded, err := json.Marshal(struct {
		URL string `json:"url"`
	}{URL: target})
	if err != nil {
		panic("assist: encoding the stub fetch arguments: " + err.Error())
	}
	return string(encoded)
}

func turnCalling(name, arguments string) stubTurn {
	var c toolCall
	// The id only has to be unique within a conversation and echoed back on
	// the matching result, which is what the agent does with it.
	c.ID = "call_" + name
	c.Type = "function"
	c.Function.Name = name
	c.Function.Arguments = arguments
	return stubTurn{ToolCalls: []toolCall{c}}
}

// Complete plays the next scripted turn.
//
// Locked because the agent is single-threaded per run but a stub built once
// and shared by a server is not: two browser tabs enriching at the same time
// would otherwise race the cursor. Each Complete still advances the same
// shared script, so concurrent runs interleave -- acceptable for a fake whose
// only real user is one developer or one CI worker at a time, and noted here
// so nobody debugs it as a mystery.
func (s *stubProvider) Complete(ctx context.Context, req chatRequest) (*chatResponse, error) {
	// Honour cancellation like the real client, so tests of the agent's
	// deadline and cancel paths do not silently pass for the wrong reason.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.n >= len(s.turns) {
		// A script running out means the agent asked for one more turn than
		// the test expected, which is a bug in one of them and worth an
		// explicit error rather than an index panic.
		return nil, fmt.Errorf("assist: the stub script ran out after %d turns", len(s.turns))
	}
	turn := s.turns[s.n]
	s.n++

	if turn.Err != nil {
		return nil, turn.Err
	}

	finish := "stop"
	if len(turn.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	return &chatResponse{
		Content:      turn.Content,
		ToolCalls:    turn.ToolCalls,
		FinishReason: finish,
		// Plausible non-zero numbers, so a budget test against the stub
		// measures something. Fixed rather than derived: a stub whose token
		// count depended on the prompt would make budget assertions fragile.
		Usage: usage{PromptTokens: 500, CompletionTokens: 100, TotalTokens: 600},
	}, nil
}

// begin selects the script for this kind of run and rewinds it, so one stub
// can serve several runs. Called by the agent at the start of a run when the
// provider is a stub.
//
// A provider built by newScriptedProvider has no per-mode scripts and keeps
// the one it was given whatever mode it is asked for: those are tests that
// chose their turns deliberately.
func (s *stubProvider) begin(mode Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if script, ok := s.byMode[mode]; ok {
		s.turns = script
	}
	s.n = 0
}
