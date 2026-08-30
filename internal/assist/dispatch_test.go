package assist

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A turn's tool calls run together. Three properties, and only the third is
// about speed -- the other two are what makes the conversation well-formed.

// recordingProvider plays a script and keeps every request it was sent, so a
// test can assert on the conversation the agent built rather than only on what
// came out the far end.
type recordingProvider struct {
	inner *stubProvider
	mu    sync.Mutex
	sent  []chatRequest
}

func (r *recordingProvider) Complete(ctx context.Context, req chatRequest) (*chatResponse, error) {
	r.mu.Lock()
	// Copied: the agent appends to the same slice between turns, so keeping
	// the header would make every recorded request the final one.
	r.sent = append(r.sent, chatRequest{Messages: append([]chatMessage(nil), req.Messages...)})
	r.mu.Unlock()
	return r.inner.Complete(ctx, req)
}

// last returns the messages as of the most recent request.
func (r *recordingProvider) last() []chatMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent[len(r.sent)-1].Messages
}

func recordingAgent(turns ...stubTurn) (*Agent, *recordingProvider) {
	rec := &recordingProvider{inner: newScriptedProvider(turns...)}
	a := &Agent{provider: rec, fetcher: newPageFetcher(), limits: DefaultLimits()}
	return a, rec
}

// callN is callTo with a distinct id per call, which matters here: the whole
// point is that each result lands against the right one.
func callN(i int, name, args string) toolCall {
	var c toolCall
	c.ID = fmt.Sprintf("call_%d_%s", i, name)
	c.Type = "function"
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

func toolMessages(messages []chatMessage) []chatMessage {
	var out []chatMessage
	for _, m := range messages {
		if m.Role == roleTool {
			out = append(out, m)
		}
	}
	return out
}

// Proof of concurrency that does not measure elapsed time: the fixture counts
// how many requests are in flight at once and remembers the peak. Sequential
// dispatch can only ever reach one, whatever the machine is doing; concurrent
// dispatch reaches three. A stopwatch assertion would be a flake on a loaded
// machine, and this is not.
//
// The first version of this test asserted only that all three requests
// *arrived*, which sequential dispatch also satisfies -- it passed against the
// old code, ten seconds slower. Peak concurrency is the property; arrival is
// not.
func TestATurnsToolCallsRunTogether(t *testing.T) {
	const n = 3

	var mu sync.Mutex
	inFlight, peak := 0, 0
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		reached := inFlight == n
		mu.Unlock()

		// The last one in lets everybody out. Sequential dispatch never
		// reaches that, so each request waits out the timeout instead and the
		// run still finishes -- slowly, and with a peak of one, which is what
		// the assertion below reads.
		if reached {
			close(release)
		}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}

		mu.Lock()
		inFlight--
		mu.Unlock()

		fmt.Fprintf(w, `<html><head><title>%s</title></head><body><p>Page.</p></body></html>`, r.URL.Path)
	}))
	defer server.Close()

	calls := make([]toolCall, 0, n)
	for i := range n {
		calls = append(calls, callN(i, toolFetchPage, fetchArgs(fmt.Sprintf("%s/page-%d", server.URL, i))))
	}

	a, _ := recordingAgent(
		stubTurn{ToolCalls: calls},
		proposeCall(t, modelProposal{Category: "stay", Notes: "Done."}),
	)
	a.fetcher = newFetcherAllowing(strings.TrimPrefix(server.URL, "http://"))

	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak != n {
		t.Errorf("peak requests in flight = %d, want %d: the calls were dispatched one at a time", peak, n)
	}
}

// A `tool` message has to follow the `tool_calls` it answers, in the order they
// were issued -- most servers reject a conversation where it does not. So the
// results are collected into a slice indexed by call and appended in call
// order, never in completion order. Here the calls finish in reverse.
func TestToolResultsFollowCallOrderNotCompletionOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// page-0 is slowest, page-2 fastest, so completion order is the
		// reverse of call order and an implementation that appended as
		// results arrived would produce exactly the wrong answer.
		switch r.URL.Path {
		case "/page-0":
			time.Sleep(150 * time.Millisecond)
		case "/page-1":
			time.Sleep(75 * time.Millisecond)
		}
		fmt.Fprintf(w, `<html><head><title>title %s</title></head><body><p>marker %s</p></body></html>`,
			r.URL.Path, r.URL.Path)
	}))
	defer server.Close()

	var calls []toolCall
	for i := range 3 {
		calls = append(calls, callN(i, toolFetchPage, fetchArgs(fmt.Sprintf("%s/page-%d", server.URL, i))))
	}

	a, rec := recordingAgent(
		stubTurn{ToolCalls: calls},
		proposeCall(t, modelProposal{Category: "stay", Notes: "Done."}),
	)
	a.fetcher = newFetcherAllowing(strings.TrimPrefix(server.URL, "http://"))

	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	results := toolMessages(rec.last())
	if len(results) != 3 {
		t.Fatalf("tool messages = %d, want 3", len(results))
	}
	for i, m := range results {
		if want := calls[i].ID; m.ToolCallID != want {
			t.Errorf("message %d answers %q, want %q -- the results are in completion order", i, m.ToolCallID, want)
		}
		if want := fmt.Sprintf("page-%d", i); !strings.Contains(m.Content, want) {
			t.Errorf("message %d carries %q, which is not %s's result", i, m.Content, want)
		}
	}
}

// The ceiling is applied before anything runs rather than inside the loop that
// runs it. Decided per call, the answer would depend on which goroutine got
// there first -- a run that cannot be reproduced even against a fixed script.
//
// Over-ceiling calls are still answered, because a tool call with no result
// leaves the conversation malformed.
func TestTheToolCallCeilingIsDecidedBeforeTheFanOut(t *testing.T) {
	var served int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served++
		mu.Unlock()
		fmt.Fprint(w, `<html><head><title>Page</title></head><body><p>Body.</p></body></html>`)
	}))
	defer server.Close()

	var calls []toolCall
	for i := range 3 {
		calls = append(calls, callN(i, toolFetchPage, fetchArgs(fmt.Sprintf("%s/page-%d", server.URL, i))))
	}

	a, rec := recordingAgent(
		stubTurn{ToolCalls: calls},
		proposeCall(t, modelProposal{Category: "stay", Notes: "Done."}),
	)
	a.fetcher = newFetcherAllowing(strings.TrimPrefix(server.URL, "http://"))
	limits := DefaultLimits()
	limits.MaxToolCalls = 1
	a.limits = limits

	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	results := toolMessages(rec.last())
	if len(results) != 3 {
		t.Fatalf("tool messages = %d, want one per call even over the ceiling", len(results))
	}
	for i, m := range results {
		if want := calls[i].ID; m.ToolCallID != want {
			t.Errorf("message %d answers %q, want %q", i, m.ToolCallID, want)
		}
	}
	if strings.Contains(results[0].Content, "No more tool calls") {
		t.Error("the one call within the ceiling was refused rather than run")
	}
	for _, i := range []int{1, 2} {
		if !strings.Contains(results[i].Content, "No more tool calls") {
			t.Errorf("message %d = %q, want the out-of-budget answer", i, results[i].Content)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if served != 1 {
		t.Errorf("the server was asked %d times, want 1: the ceiling did not stop the fan-out", served)
	}
}
