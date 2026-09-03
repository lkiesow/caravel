package assist

import (
	"strings"
	"unicode/utf8"

	"caravel/internal/tags"
)

// What happens to the tags a model proposes.
//
// Tags used to be the one field in a proposal that was passed through
// untouched, while the category was checked against an enum and the
// coordinates were re-derived from the geocoder regardless of what was said.
// That showed: with no stated limit anywhere, "a few short keywords" produced
// ten, and with the trip vocabulary in the prompt as an invitation to reuse,
// it produced ten *plausible* ones. The prompt now names a number, and this
// file enforces it -- the same belt-and-braces as maxSuggestions, which is
// stated in words and truncated as well.
//
// The other half is that a tags proposal is additive. Tags are a set, and the
// enrich prompt asks the model not to restate what is already there, so a run
// that answers with only the new tags would -- accepted -- replace the set
// with those alone. The proposal therefore carries the current set plus what
// was found, which makes accepting it strictly an addition and keeps the
// promise the rest of buildProposal keeps: this feature never offers to delete
// what somebody wrote.

// maxProposedTags is how many tags one run may add to a place.
//
// Deliberately far below tags.MaxPerItem, because the two limits answer
// different questions. That one is a resource guard on the save transaction;
// this one is about a filter staying useful, and a place carrying five tags
// still filters. It is also the number the prompt and the schema state, so
// changing it means changing all three -- placeFields and proposalSchema.
const maxProposedTags = 5

// splitTags parses the comma-separated string the wire and the model both use.
// Empty and over-long entries are dropped rather than truncated: half a tag is
// not a shorter tag, and the length limit here is the one the save enforces, so
// letting one through would only move the failure to the Save button.
func splitTags(raw string) []string {
	out := make([]string, 0, 8)
	for _, part := range strings.Split(raw, ",") {
		tag := tags.Clean(part)
		if tag == "" || utf8.RuneCountInString(tag) > tags.MaxLength {
			continue
		}
		out = append(out, tag)
	}
	return tags.Normalize(out)
}

// joinTags renders a set back into the string the wire carries.
func joinTags(list []string) string { return strings.Join(list, ", ") }

// cleanProposedTags is what a model said, reduced to what may be offered:
// parsed, capped at maxProposedTags, and re-spelled to match the trip.
//
// The re-spelling is the part worth having. The prompt asks for a vocabulary
// tag to be reused exactly, and mostly it is, but "City Centre" against an
// existing "city centre" is a near miss that normalizeTags cannot fold away --
// it folds duplicates *within* one location, and these are on two. Adopting
// the spelling already in use is the cheap fix, and it is not inventing
// anything: both strings came back meaning the tag the trip already has.
func cleanProposedTags(raw string, vocabulary []string) []string {
	known := make(map[string]string, len(vocabulary))
	for _, tag := range vocabulary {
		if tag = tags.Clean(tag); tag != "" {
			// First spelling wins, matching the vocabulary's own order.
			if _, ok := known[strings.ToLower(tag)]; !ok {
				known[strings.ToLower(tag)] = tag
			}
		}
	}

	out := splitTags(raw)
	for i, tag := range out {
		if existing, ok := known[strings.ToLower(tag)]; ok {
			out[i] = existing
		}
	}
	if len(out) > maxProposedTags {
		out = out[:maxProposedTags]
	}
	return out
}

// mergeTags adds what was found to what is already there, in that order.
//
// Nothing current is ever dropped, including when the union would exceed
// tags.MaxPerItem: it is the additions that stop, because a proposal that
// silently pushed one of the user's own tags out to fit its own would be the
// deletion this whole path exists to avoid.
func mergeTags(current, proposed []string) []string {
	out := append([]string(nil), current...)
	seen := make(map[string]bool, len(current)+len(proposed))
	for _, tag := range out {
		seen[strings.ToLower(tag)] = true
	}
	for _, tag := range proposed {
		if len(out) >= tags.MaxPerItem {
			break
		}
		if key := strings.ToLower(tag); !seen[key] {
			seen[key] = true
			out = append(out, tag)
		}
	}
	return out
}

// proposeTags builds the tags field of a proposal, or "" for no proposal.
//
// Empty covers both nothing-found and nothing-new, which is what the caller
// wants: an empty proposal is silence. Note what this does *not* do -- compare
// the two strings. Reordering or respacing the same set is not a change, and
// offering one as a suggestion badged "Replaces what is there" trains people
// to click past a review that is supposed to mean something.
func proposeTags(current, raw string, vocabulary []string) string {
	have := splitTags(current)
	merged := mergeTags(have, cleanProposedTags(raw, vocabulary))
	if len(merged) == len(have) {
		return ""
	}
	return joinTags(merged)
}
