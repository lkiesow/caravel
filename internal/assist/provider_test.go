package assist

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A fake OpenAI-compatible endpoint. Records what it was sent, so the tests
// can assert on the request shape as well as on the parsed response -- the
// request is half the contract, and a wrong tool_choice or a dropped
// tool_call_id fails only against a real server otherwise.
type fakeEndpoint struct {
	*httptest.Server
	lastRequest wireRequest
	lastRaw     []byte
	handler     func(w http.ResponseWriter, body wireRequest)
	calls       int
}

func newFakeEndpoint(t *testing.T, handler func(w http.ResponseWriter, body wireRequest)) *fakeEndpoint {
	t.Helper()
	f := &fakeEndpoint{handler: handler}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.lastRaw = raw
		_ = json.Unmarshal(raw, &f.lastRequest)
		f.calls++
		w.Header().Set("Content-Type", "application/json")
		f.handler(w, f.lastRequest)
	}))
	t.Cleanup(f.Close)
	return f
}

func writeChoice(w http.ResponseWriter, content string, calls []toolCall, finish string) {
	resp := map[string]any{
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": content, "tool_calls": calls},
			"finish_reason": finish,
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func TestProviderPlainCompletion(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		writeChoice(w, "hello", nil, "stop")
	})
	p := newHTTPProvider(f.URL, "secret-key", "some-model")

	resp, err := p.Complete(context.Background(), chatRequest{
		Messages: []chatMessage{{Role: roleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello")
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
	if f.lastRequest.Model != "some-model" {
		t.Errorf("model = %q, want the configured one", f.lastRequest.Model)
	}
	// No tools were offered, so tool_choice must be absent rather than "auto"
	// -- some servers reject "auto" with an empty tools array.
	if f.lastRequest.ToolChoice != "" {
		t.Errorf("tool_choice = %q, want empty when no tools are offered", f.lastRequest.ToolChoice)
	}
}

func TestProviderSendsAuthorizationOnlyWhenKeyed(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		writeChoice(w, "ok", nil, "stop")
	}))
	defer srv.Close()

	if _, err := newHTTPProvider(srv.URL, "sk-abc", "m").Complete(context.Background(), chatRequest{}); err != nil {
		t.Fatalf("keyed: %v", err)
	}
	if gotAuth != "Bearer sk-abc" {
		t.Errorf("Authorization = %q, want the bearer key", gotAuth)
	}

	// A local Ollama or llama.cpp needs no key, and sending an empty bearer
	// makes some of them reject the request outright.
	if _, err := newHTTPProvider(srv.URL, "", "m").Complete(context.Background(), chatRequest{}); err != nil {
		t.Fatalf("keyless: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want no header when there is no key", gotAuth)
	}
}

func TestProviderToolCallRoundTrip(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, body wireRequest) {
		// First turn: ask for a tool. Second: answer, having seen the result.
		if len(body.Messages) == 1 {
			var c toolCall
			c.ID = "call_1"
			c.Type = "function"
			c.Function.Name = toolWebSearch
			c.Function.Arguments = `{"query":"kex hostel"}`
			writeChoice(w, "", []toolCall{c}, "tool_calls")
			return
		}
		writeChoice(w, "done", nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	first, err := p.Complete(context.Background(), chatRequest{
		Messages: []chatMessage{{Role: roleUser, Content: "enrich"}},
		Tools: []toolDef{{
			Name:        toolWebSearch,
			Description: "search",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != toolWebSearch {
		t.Fatalf("tool calls = %+v, want one %s", first.ToolCalls, toolWebSearch)
	}
	if first.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", first.FinishReason)
	}
	if f.lastRequest.ToolChoice != "auto" {
		t.Errorf("tool_choice = %q, want auto when tools are offered", f.lastRequest.ToolChoice)
	}

	// The assistant turn must be echoed back with its tool_calls intact, and
	// the result must name the call it answers. Servers reject a tool message
	// whose tool_call_id matches nothing, so this is a real constraint rather
	// than tidiness.
	second, err := p.Complete(context.Background(), chatRequest{
		Messages: []chatMessage{
			{Role: roleUser, Content: "enrich"},
			{Role: roleAssistant, ToolCalls: first.ToolCalls},
			{Role: roleTool, ToolCallID: "call_1", Content: `{"results":[]}`},
		},
	})
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if second.Content != "done" {
		t.Errorf("Content = %q", second.Content)
	}
	if !strings.Contains(string(f.lastRaw), `"tool_call_id":"call_1"`) {
		t.Errorf("the tool result did not carry its tool_call_id; body was %s", f.lastRaw)
	}
}

func TestProviderJSONSchemaFormat(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		writeChoice(w, `{"title":"Kex","category":"stay","tags":"hostel","notes":"n","address":"a","place_name":"p","links":[]}`, nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	var out modelProposal
	if _, _, err := completeJSON(context.Background(), p, chatRequest{
		Messages: []chatMessage{{Role: roleUser, Content: "go"}},
		Format:   proposalFormat(),
	}, &out); err != nil {
		t.Fatalf("completeJSON: %v", err)
	}
	if out.Category != "stay" || out.Tags != "hostel" {
		t.Errorf("decoded = %+v", out)
	}

	// strict:true is the point of preferring json_schema: without it the
	// server is not constraining generation and we are back to hoping.
	var sent struct {
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string `json:"name"`
				Strict bool   `json:"strict"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(f.lastRaw, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.ResponseFormat.Type != formatJSONSchema {
		t.Errorf("response_format.type = %q", sent.ResponseFormat.Type)
	}
	if !sent.ResponseFormat.JSONSchema.Strict {
		t.Error("strict was not set on the json_schema request")
	}
	if sent.ResponseFormat.JSONSchema.Name != proposalSchemaName {
		t.Errorf("schema name = %q", sent.ResponseFormat.JSONSchema.Name)
	}
}

// The fallback that the plan calls the real work: a proxy that only speaks
// json_object. The first request is rejected, the provider downgrades, and
// every later turn goes straight to json_object without paying the 400 again.
func TestProviderFallsBackToJSONObject(t *testing.T) {
	var formats []string
	f := newFakeEndpoint(t, func(w http.ResponseWriter, body wireRequest) {
		kind := "none"
		if len(body.ResponseFormat) > 0 {
			var rf struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(body.ResponseFormat, &rf)
			kind = rf.Type
		}
		formats = append(formats, kind)

		if kind == formatJSONSchema {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"message": "response_format type json_schema is not supported"},
			})
			return
		}
		writeChoice(w, `{"title":"","category":"site","tags":"museum","notes":"","address":"","place_name":"","links":[]}`, nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	var out modelProposal
	if _, _, err := completeJSON(context.Background(), p, chatRequest{
		Messages: []chatMessage{{Role: roleUser, Content: "go"}},
		Format:   proposalFormat(),
	}, &out); err != nil {
		t.Fatalf("first completeJSON: %v", err)
	}
	if out.Tags != "museum" {
		t.Errorf("decoded = %+v", out)
	}
	if len(formats) != 2 || formats[0] != formatJSONSchema || formats[1] != formatJSONObject {
		t.Fatalf("format sequence = %v, want json_schema then json_object", formats)
	}

	// Second call: the downgrade is remembered, so no wasted 400.
	if _, _, err := completeJSON(context.Background(), p, chatRequest{
		Messages: []chatMessage{{Role: roleUser, Content: "again"}},
		Format:   proposalFormat(),
	}, &out); err != nil {
		t.Fatalf("second completeJSON: %v", err)
	}
	if len(formats) != 3 || formats[2] != formatJSONObject {
		t.Errorf("format sequence = %v, want the downgrade to stick", formats)
	}
}

// A 400 that is not about response_format must not be mistaken for one, or a
// real error gets hidden behind a confusing retry.
func TestProviderDoesNotFallBackOnUnrelatedBadRequest(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "model \"nope\" does not exist"},
		})
	})
	p := newHTTPProvider(f.URL, "", "nope")

	var out modelProposal
	_, _, err := completeJSON(context.Background(), p, chatRequest{Format: proposalFormat()}, &out)
	if err == nil {
		t.Fatal("completeJSON succeeded, want the 400 surfaced")
	}
	if f.calls != 1 {
		t.Errorf("endpoint called %d times, want 1 — an unrelated 400 must not trigger the downgrade", f.calls)
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want the server's own complaint", err)
	}
}

func TestIsUnsupportedFormat(t *testing.T) {
	cases := []struct {
		status  int
		message string
		want    bool
	}{
		{400, "response_format type json_schema is not supported", true},
		{422, "Invalid response_format: json_schema", true},
		{400, "unknown parameter response_format", true},
		{400, "model does not exist", false},
		{400, "response_format is required", false}, // mentions it, but is not a support complaint
		{500, "response_format json_schema unsupported", false},
		{401, "invalid api key", false},
	}
	for _, tc := range cases {
		if got := isUnsupportedFormat(tc.status, tc.message); got != tc.want {
			t.Errorf("isUnsupportedFormat(%d, %q) = %t, want %t", tc.status, tc.message, got, tc.want)
		}
	}
}

// json_object mode promises valid JSON and nothing about the shape, so a wrong
// shape has to be caught here and retried with the complaint attached.
func TestCompleteJSONRetriesOnWrongShape(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, body wireRequest) {
		if len(body.Messages) == 1 {
			writeChoice(w, `["not", "an", "object"]`, nil, "stop")
			return
		}
		writeChoice(w, `{"title":"","category":"site","tags":"museum","notes":"","address":"","place_name":"","links":[]}`, nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	var out modelProposal
	spent, _, err := completeJSON(context.Background(), p, chatRequest{
		Messages: []chatMessage{{Role: roleUser, Content: "go"}},
		Format:   responseFormat{Kind: formatJSONObject},
	}, &out)
	if err != nil {
		t.Fatalf("completeJSON: %v", err)
	}
	if out.Tags != "museum" {
		t.Errorf("decoded = %+v", out)
	}
	if f.calls != 2 {
		t.Errorf("endpoint called %d times, want 2", f.calls)
	}
	// A retry is billed, so it must be counted -- otherwise a model that
	// always needs one turn quietly doubles what the budget thinks it spent.
	if spent.TotalTokens != 30 {
		t.Errorf("usage = %d, want both attempts counted (30)", spent.TotalTokens)
	}
	// The retry must name what was wrong; restating the request unchanged
	// would just produce the same answer.
	if !strings.Contains(string(f.lastRaw), "not valid for the required format") {
		t.Errorf("retry did not carry the complaint; body was %s", f.lastRaw)
	}
}

func TestCompleteJSONGivesUpAfterOneRetry(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		writeChoice(w, `nonsense`, nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	var out modelProposal
	_, _, err := completeJSON(context.Background(), p, chatRequest{Format: responseFormat{Kind: formatJSONObject}}, &out)
	if err == nil {
		t.Fatal("completeJSON succeeded on unparseable output")
	}
	// Two attempts, not a loop: each one is billed, and a model that cannot
	// produce the shape twice will not produce it on the fifth attempt.
	if f.calls != 2 {
		t.Errorf("endpoint called %d times, want exactly 2", f.calls)
	}
	if !strings.Contains(err.Error(), "after a retry") {
		t.Errorf("error = %v, want it to say the retry was used", err)
	}
}

// Models emit fenced JSON even when told not to. Spending a paid retry on a
// formatting habit would be waste.
func TestCompleteJSONToleratesCodeFences(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		writeChoice(w, "```json\n{\"title\":\"\",\"category\":\"site\",\"tags\":\"museum\",\"notes\":\"\",\"address\":\"\",\"place_name\":\"\",\"links\":[]}\n```", nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	var out modelProposal
	if _, _, err := completeJSON(context.Background(), p, chatRequest{Format: responseFormat{Kind: formatJSONObject}}, &out); err != nil {
		t.Fatalf("completeJSON: %v", err)
	}
	if out.Tags != "museum" {
		t.Errorf("decoded = %+v", out)
	}
	if f.calls != 1 {
		t.Errorf("endpoint called %d times, want 1 — a code fence must not cost a retry", f.calls)
	}
}

// A field the model invented is noise to ignore, not a reason to spend a paid
// retry. (Contrast readJSON in the HTTP layer, where an unknown field means a
// bug in our own client and is rejected.)
func TestCompleteJSONIgnoresUnknownFields(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		writeChoice(w, `{"title":"","category":"site","tags":"museum","notes":"","address":"","place_name":"","links":[],"confidence":0.9}`, nil, "stop")
	})
	p := newHTTPProvider(f.URL, "", "m")

	var out modelProposal
	if _, _, err := completeJSON(context.Background(), p, chatRequest{Format: responseFormat{Kind: formatJSONObject}}, &out); err != nil {
		t.Fatalf("completeJSON: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("endpoint called %d times, want 1", f.calls)
	}
}

func TestProviderSurfacesNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>502 Bad Gateway</body></html>")
	}))
	defer srv.Close()

	_, err := newHTTPProvider(srv.URL, "", "m").Complete(context.Background(), chatRequest{})
	if err == nil {
		t.Fatal("Complete succeeded on an HTML body")
	}
	// Nearly always a wrong URL or a proxy in the way, so it says so rather
	// than reporting a JSON syntax error at byte 1.
	if !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("error = %v, want it to name the real problem", err)
	}
}

func TestProviderHonorsContextCancellation(t *testing.T) {
	f := newFakeEndpoint(t, func(w http.ResponseWriter, _ wireRequest) {
		writeChoice(w, "ok", nil, "stop")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newHTTPProvider(f.URL, "", "m").Complete(ctx, chatRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
