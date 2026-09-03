package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"caravel/internal/db"
)

// The tag rules themselves live in internal/tags, because the assistant applies
// the same ones to what a model proposes and cannot import this package. What
// stays here is the part that is about rows: writing a set, bucketing a listing
// by location, and reducing one to a trip vocabulary.

// writeItemTags replaces a location's tag set. Delete-then-insert rather than a
// diff, because a tag row carries nothing worth preserving across the rewrite:
// no id anything refers to, no order, no note. Contrast reconcileItemDates,
// which diffs precisely because the rows underneath it are itinerary entries
// somebody else may have annotated.
//
// The caller passes a transaction-bound store, so the tags and the item commit
// together.
func writeItemTags(ctx context.Context, store db.Store, itemID string, tags []string) error {
	if err := store.DeleteItemTagsByItem(ctx, itemID); err != nil {
		return err
	}
	for _, tag := range tags {
		if err := store.CreateItemTag(ctx, itemID, tag); err != nil {
			return err
		}
	}
	return nil
}

// tagsByItem buckets a trip-wide tag listing by location, for attaching tags to
// the rows of the locations list in one query rather than one per row -- the
// same shape as the coordinate map in handleListItems.
func tagsByItem(rows []db.ItemTag) map[string][]string {
	out := make(map[string][]string)
	for _, row := range rows {
		out[row.ItemID] = append(out[row.ItemID], row.Tag)
	}
	return out
}

// distinctTags reduces a trip-wide listing to the vocabulary in use on it,
// sorted case-insensitively so the editor suggestions read alphabetically
// rather than with every capitalised tag first.
//
// Two spellings of one word can both appear here, which is the visible
// consequence of the case rule in tags.Normalize. That is the point: seeing
// Museum in the suggestions is what stops the next person typing museum.
func distinctTags(rows []db.ItemTag) []string {
	seen := make(map[string]bool, len(rows))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if seen[row.Tag] {
			continue
		}
		seen[row.Tag] = true
		out = append(out, row.Tag)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i]), strings.ToLower(out[j])
		if li != lj {
			return li < lj
		}
		return out[i] < out[j]
	})
	return out
}

// handleListTripTags answers the vocabulary already in use on a trip, for the
// editor to suggest. Viewer role: it is a projection of locations a viewer can
// already read in full.
//
// The locations tab does not call this -- it already holds every location and
// derives the list from what it has. This exists for the editor, which holds
// one location and would otherwise fetch the whole trip to learn three words.
func (s *Server) handleListTripTags(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}

	rows, err := s.Store.ListItemTagsByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list tags")
		return
	}
	writeJSON(w, http.StatusOK, distinctTags(rows))
}
