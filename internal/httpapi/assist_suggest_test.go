package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The trip-level endpoint. Its stream is the single-location endpoint's stream
// -- assist_stream_test.go covers the framing, the cancellation rule, the
// error contract and the two limiters, and none of that is repeated here. What
// is new is the route, its request validation, and the shape of the final
// event.

func postAssistSuggest(t *testing.T, srv *httptest.Server, cookie *http.Cookie, tripID, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/trips/"+tripID+"/assist/locations", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

const suggestBody = `{"prompt":"things to do in Reykjavik","locale":"en"}`

func TestAssistSuggestStreamsProgressThenCandidates(t *testing.T) {
	ts := newTestServer(t)
	ts.Assist = stubAssistant(t)
	srv := ts.liveServer(t)

	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	resp := postAssistSuggest(t, srv, cookie, tripID, suggestBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	events := readSSE(t, bufio.NewReader(resp.Body))

	var final, summary *sseEvent
	progress := 0
	for i := range events {
		switch events[i].Name {
		case "suggestions":
			final = &events[i]
		case "summary":
			summary = &events[i]
		case "progress":
			progress++
		}
	}
	if progress == 0 {
		t.Error("no progress events: a run this long reads as a hang without them")
	}
	if summary == nil {
		t.Error("no summary event")
	}
	if final == nil {
		t.Fatalf("no suggestions event; got %v", eventNames(events))
	}

	var out assistSuggestionsResponse
	if err := json.Unmarshal([]byte(final.Data), &out); err != nil {
		t.Fatalf("decode suggestions: %v", err)
	}
	if len(out.Candidates) != 3 {
		t.Fatalf("candidates = %d, want the stub's 3", len(out.Candidates))
	}
	first := out.Candidates[0]
	if first.Title == "" || first.Category == "" || first.Notes == "" {
		t.Errorf("first candidate is missing what the stub proposed: %+v", first)
	}
	// The link the stub proposes points at the loopback fixture, so it
	// survives the liveness check -- which is what makes this a test of the
	// real path rather than of a fixture being echoed back.
	if len(first.Links) == 0 {
		t.Error("the first candidate lost its link")
	}
	if len(out.Sources) == 0 {
		t.Error("no sources: the run read two pages")
	}
}

// The endpoint answers 501 before it looks the trip up, so a server with the
// assistant switched off does not reveal whether a trip id exists.
func TestAssistSuggestIsNotImplementedWhenDisabled(t *testing.T) {
	ts := newTestServer(t)
	srv := ts.liveServer(t)

	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	resp := postAssistSuggest(t, srv, cookie, tripID, suggestBody)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}

// Nothing else to work from means an empty prompt is a paid run asking the
// model to guess. Refused before the slot is taken, let alone the provider.
func TestAssistSuggestRequiresAPrompt(t *testing.T) {
	ts := newTestServer(t)
	ts.Assist = stubAssistant(t)
	srv := ts.liveServer(t)

	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	resp := postAssistSuggest(t, srv, cookie, tripID, `{"prompt":"   "}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// A viewer cannot run it. Not because they could not read the answer, but
// because the request may carry the trip title and dates outward to a
// third-party API, which is not a read-only participant's call to make.
func TestAssistSuggestRefusesAViewer(t *testing.T) {
	ts := newTestServer(t)
	ts.Assist = stubAssistant(t)
	srv := ts.liveServer(t)

	owner := ts.login("owner")
	viewer := ts.login("viewer")
	tripID := ts.createTrip(owner, "Iceland")
	ts.mustCreateNoID(http.MethodPost, "/api/trips/"+tripID+"/members", owner,
		`{"username":"viewer","role":"viewer"}`, http.StatusCreated)

	resp := postAssistSuggest(t, srv, viewer, tripID, suggestBody)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a viewer ran a suggestion: status = %d", resp.StatusCode)
	}
}

// The trip's own locations reach the run, which is what lets the prompt name
// them and the dedup catch what the prompt does not. The stub proposes Kex
// Hostel among its three, so a trip that already has it comes back with two.
func TestAssistSuggestDropsWhatTheTripAlreadyHas(t *testing.T) {
	ts := newTestServer(t)
	ts.Assist = stubAssistant(t)
	srv := ts.liveServer(t)

	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")
	ts.createItem(cookie, tripID, "Kex Hostel")

	resp := postAssistSuggest(t, srv, cookie, tripID, suggestBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out assistSuggestionsResponse
	for _, e := range readSSE(t, bufio.NewReader(resp.Body)) {
		if e.Name == "suggestions" {
			if err := json.Unmarshal([]byte(e.Data), &out); err != nil {
				t.Fatalf("decode suggestions: %v", err)
			}
		}
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2 with Kex Hostel dropped", len(out.Candidates))
	}
	if out.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", out.Dropped)
	}
	for _, c := range out.Candidates {
		if c.Title == "Kex Hostel" {
			t.Error("a place already on the trip was offered again")
		}
	}
}

func eventNames(events []sseEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Name)
	}
	return out
}
