package geocode

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The stub is driven by the browser suite, where a broken fixture fails as a
// confusing UI test three layers away from the cause. These assert its
// contract here instead, where the message names the fixture.

func TestStubSentinelGivesAWorkingClient(t *testing.T) {
	c := New(StubURL)
	if c == nil {
		t.Fatal("New(StubURL) = nil, want a client")
	}
	// The whole reason the fixture is a loopback server rather than a canned
	// struct: the reverse endpoint has to be derivable, or the suite that
	// tests the reverse-geocoding control silently loses it.
	if !c.ReverseAvailable() {
		t.Error("ReverseAvailable() = false — the stub URL must look like a real /search endpoint")
	}
}

func TestStubAnswersThePreciseAndCoarseCases(t *testing.T) {
	c := New(StubURL)
	ctx := context.Background()

	// The place name finds the building.
	got, err := c.Search(ctx, "Kex Hostel, Reykjavik", "")
	if err != nil {
		t.Fatalf("Search(place name): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Search(place name) returned %d results, want one", len(got))
	}
	if !strings.HasPrefix(got[0].DisplayName, "Kex Hostel") {
		t.Errorf("display name = %q, want the hostel itself", got[0].DisplayName)
	}
	if got[0].OSMType != "node" || got[0].OSMID == "" {
		t.Errorf("OSM identity = %q/%q, want a node with an id — the feature link is built from it",
			got[0].OSMType, got[0].OSMID)
	}

	// The postal address of the same place finds the street, a couple of
	// hundred metres away. This is the case the stage exists to fix, so the
	// fixture has to be able to show it.
	street, err := c.Search(ctx, "Skulagata 28, 101 Reykjavik, Iceland", "")
	if err != nil {
		t.Fatalf("Search(address): %v", err)
	}
	if len(street) != 1 {
		t.Fatalf("Search(address) returned %d results, want one", len(street))
	}
	if street[0].Lat == got[0].Lat && street[0].Lng == got[0].Lng {
		t.Error("the address and the place name resolve to the same point — the fixture cannot show the bug")
	}
}

func TestStubMissesAreEmptyRatherThanErrors(t *testing.T) {
	got, err := New(StubURL).Search(context.Background(), "somewhere nobody mapped", "")
	if err != nil {
		t.Fatalf("Search(unknown): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Search(unknown) returned %d results, want none", len(got))
	}
	if got == nil {
		t.Error("Search(unknown) = nil, want an empty slice — callers tell 'found nothing' from 'did not search'")
	}
}

func TestStubReverseAnswersAnAddressAndAlsoNothing(t *testing.T) {
	c := New(StubURL)
	ctx := context.Background()

	got, err := c.Reverse(ctx, 64.1466, -21.9426, "")
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if got.DisplayName == "" {
		t.Error("Reverse returned no address")
	}
	// The coordinates are the ones asked about, not any the fixture holds.
	if got.Lat != 64.1466 || got.Lng != -21.9426 {
		t.Errorf("Reverse moved the point to %v,%v", got.Lat, got.Lng)
	}

	if _, err := c.Reverse(ctx, 89.5, 0, ""); !errors.Is(err, ErrNoResult) {
		t.Errorf("Reverse(nowhere) err = %v, want ErrNoResult", err)
	}
}
