package httpapi

import (
	"net/http"
	"testing"

	"caravel/internal/db"
)

// members returns the trip's member list as decoded rows, keyed by username.
func (f *roleFixture) members(t *testing.T, as *http.Cookie) map[string]map[string]any {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/members", as, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET members: got %d, body %s", w.Code, w.Body.String())
	}
	out := map[string]map[string]any{}
	for _, m := range decode[[]map[string]any](t, w) {
		out[m["username"].(string)] = m
	}
	return out
}

// The owner has no trip_members row, so the list has to synthesize them. If it
// didn't, a solo trip would show an empty member list — which reads as "nobody
// has access", the opposite of the truth.
func TestMemberListIncludesTheOwner(t *testing.T) {
	f := setupRole(t, db.RoleOwner)

	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/members", f.owner, "")
	rows := decode[[]map[string]any](t, w)
	if len(rows) != 1 {
		t.Fatalf("solo trip has %d member(s), want 1 (the owner): %v", len(rows), rows)
	}
	if rows[0]["role"] != "owner" || rows[0]["username"] != "owner" {
		t.Errorf("first row is %v, want the owner", rows[0])
	}
	if rows[0]["is_self"] != true {
		t.Errorf("owner reading their own trip gets is_self=%v, want true", rows[0]["is_self"])
	}
	// Owner first is a promise the UI leans on (its row is inert), and it comes
	// from how the list is assembled rather than from a sort.
	f2 := setupRole(t, db.RoleEditor)
	rows = decode[[]map[string]any](t, f2.ts.do(http.MethodGet, "/api/trips/"+f2.tripID+"/members", f2.owner, ""))
	if len(rows) != 2 || rows[0]["role"] != "owner" {
		t.Errorf("with a member added, rows are %v — want the owner first", rows)
	}
}

// Everyone on a trip may see who else is on it; a stranger may not learn the
// trip exists at all.
func TestMemberListVisibility(t *testing.T) {
	for _, tc := range []struct {
		role db.TripRole
		want int
	}{
		{"", http.StatusNotFound},
		{db.RoleViewer, http.StatusOK},
		{db.RoleEditor, http.StatusOK},
		{db.RoleOwner, http.StatusOK},
	} {
		name := string(tc.role)
		if name == "" {
			name = "stranger"
		}
		t.Run(name, func(t *testing.T) {
			f := setupRole(t, tc.role)
			w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/members", f.actor, "")
			if w.Code != tc.want {
				t.Errorf("GET members as %s: got %d, want %d — body %s", name, w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAddMemberByUsername(t *testing.T) {
	f := setupRole(t, db.RoleOwner)
	f.ts.login("newcomer")
	base := "/api/trips/" + f.tripID + "/members"

	w := f.ts.do(http.MethodPost, base, f.owner, `{"username":"newcomer","role":"editor"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: got %d, body %s", w.Code, w.Body.String())
	}
	added := decode[map[string]any](t, w)
	if added["role"] != "editor" || added["username"] != "newcomer" {
		t.Errorf("added member is %v", added)
	}

	// The membership must be real, not just echoed.
	if got := f.members(t, f.owner)["newcomer"]; got == nil || got["role"] != "editor" {
		t.Errorf("newcomer is %v in the member list, want an editor", got)
	}

	// Whitespace is trimmed, the way registration trims a username, so a
	// pasted name with a trailing space is not a mysterious "no such user".
	f.ts.login("spaced")
	w = f.ts.do(http.MethodPost, base, f.owner, `{"username":"  spaced  ","role":"viewer"}`)
	if w.Code != http.StatusCreated {
		t.Errorf("add member with padded username: got %d, body %s", w.Code, w.Body.String())
	}
}

// Each refusal carries a distinct code, because the form has to say something
// different for each and a status alone cannot tell 409 from 409.
func TestAddMemberRefusals(t *testing.T) {
	f := setupRole(t, db.RoleEditor) // actor is an editor: not allowed to add
	base := "/api/trips/" + f.tripID + "/members"

	for _, tc := range []struct {
		name, body, code string
		as               *http.Cookie
		want             int
	}{
		{"unknown username", `{"username":"nobody","role":"viewer"}`, "user_not_found", f.owner, http.StatusNotFound},
		{"the owner themselves", `{"username":"owner","role":"editor"}`, "already_owner", f.owner, http.StatusConflict},
		{"already a member", `{"username":"actor","role":"viewer"}`, "already_member", f.owner, http.StatusConflict},
		{"blank username", `{"username":"  ","role":"viewer"}`, "", f.owner, http.StatusBadRequest},
		{"role owner", `{"username":"nobody","role":"owner"}`, "", f.owner, http.StatusBadRequest},
		{"role nonsense", `{"username":"nobody","role":"admin"}`, "", f.owner, http.StatusBadRequest},
		{"missing role", `{"username":"nobody"}`, "", f.owner, http.StatusBadRequest},
		{"an editor adding someone", `{"username":"nobody","role":"viewer"}`, "", f.actor, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.ts.do(http.MethodPost, base, tc.as, tc.body)
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d — body %s", w.Code, tc.want, w.Body.String())
			}
			if tc.code != "" {
				if got := decode[map[string]any](t, w)["code"]; got != tc.code {
					t.Errorf("error code is %v, want %q — the form branches on this", got, tc.code)
				}
			}
		})
	}

	// A rejected add must not have created anything: the trip still has exactly
	// the owner and the one editor it started with.
	if got := len(f.members(t, f.owner)); got != 2 {
		t.Errorf("trip has %d members after the refusals, want 2", got)
	}
}

func TestSetMemberRole(t *testing.T) {
	f := setupRole(t, db.RoleViewer)
	actorID := f.members(t, f.owner)["actor"]["user_id"].(string)
	path := "/api/trips/" + f.tripID + "/members/" + actorID

	// A viewer cannot write; promote them and the same request succeeds. That
	// pair is the point of the whole role model, so it is asserted directly
	// rather than trusting the role string.
	w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/checklists", f.actor, `{"title":"probe"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer creating a checklist: got %d, want 403", w.Code)
	}
	if w := f.ts.do(http.MethodPut, path, f.owner, `{"role":"editor"}`); w.Code != http.StatusOK {
		t.Fatalf("promote to editor: got %d, body %s", w.Code, w.Body.String())
	}
	if w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/checklists", f.actor, `{"title":"probe"}`); w.Code != http.StatusCreated {
		t.Errorf("promoted editor creating a checklist: got %d, body %s", w.Code, w.Body.String())
	}

	// And back down again — a demotion has to take effect too, or a mistaken
	// promotion could not be undone.
	if w := f.ts.do(http.MethodPut, path, f.owner, `{"role":"viewer"}`); w.Code != http.StatusOK {
		t.Fatalf("demote to viewer: got %d, body %s", w.Code, w.Body.String())
	}
	if w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/checklists", f.actor, `{"title":"probe2"}`); w.Code != http.StatusForbidden {
		t.Errorf("demoted viewer creating a checklist: got %d, want 403", w.Code)
	}

	for _, tc := range []struct {
		name, target, body string
		as                 *http.Cookie
		want               int
	}{
		{"to owner", actorID, `{"role":"owner"}`, f.owner, http.StatusBadRequest},
		{"nonsense role", actorID, `{"role":"admin"}`, f.owner, http.StatusBadRequest},
		// PUT on a non-member is an add wearing the wrong verb; the upsert would
		// have happily performed it.
		{"a non-member", "00000000-0000-0000-0000-000000000000", `{"role":"editor"}`, f.owner, http.StatusNotFound},
		{"the owner", f.members(t, f.owner)["owner"]["user_id"].(string), `{"role":"viewer"}`, f.owner, http.StatusNotFound},
		{"by the member themselves", actorID, `{"role":"editor"}`, f.actor, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.ts.do(http.MethodPut, "/api/trips/"+f.tripID+"/members/"+tc.target, tc.as, tc.body)
			if w.Code != tc.want {
				t.Errorf("got %d, want %d — body %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestRemoveMemberAndLeaveTrip(t *testing.T) {
	// The owner removing someone.
	f := setupRole(t, db.RoleEditor)
	actorID := f.members(t, f.owner)["actor"]["user_id"].(string)
	if w := f.ts.do(http.MethodDelete, "/api/trips/"+f.tripID+"/members/"+actorID, f.owner, ""); w.Code != http.StatusNoContent {
		t.Fatalf("owner removes member: got %d, body %s", w.Code, w.Body.String())
	}
	// Removal must actually revoke access, not just change a list.
	if w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.actor, ""); w.Code != http.StatusNotFound {
		t.Errorf("removed member reading the trip: got %d, want 404", w.Code)
	}

	// A member removing themselves: the same route, allowed for a viewer, but
	// only on their own id.
	f = setupRole(t, db.RoleViewer)
	actorID = f.members(t, f.owner)["actor"]["user_id"].(string)
	if w := f.ts.do(http.MethodDelete, "/api/trips/"+f.tripID+"/members/"+actorID, f.actor, ""); w.Code != http.StatusNoContent {
		t.Fatalf("viewer leaves trip: got %d, body %s", w.Code, w.Body.String())
	}
	if w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.actor, ""); w.Code != http.StatusNotFound {
		t.Errorf("after leaving, reading the trip: got %d, want 404", w.Code)
	}

	// A member may not remove *someone else*, which is the case that would turn
	// "leave" into a privilege escalation.
	f = setupRole(t, db.RoleEditor)
	f.ts.login("third")
	f.ts.mustCreateNoID(http.MethodPost, "/api/trips/"+f.tripID+"/members", f.owner, `{"username":"third","role":"viewer"}`, http.StatusCreated)
	thirdID := f.members(t, f.owner)["third"]["user_id"].(string)
	if w := f.ts.do(http.MethodDelete, "/api/trips/"+f.tripID+"/members/"+thirdID, f.actor, ""); w.Code != http.StatusForbidden {
		t.Errorf("editor removing another member: got %d, want 403 — body %s", w.Code, w.Body.String())
	}
	if got := f.members(t, f.owner)["third"]; got == nil {
		t.Error("the refused removal happened anyway")
	}
}

// The owner has no membership row, so removing them would be a 404 by accident
// rather than a refusal on purpose — and if it ever worked it would leave a
// trip nobody owns.
func TestOwnerCannotBeRemoved(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	ownerID := f.members(t, f.owner)["owner"]["user_id"].(string)

	for _, as := range []struct {
		name string
		c    *http.Cookie
		want int
	}{
		{"by themselves", f.owner, http.StatusConflict},
		{"by an editor", f.actor, http.StatusForbidden},
	} {
		w := f.ts.do(http.MethodDelete, "/api/trips/"+f.tripID+"/members/"+ownerID, as.c, "")
		if w.Code != as.want {
			t.Errorf("remove owner %s: got %d, want %d — body %s", as.name, w.Code, as.want, w.Body.String())
		}
		if as.want == http.StatusConflict {
			if got := decode[map[string]any](t, w)["code"]; got != "owner_cannot_leave" {
				t.Errorf("code is %v, want owner_cannot_leave", got)
			}
		}
	}
	if w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID, f.owner, ""); w.Code != http.StatusOK {
		t.Error("the owner lost access to their own trip")
	}
}
