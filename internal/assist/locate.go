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
// # Two sources, and what to do when they disagree
//
// Stage 33 Milestone 3 asks a second service as well, when one is configured:
// Serper's /places, which is Google Maps data. The two are good at different
// things. OpenStreetMap is better for landmarks, museums, churches and
// stations, and it is the only one that carries a feature identity worth
// linking to; Google is far better on the restaurants, cafes, bars, shops and
// hotels a trip is actually made of, and its pin is the business own position
// rather than an address interpolation.
//
// Both are asked for every place, rather than escalating to the second only
// when the first disappoints. That costs a paid call per location -- up to six
// in a trip-suggestion run -- and buys the one thing a single source can never
// give: disagreement. Two services putting a place kilometres apart is not a
// pin to show with confidence, it is a question, and the only way to know is
// to have asked twice.
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

	// Alternatives are the other sources answers, when they are far enough
	// away that both cannot be describing the same place. Empty in the
	// ordinary case, which is the two services agreeing to within a street.
	//
	// Non-empty means "do not accept this without looking": it is the signal
	// the UI turns into a question, and the one case where Accept all must
	// not decide on somebody behalf. An alternative never carries
	// alternatives of its own.
	Alternatives []Position
}

// Ambiguous reports that the sources disagreed and a person has to choose.
//
// Nil-safe, because "there is no position" and "the position is not in doubt"
// are both false here and every caller wants that.
func (p *Position) Ambiguous() bool {
	return p != nil && len(p.Alternatives) > 0
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
	// SourceGoogle is Google Maps data, through a PlaceLocator backend.
	// Named for the data rather than for Serper, because the reseller is not
	// what a person reading a badge cares about and is not what would change
	// if the reseller did.
	SourceGoogle = "google"
)

// ambiguousMetres is how far apart two sources have to put a place before the
// answer is a question rather than a position.
//
// 150m, which is deliberately tight, and chosen against measurement rather
// than intuition. Five live enrichments during Stage 33 Milestone 3 separated
// the two services by 12m, 14m, 34m and 1116m. The first draft of this
// constant was 2000, on the reasoning that two services can legitimately pin
// opposite ends of one complex -- which is true, and let the 1116m pair
// through as "agreement" in a city centre where 1.1km is a different
// neighbourhood. A kilometre in a dense city is not a rounding error; it is
// being lost.
//
// So the threshold is set where the evidence puts the boundary: everything
// that plainly agreed was under 40m, and everything above it is worth a look.
// The cost of being too tight is a question the user did not need, which is
// visible and cheap; the cost of being too loose is a wrong pin nobody was
// asked about, which is the bug this stage exists to fix. Wrong in the
// direction that shows itself.
//
// Expected to move. It is one number and the thing to do with it is watch how
// often the question actually fires in use, then raise it if the answer is
// "constantly". plans/todo.md carries that as an open item.
//
// Note it happens to equal samePlaceMetres in agent.go, and the two are not
// related: that one asks whether two *candidates* are the same place, this one
// asks whether two *sources* are describing the same one. Changing either
// should not drag the other along.
const ambiguousMetres = 150

// resolvePosition finds where a proposed place is, or returns nil.
//
// Nil rather than an error: not finding a place is an ordinary outcome, and it
// costs the proposal its coordinates and nothing else. Every failure below --
// no geocoder configured, both queries empty, both queries missing, an
// upstream that is down -- ends the same way, because the user-visible
// consequence is the same and a proposal without a pin is still worth having.
func (a *Agent) resolvePosition(ctx context.Context, raw modelProposal, log *slog.Logger) *Position {
	name := strings.TrimSpace(raw.PlaceName)
	address := strings.TrimSpace(raw.Address)
	if name == "" && address == "" {
		// Nothing to ask about, so nothing is asked. The stub script includes
		// a candidate like this on purpose: sparse is the common case, and it
		// must not cost a request to either service.
		return nil
	}

	// The two lookups run at once, because they are different services and
	// neither is waiting on the other. Note what is *not* parallelised: the
	// loop over candidates in buildCandidates stays sequential, because
	// nominatim.openstreetmap.org asks for at most one request a second and
	// six candidates resolving together is exactly the traffic it asks people
	// not to send.
	type answer struct{ pos *Position }
	osmCh := make(chan answer, 1)
	googleCh := make(chan answer, 1)

	go func() { osmCh <- answer{a.locateViaOSM(ctx, name, address, log)} }()
	go func() { googleCh <- answer{a.locateViaPlaces(ctx, name, address, log)} }()

	osm, google := (<-osmCh).pos, (<-googleCh).pos
	return choosePosition(osm, google, log)
}

// locateViaOSM asks the geocoder, place name first and postal address second.
//
// The order is the point of this stage. See the file comment: an address
// resolves to the street, a name resolves to the building.
func (a *Agent) locateViaOSM(ctx context.Context, name, address string, log *slog.Logger) *Position {
	if a.geocoder == nil {
		log.Debug("assist: no osm lookup", "reason", "no geocoder configured")
		return nil
	}
	for _, from := range []struct{ source, query string }{
		{"place_name", name},
		{"address", address},
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
		log.Debug("assist: osm resolved",
			"from", from.source, "query", from.query, "matches", len(results),
			"class", best.Class, "type", best.Kind, "precise", best.Precise())
		return &Position{
			Lat:     best.Lat,
			Lng:     best.Lng,
			Label:   best.DisplayName,
			Source:  SourceOSM,
			Precise: best.Precise(),
			From:    from.source,
			OSMType: best.OSMType,
			OSMID:   best.OSMID,
		}
	}
	return nil
}

// locateViaPlaces asks the maps-style backend, when the configured search
// provider has one.
//
// The same two-query ladder as the OSM half, and for the same reason: name
// first, address as the fallback. The schema already asks the model to put the
// city in the place name ("Kex Hostel, Reykjavik"), which is exactly the query
// a maps search wants.
//
// The first version of this concatenated the two into one query, on the
// reasoning that a maps search is happy to be given more than it strictly
// needs. Measured against the live API, that is false and expensively so:
// "Hotel Rangá, Hella, Suðurlandsvegur, 851 Hella, Iceland" found nothing,
// where the name alone finds the hotel. Over-specifying makes a search engine
// miss. The second query costs a second credit and is only reached when the
// first found nothing, which is the case where the alternative is no answer at
// all.
//
// A place found this way is always treated as precise. That is not a claim
// about accuracy so much as about kind: a maps API answers with businesses and
// landmarks, never with "the street this is on", so there is no coarse case
// for it to report.
func (a *Agent) locateViaPlaces(ctx context.Context, name, address string, log *slog.Logger) *Position {
	if a.search == nil {
		return nil
	}
	locator, ok := a.search.(PlaceLocator)
	if !ok {
		// ddgs, Ollama Cloud, and any future backend without a maps endpoint.
		// Silent rather than logged: it is a property of the configuration,
		// not an event, and it would otherwise be logged once per place.
		return nil
	}

	for _, from := range []struct{ source, query string }{
		{"place_name", name},
		{"address", address},
	} {
		if from.query == "" {
			continue
		}
		results, err := locator.SearchPlaces(ctx, from.query)
		if err != nil || len(results) == 0 {
			log.Debug("assist: places missed", "from", from.source, "query", from.query, "err", err)
			continue
		}
		best := results[0]
		log.Debug("assist: places resolved",
			"from", from.source, "query", from.query, "matches", len(results), "category", best.Category)
		return &Position{
			Lat:     best.Lat,
			Lng:     best.Lng,
			Label:   placeLabel(best),
			Source:  SourceGoogle,
			Precise: true,
			From:    from.source,
		}
	}
	return nil
}

// placeLabel is what the user is shown as evidence for a Google-sourced pin:
// the same thing a geocoder display name gives them, assembled from the two
// fields this API reports separately.
func placeLabel(p PlaceResult) string {
	switch {
	case p.Title != "" && p.Address != "":
		return p.Title + ", " + p.Address
	case p.Title != "":
		return p.Title
	default:
		return p.Address
	}
}

// choosePosition decides between the two answers.
//
// The rules, in the order they are applied:
//
//  1. Only one answered -- that one, nothing to weigh.
//  2. They disagree by more than ambiguousMetres -- neither is trusted. The
//     better-placed one leads and the other is kept as an alternative, so the
//     UI can ask rather than guess. This is checked *first*, because a precise
//     OSM match 40km from Google's is not made right by being precise.
//  3. The OSM match is precise -- OSM. It is the element somebody mapped, and
//     it is the only one of the two that carries a feature identity.
//  4. Otherwise -- Google. Which is to say: OSM answered with a street or a
//     district, and a maps API has a business at a real address. That is the
//     case this whole milestone exists for.
func choosePosition(osm, google *Position, log *slog.Logger) *Position {
	switch {
	case osm == nil && google == nil:
		log.Debug("assist: no coordinates", "reason", "neither source found the place")
		return nil
	case google == nil:
		return osm
	case osm == nil:
		return google
	}

	apart := metresBetween(osm.Lat, osm.Lng, google.Lat, google.Lng)
	if apart > ambiguousMetres {
		// Which one leads still matters -- it is what a person sees first --
		// so the same preference applies, and the loser is kept whole rather
		// than reduced to a coordinate, because the label is how anybody
		// tells the two apart.
		lead, other := google, osm
		if osm.Precise {
			lead, other = osm, google
		}
		log.Debug("assist: sources disagree",
			"metres", int(apart), "osm", osm.Label, "google", google.Label, "leading", lead.Source)
		chosen := *lead
		alt := *other
		alt.Alternatives = nil
		chosen.Alternatives = []Position{alt}
		return &chosen
	}

	if osm.Precise {
		log.Debug("assist: sources agree", "metres", int(apart), "chose", SourceOSM, "reason", "precise match")
		return osm
	}
	log.Debug("assist: sources agree", "metres", int(apart), "chose", SourceGoogle, "reason", "the osm match was coarse")
	return google
}
