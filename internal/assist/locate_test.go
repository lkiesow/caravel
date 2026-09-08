package assist

import (
	"context"
	"log/slog"
	"testing"

	"caravel/internal/geocode"
)

// The resolver against the fixture geocoder, which is the pair Stage 33 exists
// for: "Kex Hostel, Reykjavik" is the hostel, and its own postal address
// "Skulagata 28, 101 Reykjavik, Iceland" is the street 176m away.
//
// Asserted here rather than only through Propose because it is the *ordering*
// that changed, and a test that has to run a whole agent to see it would not
// say so plainly.

func TestResolvePositionPrefersTheNameOverTheAddress(t *testing.T) {
	a := &Agent{geocoder: geocode.New(geocode.StubURL)}
	log := slog.New(slog.DiscardHandler)

	got := a.resolvePosition(context.Background(), modelProposal{
		PlaceName: "Kex Hostel, Reykjavik",
		Address:   "Skulagata 28, 101 Reykjavik, Iceland",
	}, log)
	if got == nil {
		t.Fatal("no position")
	}
	// The hostel, not the street. Both queries would have resolved -- that is
	// exactly what made the old order look correct.
	if got.From != "place_name" {
		t.Errorf("From = %q, want the place name to have answered", got.From)
	}
	if !got.Precise {
		t.Errorf("Precise = false for %q — the name should find the mapped element", got.Label)
	}
	if got.OSMType != "node" {
		t.Errorf("OSMType = %q, want the hostel node", got.OSMType)
	}

	// And the address on its own is the coarse answer, which is what the old
	// order was returning for this place every time.
	street := a.resolvePosition(context.Background(), modelProposal{
		Address: "Skulagata 28, 101 Reykjavik, Iceland",
	}, log)
	if street == nil {
		t.Fatal("no position from the address")
	}
	if street.Precise {
		t.Error("the street matched as precise")
	}
	if street.Lat == got.Lat && street.Lng == got.Lng {
		t.Fatal("the two queries resolve to the same point — the fixture cannot show the difference")
	}
}

func TestResolvePositionFallsBackToTheAddress(t *testing.T) {
	a := &Agent{geocoder: geocode.New(geocode.StubURL)}

	got := a.resolvePosition(context.Background(), modelProposal{
		PlaceName: "A guesthouse nobody has mapped",
		Address:   "Skulagata 28, 101 Reykjavik, Iceland",
	}, slog.New(slog.DiscardHandler))
	if got == nil {
		t.Fatal("no position after the fallback")
	}
	if got.From != "address" {
		t.Errorf("From = %q, want the address to have answered", got.From)
	}
}

func TestResolvePositionIsNilRatherThanAGuess(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	// Nothing configured.
	none := (&Agent{}).resolvePosition(context.Background(), modelProposal{PlaceName: "Kex Hostel, Reykjavik"}, log)
	if none != nil {
		t.Error("a position appeared with no geocoder configured")
	}

	a := &Agent{geocoder: geocode.New(geocode.StubURL)}
	// Nothing to ask about. The model proposing neither is the sparse
	// candidate the stub script includes on purpose.
	if got := a.resolvePosition(context.Background(), modelProposal{}, log); got != nil {
		t.Error("a position appeared with nothing to search for")
	}
	// Asked, and found nothing. Distinct from the above and the same outcome.
	if got := a.resolvePosition(context.Background(), modelProposal{PlaceName: "somewhere nobody mapped"}, log); got != nil {
		t.Error("a position appeared for a query that missed")
	}
}

// The choice between the two sources. Table-driven over choosePosition rather
// than through a whole agent, because what is being asserted is a decision and
// nothing else -- the plumbing that reaches it has its own tests below.

func TestChoosePosition(t *testing.T) {
	// Two points about 30m apart, which is what genuine agreement measured
	// like against the live services: a building and its doorway, two surveys
	// of the same thing. All five live enrichments that plainly agreed were
	// under 40m.
	const nearLat, nearLng = 64.1466, -21.9253
	const alsoNearLat, alsoNearLng = 64.14684, -21.92512
	// And one 3.4km away, which is plainly a different place.
	const farLat, farLng = 64.1505, -21.8620

	osm := func(precise bool, lat, lng float64) *Position {
		return &Position{Lat: lat, Lng: lng, Label: "osm answer", Source: SourceOSM, Precise: precise, OSMType: "node", OSMID: "1"}
	}
	google := func(lat, lng float64) *Position {
		return &Position{Lat: lat, Lng: lng, Label: "google answer", Source: SourceGoogle, Precise: true}
	}

	for _, tc := range []struct {
		name      string
		osm       *Position
		google    *Position
		want      string // the source expected to lead, or "" for no position
		ambiguous bool
	}{
		{
			name: "neither answered",
			want: "",
		},
		{
			name: "only OpenStreetMap answered",
			osm:  osm(true, nearLat, nearLng),
			want: SourceOSM,
		},
		{
			name:   "only the maps backend answered -- the commercial place OSM misses",
			google: google(nearLat, nearLng),
			want:   SourceGoogle,
		},
		{
			name:   "they agree and the OSM match is the mapped element",
			osm:    osm(true, nearLat, nearLng),
			google: google(alsoNearLat, alsoNearLng),
			want:   SourceOSM,
		},
		{
			name:   "they agree and OSM only found the street",
			osm:    osm(false, alsoNearLat, alsoNearLng),
			google: google(nearLat, nearLng),
			want:   SourceGoogle,
		},
		{
			name:      "they disagree by kilometres -- a precise OSM match does not settle it",
			osm:       osm(true, nearLat, nearLng),
			google:    google(farLat, farLng),
			want:      SourceOSM,
			ambiguous: true,
		},
		{
			name:      "they disagree and the OSM match was coarse, so Google leads",
			osm:       osm(false, nearLat, nearLng),
			google:    google(farLat, farLng),
			want:      SourceGoogle,
			ambiguous: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := choosePosition(tc.osm, tc.google, slog.New(slog.DiscardHandler))
			if tc.want == "" {
				if got != nil {
					t.Fatalf("position = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("no position")
			}
			if got.Source != tc.want {
				t.Errorf("Source = %q, want %q", got.Source, tc.want)
			}
			if got.Ambiguous() != tc.ambiguous {
				t.Errorf("Ambiguous() = %v, want %v", got.Ambiguous(), tc.ambiguous)
			}
			if tc.ambiguous {
				if len(got.Alternatives) != 1 {
					t.Fatalf("alternatives = %d, want exactly the other answer", len(got.Alternatives))
				}
				// The label is how a person tells the two apart, so the loser
				// has to arrive whole rather than as a coordinate.
				if got.Alternatives[0].Label == "" {
					t.Error("the alternative lost its label")
				}
				if got.Alternatives[0].Source == got.Source {
					t.Error("the alternative is from the same source as the winner")
				}
				// One level only: an alternative with alternatives of its own
				// is a shape nothing downstream expects.
				if len(got.Alternatives[0].Alternatives) != 0 {
					t.Error("the alternative carries alternatives of its own")
				}
			}
		})
	}
}

// Choosing must not scribble on either input. choosePosition returns one of
// them in most branches and a copy in the ambiguous one, and the copy is the
// branch where getting it wrong would be invisible.
func TestChoosePositionDoesNotMutateItsInputs(t *testing.T) {
	osm := &Position{Lat: 64.1466, Lng: -21.9253, Label: "osm", Source: SourceOSM, Precise: true}
	google := &Position{Lat: 64.1505, Lng: -21.8620, Label: "google", Source: SourceGoogle, Precise: true}

	got := choosePosition(osm, google, slog.New(slog.DiscardHandler))
	if !got.Ambiguous() {
		t.Fatal("these two are 3.4km apart and should be ambiguous")
	}
	if len(osm.Alternatives) != 0 || len(google.Alternatives) != 0 {
		t.Error("choosePosition attached alternatives to one of its arguments")
	}
}

// End to end through the resolver with both fixtures configured, which is the
// arrangement an operator running `serper` actually gets.
func TestResolvePositionAsksBothSources(t *testing.T) {
	a := &Agent{geocoder: geocode.New(geocode.StubURL), search: &stubSearcher{}}
	log := slog.New(slog.DiscardHandler)

	// A place both know, where OSM has the mapped element. OSM wins, and the
	// feature identity survives -- which is the reason it wins.
	both := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Kex Hostel, Reykjavik"}, log)
	if both == nil {
		t.Fatal("no position")
	}
	if both.Source != SourceOSM || both.OSMID == "" {
		t.Errorf("source = %q, osm id = %q — a precise OSM match should win and keep its identity", both.Source, both.OSMID)
	}
	if both.Ambiguous() {
		t.Error("the two fixtures put Kex Hostel a few metres apart and should agree")
	}

	// A small commercial place the fixture geocoder does not know at all. This
	// is the case the milestone exists for.
	only := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Braud and Co, Reykjavik"}, log)
	if only == nil {
		t.Fatal("no position for a place only the maps backend knows")
	}
	if only.Source != SourceGoogle {
		t.Errorf("source = %q, want %q", only.Source, SourceGoogle)
	}
	if only.Label == "" {
		t.Error("a Google-sourced position needs a label too, or there is nothing to judge it by")
	}

	// And the disagreement.
	split := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Harpa, Reykjavik"}, log)
	if split == nil {
		t.Fatal("no position")
	}
	if !split.Ambiguous() {
		t.Errorf("the fixtures put Harpa 3.4km apart; Ambiguous() = false (label %q)", split.Label)
	}
}

// A backend with no maps endpoint, which is ddgs, Ollama Cloud and every
// unconfigured instance: the resolver must fall back to OpenStreetMap alone
// rather than losing the position.
func TestResolvePositionWithoutAPlaceLocator(t *testing.T) {
	a := &Agent{geocoder: geocode.New(geocode.StubURL), search: searcherWithoutPlaces{}}

	got := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Kex Hostel, Reykjavik"}, slog.New(slog.DiscardHandler))
	if got == nil {
		t.Fatal("no position — the OSM half should still work")
	}
	if got.Source != SourceOSM {
		t.Errorf("source = %q, want %q", got.Source, SourceOSM)
	}
}

// A Searcher that is deliberately *not* a PlaceLocator.
type searcherWithoutPlaces struct{}

func (searcherWithoutPlaces) Name() string { return "no-places" }
func (searcherWithoutPlaces) Search(context.Context, string) ([]SearchResult, error) {
	return nil, nil
}

// The maps backend is asked the name on its own, not the name with the address
// concatenated onto it.
//
// This is a regression test for a measured mistake rather than a guess. The
// first version of locateViaPlaces sent "<name>, <address>" as one query, and
// against the live API "Hotel Rangá, Hella, Suðurlandsvegur, 851 Hella,
// Iceland" found nothing at all -- a hotel Google certainly knows.
// Over-specifying makes a search engine miss, and the failure is silent: the
// resolver falls back to whatever coarse thing OSM said, which in that run was
// a trunk road.
func TestPlacesIsAskedTheNameAloneFirst(t *testing.T) {
	var asked []string
	a := &Agent{search: &recordingLocator{asked: &asked, hitOn: "Hotel Ranga, Hella"}}

	got := a.locateViaPlaces(context.Background(),
		"Hotel Ranga, Hella", "Sudurlandsvegur, 851 Hella, Iceland",
		slog.New(slog.DiscardHandler))
	if got == nil {
		t.Fatal("no position")
	}
	if len(asked) != 1 || asked[0] != "Hotel Ranga, Hella" {
		t.Errorf("queries = %v, want just the place name", asked)
	}
}

// And the address is still the fallback when the name finds nothing, which is
// the case a second credit is worth spending on.
func TestPlacesFallsBackToTheAddress(t *testing.T) {
	var asked []string
	a := &Agent{search: &recordingLocator{asked: &asked, hitOn: "Sudurlandsvegur, 851 Hella, Iceland"}}

	got := a.locateViaPlaces(context.Background(),
		"Somewhere with no findable name", "Sudurlandsvegur, 851 Hella, Iceland",
		slog.New(slog.DiscardHandler))
	if got == nil {
		t.Fatal("no position after the fallback")
	}
	if len(asked) != 2 {
		t.Fatalf("queries = %v, want the name then the address", asked)
	}
	if got.From != "address" {
		t.Errorf("From = %q, want the address to have answered", got.From)
	}
}

// A PlaceLocator that records what it was asked and answers exactly one query.
type recordingLocator struct {
	asked *[]string
	hitOn string
}

func (r *recordingLocator) Name() string { return "recording" }
func (r *recordingLocator) Search(context.Context, string) ([]SearchResult, error) {
	return nil, nil
}
func (r *recordingLocator) SearchPlaces(_ context.Context, query string) ([]PlaceResult, error) {
	*r.asked = append(*r.asked, query)
	if query != r.hitOn {
		return nil, nil
	}
	return []PlaceResult{{Title: "Found", Address: "An address", Lat: 63.84, Lng: -20.31}}, nil
}

// The city, which rides along with the position because it comes from the same
// match. Three cases, and the middle one is the one worth having a test for.

func TestPositionCarriesTheCity(t *testing.T) {
	a := &Agent{geocoder: geocode.New(geocode.StubURL), search: &stubSearcher{}}
	log := slog.New(slog.DiscardHandler)

	// The ordinary case: OSM answered and OSM won.
	osm := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Kex Hostel, Reykjavik"}, log)
	if osm == nil {
		t.Fatal("no position")
	}
	if osm.City != "Reykjavik" {
		t.Errorf("City = %q, want the fixture's city", osm.City)
	}

	// Only the maps backend knows this one, and it reports no settlement as a
	// field -- so there is no city, and that is not a bug to work around by
	// parsing its address string.
	google := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Braud and Co, Reykjavik"}, log)
	if google == nil {
		t.Fatal("no position")
	}
	if google.Source != SourceGoogle {
		t.Fatalf("source = %q, want the maps backend to have answered alone", google.Source)
	}
	if google.City != "" {
		t.Errorf("City = %q, want empty — nothing structured said which city", google.City)
	}

	// The disagreement: 3.4km apart, so the two may not be the same place, and
	// a city tag would assert something the pin itself is being questioned
	// about.
	split := a.resolvePosition(context.Background(), modelProposal{PlaceName: "Harpa, Reykjavik"}, log)
	if split == nil {
		t.Fatal("no position")
	}
	if !split.Ambiguous() {
		t.Fatal("the fixtures put Harpa 3.4km apart and should disagree")
	}
	if split.City != "" {
		t.Errorf("City = %q, want empty while the sources disagree", split.City)
	}
}

// Google's pin, OSM's city: the two agreed about where the place is, and only
// one of them says which city that is. Losing it because the coordinates came
// from the better-placed source would make the tag depend on a decision that
// has nothing to do with it.
func TestAnAgreedGooglePositionKeepsTheOSMCity(t *testing.T) {
	coarse := &Position{
		// A few metres apart, so the two agree that this is one place.
		Lat: 64.14661, Lng: -21.92538,
		Label: "Skulagata, Reykjavik", Source: SourceOSM, City: "Reykjavik",
		// A street: the case where Google's business pin is preferred.
		Precise: false,
	}
	precise := &Position{
		Lat: 64.14659, Lng: -21.92535,
		Label: "Kex Hostel", Source: SourceGoogle, Precise: true,
	}

	got := choosePosition(coarse, precise, slog.New(slog.DiscardHandler))
	if got.Source != SourceGoogle {
		t.Fatalf("source = %q, want %q — a coarse OSM match should lose the pin", got.Source, SourceGoogle)
	}
	if got.City != "Reykjavik" {
		t.Errorf("City = %q, want the city OSM reported", got.City)
	}
	if precise.City != "" {
		t.Error("choosePosition wrote the city onto its argument rather than a copy")
	}
}
