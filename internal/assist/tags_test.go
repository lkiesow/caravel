package assist

import (
	"context"
	"strings"
	"testing"

	"caravel/internal/geocode"
)

// Coverage for what happens to the tags a model proposes: the cap, the
// re-spelling against the trip vocabulary, and the rule that a proposal adds
// to the user's set rather than replacing it.

func TestCleanProposedTags(t *testing.T) {
	long := strings.Repeat("x", 41)

	for _, tc := range []struct {
		name       string
		city       string
		raw        string
		vocabulary []string
		want       string
	}{
		{"trims and splits", "", " museum ,  city centre ", nil, "museum|city centre"},
		{"drops empties", "", "museum,,  ,ferry", nil, "museum|ferry"},
		{"collapses inner space", "", "national    park", nil, "national park"},
		{"dedupes case-insensitively", "", "Museum, museum, MUSEUM", nil, "Museum"},
		{
			"caps at the stated number",
			"",
			"one, two, three, four, five, six, seven",
			nil,
			"one|two|three|four|five",
		},
		{
			"drops an over-long tag rather than truncating it",
			"",
			"museum," + long + ",ferry",
			nil,
			"museum|ferry",
		},
		{
			"adopts the spelling the trip already uses",
			"",
			"City Centre, HOSTEL, brewery",
			[]string{"city centre", "hostel"},
			"city centre|hostel|brewery",
		},
		{
			"folds two near-spellings of a known tag into one",
			"",
			"City Centre, city centre",
			[]string{"city centre"},
			"city centre",
		},
		{"nothing at all", "", "   ", []string{"museum"}, ""},

		// The city the geocoder resolved to, which is the half of the set
		// that did not come from the model.
		{"the city is added, lowercased", "Berlin", "monument", nil, "berlin|monument"},
		{"and leads, so the cap cannot push it out", "Paris", "one, two, three, four, five", nil, "paris|one|two|three|four"},
		{
			"the trip's own spelling wins over lowercase",
			"Berlin", "monument", []string{"Berlin"},
			"Berlin|monument",
		},
		{
			"the model naming it too is not two tags",
			"Tokyo", "Tokyo, tower", nil,
			"tokyo|tower",
		},
		{"no city resolved, nothing added", "", "monument", nil, "monument"},
		{"nor an unusable one", strings.Repeat("x", 41), "monument", nil, "monument"},
		{"a city alone is still a proposal", "Reykjavik", "", nil, "reykjavik"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(cleanProposedTags(tc.city, tc.raw, tc.vocabulary), "|")
			if got != tc.want {
				t.Errorf("cleanProposedTags(%q, %q) = %q, want %q", tc.city, tc.raw, got, tc.want)
			}
		})
	}
}

func TestProposeTagsIsAdditive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current string
		raw     string
		want    string
	}{
		{
			"keeps what is there and appends what was found",
			"hostel, harbour",
			"reykjavik",
			"hostel, harbour, reykjavik",
		},
		{
			// The enrich prompt asks the model not to restate what is there,
			// so this is the shape a well-behaved run actually returns -- and
			// accepting it must not leave the place tagged only "reykjavik".
			"an answer of only new tags does not replace the old ones",
			"hostel",
			"reykjavik, harbour",
			"hostel, reykjavik, harbour",
		},
		{"nothing new is no proposal", "hostel, harbour", "harbour, hostel", ""},
		{"a reordering is not a change", "a, b, c", "c, b, a", ""},
		{"neither is a difference of case alone", "Hostel", "hostel", ""},
		{"nor one of spacing", "hostel,harbour", "hostel , harbour", ""},
		{"nothing found is no proposal", "hostel", "", ""},
		{"an empty field takes the lot", "", "hostel, harbour", "hostel, harbour"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := proposeTags(tc.current, "", tc.raw, nil); got != tc.want {
				t.Errorf("proposeTags(%q, %q) = %q, want %q", tc.current, tc.raw, got, tc.want)
			}
		})
	}
}

// The additions stop at the per-location limit; the user's own tags never do.
func TestProposeTagsNeverDropsExistingTags(t *testing.T) {
	// One short of the limit, so exactly one addition fits.
	current := []string{
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j",
		"k", "l", "m", "n", "o", "p", "q", "r", "s",
	}
	got := splitTags(proposeTags(joinTags(current), "", "one, two, three", nil))

	if len(got) != 20 {
		t.Fatalf("merged to %d tags, want the 20 the save allows", len(got))
	}
	for i, tag := range current {
		if got[i] != tag {
			t.Fatalf("position %d is %q, want the existing %q -- an existing tag was pushed out", i, got[i], tag)
		}
	}
	if got[19] != "one" {
		t.Errorf("last tag = %q, want the first addition that fit", got[19])
	}
}

// End to end through Propose, because the cap is worth proving where a run
// actually reaches it rather than only on the helper.
func TestProposedTagsAreCappedAndRespelled(t *testing.T) {
	req := enrichRequest()
	req.Current.Tags = "hostel"
	req.TagVocabulary = []string{"city centre"}

	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{
			Category: "stay",
			Tags:     "City Centre, harbour, cheap, central, lively, backpackers, bar",
		})},
	)
	p, err := a.Propose(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	f, ok := fieldNamed(p, "tags")
	if !ok {
		t.Fatal("no tags proposed")
	}
	want := "hostel, city centre, harbour, cheap, central, lively"
	if f.Proposed != want {
		t.Errorf("proposed %q, want %q", f.Proposed, want)
	}
	// Five found plus the one already there: the run does not get to spend the
	// user's whole tag budget.
	if got := len(splitTags(f.Proposed)); got != maxProposedTags+1 {
		t.Errorf("%d tags, want %d", got, maxProposedTags+1)
	}
	if f.Overwrites() {
		t.Error("an addition was badged as replacing what is there")
	}
}

// A candidate is a place that does not exist yet, so there is nothing to merge
// with -- but the cap and the vocabulary still apply.
func TestCandidateTagsAreCleaned(t *testing.T) {
	got := joinTags(cleanProposedTags("", "Church, church, historic, old, tall, stone, famous", []string{"church"}))
	if want := "church, historic, old, tall, stone"; got != want {
		t.Errorf("candidate tags = %q, want %q", got, want)
	}
}

// End to end through Propose: the city the geocoder resolved to reaches the
// user as a tag, without the model having said anything about it.
//
// The model's answer below deliberately does not mention Reykjavik, because
// that is the whole point -- this tag is derived from where the place turned
// out to be, not from what the model chose to say about it.
func TestTheGeocodedCityBecomesATag(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{
			Category:  "stay",
			Tags:      "hostel, harbour",
			PlaceName: "Kex Hostel, Reykjavik",
		})},
	)
	a.geocoder = geocode.New(geocode.StubURL)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Position == nil || p.Position.City != "Reykjavik" {
		t.Fatalf("the fixture did not resolve a city: %+v", p.Position)
	}

	f, ok := fieldNamed(p, fieldTags)
	if !ok {
		t.Fatal("no tags proposed")
	}
	// Lowercase, and ahead of the model's own tags -- see cleanProposedTags.
	if want := "reykjavik, hostel, harbour"; f.Proposed != want {
		t.Errorf("proposed %q, want %q", f.Proposed, want)
	}
}

// The same tag on a trip that already spells the city its own way. The trip
// wins: a filter is only useful if everything in it is spelled alike, and the
// user's spelling is the one already on other locations.
func TestTheCityTagAdoptsTheTripsSpelling(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{
			Category:  "stay",
			Tags:      "hostel",
			PlaceName: "Kex Hostel, Reykjavik",
		})},
	)
	a.geocoder = geocode.New(geocode.StubURL)

	req := enrichRequest()
	req.TagVocabulary = []string{"Reykjavik", "hostel"}

	p, err := a.Propose(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	f, ok := fieldNamed(p, fieldTags)
	if !ok {
		t.Fatal("no tags proposed")
	}
	if want := "Reykjavik, hostel"; f.Proposed != want {
		t.Errorf("proposed %q, want %q", f.Proposed, want)
	}
}

// A place that resolved nowhere proposes no city, and does not lose the tags
// the model did find. The geocoder is unconfigured here, which is also every
// instance that has not set CARAVEL_GEOCODER_URL.
func TestNoPositionMeansNoCityTag(t *testing.T) {
	a := agentWith(
		stubTurn{Content: "done"},
		stubTurn{Content: answerJSON(t, modelProposal{
			Category:  "stay",
			Tags:      "hostel, harbour",
			PlaceName: "Somewhere nobody has mapped",
		})},
	)

	p, err := a.Propose(context.Background(), enrichRequest(), nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p.Position != nil {
		t.Fatalf("a position appeared with no geocoder: %+v", p.Position)
	}
	f, ok := fieldNamed(p, fieldTags)
	if !ok {
		t.Fatal("no tags proposed")
	}
	if want := "hostel, harbour"; f.Proposed != want {
		t.Errorf("proposed %q, want %q", f.Proposed, want)
	}
}
