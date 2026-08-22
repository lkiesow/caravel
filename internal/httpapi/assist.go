package httpapi

import (
	"net/http"

	"caravel/internal/db"
)

// AI-assisted location metadata.
//
// Milestone 1 wires the seam only: the route exists, refuses when the feature
// is off, and authorizes when it is on. The streaming implementation lands in
// Milestone 6 -- see docs/plans/stage-16.md.

func (s *Server) handleAssistLocation(w http.ResponseWriter, r *http.Request) {
	// 501 rather than 404, matching handleGeocode: the route exists, the
	// capability is switched off. The client already knows from /auth/me and
	// should not be asking, so this is a backstop rather than a path anyone
	// should reach.
	if s.Assist == nil {
		writeError(w, http.StatusNotImplemented, "the assistant is not enabled on this server")
		return
	}

	// Editor rather than viewer. Two reasons, and the second is the load
	// bearing one: a viewer could not save the result anyway, and the request
	// may carry the trip title and dates to a third-party API, which is not a
	// read-only participant's decision to make.
	if _, _, ok := s.loadTrip(w, r, db.RoleEditor); !ok {
		return
	}

	writeError(w, http.StatusNotImplemented, "the assistant is not implemented yet")
}
