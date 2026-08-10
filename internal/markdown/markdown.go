// Package markdown renders user-supplied notes (trip/item notes) to safe
// HTML. Rendering happens server-side so the sanitization boundary lives in
// one trusted place — the frontend never needs to parse or sanitize
// markdown itself, it just inserts the HTML this package returns.
package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
)

var sanitizer = bluemonday.UGCPolicy()

// ToSafeHTML renders raw CommonMark to sanitized HTML. goldmark does not
// render raw HTML embedded in the source by default, but bluemonday runs
// afterward regardless — sanitizing the *output*, not the input, is what
// keeps this safe even if that default ever changes.
func ToSafeHTML(raw string) (string, error) {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(raw), &buf); err != nil {
		return "", err
	}
	return string(sanitizer.SanitizeBytes(buf.Bytes())), nil
}
