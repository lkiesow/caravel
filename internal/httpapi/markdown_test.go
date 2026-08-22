package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// jsonBody exists because this file's inputs are markdown: newlines, quotes,
// backslashes and angle brackets. Every other test in the package hand-writes
// its JSON, which is fine for {"title":"Hotel"} and would silently mangle
// "first\nsecond" - and a mangled input would make an assertion about hard
// wraps pass or fail for the wrong reason.
func jsonBody(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// The preview endpoint's whole job is to agree with the view page. So the
// assertions here are of two kinds: concrete expected HTML written out by hand
// (never computed by calling the code under test - see the todo.md note about
// tests that ask the code what to expect), and one cross-endpoint check that
// compares two independent HTTP responses.

func previewHTML(t *testing.T, ts *testServer, as *http.Cookie, source string) string {
	t.Helper()
	body, err := jsonBody(map[string]string{"markdown": source})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	w := ts.do(http.MethodPost, "/api/markdown/preview", as, body)
	if w.Code != http.StatusOK {
		t.Fatalf("preview %q: got %d, body %s", source, w.Code, w.Body.String())
	}
	return decode[struct {
		HTML string `json:"html"`
	}](t, w).HTML
}

func TestMarkdownPreviewRendersCommonMark(t *testing.T) {
	ts := newTestServer(t)
	as := ts.login("writer")

	for _, tc := range []struct{ name, source, want string }{
		{"heading", "# Packing", "<h1>Packing</h1>"},
		{"emphasis", "**warm** socks", "<strong>warm</strong>"},
		{"list", "- one\n- two", "<li>one</li>"},
		{"link", "[map](https://example.com/)", `href="https://example.com/"`},
		// Hard wraps are a deliberate deviation from CommonMark, which would
		// collapse a single newline to a space. If the preview lost them it
		// would disagree with the view page on every multi-line note.
		{"hard wrap", "first\nsecond", "<br>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := previewHTML(t, ts, as, tc.source); !strings.Contains(got, tc.want) {
				t.Errorf("preview of %q is %q, want it to contain %q", tc.source, got, tc.want)
			}
		})
	}
}

// The sanitizer is covered in internal/markdown; this asserts the *endpoint*
// goes through it, which is a different claim and the one that matters for a
// route that renders whatever it is handed.
func TestMarkdownPreviewSanitizes(t *testing.T) {
	ts := newTestServer(t)
	as := ts.login("writer")

	for _, tc := range []struct{ name, source, mustNotContain string }{
		{"script tag", "hello <script>alert(1)</script>", "<script"},
		{"event handler", `<img src=x onerror="alert(1)">`, "onerror"},
		{"javascript url", "[click](javascript:alert(1))", "javascript:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := previewHTML(t, ts, as, tc.source)
			if strings.Contains(got, tc.mustNotContain) {
				t.Errorf("preview of %q is %q, which still contains %q", tc.source, got, tc.mustNotContain)
			}
		})
	}
}

// The point of the whole milestone: what the preview shows is what the view page
// will show. Two independent responses compared against each other, so neither
// side is the other's expectation.
func TestMarkdownPreviewMatchesTheItemPayload(t *testing.T) {
	ts := newTestServer(t)
	as := ts.login("writer")
	tripID := ts.createTrip(as, "Iceland")

	const notes = "## Day one\n\nDrive to *Vik*, then:\n\n- check in\n- eat\n\nA second line\nafter a hard wrap."

	body, err := jsonBody(map[string]any{"title": "Hotel", "category": "stay", "notes": notes})
	if err != nil {
		t.Fatalf("encode item: %v", err)
	}
	itemID := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/items", as, body, http.StatusCreated)

	w := ts.do(http.MethodGet, "/api/items/"+itemID, as, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get item: got %d, body %s", w.Code, w.Body.String())
	}
	saved := decode[struct {
		NotesHTML *string `json:"notes_html"`
	}](t, w)
	if saved.NotesHTML == nil {
		t.Fatal("the item payload carries no notes_html, so there is nothing to agree with")
	}

	if got := previewHTML(t, ts, as, notes); got != *saved.NotesHTML {
		t.Errorf("preview and the saved item disagree:\n preview: %q\n   saved: %q", got, *saved.NotesHTML)
	}
}

func TestMarkdownPreviewRejectsAnOversizedNote(t *testing.T) {
	ts := newTestServer(t)
	as := ts.login("writer")

	body, err := jsonBody(map[string]string{"markdown": strings.Repeat("x", maxPreviewMarkdownBytes+1)})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	if w := ts.do(http.MethodPost, "/api/markdown/preview", as, body); w.Code != http.StatusBadRequest {
		t.Errorf("oversized note: got %d, want 400", w.Code)
	}

	// And the boundary itself is accepted, so the cap is off-by-none.
	body, err = jsonBody(map[string]string{"markdown": strings.Repeat("x", maxPreviewMarkdownBytes)})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	if w := ts.do(http.MethodPost, "/api/markdown/preview", as, body); w.Code != http.StatusOK {
		t.Errorf("note exactly at the cap: got %d, want 200", w.Code)
	}
}

// An empty note is a 200 with empty HTML, not an error: it is a field nobody has
// typed into, and the client decides what to say about that.
func TestMarkdownPreviewEmptyNote(t *testing.T) {
	ts := newTestServer(t)
	as := ts.login("writer")

	if got := previewHTML(t, ts, as, ""); strings.TrimSpace(got) != "" {
		t.Errorf("preview of an empty note is %q, want empty", got)
	}
}

func TestMarkdownPreviewRequiresAuth(t *testing.T) {
	ts := newTestServer(t)

	body, err := jsonBody(map[string]string{"markdown": "# hello"})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	if w := ts.do(http.MethodPost, "/api/markdown/preview", nil, body); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous preview: got %d, want 401", w.Code)
	}
}
