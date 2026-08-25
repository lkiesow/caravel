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
}

// newStubProvider returns the default script: search, read two pages, stop
// gathering, answer.
//
// Five turns rather than one because the number of turns is the interesting
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
		Type:      "hostel",
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

	return newScriptedProvider(
		turnCalling(toolWebSearch, `{"query":"Kex Hostel Reykjavik"}`),
		// Two page reads rather than one, so the sources list has more than a
		// single entry in it. A list of one renders the same whether it is
		// built correctly or by accident.
		turnCalling(toolFetchPage, fetchArgs(fixture.base+"/kex")),
		turnCalling(toolFetchPage, fetchArgs(fixture.base+"/reykjavik")),
		// A turn with no tool calls: this is what tells the loop the model has
		// finished gathering. The structured answer is a separate turn after
		// it -- see the two-phase note in agent.go.
		stubTurn{Content: "I have enough to describe this place."},
		stubTurn{Content: string(answer)},
	)
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

// reset rewinds the script, so one stub can serve several runs. Called by the
// agent at the start of a run when the provider is a stub.
func (s *stubProvider) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = 0
}
