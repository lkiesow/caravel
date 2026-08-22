package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// An OpenAI-compatible chat-completions client, which is the only wire format
// this package speaks. Ollama, vLLM, llama.cpp, LM Studio and every hosted
// proxy worth pointing at implement it, so supporting one shape reaches almost
// everything without an SDK or a vendor abstraction.
//
// CARAVEL_LLM_URL is the *full* endpoint ("https://host/v1/chat/completions"),
// not a base URL to which a path gets appended. Same convention as
// CARAVEL_GEOCODER_URL, and it means an endpoint mounted somewhere unusual
// needs no special case.

// provider is one turn of conversation. The HTTP client and the in-process
// stub both implement it, which is what lets the agent loop, the validation
// and the SSE transport all run unchanged in tests with no key and no network.
type provider interface {
	Complete(ctx context.Context, req chatRequest) (*chatResponse, error)
}

// How long a single turn may take. The whole-run deadline is the agent's job
// (see agent.go); this is the narrower "this one request has stopped
// responding" guard, generous because a large model on a cold cache genuinely
// is slow.
const turnTimeout = 90 * time.Second

type chatRequest struct {
	Messages []chatMessage
	Tools    []toolDef
	// Format asks for structured output. Zero value means ordinary text.
	Format responseFormat
}

// Role constants for chatMessage. Strings rather than a type because they go
// straight onto the wire and nothing branches on them exhaustively.
const (
	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCalls is set on an assistant message that asked for tools. It must
	// be echoed back verbatim in the history, or the following tool results
	// have nothing to attach to and most servers reject the request.
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	// ToolCallID is set on a roleTool message, naming the call it answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		// Arguments is a JSON *string* containing JSON, not a nested object.
		// That is the API's shape, not a mistake, and it is why callers have
		// to unmarshal it a second time.
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolDef struct {
	Name        string
	Description string
	// Parameters is a JSON Schema object describing the arguments.
	Parameters json.RawMessage
}

// responseFormat selects structured output.
//
// Two kinds, because servers disagree about what they support. json_schema
// with strict:true is what we want -- the server constrains generation, so
// malformed output is impossible rather than merely unlikely. json_object only
// promises "some valid JSON", which means the shape has to be validated
// afterwards and a wrong shape retried. See completeJSON.
type responseFormat struct {
	// Kind is "", "json_object" or "json_schema".
	Kind   string
	Name   string
	Schema json.RawMessage
}

type chatResponse struct {
	Content   string
	ToolCalls []toolCall
	// FinishReason is the server's word for why it stopped: "stop",
	// "tool_calls", "length". Reported so the agent can tell a truncated
	// answer from a complete one.
	FinishReason string
	Usage        usage
}

// usage is the token accounting the agent's budget is spent against. Servers
// that omit it leave zeroes, which the agent treats as "unknown" rather than
// as "free" -- see the iteration ceiling, which exists for exactly that case.
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type httpProvider struct {
	url    string
	key    string
	model  string
	client *http.Client

	// schemaUnsupported is set once a server has rejected json_schema, so the
	// fallback is paid for with one failed request per process rather than one
	// per turn. Not guarded by a mutex: it is written at most once per
	// provider and a benign double-write costs one extra retry, where locking
	// every turn to protect a bool costs more than it saves.
	schemaUnsupported bool
}

func newHTTPProvider(url, key, model string) *httpProvider {
	return &httpProvider{
		url:    url,
		key:    key,
		model:  model,
		client: &http.Client{Timeout: turnTimeout},
	}
}

// The wire types. Separate from the domain types above so the JSON shape can
// follow the API rather than the other way round -- the same split as
// nominatimResult in internal/geocode.
type wireRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Tools          []wireTool      `json:"tools,omitempty"`
	ToolChoice     string          `json:"tool_choice,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	// Low but not zero. This is an extraction task with a right answer, so
	// creativity is not wanted; zero outright makes some models loop on a
	// tool call they have already made.
	Temperature float64 `json:"temperature"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
	// Error carries the server's own complaint on a non-200, which is what
	// the json_schema fallback sniffs.
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *httpProvider) Complete(ctx context.Context, req chatRequest) (*chatResponse, error) {
	format := req.Format
	if format.Kind == formatJSONSchema && p.schemaUnsupported {
		format = responseFormat{Kind: formatJSONObject}
	}

	resp, err := p.post(ctx, req, format)
	if err == nil {
		return resp, nil
	}

	// The one retry worth making automatically: a server that does not know
	// json_schema. Downgrading and trying again turns a hard failure into a
	// validated-and-retried path, and the flag means the next turn goes
	// straight to json_object.
	var unsupported errUnsupportedFormat
	if req.Format.Kind == formatJSONSchema && errors.As(err, &unsupported) {
		p.schemaUnsupported = true
		return p.post(ctx, req, responseFormat{Kind: formatJSONObject})
	}
	return nil, err
}

func (p *httpProvider) post(ctx context.Context, req chatRequest, format responseFormat) (*chatResponse, error) {
	body := wireRequest{
		Model:       p.model,
		Messages:    req.Messages,
		Temperature: 0.2,
	}
	for _, t := range req.Tools {
		var wt wireTool
		wt.Type = "function"
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.Parameters
		body.Tools = append(body.Tools, wt)
	}
	if len(body.Tools) > 0 {
		body.ToolChoice = "auto"
	}
	if rf := format.marshal(); rf != nil {
		body.ResponseFormat = rf
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("assist: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("assist: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.key)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		// Deliberately not wrapped with the URL: it is operator configuration,
		// and this error can reach a log a user sees.
		return nil, fmt.Errorf("assist: the model endpoint could not be reached: %w", err)
	}
	defer httpResp.Body.Close()

	// Capped: an HTML error page from a misconfigured proxy should not be read
	// into memory in full, and nothing legitimate here is large.
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("assist: read response: %w", err)
	}

	var decoded wireResponse
	// A body that is not JSON at all is common enough to deserve its own
	// message: it is nearly always a proxy or a wrong URL, not a model.
	decodeErr := json.Unmarshal(raw, &decoded)

	if httpResp.StatusCode != http.StatusOK {
		msg := ""
		if decoded.Error != nil {
			msg = decoded.Error.Message
		}
		if isUnsupportedFormat(httpResp.StatusCode, msg) {
			return nil, errUnsupportedFormat{message: msg}
		}
		return nil, errProviderStatus{code: httpResp.StatusCode, message: truncate(msg, 300)}
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("assist: the model endpoint returned a response that is not JSON (%d bytes)", len(raw))
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("assist: the model returned no choices")
	}

	choice := decoded.Choices[0]
	return &chatResponse{
		Content:      choice.Message.Content,
		ToolCalls:    choice.Message.ToolCalls,
		FinishReason: choice.FinishReason,
		Usage:        decoded.Usage,
	}, nil
}

const (
	formatJSONObject = "json_object"
	formatJSONSchema = "json_schema"
)

func (f responseFormat) marshal() json.RawMessage {
	switch f.Kind {
	case formatJSONObject:
		return json.RawMessage(`{"type":"json_object"}`)
	case formatJSONSchema:
		// strict:true is what makes this worth preferring: the server
		// constrains generation to the schema, so a malformed shape is
		// impossible rather than merely unlikely.
		wrapper := struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string          `json:"name"`
				Strict bool            `json:"strict"`
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		}{Type: formatJSONSchema}
		wrapper.JSONSchema.Name = f.Name
		wrapper.JSONSchema.Strict = true
		wrapper.JSONSchema.Schema = f.Schema
		out, err := json.Marshal(wrapper)
		if err != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

// errUnsupportedFormat means the server rejected the response_format, rather
// than rejecting anything about the conversation. Its own type because it is
// the one provider error with an automatic recovery.
type errUnsupportedFormat struct{ message string }

func (e errUnsupportedFormat) Error() string {
	return "assist: the model endpoint does not support json_schema response format: " + e.message
}

type errProviderStatus struct {
	code    int
	message string
}

func (e errProviderStatus) Error() string {
	if e.message == "" {
		return fmt.Sprintf("assist: the model endpoint responded with status %d", e.code)
	}
	return fmt.Sprintf("assist: the model endpoint responded with status %d: %s", e.code, e.message)
}

// isUnsupportedFormat guesses whether a 4xx is about response_format.
//
// A guess by necessity: there is no standard error code for it, and the
// servers that lack json_schema disagree about the wording. Being wrong is
// cheap in one direction and not the other, so the match is deliberately
// narrow -- a false positive costs one extra request that fails the same way,
// while being too eager would hide real errors behind a confusing retry.
func isUnsupportedFormat(status int, message string) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	m := strings.ToLower(message)
	if !strings.Contains(m, "response_format") && !strings.Contains(m, "json_schema") {
		return false
	}
	for _, hint := range []string{"unsupported", "not supported", "invalid", "unknown", "unrecognized", "unrecognised"} {
		if strings.Contains(m, hint) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// completeJSON asks for a structured answer and decodes it into out.
//
// The retry is the whole reason this exists. With json_schema the server
// constrains generation and the first answer is well-formed by construction;
// with json_object -- all a lot of proxies support -- "valid JSON" is the only
// promise, so the shape has to be checked here and a wrong one sent back with
// the complaint attached. One retry, not a loop: a model that cannot produce
// the shape twice will not produce it on the fifth attempt either, and each
// attempt is billed.
//
// Returns the usage summed across every attempt, so a retry is not free in the
// agent's budget.
func completeJSON(ctx context.Context, p provider, req chatRequest, out any) (usage, error) {
	var total usage

	resp, err := p.Complete(ctx, req)
	if err != nil {
		return total, err
	}
	total = addUsage(total, resp.Usage)

	decodeErr := decodeJSONAnswer(resp.Content, out)
	if decodeErr == nil {
		return total, nil
	}

	// Feed the failure back rather than restating the request: naming what was
	// wrong with the previous answer is what makes the second attempt
	// different from the first.
	retry := req
	retry.Messages = append(append([]chatMessage{}, req.Messages...),
		chatMessage{Role: roleAssistant, Content: resp.Content},
		chatMessage{Role: roleUser, Content: "That was not valid for the required format: " + decodeErr.Error() +
			". Reply with the JSON object only -- no prose, no code fences."},
	)

	resp, err = p.Complete(ctx, retry)
	if err != nil {
		return total, err
	}
	total = addUsage(total, resp.Usage)

	if err := decodeJSONAnswer(resp.Content, out); err != nil {
		return total, fmt.Errorf("assist: the model did not return the required shape after a retry: %w", err)
	}
	return total, nil
}

// decodeJSONAnswer unmarshals a model's answer, tolerating the one deviation
// that is near-universal and harmless: wrapping the object in a markdown code
// fence. Models do this even when told not to, and rejecting it would spend a
// retry on a formatting habit rather than on a wrong answer.
func decodeJSONAnswer(content string, out any) error {
	trimmed := stripCodeFence(strings.TrimSpace(content))
	if trimmed == "" {
		return fmt.Errorf("the answer was empty")
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	// Unknown fields are *not* rejected. A model adding a field it invented is
	// noise to ignore, not a reason to spend a paid retry -- unlike a request
	// body from our own client, where an unknown field means a bug.
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence and its optional language tag.
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		return s
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func addUsage(a, b usage) usage {
	return usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
	}
}
