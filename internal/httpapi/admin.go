package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"caravel/internal/auth"
	"caravel/internal/db"

	"github.com/go-chi/chi/v5"
)

// Account administration: the screen an admin uses to create, change and remove
// other accounts, and to decide whether registration is open.
//
// Worth restating here because it is the invariant most at risk from a later
// well-meaning change: an admin administers *accounts*, not data. Nothing in
// this file reaches into anyone's trips, and Server.tripRole never consults
// is_admin. A "personal" file the operator can read is not a personal file.
//
// The last-admin guard rails below all protect the same thing: an instance must
// never reach a state where nobody can administer it. There is no recovery path
// from that short of editing the database by hand.

// requireAdmin rejects a non-admin with 403.
//
// 403 rather than the 404 the trip routes use: these routes are not secret —
// every client knows /api/admin exists, it is in the shipped JavaScript — and
// there is no resource whose existence could leak. The trip routes' 404 exists
// to hide *which trips exist*, which has no analogue here.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok || !user.IsAdmin {
			writeError(w, http.StatusForbidden, "administrator access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type adminUserResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
	// TripCount is trips *owned*, which is what deleting the account would
	// destroy. The confirm dialog says the number out loud for that reason.
	TripCount int64 `json:"trip_count"`
	// IsSelf so the client can mark the row and refuse the two things an admin
	// must not do to themselves, without comparing against /auth/me.
	IsSelf bool `json:"is_self"`
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.UserFromContext(r.Context())

	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	resp := make([]adminUserResponse, len(users))
	for i, u := range users {
		resp[i] = adminUserResponse{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			IsAdmin:     u.IsAdmin,
			CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
			TripCount:   u.TripCount,
			IsSelf:      u.ID == me.ID,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type adminCreateUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	IsAdmin     bool   `json:"is_admin"`
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req adminCreateUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	// Same floor as self-registration. An admin-created account is a real
	// account its owner will log into, so it gets the same rules rather than
	// whatever the admin can be bothered to type.
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
			writeErrorCode(w, http.StatusConflict, "username_taken", "that username is already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}
	// Register decides is_admin itself (the first account on an instance), so
	// an explicitly requested admin is a second step rather than a parameter.
	// Both paths converge here: whatever Register decided, the request wins.
	if req.IsAdmin != user.IsAdmin {
		user, err = s.Store.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			IsAdmin:     req.IsAdmin,
			UpdatedAt:   time.Now().UTC(),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not set administrator flag")
			return
		}
	}
	writeJSON(w, http.StatusCreated, adminUserResponse{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		IsAdmin:     user.IsAdmin,
		CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339),
	})
}

type adminUpdateUserRequest struct {
	DisplayName string `json:"display_name"`
	IsAdmin     *bool  `json:"is_admin"`
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	me, _ := auth.UserFromContext(r.Context())
	userID := chi.URLParam(r, "userId")

	target, err := s.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load user")
		}
		return
	}

	var req adminUpdateUserRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = target.DisplayName
	}
	isAdmin := target.IsAdmin
	if req.IsAdmin != nil {
		isAdmin = *req.IsAdmin
	}

	// Removing the last admin flag on the instance would leave nobody able to
	// undo it. Checked for any target, not just self: demoting the other admin
	// is the same hole.
	if target.IsAdmin && !isAdmin {
		last, err := s.isLastAdmin(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not count administrators")
			return
		}
		if last {
			writeLastAdminRefusal(w)
			return
		}
	}

	updated, err := s.Store.UpdateUser(r.Context(), db.UpdateUserParams{
		ID:          target.ID,
		DisplayName: displayName,
		IsAdmin:     isAdmin,
		UpdatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update user")
		return
	}
	writeJSON(w, http.StatusOK, adminUserResponse{
		ID:          updated.ID,
		Username:    updated.Username,
		DisplayName: updated.DisplayName,
		IsAdmin:     updated.IsAdmin,
		CreatedAt:   updated.CreatedAt.UTC().Format(time.RFC3339),
		IsSelf:      updated.ID == me.ID,
	})
}

// isLastAdmin reports whether target is the only administrator left, in which
// case demoting or deleting them would leave the instance with nobody able to
// undo it. A pure predicate: both callers write their own refusal, since one is
// a demotion and the other a deletion.
func (s *Server) isLastAdmin(r *http.Request) (bool, error) {
	admins, err := s.Store.CountAdmins(r.Context())
	if err != nil {
		return false, err
	}
	return admins <= 1, nil
}

// writeLastAdminRefusal is the shared wording, so a client branching on the
// code sees one thing whichever route it hit.
func writeLastAdminRefusal(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusConflict, "last_admin",
		"this is the only administrator; promote someone else first")
}

type adminPasswordRequest struct {
	Password string `json:"password"`
}

// handleAdminResetPassword sets a password without knowing the old one, which
// is the whole point of an admin reset.
//
// auth.SetPassword has existed since the seeder needed it and has deliberately
// been unreachable over HTTP until now. Note what it does *not* do, unlike
// ChangePassword: it leaves existing sessions alone. That is the right choice
// here — an admin resetting a forgotten password should not sign the user out
// of the device they are holding — but it does mean a reset is not a way to
// evict someone. Removing the account is.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")

	target, err := s.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load user")
		}
		return
	}

	var req adminPasswordRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	// SetPassword updates an existing local identity and would otherwise
	// succeed silently for an account that has none — reporting 204 while
	// changing nothing. No such account exists today (Register always creates
	// one), but the moment an external provider does, a lying 204 is the worst
	// possible answer here.
	hasPassword, err := s.Auth.HasPassword(r.Context(), target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check password")
		return
	}
	if !hasPassword {
		writeErrorCode(w, http.StatusConflict, "no_local_password",
			"that account has no password to reset")
		return
	}
	if err := s.Auth.SetPassword(r.Context(), target.Username, req.Password); err != nil {
		writeError(w, http.StatusInternalServerError, "could not set password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Deleting your own account is allowed — an admin leaving should be able to
// remove themselves — so there is no self check here beyond the last-admin one.
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")

	target, err := s.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load user")
		}
		return
	}

	// Not if you are the last one who could let anyone back in, though.
	if target.IsAdmin {
		last, err := s.isLastAdmin(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not count administrators")
			return
		}
		if last {
			writeLastAdminRefusal(w)
			return
		}
	}

	deleted, err := s.Store.DeleteUser(r.Context(), target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete user")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type adminOpenSignupRequest struct {
	OpenSignup bool `json:"open_signup"`
}

func (s *Server) handleAdminSetOpenSignup(w http.ResponseWriter, r *http.Request) {
	var req adminOpenSignupRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.setOpenSignup(r.Context(), req.OpenSignup); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save setting")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open_signup": req.OpenSignup})
}
