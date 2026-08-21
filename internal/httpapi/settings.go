package httpapi

import (
	"context"
	"net/http"
	"strconv"
)

// Instance-wide settings, stored in app_settings so an admin can change them at
// runtime. Deliberately *not* environment variables: Stage 14 Milestone 5
// removed CARAVEL_OPEN_SIGNUP rather than keeping both, because two sources for
// one answer means the admin screen can show something the server does not
// believe, and there is no way for a user to tell which one is lying.
const settingOpenSignup = "open_signup"

// openSignupEnabled reports whether an anonymous visitor may register.
//
// A missing row, an unparseable value or a failed read all answer false. This
// is the one place in the app where failing closed costs nothing and failing
// open is a real problem: the worst case of a false negative is an admin
// re-ticking a box, and of a false positive is an open instance.
func (s *Server) openSignupEnabled(ctx context.Context) bool {
	value, err := s.Store.GetAppSetting(ctx, settingOpenSignup)
	if err != nil {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return enabled
}

func (s *Server) setOpenSignup(ctx context.Context, enabled bool) error {
	return s.Store.SetAppSetting(ctx, settingOpenSignup, strconv.FormatBool(enabled))
}

// registrationAllowed reports whether a registration attempt may proceed.
//
// Two ways to qualify. The setting being on is the obvious one. The other is
// that the instance has no users at all — a fresh install has to be able to
// create its first account, and that account becomes the admin who can then
// decide whether to leave the door open. Without this a new deployment would be
// bricked behind its own default.
func (s *Server) registrationAllowed(ctx context.Context) (bool, error) {
	if s.openSignupEnabled(ctx) {
		return true, nil
	}
	count, err := s.Store.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// handleAuthConfig is unauthenticated on purpose: the login page has to know
// whether to offer a register link *before* anyone has a session, and the
// alternative is a register form that leads to a 403.
//
// It leaks one boolean about the instance. That is the same thing an anonymous
// visitor learns by submitting the register form once, so publishing it costs
// nothing and saves a confusing round trip.
//
// Not rate limited, unlike its neighbours: one boolean read per page load, and
// the login limiter's 10/min/IP would break a reload.
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	allowed, err := s.registrationAllowed(r.Context())
	if err != nil {
		// Report closed rather than 500: the login page still works, it just
		// does not offer registration, which is the safe half of the guess.
		allowed = false
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open_signup": allowed})
}
