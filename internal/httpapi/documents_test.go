package httpapi

import (
	"io"
	"strings"
	"testing"
)

func TestIsInlineSafeContentType(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{"application/pdf", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"text/plain; charset=utf-8", true},
		{"image/svg+xml", false},
		{"application/zip", false},
		{"text/html", false},
	}
	for _, c := range cases {
		if got := isInlineSafeContentType(c.contentType); got != c.want {
			t.Errorf("isInlineSafeContentType(%q) = %v, want %v", c.contentType, got, c.want)
		}
	}
}

func TestSniffContentType(t *testing.T) {
	pdfMagic := "%PDF-1.4\n" + strings.Repeat("x", 100)
	r := strings.NewReader(pdfMagic)
	got, err := sniffContentType(r)
	if err != nil {
		t.Fatalf("sniffContentType: %v", err)
	}
	if got != "application/pdf" {
		t.Errorf("got %q, want application/pdf", got)
	}

	// Confirm the reader was rewound so the caller can still read the full content.
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read after sniff: %v", err)
	}
	if string(rest) != pdfMagic {
		t.Errorf("reader was not rewound to the start after sniffing")
	}
}
