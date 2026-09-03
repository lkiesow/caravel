package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// jsonString quotes a string for embedding in a request body literal, so a
// test can use a note with real newlines in it without hand-escaping them.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// The trip notepad: GET/PUT /api/trips/{tripId}/notes.
//
// Authorization is covered by the tables in roles_test.go and
// ownership_test.go, which both carry rows for these two routes. What is left
// for this file is the behaviour those tables deliberately do not assert: the
// response shape, the markdown rendering, and the two states a note can be in.

type noteBody struct {
	Body      string  `json:"body"`
	BodyHTML  string  `json:"body_html"`
	UpdatedAt *string `json:"updated_at"`
}

func (ts *testServer) getNote(t *testing.T, cookie *http.Cookie, tripID string) noteBody {
	t.Helper()
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/notes", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get note: got %d, body %s", w.Code, w.Body.String())
	}
	return decode[noteBody](t, w)
}

func (ts *testServer) putNote(t *testing.T, cookie *http.Cookie, tripID, body string) noteBody {
	t.Helper()
	w := ts.do(http.MethodPut, "/api/trips/"+tripID+"/notes", cookie, body)
	if w.Code != http.StatusOK {
		t.Fatalf("put note: got %d, body %s", w.Code, w.Body.String())
	}
	return decode[noteBody](t, w)
}

// A trip nobody has written on answers 200 with an empty body, not 404. The
// tab renders one response shape and reads the empty string as "start typing";
// a 404 here would be indistinguishable from a trip that does not exist.
func TestTripNoteIsEmptyBeforeAnyoneWrites(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	got := ts.getNote(t, owner, tripID)
	if got.Body != "" {
		t.Errorf("body = %q, want empty", got.Body)
	}
	if got.BodyHTML != "" {
		t.Errorf("body_html = %q, want empty", got.BodyHTML)
	}
	if got.UpdatedAt != nil {
		t.Errorf("updated_at = %v, want null on a note nobody has saved", *got.UpdatedAt)
	}
}

// The markdown is stored as typed and rendered on read. Both halves matter:
// the editor needs the source back verbatim, the view needs real HTML.
func TestTripNoteRoundTripsAndRenders(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	const src = "## Ferry\n\n- book by *May*\n- ask about the car\n"

	saved := ts.putNote(t, owner, tripID, `{"body":`+jsonString(src)+`}`)
	reread := ts.getNote(t, owner, tripID)

	for name, got := range map[string]noteBody{"PUT response": saved, "GET after": reread} {
		if got.Body != src {
			t.Errorf("%s: body = %q, want the source verbatim %q", name, got.Body, src)
		}
		if !strings.Contains(got.BodyHTML, "<h2") {
			t.Errorf("%s: body_html has no heading: %q", name, got.BodyHTML)
		}
		if n := strings.Count(got.BodyHTML, "<li>"); n != 2 {
			t.Errorf("%s: body_html has %d list items, want 2: %q", name, n, got.BodyHTML)
		}
		if got.UpdatedAt == nil {
			t.Errorf("%s: updated_at is null on a saved note", name)
		}
	}
}

// The rendered HTML is sanitized, because it is the one string the client
// inserts with innerHTML. This is internal/markdown's job and it has its own
// tests; the assertion here is that this endpoint actually goes through it.
func TestTripNoteRendersSanitizedHTML(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	got := ts.putNote(t, owner, tripID, `{"body":"<script>alert(1)</script> and <b>bold</b>"}`)
	if strings.Contains(got.BodyHTML, "<script") {
		t.Errorf("body_html carries a script tag: %q", got.BodyHTML)
	}
}

// Clearing the note removes the row, so a cleared note and a never-written one
// are the same state — which is what lets the tab open in the editor for both.
func TestTripNoteClearedGoesBackToEmpty(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	ts.putNote(t, owner, tripID, `{"body":"something"}`)
	cleared := ts.putNote(t, owner, tripID, `{"body":"   \n  "}`)

	if cleared.Body != "" {
		t.Errorf("body after clearing = %q, want empty", cleared.Body)
	}
	if cleared.UpdatedAt != nil {
		t.Errorf("updated_at after clearing = %v, want null", *cleared.UpdatedAt)
	}
	if again := ts.getNote(t, owner, tripID); again.Body != "" {
		t.Errorf("body on re-read = %q, want empty", again.Body)
	}
}

// Last write wins: no version is carried, so the later save simply stands.
func TestTripNoteLastWriteWins(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	ts.putNote(t, owner, tripID, `{"body":"first"}`)
	ts.putNote(t, owner, tripID, `{"body":"second"}`)

	if got := ts.getNote(t, owner, tripID).Body; got != "second" {
		t.Errorf("body = %q, want %q", got, "second")
	}
}

// The cap is maxPreviewMarkdownBytes, shared with the preview endpoint so a
// saveable note is always a previewable one.
func TestTripNoteTooLongIsRejected(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")

	huge := strings.Repeat("x", maxTripNoteBytes+1)
	w := ts.do(http.MethodPut, "/api/trips/"+tripID+"/notes", owner, `{"body":`+jsonString(huge)+`}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized note: got %d, want 400", w.Code)
	}
	if got := ts.getNote(t, owner, tripID).Body; got != "" {
		t.Errorf("a rejected save still stored %q", got)
	}
}

// Deleting the trip takes the note with it, via the FK cascade.
func TestTripNoteGoesWithTheTrip(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")
	ts.putNote(t, owner, tripID, `{"body":"packing thoughts"}`)

	if w := ts.do(http.MethodDelete, "/api/trips/"+tripID, owner, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete trip: got %d, body %s", w.Code, w.Body.String())
	}
	if _, err := ts.Store.GetTripNote(t.Context(), tripID); err == nil {
		t.Error("the note outlived its trip")
	}
}
