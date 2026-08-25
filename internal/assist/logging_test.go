package assist

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// The run trace, and the level that gates it.
//
// The gate is the whole point: a run is user-triggered, so anything it emits
// above debug is a log somebody else fills up by pressing a button. These
// tests read the records back out of a buffer rather than watching them go to
// stderr, which is why Options carries a Logger at all.

// runWithLogger drives one full stub run at the given level and returns
// everything it logged.
func runWithLogger(t *testing.T, level slog.Level) string {
	t.Helper()
	var buf bytes.Buffer
	a, err := New(Options{
		LLMURL:         LLMStub,
		LLMModel:       "stub-model",
		SearchProvider: "stub",
		Logger:         slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	return buf.String()
}

func TestTheRunTraceIsSilentAboveDebug(t *testing.T) {
	// Not "fewer records" -- none at all. A successful run is not news, and an
	// instance at the default level should be able to serve the assistant all
	// day without the log noticing.
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if out := runWithLogger(t, level); out != "" {
			t.Errorf("a successful run at %s logged:\n%s", level, out)
		}
	}
}

func TestTheRunTraceAccountsForAWholeRun(t *testing.T) {
	out := runWithLogger(t, slog.LevelDebug)

	// The questions the trace exists to answer, each pinned to the record that
	// answers it. Named rather than counted: a count would pass on the wrong
	// records.
	for _, want := range []string{
		"assist: run started",        // what was configured
		"assist: turn",               // where the model time went
		"assist: tool call",          // where the tool time went
		"assist: gathering finished", // why the loop stopped
		"assist: composed",           // the slowest single request of a run
		"assist: run finished",       // the total
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the trace has no %q record:\n%s", want, out)
		}
	}

	// The per-turn accounting has to carry the numbers, not just the fact that
	// a turn happened.
	for _, want := range []string{"ms=", "spent_tokens=", "finish=", "model=stub-model", "search=stub"} {
		if !strings.Contains(out, want) {
			t.Errorf("the trace is missing %q:\n%s", want, out)
		}
	}

	// Why the gathering ended, which is the difference between a model that
	// finished and one that hit a ceiling.
	if !strings.Contains(out, "reason=answered") {
		t.Errorf("the trace does not say why gathering stopped:\n%s", out)
	}

	// Both tool calls the stub makes are traced, with the URL each read.
	if n := strings.Count(out, "assist: tool call"); n < 3 {
		t.Errorf("traced %d tool calls, want the search and both page reads:\n%s", n, out)
	}
}

// The rule from the package comment, asserted rather than left as prose. The
// page body is the one that would actually happen by accident: it is right
// there in the tool result, and logging "the result" rather than its size is
// the obvious thing to write.
func TestTheRunTraceLeaksNeitherKeysNorPageBodies(t *testing.T) {
	var buf bytes.Buffer
	a, err := New(Options{
		LLMURL:         LLMStub,
		LLMModel:       "stub-model",
		LLMKey:         "sk-do-not-log-me-0123456789",
		SearchProvider: "stub",
		Logger:         slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Propose(context.Background(), enrichRequest(), nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "sk-do-not-log-me") {
		t.Error("the API key reached the log")
	}
	// A distinctive sentence from the fixture pages the run reads. The URL and
	// the byte count are what a person debugging needs; the text is up to
	// fetchMaxTextBytes of third-party content.
	if strings.Contains(out, "former biscuit factory") {
		t.Error("the body of a fetched page reached the log")
	}
	// But the size did, which is the number that answers "why did this cost so
	// much".
	if !strings.Contains(out, "result_bytes=") {
		t.Errorf("the trace records no result size:\n%s", out)
	}
}

// The composing phase is the slowest single thing a run does, and "one slow
// request" and "two ordinary requests" want opposite fixes. Before `calls` the
// trace could not tell them apart, which is exactly the ambiguity that made a
// first round of measurements misleading.
func TestTheTraceCountsComposingRequests(t *testing.T) {
	out := runWithLogger(t, slog.LevelDebug)
	if !strings.Contains(out, "calls=1") {
		t.Errorf("the composing record does not report its request count:\n%s", out)
	}
}
