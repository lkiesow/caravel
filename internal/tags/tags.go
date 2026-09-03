// Package tags holds the rules for a location's tag set.
//
// Tags are keywords on a location whose meaning the user chooses -- a kind of
// site, a city, a region, whose idea it was. The app never interprets one; it
// stores what was typed and offers it back.
//
// The rules here are about keeping the *set* honest, not about policing
// vocabulary. Two tags differing only in case or in surrounding space are the
// same tag to a reader, so they must not both survive on one location; beyond
// that, anything goes.
//
// This lives in its own package rather than in internal/httpapi because the
// assistant applies the same rules to what a model proposes, and internal/assist
// cannot import the API package that imports it. Two copies of a normalisation
// rule drift, and a tag folded away on save that the assistant had shown as a
// suggestion looks like data loss.
package tags

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxLength is per tag. A tag is a keyword, not a sentence: long enough for
	// "national park" or a hyphenated place name, short enough that the chip
	// stays a chip.
	MaxLength = 40
	// MaxPerItem is per location. Nothing needs this many, and without a cap
	// one request can write an unbounded number of rows inside the save
	// transaction.
	MaxPerItem = 20
)

// Clean trims and collapses inner whitespace. It does NOT change case: what
// somebody typed is what the chip shows, and the editor suggests the trip's
// existing tags so spellings converge by use rather than by a rule that would
// have to pick a winner.
func Clean(tag string) string {
	return strings.Join(strings.Fields(tag), " ")
}

// Normalize cleans a submitted set: each tag trimmed, empties dropped, and
// duplicates removed case-insensitively, keeping the first spelling seen.
//
// Case-insensitive here and exact in SQL is deliberate and worth stating,
// because it is the one place the two disagree. Within one location Museum and
// museum are the same tag and only the first survives. Across two locations
// both can exist, and the primary key on (item_id, tag) does not care. Making
// them agree would mean either a case-folded column -- storing something the
// user did not type -- or a trip-wide uniqueness rule that would have to
// rewrite one location to save another.
func Normalize(list []string) []string {
	out := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, raw := range list {
		tag := Clean(raw)
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

// Validate checks an already-normalized set. Counting runes rather than bytes,
// so a limit of 40 means 40 characters to the person typing them and not 13 of
// anything outside ASCII.
func Validate(list []string) error {
	if len(list) > MaxPerItem {
		return fmt.Errorf("a location may carry at most %d tags", MaxPerItem)
	}
	for _, tag := range list {
		if utf8.RuneCountInString(tag) > MaxLength {
			return fmt.Errorf("a tag may be at most %d characters", MaxLength)
		}
	}
	return nil
}
