package assist

import (
	"context"
	"log/slog"
	"strings"
)

// Turning what the model said into where the place is.
//
// The model is forbidden to give coordinates (see researchRules in prompt.go)
// and never sees the ones that reach the user: it supplies a postal address
// and a searchable place name, and this file resolves them. That division is
// the point -- a plausible lat/lng 40km from the real hotel is the one
// hallucination with no visible tell, because it looks entirely correct in the
// form and is wrong only on the map.
//
// # Why the place name is tried first
//
// Until Stage 33 the address was tried first and the place name was the
// fallback, and that is backwards for almost everything a trip is made of. A
// postal address is a question about a delivery point, and Nominatim answers
// it with a house-number node, an interpolated point along the way, or -- most
// often -- the street itself. The result is a pin on the road outside the
// hotel: near enough to look right in a screenshot and wrong enough to walk to
// the wrong door. Searching the *name* usually finds the element somebody
// actually mapped, which is the building.
//
// The address is still worth having as the fallback. It is what rescues the
// case with no findable name: a rented apartment, a friend's spare room, an
// office. So the order is name, then address -- and, since Stage 33, a coarse
// answer to either is recorded as coarse rather than passed off as a position
// somebody can trust.
//
// # One resolver, not two
//
// buildProposal and buildCandidates used to carry the same loop twice, with
// the same bug in both copies. They now share this.

// Position is a resolved place: where it is, and what is known about how good
// that answer is.
//
// A struct rather than the pair of *float64 the agent used to pass around,
// because the coordinates alone were never enough to review. What the user
// needs in order to judge a pin is what was matched and how -- see Label and
// Precise, which the panel shows in place of six decimal places nobody can
// read.
type Position struct {
	Lat float64
	Lng float64

	// Label is the matched place as the source names it -- a whole formatted
	// address for Nominatim. Shown to the user, never stored: it is evidence
	// about the pin, not a field of the location.
	Label string

	// Source is which service answered. One of the source constants below.
	Source string

	// Precise reports that the match is the place itself rather than the
	// street or the district it is in. See geocode.Result.Precise.
	Precise bool

	// From is which of the model's two strings resolved -- "place_name" or
	// "address". Kept for the log rather than for the user: the address
	// answering means the model got the street right, and falling through to
	// it means the name found nothing, which is the interesting difference
	// when a run positions something badly.
	From string

	// OSMType and OSMID are the OpenStreetMap identity of the match, when it
	// has one. Empty for any source that is not OpenStreetMap -- a coordinate
	// has no OSM identity. Carried through so that accepting a proposed
	// position gives the location the same feature link an address search
	// would have (Stage 29).
	OSMType string
	OSMID   string
}

// latLng exposes the pair placeIndex and the transport still work in, or two
// nils for no position. Nil-safe on purpose: "this candidate has nowhere" is
// the ordinary case and should not need a check at every call site.
func (p *Position) latLng() (*float64, *float64) {
	if p == nil {
		return nil, nil
	}
	return &p.Lat, &p.Lng
}

// The sources a Position can come from. Strings rather than an enum because
// they cross the wire to the client, which shows a badge per source.
const (
	// SourceOSM is OpenStreetMap, through internal/geocode.
	SourceOSM = "osm"
)

// resolvePosition finds where a proposed place is, or returns nil.
//
// Nil rather than an error: not finding a place is an ordinary outcome, and it
// costs the proposal its coordinates and nothing else. Every failure below --
// no geocoder configured, both queries empty, both queries missing, an
// upstream that is down -- ends the same way, because the user-visible
// consequence is the same and a proposal without a pin is still worth having.
func (a *Agent) resolvePosition(ctx context.Context, raw modelProposal, log *slog.Logger) *Position {
	if a.geocoder == nil {
		log.Debug("assist: no coordinates", "reason", "no geocoder configured")
		return nil
	}

	// Name before address. See the file comment for why that order is the
	// whole point of Stage 33.
	for _, from := range []struct{ source, query string }{
		{"place_name", strings.TrimSpace(raw.PlaceName)},
		{"address", strings.TrimSpace(raw.Address)},
	} {
		if from.query == "" {
			continue
		}
		// No locale: the display name is shown as evidence about the pin
		// rather than saved, and asking for one language over another would
		// not change which place is matched.
		results, err := a.geocoder.Search(ctx, from.query, "")
		if err != nil || len(results) == 0 {
			log.Debug("assist: geocode missed", "from", from.source, "query", from.query, "err", err)
			continue
		}

		best := results[0]
		pos := &Position{
			Lat:     best.Lat,
			Lng:     best.Lng,
			Label:   best.DisplayName,
			Source:  SourceOSM,
			Precise: best.Precise(),
			From:    from.source,
			OSMType: best.OSMType,
			OSMID:   best.OSMID,
		}
		// Which query answered and how good the match is are the two things
		// that explain a bad pin afterwards, so both are logged. `precise` in
		// particular: a coarse match is not a failure, and it is also not a
		// position anybody should be told to trust.
		log.Debug("assist: coordinates resolved",
			"from", from.source, "query", from.query, "matches", len(results),
			"class", best.Class, "type", best.Kind, "precise", pos.Precise)
		return pos
	}

	return nil
}
