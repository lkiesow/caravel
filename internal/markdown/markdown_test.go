package markdown

import (
	"strings"
	"testing"
)

func TestToSafeHTML(t *testing.T) {
	html, err := ToSafeHTML("A **bold** note with a [link](https://example.com).")
	if err != nil {
		t.Fatalf("ToSafeHTML: %v", err)
	}
	if !strings.Contains(html, "<strong>bold</strong>") {
		t.Errorf("expected rendered <strong>, got: %s", html)
	}
	if !strings.Contains(html, `href="https://example.com"`) {
		t.Errorf("expected rendered link, got: %s", html)
	}
}

// A single newline is a break the author meant, so it must survive as a <br>
// rather than being collapsed into a space — the view page no longer sets
// `white-space: pre-wrap`, so this rendering is the only thing preserving it.
// A blank line still has to produce two separate paragraphs, not one paragraph
// with a <br> in it.
func TestToSafeHTML_HardWraps(t *testing.T) {
	html, err := ToSafeHTML("first line\nsecond line")
	if err != nil {
		t.Fatalf("ToSafeHTML: %v", err)
	}
	if !strings.Contains(html, "<br") {
		t.Errorf("expected a single newline to render as <br>, got: %s", html)
	}
	if strings.Count(html, "<p>") != 1 {
		t.Errorf("expected one paragraph, got: %s", html)
	}

	html, err = ToSafeHTML("first para\n\nsecond para")
	if err != nil {
		t.Fatalf("ToSafeHTML: %v", err)
	}
	if strings.Count(html, "<p>") != 2 {
		t.Errorf("expected a blank line to start a new paragraph, got: %s", html)
	}
	if strings.Contains(html, "<br") {
		t.Errorf("expected no <br> between paragraphs, got: %s", html)
	}
}

func TestToSafeHTML_StripsScripts(t *testing.T) {
	html, err := ToSafeHTML("hello <script>alert(1)</script> world")
	if err != nil {
		t.Fatalf("ToSafeHTML: %v", err)
	}
	if strings.Contains(html, "<script") {
		t.Errorf("expected <script> to be stripped, got: %s", html)
	}
}

func TestToSafeHTML_StripsEventHandlerAttrs(t *testing.T) {
	html, err := ToSafeHTML(`<a href="https://example.com" onclick="alert(1)">click</a>`)
	if err != nil {
		t.Fatalf("ToSafeHTML: %v", err)
	}
	if strings.Contains(html, "onclick") {
		t.Errorf("expected onclick attribute to be stripped, got: %s", html)
	}
}
