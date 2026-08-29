package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"caravel/internal/db"
)

// Tags are keywords on a location whose meaning the user chooses -- a kind of
// site, a city, a region, whose idea it was. The app never interprets one; it
// stores what was typed and offers it back.
//
// The rules below are about keeping the *set* honest, not about policing
// vocabulary. Two tags differing only in case or in surrounding space are the
// same tag to a reader, so they must not both survive on one location; beyond
// that, anything goes.

const (
	// A tag is a keyword, not a sentence. Long enough for "national park" or
	// a hyphenated place name, short enough that the chip stays a chip.
	maxTagLength = 40
	// Per location. Nothing needs this many, and without a cap one request
	// can write an unbounded number of rows inside the save transaction --
	// the same reasoning as maxItemDateSpan above.
	maxTagsPerItem = 20
)

// normalizeTag trims and collapses inner whitespace. It does NOT change case:
// what somebody typed is what the chip shows, and the editor suggests the
// trip's existing tags so spellings converge by use rather than by a rule that
// would have to pick a winner.
func normalizeTag(tag string) string {
	return strings.Join(strings.Fields(tag), " ")
}

// normalizeTags cleans a submitted set: each tag trimmed, empties dropped, and
// duplicates removed case-insensitively, keeping the first spelling seen.
//
// Case-insensitive here and exact in SQL is deliberate and worth stating,
// because it is the one place the two disagree. Within one location Museum and
// museum are the same tag and only the first survives. Across two locations
// both can exist, and the primary key on (item_id, tag) does not care. Making
// them agree would mean either a case-folded column -- storing something the
// user did not type -- or a trip-wide uniqueness rule that would have to
// rewrite one location to save another.
func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, raw := range tags {
		tag := normalizeTag(raw)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	return out
}

// validateTags checks an already-normalized set. Counting runes rather than
// bytes, so a limit of 40 means 40 characters to the person typing them and
// not 13 of anything outside ASCII.
func validateTags(tags []string) error {
	if len(tags) > maxTagsPerItem {
		return fmt.Errorf("a location may carry at most %d tags", maxTagsPerItem)
	}
	for _, tag := range tags {
		if utf8.RuneCountInString(tag) > maxTagLength {
			return fmt.Errorf("a tag may be at most %d characters", maxTagLength)
		}
	}
	return nil
}

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
// consequence of the case rule in normalizeTags. That is the point: seeing
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
