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
