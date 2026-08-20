// Package markdown renders user-supplied item notes to safe HTML.
// Rendering happens server-side so the sanitization boundary lives in
// one trusted place — the frontend never needs to parse or sanitize
// markdown itself, it just inserts the HTML this package returns.
package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/renderer/html"
)

var sanitizer = bluemonday.UGCPolicy()

// Hard wraps, because notes are written in a plain <textarea> where pressing
// Enter once obviously means "break here". CommonMark disagrees — it collapses
// a single newline into a space — and the view page used to paper over that
// with `white-space: pre-wrap`, which also preserved the newlines *between*
// block elements and so tripled the spacing around every heading and list.
// Rendering the break as a real <br> is what let that CSS go.
var md = goldmark.New(goldmark.WithRendererOptions(html.WithHardWraps()))

// ToSafeHTML renders raw CommonMark to sanitized HTML. goldmark does not
// render raw HTML embedded in the source by default, but bluemonday runs
// afterward regardless — sanitizing the *output*, not the input, is what
// keeps this safe even if that default ever changes.
func ToSafeHTML(raw string) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(raw), &buf); err != nil {
		return "", err
	}
	return string(sanitizer.SanitizeBytes(buf.Bytes())), nil
}
