package assist

import (
	"context"
	"strings"
	"testing"
)

// Coverage for what happens to the tags a model proposes: the cap, the
// re-spelling against the trip vocabulary, and the rule that a proposal adds
// to the user's set rather than replacing it.

func TestCleanProposedTags(t *testing.T) {
	long := strings.Repeat("x", 41)

	for _, tc := range []struct {
		name       string
		raw        string
		vocabulary []string
		want       string
	}{
		{"trims and splits", " museum ,  city centre ", nil, "museum|city centre"},
		{"drops empties", "museum,,  ,ferry", nil, "museum|ferry"},
		{"collapses inner space", "national    park", nil, "national park"},
		{"dedupes case-insensitively", "Museum, museum, MUSEUM", nil, "Museum"},
		{
			"caps at the stated number",
			"one, two, three, four, five, six, seven",
			nil,
			"one|two|three|four|five",
		},
		{
			"drops an over-long tag rather than truncating it",
			"museum," + long + ",ferry",
			nil,
			"museum|ferry",
		},
		{
			"adopts the spelling the trip already uses",
			"City Centre, HOSTEL, brewery",
			[]string{"city centre", "hostel"},
			"city centre|hostel|brewery",
		},
		{
			"folds two near-spellings of a known tag into one",
			"City Centre, city centre",
			[]string{"city centre"},
			"city centre",
		},
		{"nothing at all", "   ", []string{"museum"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(cleanProposedTags(tc.raw, tc.vocabulary), "|")
			if got != tc.want {
				t.Errorf("cleanProposedTags(%q) = %q, want %q", tc.raw, got, tc.want)
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
			if got := proposeTags(tc.current, tc.raw, nil); got != tc.want {
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
	got := splitTags(proposeTags(joinTags(current), "one, two, three", nil))

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
	got := joinTags(cleanProposedTags("Church, church, historic, old, tall, stone, famous", []string{"church"}))
	if want := "church, historic, old, tall, stone"; got != want {
		t.Errorf("candidate tags = %q, want %q", got, want)
	}
}
