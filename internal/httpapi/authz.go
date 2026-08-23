package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"caravel/internal/auth"
	"caravel/internal/db"

	"github.com/go-chi/chi/v5"
)

// Trip authorization lives in this one file. Every trip-scoped handler reaches
// it through one of the load* helpers below, which resolve the caller's
// db.TripRole and compare it against the minimum that handler requires.
//
// The status codes are the interesting part, and they are not the same in both
// failure directions:
//
//   - **No role at all → 404**, with the resource's own "not found" wording.
//     A stranger must not be able to tell an existing trip from a missing one,
//     which is why errNoAccess deliberately collapses "the trip isn't there"
//     and "it isn't yours" into a single error. internal/httpapi/ownership_test.go
//     pins this.
//   - **A role that is merely insufficient → 403.** A viewer already knows the
//     trip exists — they can read it — so answering 404 when they try to write
//     would be a lie, and one the client cannot act on: it could not tell
//     "deleted" apart from "not allowed".

// errNoAccess means the current user has no role whatsoever on the trip in
// question, *including* the case where the trip does not exist. Collapsing the
// two is the point; see the note above.
var errNoAccess = errors.New("no trip access")

// tripRole resolves the current user's role on tripID.
//
// The owner is not stored in trip_members (see migration 0007), so ownership is
// decided from trips.owner_id first and the members table is consulted only if
// that misses. Returns errNoAccess when the user has no role, or a real error
// if a lookup fails.
func (s *Server) tripRole(ctx context.Context, tripID string) (db.Trip, db.TripRole, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return db.Trip{}, "", errNoAccess
	}

	trip, err := s.Store.GetTripByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.Trip{}, "", errNoAccess
		}
		return db.Trip{}, "", err
	}
	if trip.OwnerID == user.ID {
		return trip, db.RoleOwner, nil
	}

	member, err := s.Store.GetTripMember(ctx, tripID, user.ID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.Trip{}, "", errNoAccess
		}
		return db.Trip{}, "", err
	}
	// A role the code doesn't recognise fails closed rather than being read
	// as something powerful. The CHECK constraint should make this
	// unreachable; it is here because "should" is doing a lot of work in that
	// sentence, and the failure mode of guessing wrong is privilege
	// escalation.
	if !member.Role.Valid() {
		return db.Trip{}, "", errNoAccess
	}
	return trip, member.Role, nil
}

// authorizeTrip resolves the caller's role on tripID, checks it against min,
// and writes the error response itself if the check fails. notFound is the
// message used for the 404 case, so each resource keeps its own wording
// ("item not found" rather than "trip not found") and the URL a client hit
// still describes what it could not have.
func (s *Server) authorizeTrip(w http.ResponseWriter, r *http.Request, tripID string, min db.TripRole, notFound string) (db.Trip, db.TripRole, bool) {
	trip, role, err := s.tripRole(r.Context(), tripID)
	if err != nil {
		if errors.Is(err, errNoAccess) {
			writeError(w, http.StatusNotFound, notFound)
		} else {
			writeError(w, http.StatusInternalServerError, "could not load trip")
		}
		return db.Trip{}, "", false
	}
	if !role.AtLeast(min) {
		writeError(w, http.StatusForbidden, "you do not have permission to do that on this trip")
		return db.Trip{}, "", false
	}
	return trip, role, true
}

// loadTrip fetches the trip named by the {tripId} route param and confirms the
// caller holds at least min on it.
func (s *Server) loadTrip(w http.ResponseWriter, r *http.Request, min db.TripRole) (db.Trip, db.TripRole, bool) {
	return s.authorizeTrip(w, r, chi.URLParam(r, "tripId"), min, "trip not found")
}

// loadItem fetches the item named by {itemId} and authorizes against its trip.
func (s *Server) loadItem(w http.ResponseWriter, r *http.Request, min db.TripRole) (db.Item, db.TripRole, bool) {
	item, err := s.Store.GetItemByID(r.Context(), chi.URLParam(r, "itemId"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load item")
		}
		return db.Item{}, "", false
	}
	_, role, ok := s.authorizeTrip(w, r, item.TripID, min, "item not found")
	if !ok {
		return db.Item{}, "", false
	}
	return item, role, true
}

// loadChecklist fetches the checklist named by {checklistId} and authorizes
// against its trip.
func (s *Server) loadChecklist(w http.ResponseWriter, r *http.Request, min db.TripRole) (db.Checklist, db.TripRole, bool) {
	checklist, err := s.Store.GetChecklistByID(r.Context(), chi.URLParam(r, "checklistId"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "checklist not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load checklist")
		}
		return db.Checklist{}, "", false
	}
	_, role, ok := s.authorizeTrip(w, r, checklist.TripID, min, "checklist not found")
	if !ok {
		return db.Checklist{}, "", false
	}
	// A personal list belongs to whoever made it, and having its id is not
	// access. 404 rather than 403 for the same reason as a personal file: the
	// point of a personal list is that other people on the trip do not know it
	// exists. Duplicates the predicate in ListChecklistsByTrip on purpose —
	// that hides it from a listing, this stops a remembered id reaching it.
	if checklist.Visibility == db.ChecklistPersonal {
		me, _ := auth.UserFromContext(r.Context())
		if checklist.OwnerUserID == nil || *checklist.OwnerUserID != me.ID {
			writeError(w, http.StatusNotFound, "checklist not found")
			return db.Checklist{}, "", false
		}
	}
	return checklist, role, true
}

// loadExpense fetches the expense named by {expenseId} and authorizes against
// its trip.
//
// Nothing follows the authorization, which is the whole difference from
// loadChecklist and loadFile: those re-check a personal row's owner, because
// holding an id is not access to somebody's private list. An expense has no
// personal state to protect — everyone on the trip may see every expense on it
// — so a role on the trip is the entire question.
func (s *Server) loadExpense(w http.ResponseWriter, r *http.Request, min db.TripRole) (db.Expense, db.TripRole, bool) {
	expense, err := s.Store.GetExpenseByID(r.Context(), chi.URLParam(r, "expenseId"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "expense not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load expense")
		}
		return db.Expense{}, "", false
	}
	_, role, ok := s.authorizeTrip(w, r, expense.TripID, min, "expense not found")
	if !ok {
		return db.Expense{}, "", false
	}
	return expense, role, true
}

// requireTripMember confirms that userID holds some role on trip, writing the
// error response itself if not. A nil userID is allowed and means nobody, which
// is a legitimate value for an expense payer.
//
// This is requireSameTrip's problem in a different shape: the id arrives in a
// request body, so no route param has authorized it, and without this check any
// user id at all could be recorded as having paid for something on a trip they
// have nothing to do with. 400 rather than 403 or 404 — the caller is
// authorized, the request is what is wrong.
//
// Note this asks whether the *named* user has a role, not the caller, so it
// cannot go through tripRole: that reads the user from the request context.
func (s *Server) requireTripMember(w http.ResponseWriter, r *http.Request, trip db.Trip, userID *string) bool {
	if userID == nil {
		return true
	}
	if trip.OwnerID == *userID {
		return true
	}
	if _, err := s.Store.GetTripMember(r.Context(), trip.ID, *userID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "that person is not on this trip")
		} else {
			writeError(w, http.StatusInternalServerError, "could not check trip membership")
		}
		return false
	}
	return true
}

// tripParticipantIDs is everyone who holds a role on a trip: the owner, who has
// no trip_members row (see migration 0007), plus every member. Sorted, so
// anything derived from it is deterministic.
//
// This is what "everyone on the trip" means when an expense names no shares,
// and it is also the set a named share has to belong to. Fetched once and
// checked against, rather than asking requireTripMember per id: a share list is
// several ids and that would be a query each.
func (s *Server) tripParticipantIDs(ctx context.Context, trip db.Trip) ([]string, error) {
	members, err := s.Store.ListTripMembers(ctx, trip.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members)+1)
	ids = append(ids, trip.OwnerID)
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	slices.Sort(ids)
	return ids, nil
}

// loadFile fetches the file named by {fileId} and authorizes against its trip.
func (s *Server) loadFile(w http.ResponseWriter, r *http.Request, min db.TripRole) (db.File, db.TripRole, bool) {
	file, err := s.Store.GetFileByID(r.Context(), chi.URLParam(r, "fileId"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load file")
		}
		return db.File{}, "", false
	}
	_, role, ok := s.authorizeTrip(w, r, file.TripID, min, "file not found")
	if !ok {
		return db.File{}, "", false
	}
	// A personal file belongs to whoever uploaded it, and having its id is not
	// access. Answering 404 rather than 403 here is the same reasoning as for a
	// trip nobody shared with you: the point of a personal file is that other
	// people on the trip do not know it exists.
	//
	// This duplicates the predicate in ListTripFiles and ListItemFiles on
	// purpose. Those hide the file from a listing; this is what stops a
	// remembered or guessed id from reaching it.
	if file.Visibility == db.FileVisibilityPersonal {
		me, _ := auth.UserFromContext(r.Context())
		if file.OwnerUserID == nil || *file.OwnerUserID != me.ID {
			writeError(w, http.StatusNotFound, "file not found")
			return db.File{}, "", false
		}
	}
	return file, role, true
}

// loadItineraryDay fetches the day named by {dayId} and authorizes against its
// trip.
func (s *Server) loadItineraryDay(w http.ResponseWriter, r *http.Request, min db.TripRole) (db.ItineraryDay, db.TripRole, bool) {
	day, err := s.Store.GetItineraryDayByID(r.Context(), chi.URLParam(r, "dayId"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "day not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load day")
		}
		return db.ItineraryDay{}, "", false
	}
	_, role, ok := s.authorizeTrip(w, r, day.TripID, min, "day not found")
	if !ok {
		return db.ItineraryDay{}, "", false
	}
	return day, role, true
}

// requireSameTrip guards a client-supplied id that names a row in another
// table: a media asset id arriving in a request body has no route param to
// authorize, so the only thing standing between it and a cross-trip reference
// is a check that it belongs to the trip being edited. handleCreateItineraryEntry
// has always done this for item ids; the media handlers did not, which was
// harmless only while every trip had exactly one owner.
func (s *Server) requireSameTrip(w http.ResponseWriter, assetTripID, tripID, message string) bool {
	if assetTripID != tripID {
		writeError(w, http.StatusBadRequest, message)
		return false
	}
	return true
}
