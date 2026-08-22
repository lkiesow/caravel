package httpapi

import (
	"net/http"
)

// Markdown preview, for the location editor's notes field.
//
// Notes are authored in a plain textarea and, before Stage 15 Milestone 3, were
// only ever rendered after saving - on the view page, from the notes_html the
// item payload carries. So formatting was written blind: you found out whether
// the list you typed was a list by leaving the editor.
//
// This endpoint exists rather than a client-side renderer for one reason: the
// preview has to be what the view page will actually show. A second markdown
// implementation in JS would be a second sanitizer and a second set of
// CommonMark quirks, and the whole point of internal/markdown's package comment
// is that the sanitization boundary lives in one trusted place. It goes through
// renderNotesHTML - the same call the item payload makes, not merely the same
// library - so the two cannot drift.
//
// Not trip-scoped, and deliberately: it renders text the caller already has in
// a textarea in front of them, so there is no trip to authorize against. It is
// behind RequireAuth all the same, because it is a CPU cost and an anonymous
// caller has no notes to preview.

// A note is short-form text. This cap is not a schema limit - the column is
// TEXT - it is here so an endpoint that renders arbitrary input on demand
// cannot be used as a general-purpose markdown service.
const maxPreviewMarkdownBytes = 64 * 1024

type markdownPreviewRequest struct {
	Markdown string `json:"markdown"`
}

type markdownPreviewResponse struct {
	HTML string `json:"html"`
}

func (s *Server) handleMarkdownPreview(w http.ResponseWriter, r *http.Request) {
	var req markdownPreviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Markdown) > maxPreviewMarkdownBytes {
		writeError(w, http.StatusBadRequest, "note is too long to preview")
		return
	}
	// Empty is not an error: it is a note nobody has typed into yet, and the
	// client decides what to show for that.
	html := renderNotesHTML(&req.Markdown)
	if html == nil {
		writeError(w, http.StatusInternalServerError, "could not render the note")
		return
	}
	writeJSON(w, http.StatusOK, markdownPreviewResponse{HTML: *html})
}
