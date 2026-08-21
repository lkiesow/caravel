package httpapi

import (
	"net/http"
	"testing"

	"caravel/internal/db"
)

// A personal file is visible only to whoever uploaded it — including to the
// trip's owner, who can see everything else on the trip. That is the whole
// point: the motivating case is putting a boarding pass or an identity card on
// a shared trip, and an owner who can read those has not been given a private
// file.

// upload puts a file on the trip with the given visibility and returns its id.
func (f *roleFixture) uploadFile(t *testing.T, as *http.Cookie, name, visibility string) string {
	t.Helper()
	w := f.ts.uploadWithFields("/api/trips/"+f.tripID+"/files", as, name, "text/plain",
		[]byte("contents of "+name), map[string]string{"visibility": visibility})
	if w.Code != http.StatusCreated {
		t.Fatalf("upload %s: got %d, body %s", name, w.Code, w.Body.String())
	}
	resp := decode[map[string]any](t, w)
	if visibility != "" && resp["visibility"] != visibility {
		t.Fatalf("uploaded %s with visibility %v, wanted %q", name, resp["visibility"], visibility)
	}
	return resp["id"].(string)
}

func (f *roleFixture) fileNames(t *testing.T, as *http.Cookie) []string {
	t.Helper()
	w := f.ts.do(http.MethodGet, "/api/trips/"+f.tripID+"/files", as, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list files: got %d, body %s", w.Code, w.Body.String())
	}
	out := []string{}
	for _, row := range decode[[]map[string]any](t, w) {
		out = append(out, row["filename"].(string))
	}
	return out
}

func TestPersonalFileIsInvisibleToOthers(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	ownerPersonal := f.uploadFile(t, f.owner, "owner-passport.txt", "personal")
	actorPersonal := f.uploadFile(t, f.actor, "actor-passport.txt", "personal")
	shared := f.uploadFile(t, f.actor, "hotel-booking.txt", "trip")

	// setupRole's fixture already uploaded one trip-visible file.
	ownerSees := f.fileNames(t, f.owner)
	actorSees := f.fileNames(t, f.actor)

	if !contains(ownerSees, "owner-passport.txt") || contains(ownerSees, "actor-passport.txt") {
		t.Errorf("owner sees %v — wants their own personal file and not the actor's", ownerSees)
	}
	if !contains(actorSees, "actor-passport.txt") || contains(actorSees, "owner-passport.txt") {
		t.Errorf("actor sees %v — wants their own personal file and not the owner's", actorSees)
	}
	// The trip owner is not exempt: that is the case a naive implementation
	// gets wrong, since the owner can otherwise reach everything on the trip.
	if !contains(ownerSees, "hotel-booking.txt") || !contains(actorSees, "hotel-booking.txt") {
		t.Errorf("a trip file is missing: owner=%v actor=%v", ownerSees, actorSees)
	}

	// Having the id is not access. Hiding it from a listing while leaving the
	// download open would be no privacy at all.
	for _, tc := range []struct {
		name, id string
		as       *http.Cookie
	}{
		{"owner reaching the actor's personal file", actorPersonal, f.owner},
		{"actor reaching the owner's personal file", ownerPersonal, f.actor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, req := range []struct{ method, path, body string }{
				{http.MethodGet, "/api/files/" + tc.id + "/download", ""},
				{http.MethodPatch, "/api/files/" + tc.id, `{"note":"mine now"}`},
				{http.MethodDelete, "/api/files/" + tc.id, ""},
				{http.MethodPut, "/api/files/" + tc.id + "/visibility", `{"visibility":"trip"}`},
			} {
				w := f.ts.do(req.method, req.path, tc.as, req.body)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s %s: got %d, want 404 — body %s", req.method, req.path, w.Code, w.Body.String())
				}
			}
		})
	}

	// The uploader can still reach their own.
	if w := f.ts.do(http.MethodGet, "/api/files/"+actorPersonal+"/download", f.actor, ""); w.Code != http.StatusOK {
		t.Errorf("actor downloading their own personal file: got %d", w.Code)
	}
	if w := f.ts.do(http.MethodGet, "/api/files/"+shared+"/download", f.owner, ""); w.Code != http.StatusOK {
		t.Errorf("owner downloading a trip file: got %d", w.Code)
	}
}

// Item-attached files go through a different list query, so the predicate has
// to be on both. A personal file on a location is the likelier case, if
// anything — that is where a booking lives.
func TestPersonalItemFileIsInvisibleToOthers(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	path := "/api/items/" + f.itemID + "/files"

	w := f.ts.uploadWithFields(path, f.actor, "actor-ticket.txt", "text/plain",
		[]byte("x"), map[string]string{"visibility": "personal"})
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: got %d, body %s", w.Code, w.Body.String())
	}
	w = f.ts.uploadWithFields(path, f.actor, "shared-map.txt", "text/plain",
		[]byte("x"), map[string]string{"visibility": "trip"})
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: got %d", w.Code)
	}

	names := func(as *http.Cookie) []string {
		out := []string{}
		for _, row := range decode[[]map[string]any](t, f.ts.do(http.MethodGet, path, as, "")) {
			out = append(out, row["filename"].(string))
		}
		return out
	}
	if got := names(f.owner); contains(got, "actor-ticket.txt") || !contains(got, "shared-map.txt") {
		t.Errorf("owner sees %v on the item — wants the shared file only", got)
	}
	if got := names(f.actor); !contains(got, "actor-ticket.txt") {
		t.Errorf("actor cannot see their own personal item file: %v", got)
	}
}

// The default is `trip`, deliberately. An upload with no visibility field, or
// one the server does not recognise, must produce a visible file rather than a
// silently hidden one.
func TestUploadVisibilityDefault(t *testing.T) {
	f := setupRole(t, db.RoleEditor)

	for _, tc := range []struct{ name, value string }{
		{"omitted", ""},
		{"empty", ""},
		{"nonsense", "secret"},
		{"shared (a checklist value, not a file one)", "shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]string{}
			if tc.value != "" {
				fields["visibility"] = tc.value
			}
			w := f.ts.uploadWithFields("/api/trips/"+f.tripID+"/files", f.actor,
				"x.txt", "text/plain", []byte("x"), fields)
			if w.Code != http.StatusCreated {
				t.Fatalf("upload: got %d, body %s", w.Code, w.Body.String())
			}
			if got := decode[map[string]any](t, w)["visibility"]; got != "trip" {
				t.Errorf("visibility is %v, want trip", got)
			}
		})
	}
}

// Only the uploader may change who sees their file — an editor may rename or
// delete shared content, but not decide who reads someone else's document.
func TestOnlyUploaderChangesVisibility(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	id := f.uploadFile(t, f.actor, "actor-file.txt", "trip")

	w := f.ts.do(http.MethodPut, "/api/files/"+id+"/visibility", f.owner, `{"visibility":"personal"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("owner changing the actor's file visibility: got %d, want 403 — body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["code"]; got != "not_file_owner" {
		t.Errorf("code is %v, want not_file_owner", got)
	}
	// The owner may still edit and delete it: this is a narrower rule layered on
	// the editor role, not a replacement for it.
	if w := f.ts.do(http.MethodPatch, "/api/files/"+id, f.owner, `{"note":"renamed"}`); w.Code != http.StatusOK {
		t.Errorf("owner editing a trip-visible file's note: got %d", w.Code)
	}

	// The uploader can, in both directions, and it takes effect.
	if w := f.ts.do(http.MethodPut, "/api/files/"+id+"/visibility", f.actor, `{"visibility":"personal"}`); w.Code != http.StatusOK {
		t.Fatalf("uploader hiding their own file: got %d, body %s", w.Code, w.Body.String())
	}
	if got := f.fileNames(t, f.owner); contains(got, "actor-file.txt") {
		t.Error("the file is still visible to the owner after being made personal")
	}
	if w := f.ts.do(http.MethodPut, "/api/files/"+id+"/visibility", f.actor, `{"visibility":"trip"}`); w.Code != http.StatusOK {
		t.Fatalf("uploader sharing it again: got %d", w.Code)
	}
	if got := f.fileNames(t, f.owner); !contains(got, "actor-file.txt") {
		t.Error("the file did not come back when made trip-visible again")
	}

	// A viewer cannot change anything, including their own uploads — they
	// cannot upload in the first place.
	if w := f.ts.do(http.MethodPut, "/api/files/"+id+"/visibility", f.actor, `{"visibility":"nonsense"}`); w.Code != http.StatusBadRequest {
		t.Errorf("nonsense visibility: got %d, want 400", w.Code)
	}
}

// Removing someone from a trip takes their personal files with them: they are
// invisible to everyone else by definition, and their owner has just lost the
// trip the files live on.
func TestRemovingMemberDeletesTheirPersonalFiles(t *testing.T) {
	f := setupRole(t, db.RoleEditor)
	actorID := f.members(t, f.owner)["actor"]["user_id"].(string)

	personal := f.uploadFile(t, f.actor, "actor-private.txt", "personal")
	shared := f.uploadFile(t, f.actor, "actor-shared.txt", "trip")

	// The member list tells the owner what a removal would destroy.
	if got := f.members(t, f.owner)["actor"]["personal_file_count"]; got != float64(1) {
		t.Errorf("personal_file_count is %v, want 1", got)
	}

	if w := f.ts.do(http.MethodDelete, "/api/trips/"+f.tripID+"/members/"+actorID, f.owner, ""); w.Code != http.StatusNoContent {
		t.Fatalf("remove member: got %d, body %s", w.Code, w.Body.String())
	}

	// The trip-visible file stays: that one is the trip's, not theirs.
	if w := f.ts.do(http.MethodGet, "/api/files/"+shared+"/download", f.owner, ""); w.Code != http.StatusOK {
		t.Errorf("a trip-visible file was deleted along with the membership: got %d", w.Code)
	}
	if got := f.fileNames(t, f.owner); !contains(got, "actor-shared.txt") {
		t.Errorf("owner sees %v after the removal, wanted the shared file to remain", got)
	}

	// And the personal file is really gone. Checking this from the owner's side
	// would prove nothing: they get 404 on someone else's personal file whether
	// it exists or not, which is exactly what the first version of this test
	// did — it passed with the deletion removed entirely.
	//
	// Re-adding the member is the observable difference, and it is the real
	// contract too: being removed and added back does not bring your private
	// files back with you.
	f.ts.mustCreateNoID(http.MethodPost, "/api/trips/"+f.tripID+"/members", f.owner,
		`{"username":"actor","role":"editor"}`, http.StatusCreated)
	if got := f.fileNames(t, f.actor); contains(got, "actor-private.txt") {
		t.Errorf("re-added member sees %v — their personal file survived the removal", got)
	}
	if w := f.ts.do(http.MethodGet, "/api/files/"+personal+"/download", f.actor, ""); w.Code != http.StatusNotFound {
		t.Errorf("the removed member's own personal file is still downloadable: got %d, want 404", w.Code)
	}
	// Their shared upload did survive, so the removal was surgical rather than
	// deleting everything they had touched.
	if got := f.fileNames(t, f.actor); !contains(got, "actor-shared.txt") {
		t.Errorf("re-added member sees %v — their trip-visible upload should have survived", got)
	}
}

// A viewer may read trip-visible files and cannot upload at all, so they never
// have a personal file of their own. Pinned because "let a viewer keep private
// notes" is a tempting exception that would make read-only mean something else.
func TestViewerCannotUploadEvenPersonally(t *testing.T) {
	f := setupRole(t, db.RoleViewer)

	w := f.ts.uploadWithFields("/api/trips/"+f.tripID+"/files", f.actor, "x.txt", "text/plain",
		[]byte("x"), map[string]string{"visibility": "personal"})
	if w.Code != http.StatusForbidden {
		t.Errorf("viewer uploading a personal file: got %d, want 403 — body %s", w.Code, w.Body.String())
	}
}
