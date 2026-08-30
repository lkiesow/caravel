package assist

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// The stub's value is that it drives a *multi-step* exchange, so the loop, the
// dispatcher and the history rules all run for real in CI. A stub that
// answered in one turn would prove none of that.
func TestStubDrivesAMultiStepExchange(t *testing.T) {
	s := newStubProvider()
	ctx := context.Background()

	first, err := s.Complete(ctx, chatRequest{})
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != toolWebSearch {
		t.Fatalf("turn 1 = %+v, want a %s call", first.ToolCalls, toolWebSearch)
	}
	if first.FinishReason != "tool_calls" {
		t.Errorf("turn 1 FinishReason = %q", first.FinishReason)
	}

	// Two page reads, so the run records more than one source. Both point at
	// the fixture host, which is the whole reason a stub run can produce a
	// live link and a sources list at all.
	for _, turn := range []int{2, 3} {
		resp, err := s.Complete(ctx, chatRequest{})
		if err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
		if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != toolFetchPage {
			t.Fatalf("turn %d = %+v, want a %s call", turn, resp.ToolCalls, toolFetchPage)
		}
		if !strings.Contains(resp.ToolCalls[0].Function.Arguments, startStubFixture().base) {
			t.Errorf("turn %d fetches %s, want the fixture host", turn, resp.ToolCalls[0].Function.Arguments)
		}
	}

	// Turn 4 ends the run with a propose call whose arguments are the answer.
	// There is no fifth turn: removing that second request is what Milestone
	// 4a was for.
	fourth, err := s.Complete(ctx, chatRequest{})
	if err != nil {
		t.Fatalf("turn 4: %v", err)
	}
	if len(fourth.ToolCalls) != 1 || fourth.ToolCalls[0].Function.Name != toolPropose {
		t.Fatalf("turn 4 = %+v, want a %s call", fourth.ToolCalls, toolPropose)
	}
	if fourth.ToolCalls[0].Function.Arguments == "" {
		t.Error("the propose call carried no arguments; they are the answer")
	}

	if _, err := s.Complete(ctx, chatRequest{}); err == nil {
		t.Error("a fifth turn was available; the script should end at the propose call")
	}
}

// The default answer has to satisfy the same contract a real model's does, or
// the Playwright suite proves the UI can render something no real run produces.
func TestStubAnswerMatchesTheSchema(t *testing.T) {
	s := newStubProvider()
	ctx := context.Background()
	for range 3 {
		if _, err := s.Complete(ctx, chatRequest{}); err != nil {
			t.Fatalf("advancing the script: %v", err)
		}
	}
	final, err := s.Complete(ctx, chatRequest{})
	if err != nil {
		t.Fatalf("final turn: %v", err)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("final turn = %+v, want the propose call", final)
	}
	// The answer now rides in the tool arguments rather than in the message
	// content, but it is the same contract and has to satisfy the same schema.
	answer := final.ToolCalls[0].Function.Arguments

	var out modelProposal
	if err := decodeJSONAnswer(answer, &out); err != nil {
		t.Fatalf("the stub answer does not decode: %v", err)
	}
	if out.Category != "stay" {
		t.Errorf("category = %q, want one the enum allows", out.Category)
	}
	if out.PlaceName == "" || out.Address == "" {
		t.Error("the stub answer needs a place name and address: the geocoding path resolves them")
	}
	// The model must never produce coordinates, so the schema has no field
	// for them and the stub must not smuggle any in.
	if strings.Contains(strings.ToLower(answer), "\"lat\"") {
		t.Error("the stub answer carries coordinates; they are resolved from the address, never returned")
	}
}

func TestStubReportsUsage(t *testing.T) {
	s := newStubProvider()
	resp, err := s.Complete(context.Background(), chatRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Non-zero so a budget assertion against the stub measures something.
	if resp.Usage.TotalTokens == 0 {
		t.Error("the stub reported zero tokens; budget tests against it would be vacuous")
	}
}

func TestStubRunningOutIsAnError(t *testing.T) {
	s := newScriptedProvider(stubTurn{Content: "{}"})
	ctx := context.Background()
	if _, err := s.Complete(ctx, chatRequest{}); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	// An agent asking for one more turn than scripted is a bug in one of them.
	// An explicit error beats an index panic.
	_, err := s.Complete(ctx, chatRequest{})
	if err == nil || !strings.Contains(err.Error(), "ran out") {
		t.Fatalf("error = %v, want the script-exhausted message", err)
	}
}

func TestStubReplaysAfterReset(t *testing.T) {
	s := newScriptedProvider(stubTurn{Content: "one"}, stubTurn{Content: "two"})
	ctx := context.Background()
	for range 2 {
		if _, err := s.Complete(ctx, chatRequest{}); err != nil {
			t.Fatalf("first pass: %v", err)
		}
	}
	s.begin(ModeEnrich)
	resp, err := s.Complete(ctx, chatRequest{})
	if err != nil {
		t.Fatalf("after reset: %v", err)
	}
	if resp.Content != "one" {
		t.Errorf("Content = %q, want the script rewound", resp.Content)
	}
}

func TestStubCanFailOnDemand(t *testing.T) {
	boom := errors.New("upstream exploded")
	s := newScriptedProvider(stubTurn{Err: boom})
	_, err := s.Complete(context.Background(), chatRequest{})
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the scripted failure", err)
	}
}

// The stub must honour cancellation like the real client, or the agent's
// deadline and cancel tests would pass against it for the wrong reason.
func TestStubHonorsContextCancellation(t *testing.T) {
	s := newStubProvider()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Complete(ctx, chatRequest{}); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// One stub is built per server and two browser tabs can enrich at once, so the
// cursor must not race even though the interleaving that results is accepted.
func TestStubIsSafeForConcurrentUse(t *testing.T) {
	s := newScriptedProvider(make([]stubTurn, 50)...)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				_, _ = s.Complete(context.Background(), chatRequest{})
			}
		}()
	}
	wg.Wait()
	if s.n != 50 {
		t.Errorf("cursor = %d after 50 calls, want 50", s.n)
	}
}

// New must pick the fake for the sentinel and the real client otherwise --
// the whole reason CI can run this feature.
func TestNewSelectsTheStubProvider(t *testing.T) {
	a, err := New(Options{LLMURL: LLMStub, LLMModel: "stub"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	agent, ok := a.(*Agent)
	if !ok {
		t.Fatalf("New returned %T, want *Agent", a)
	}
	if _, isStub := agent.provider.(*stubProvider); !isStub {
		t.Errorf("provider = %T, want the stub for CARAVEL_LLM_URL=%q", agent.provider, LLMStub)
	}

	a, err = New(Options{LLMURL: "https://example.invalid/v1/chat/completions", LLMModel: "m"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, isHTTP := a.(*Agent).provider.(*httpProvider); !isHTTP {
		t.Errorf("provider = %T, want the HTTP client for a real URL", a.(*Agent).provider)
	}
}

func TestNewReturnsNilWhenUnconfigured(t *testing.T) {
	a, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Nil-and-no-error is the off switch. A caller that checked only err would
	// then nil-panic, which is why this is asserted rather than assumed.
	if a != nil {
		t.Errorf("New returned %v, want nil when no endpoint is configured", a)
	}
}

// Guards the hand-written literal against a typo that would only surface as a
// 400 from a real provider.
func TestProposalSchemaIsValidJSON(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(proposalSchema, &parsed); err != nil {
		t.Fatalf("proposalSchema is not valid JSON: %v", err)
	}
	// strict mode rejects a schema without these two, and the failure arrives
	// as an opaque 400 from the server rather than as anything local.
	if parsed["additionalProperties"] != false {
		t.Error("additionalProperties must be false for strict mode")
	}
	required, _ := parsed["required"].([]any)
	props, _ := parsed["properties"].(map[string]any)
	if len(required) != len(props) {
		t.Errorf("required lists %d fields but there are %d properties; strict mode wants every one", len(required), len(props))
	}
}
