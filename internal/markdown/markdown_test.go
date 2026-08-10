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
