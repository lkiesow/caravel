package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"caravel/internal/auth"
	"caravel/internal/db"
)

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	// HasPassword tells the client whether a password-change control makes
	// sense for this account. Every account is local today, but the settings
	// screen asks rather than assuming, so it is already right when an external
	// provider arrives.
	HasPassword bool `json:"has_password"`
	// Capabilities is what this *server* can do, not anything about this user.
	// It rides along on /auth/me because that is the one call the app already
	// makes at boot, and several screens need the answer before they decide
	// what to render -- the alternative was a second endpoint fetched by the
	// one screen that needs it.
	//
	// Nested since Stage 22. The three flags were flat fields for three
	// stages, each arriving with a comment saying the next one would make the
	// case for a reshape; the third one said so outright. Grouping them says
	// what they have in common -- none of them is a property of the account
	// they are sitting next to -- and stops a fourth from making /auth/me read
	// like a settings dump.
	Capabilities capabilitiesResponse `json:"capabilities"`
	// IsAdmin governs account administration only — never access to another
	// user's trips. It stays a user field, because unlike the three above it
	// genuinely is one. The client uses it to decide whether to show the admin
	// entry in the user menu; the server checks it again on every /api/admin
	// route, because a hidden menu item is not a permission.
	IsAdmin bool `json:"is_admin"`
}

// capabilitiesResponse is what the instance is configured to do.
//
// Every one of these is a "the operator did not set this up" switch rather than
// a permission: a client that finds one false must not render the control at
// all, because the endpoint behind it answers 501 and a control whose only
// possible answer is "not enabled on this server" is worse than no control.
// The server checks its own configuration again on every such route regardless.
type capabilitiesResponse struct {
	// Geocoding is address search, from CARAVEL_GEOCODER_URL.
	Geocoding bool `json:"geocoding"`
	// Assist is the AI location assistant, from CARAVEL_LLM_URL.
	Assist bool `json:"assist"`
	// ImageSearch is true whenever *either* half of the picker can answer,
	// because either alone is a working control: Wikipedia needs no
	// configuration, and a search backend covers what Wikipedia has never
	// heard of.
	ImageSearch bool `json:"image_search"`
}

func (s *Server) userToResponse(r *http.Request, u db.User) userResponse {
	// A failed lookup is reported as "no password" rather than as a 500: the
	// worst case is one hidden card on the settings screen, and failing the
	// whole /auth/me call over it would log the user out of a working app.
	hasPassword, err := s.Auth.HasPassword(r.Context(), u)
	if err != nil {
		hasPassword = false
	}
	return userResponse{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		HasPassword: hasPassword,
		Capabilities: capabilitiesResponse{
			Geocoding:   s.Geocoder != nil,
			Assist:      s.Assist != nil,
			ImageSearch: s.imageSearchAvailable(),
		},
		IsAdmin: u.IsAdmin,
	}
}

type registerRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Open registration, or the very first account on the instance — see
	// registrationAllowed. A failure to determine it is treated as closed
	// rather than as a 500: the caller gets a clear refusal instead of an
	// error, and failing open here would be the wrong way round.
	allowed, err := s.registrationAllowed(r.Context())
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "registration is disabled")
		return
	}

	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username is required and password must be at least 8 characters")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	user, err := s.Auth.Register(r.Context(), req.Username, req.Password, req.DisplayName)
	if err != nil {
		if errors.Is(err, auth.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "username already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not register user")
		return
	}

	s.startSessionAndRespond(w, r, user)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.Auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	s.startSessionAndRespond(w, r, user)
}

func (s *Server) startSessionAndRespond(w http.ResponseWriter, r *http.Request, user db.User) {
	token, session, err := s.Auth.StartSession(r.Context(), user.ID, r.UserAgent(), clientIP(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})

	writeJSON(w, http.StatusOK, s.userToResponse(r, user))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		_ = s.Auth.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, s.userToResponse(r, user))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword changes a local account's password and then re-issues the
// caller's own session.
//
// The re-issue is not optional: auth.ChangePassword deletes every session the
// user has, which includes the one making this request, so without a fresh
// cookie the response would arrive already logged out. Other devices stay
// logged out, which is the point.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req changePasswordRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Same floor as registration: one place deciding what a password may be,
	// or the two drift and the weaker one wins.
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	if err := s.Auth.ChangePassword(r.Context(), user, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
		case errors.Is(err, auth.ErrNoLocalPassword):
			writeError(w, http.StatusBadRequest, "this account has no password to change")
		default:
			writeError(w, http.StatusInternalServerError, "could not change password")
		}
		return
	}

	s.startSessionAndRespond(w, r, user)
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i != -1 {
		host = host[:i]
	}
	return host
}
