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

// Trip membership: who else is on a trip, and what they may do.
//
// Adding someone is by exact username rather than by email invitation or a
// join token. This is a self-hosted app among people who know each other, and
// from Stage 14 Milestone 6 an admin creates the accounts anyway, so a username
// is something the owner can reasonably be expected to have. The cost is that
// you cannot share with someone who has no account yet; invite links are on the
// backlog next to public share links, which they overlap.

type memberResponse struct {
	// UserID is needed here, unlike in tripResponse.Owner: the members UI has
	// to address a specific person to change or remove their role, and the
	// route param is that id. Only visible to people already on the trip.
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	// IsSelf lets the client mark "you" and offer Leave instead of Remove
	// without having to compare against /auth/me itself.
	IsSelf bool `json:"is_self"`
	// PersonalFileCount is how many of their own personal files removing them
	// would delete, so the confirmation can say so. Only meaningful to whoever
	// can act on it: it is filled in for the owner reading the list, and left
	// at zero otherwise — the count of someone's private files is itself
	// information about them.
	PersonalFileCount int `json:"personal_file_count"`
}

// handleListMembers returns everyone on the trip, owner first.
//
// The owner is synthesized rather than read from trip_members (they have no row
// there — see migration 0007), which is also why they are always first: the
// list is assembled owner-then-members, not sorted after the fact.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())

	owner, err := s.Store.GetUserByID(r.Context(), trip.OwnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load trip owner")
		return
	}
	resp := []memberResponse{{
		UserID:      owner.ID,
		Username:    owner.Username,
		DisplayName: owner.DisplayName,
		Role:        string(db.RoleOwner),
		IsSelf:      owner.ID == me.ID,
	}}

	members, err := s.Store.ListTripMembers(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list members")
		return
	}
	canManage := me.ID == trip.OwnerID
	for _, m := range members {
		row := memberResponse{
			UserID:      m.UserID,
			Username:    m.Username,
			DisplayName: m.DisplayName,
			Role:        string(m.Role),
			IsSelf:      m.UserID == me.ID,
		}
		// One query per member, on a list that is a handful of people at most.
		// Worth it to make the removal confirmation honest; if a trip ever has
		// enough members for this to matter, it wants a grouped count instead.
		if canManage || m.UserID == me.ID {
			if personal, err := s.Store.ListPersonalFilesForUser(r.Context(), trip.ID, m.UserID); err == nil {
				row.PersonalFileCount = len(personal)
			}
		}
		resp = append(resp, row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// User search, behind RequireAuth, for the Members tab's username field.
//
// Scope, decided with the user: any authenticated caller can search every
// account on the instance. That is a real widening — usernames become
// enumerable by walking two-letter prefixes rather than one guess at a time —
// and it was chosen knowingly for a self-hosted instance whose users know each
// other. Two things keep it from being worse than the add-member 404 already
// is: the response carries only what is needed to recognise a person (no email,
// no timestamps, see db.UserSummary), and it is capped.
const (
	// Two characters, because one matches most of the instance and makes the
	// suggestion list noise rather than help.
	userSearchMinQuery = 2
	// Ten is a list you can scan. Past that the right answer is to type more,
	// not to scroll.
	userSearchLimit = 10
)

func (s *Server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	// Below the floor this is an empty result, not an error: the field calls
	// this on every keystroke, and a 400 on the way to a valid query is noise
	// in the console for something that is working correctly.
	if len([]rune(query)) < userSearchMinQuery {
		writeJSON(w, http.StatusOK, []db.UserSummary{})
		return
	}

	users, err := s.Store.SearchUsers(r.Context(), query, userSearchLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not search users")
		return
	}
	resp := make([]memberSuggestion, 0, len(users))
	for _, u := range users {
		resp = append(resp, memberSuggestion{Username: u.Username, DisplayName: u.DisplayName})
	}
	writeJSON(w, http.StatusOK, resp)
}

// memberSuggestion deliberately omits the user id. The Members tab adds people
// by username, so an id would be unused here — and handing every authenticated
// caller a map of ids to names is a bigger disclosure than the feature needs.
type memberSuggestion struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type addMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// handleAddMember adds a user to a trip by username.
//
// The 404 for an unknown username carries the code "user_not_found" so the form
// can say "no such user" rather than falling back to a generic failure. That is
// a deliberate disclosure: the caller learns whether a username exists on this
// instance. It is the price of add-by-username — without it the feature is
// unusable, since a typo and a real refusal would look identical — and it tells
// them nothing about that user beyond existence, which anyone able to attempt a
// registration could already discover from ErrUsernameTaken.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleOwner)
	if !ok {
		return
	}

	var req addMemberRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	role := db.TripRole(req.Role)
	if !role.Assignable() {
		writeError(w, http.StatusBadRequest, "role must be editor or viewer")
		return
	}

	user, err := s.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErrorCode(w, http.StatusNotFound, "user_not_found", "no user with that username")
		} else {
			writeError(w, http.StatusInternalServerError, "could not look up user")
		}
		return
	}
	if user.ID == trip.OwnerID {
		writeErrorCode(w, http.StatusConflict, "already_owner", "that user already owns this trip")
		return
	}
	// Distinguished from a role change on purpose: POST means "add", and
	// silently updating an existing member's role would hide the fact that they
	// were already on the trip, possibly at a role the owner didn't intend.
	if _, err := s.Store.GetTripMember(r.Context(), trip.ID, user.ID); err == nil {
		writeErrorCode(w, http.StatusConflict, "already_member", "that user is already on this trip")
		return
	} else if !errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "could not check membership")
		return
	}

	if _, err := s.Store.UpsertTripMember(r.Context(), trip.ID, user.ID, role, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not add member")
		return
	}
	writeJSON(w, http.StatusCreated, memberResponse{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        string(role),
	})
}

type setMemberRoleRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleOwner)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "userId")

	var req setMemberRoleRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role := db.TripRole(req.Role)
	if !role.Assignable() {
		// Covers the interesting case as well as a typo: PUT role=owner would
		// otherwise be a way to create a second owner, which trips.owner_id
		// cannot represent.
		writeError(w, http.StatusBadRequest, "role must be editor or viewer")
		return
	}

	// Upsert would happily create a membership here, turning a PUT on a
	// non-member into an add — a different operation with a different status
	// code. Check first so the route means what it says.
	if _, err := s.Store.GetTripMember(r.Context(), trip.ID, userID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "that user is not on this trip")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load member")
		}
		return
	}
	member, err := s.Store.UpsertTripMember(r.Context(), trip.ID, userID, role, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not change role")
		return
	}
	user, err := s.Store.GetUserByID(r.Context(), member.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load member")
		return
	}
	writeJSON(w, http.StatusOK, memberResponse{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        string(member.Role),
	})
}

// handleRemoveMember removes someone from a trip. Two callers are allowed: the
// owner removing anyone, and a member removing themselves — which is "leave
// trip", the same operation seen from the other side.
//
// It therefore cannot use loadTrip's minimum-role check, because a viewer may
// perform it on exactly one target. The role is resolved first and the two
// cases are spelled out.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	trip, role, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	userID := chi.URLParam(r, "userId")

	if role != db.RoleOwner && userID != me.ID {
		writeError(w, http.StatusForbidden, "only the trip owner can remove other members")
		return
	}
	// The owner has no membership row to delete, so this would otherwise be a
	// confusing 404 rather than a refusal — and it is the request that would
	// leave a trip with no owner if it worked.
	if userID == trip.OwnerID {
		writeErrorCode(w, http.StatusConflict, "owner_cannot_leave",
			"the trip owner cannot be removed; transfer or delete the trip instead")
		return
	}

	// Their personal files on this trip go with the membership. Bytes that
	// nobody can ever reach again are worse than a removal that says what it
	// will take: the file is invisible to everyone else by definition, and its
	// owner has just lost the trip it lives on. The Members tab's confirmation
	// names the count for exactly this reason.
	//
	// Done before the membership row so a failure leaves the person still on
	// the trip with their files intact, rather than removed with orphans behind
	// them. Trip-visible files stay: those are the trip's, not theirs.
	personal, err := s.Store.ListPersonalFilesForUser(r.Context(), trip.ID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check for personal files")
		return
	}
	for _, f := range personal {
		if _, err := s.Store.DeleteFile(r.Context(), f.ID, trip.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not remove personal files")
			return
		}
		// Best-effort, like every other blob delete in the app: the row is
		// gone, and a leaked blob is a disk-space problem rather than a
		// correctness one.
		_ = s.Blob.Delete(r.Context(), f.StoragePath)
	}

	removed, err := s.Store.DeleteTripMember(r.Context(), trip.ID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove member")
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "that user is not on this trip")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
