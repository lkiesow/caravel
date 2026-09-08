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

// cityTag is the geocoded settlement, as a tag, or "" for nothing to add.
//
// Lowercase, because a tag is a filter label rather than a name in prose and
// the rest of a trip vocabulary tends to be written that way -- "museum",
// "ferry", "free entry". Where the trip already spells the city some other
// way, cleanProposedTags re-spells this to match it, which is how "Berlin"
// stays "Berlin" on a trip that already uses it.
//
// The length limit is the one the save enforces, applied here for the same
// reason splitTags applies it: letting an over-long tag through would only
// move the failure to the Save button. A settlement name that long is a
// Nominatim answer that is not really a settlement name.
func cityTag(city string) string {
	tag := strings.ToLower(tags.Clean(city))
	if tag == "" || utf8.RuneCountInString(tag) > tags.MaxLength {
		return ""
	}
	return tag
}

// cleanProposedTags is what this run found, reduced to what may be offered:
// the geocoded city plus what the model said, parsed, capped at
// maxProposedTags, and re-spelled to match the trip.
//
// The city goes first, ahead of every tag the model proposed, and that
// ordering is the cap talking. A model that answered with five tags of its own
// would otherwise push the city out of a five-tag budget -- and of the two
// kinds of tag in here, the city is the one that came from a geocoder rather
// than from a language model, so it is the last one that should lose a
// tie-break. It is also the only one whose accuracy the position review
// already covers: an accepted pin and a city tag come from the same match.
//
// The re-spelling is the other part worth having. The prompt asks for a
// vocabulary tag to be reused exactly, and mostly it is, but "City Centre"
// against an existing "city centre" is a near miss that normalizeTags cannot
// fold away -- it folds duplicates *within* one location, and these are on
// two. Adopting the spelling already in use is the cheap fix, and it is not
// inventing anything: both strings came back meaning the tag the trip already
// has. The city rides the same path, which is what makes "prefer the trip's
// own capitalisation, lowercase otherwise" a property of one loop rather than
// a rule stated twice.
func cleanProposedTags(city, raw string, vocabulary []string) []string {
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
	if tag := cityTag(city); tag != "" {
		// Normalize again, because the model naming the city too is the
		// ordinary case rather than a surprise, and the duplicate has to fold
		// away before the cap counts it. Prepending keeps the city's spelling
		// as the survivor, since Normalize keeps the first one it sees.
		out = tags.Normalize(append([]string{tag}, out...))
	}
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
// city is the settlement the position resolved to, which is why this is called
// after resolvePosition rather than alongside the other fields.
//
// Empty covers both nothing-found and nothing-new, which is what the caller
// wants: an empty proposal is silence. Note what this does *not* do -- compare
// the two strings. Reordering or respacing the same set is not a change, and
// offering one as a suggestion badged "Replaces what is there" trains people
// to click past a review that is supposed to mean something.
func proposeTags(current, city, raw string, vocabulary []string) string {
	have := splitTags(current)
	merged := mergeTags(have, cleanProposedTags(city, raw, vocabulary))
	if len(merged) == len(have) {
		return ""
	}
	return joinTags(merged)
}
