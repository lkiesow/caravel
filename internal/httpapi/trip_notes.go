package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"caravel/internal/auth"
	"caravel/internal/db"
)

// The trip notepad: one markdown document per trip, at
// GET/PUT /api/trips/{tripId}/notes.
//
// A trip already has a structured home for everything structured -- locations,
// itinerary days, checklists, expenses. This is the home for the prose that
// goes with planning one, which until now had to be wedged into a location
// note that was not about that location.
//
// One document, not a list of them, so there is no note id and no collection:
// the trip is the key. See db.TripNote for why the table is shaped that way.

// The body cap is the preview endpoint cap, deliberately reused rather than
// chosen again. The editor previews through POST /api/markdown/preview, so a
// note allowed to grow past what that endpoint accepts would be a note its own
// editor could not render. Raising one means raising both.
const maxTripNoteBytes = maxPreviewMarkdownBytes

type tripNoteResponse struct {
	Body      string  `json:"body"`
	BodyHTML  string  `json:"body_html"`
	UpdatedAt *string `json:"updated_at"`
}

type tripNoteRequest struct {
	Body string `json:"body"`
}

// writeTripNote renders the note and sends it. Rendering goes through
// renderNotesHTML -- the same call the item payload and the markdown preview
// endpoint make, not merely the same library -- so the tab, the preview and
// the location view page cannot drift in how they read the same markdown.
func writeTripNote(w http.ResponseWriter, body string, updatedAt *time.Time) {
	html := renderNotesHTML(&body)
	if html == nil {
		writeError(w, http.StatusInternalServerError, "could not render the note")
		return
	}
	resp := tripNoteResponse{Body: body, BodyHTML: *html}
	if updatedAt != nil {
		stamp := updatedAt.UTC().Format(time.RFC3339)
		resp.UpdatedAt = &stamp
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetTripNote returns the trip note, or an empty one.
//
// A trip nobody has written on has no row, and that is answered with 200 and
// an empty body rather than 404. The client then has exactly one response
// shape to render, and "nothing written yet" is just an empty string rather
// than an error path the tab would have to special-case. A real 404 here would
// also be indistinguishable from a trip that does not exist, which is a
// genuinely different thing and is what loadTrip already reports.
func (s *Server) handleGetTripNote(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	note, err := s.Store.GetTripNote(r.Context(), trip.ID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeTripNote(w, "", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load the note")
		return
	}
	writeTripNote(w, note.Body, &note.UpdatedAt)
}

// handleSetTripNote saves the note. Last write wins: the request carries no
// expected version, so two people editing at once means the later save stands,
// which is how itinerary day notes already behave.
//
// Saving a blank body deletes the row rather than storing an empty string, so
// a cleared note and a never-written one are the same state -- one thing for
// the tab to recognise, and it opens in the editor for both.
func (s *Server) handleSetTripNote(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}
	var req tripNoteRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Body) > maxTripNoteBytes {
		writeError(w, http.StatusBadRequest, "note is too long")
		return
	}
	// Trimmed only to decide empty-or-not. What gets stored is what was typed:
	// trailing blank lines are a markdown author's business.
	if strings.TrimSpace(req.Body) == "" {
		if _, err := s.Store.DeleteTripNote(r.Context(), trip.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save the note")
			return
		}
		writeTripNote(w, "", nil)
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	note, err := s.Store.UpsertTripNote(r.Context(), db.UpsertTripNoteParams{
		TripID:    trip.ID,
		Body:      req.Body,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: &user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the note")
		return
	}
	writeTripNote(w, note.Body, &note.UpdatedAt)
}
