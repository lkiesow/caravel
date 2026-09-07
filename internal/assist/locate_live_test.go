package assist

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"caravel/internal/geocode"
)

// A hand-run probe against the *real* OpenStreetMap and the *real* Serper, for
// the one question that cannot be answered from a fixture: how often do the
// two sources actually disagree by more than ambiguousMetres, and is that
// number a useful signal or a nuisance?
//
// # Why it exists
//
// ambiguousMetres is expected to move (see its comment). The thing to do with
// it is measure, and measuring means live calls -- a fixture can only tell you
// what you already put in it. This is the harness for that, so the next person
// to wonder whether 150m is right does not have to rebuild it.
//
// # Why it cannot run by accident
//
// It costs money -- one Serper credit per place per query -- and it sends
// traffic to a volunteer-run service. So it needs *two* things to be true: a
// key, and CARAVEL_LIVE_PROBE=1 set on purpose. A developer with the project's
// .env exported into their shell has the first and not the second, which is
// exactly the accident this guards against; `go test ./...` and `make ci` skip
// it silently.
//
//	CARAVEL_LIVE_PROBE=1 go test ./internal/assist/ -run TestLiveSourceAgreement -v
//
// It asserts almost nothing on purpose. There is no correct answer to compare
// against -- that is the whole problem -- so it prints a table and leaves the
// judgement to the person who ran it.
func TestLiveSourceAgreement(t *testing.T) {
	key := os.Getenv("CARAVEL_SEARCH_KEY")
	if os.Getenv("CARAVEL_LIVE_PROBE") != "1" || key == "" {
		t.Skip("set CARAVEL_LIVE_PROBE=1 and CARAVEL_SEARCH_KEY to run this; it makes paid, outbound calls")
	}

	search, err := NewSearcher("serper", key, "")
	if err != nil {
		t.Fatalf("building the search backend: %v", err)
	}
	a := &Agent{
		geocoder: geocode.New("https://nominatim.openstreetmap.org/search"),
		search:   search,
	}
	log := slog.New(slog.DiscardHandler)

	// A spread on purpose: places OSM maps well, places it maps badly, a large
	// complex where two pins can honestly differ, and three cities so the
	// answer is not a fact about Iceland.
	places := []struct{ name, address string }{
		{"Kex Hostel, Reykjavik", "Skulagata 28, 101 Reykjavik, Iceland"},
		{"Hallgrimskirkja, Reykjavik", "Hallgrimstorg 1, 101 Reykjavik, Iceland"},
		{"Kaffibarinn, Reykjavik", "Bergstadastraeti 1, 101 Reykjavik, Iceland"},
		{"Hotel Ranga, Hella, Iceland", "Sudurlandsvegur, 851 Hella, Iceland"},
		{"Braud and Co, Reykjavik", "Frakkastigur 16, 101 Reykjavik, Iceland"},
		{"Harpa Concert Hall, Reykjavik", "Austurbakki 2, 101 Reykjavik, Iceland"},
		{"Sensoji Temple, Tokyo", "2-3-1 Asakusa, Taito City, Tokyo, Japan"},
		{"Tsuta ramen, Tokyo", "1-14-1 Sugamo, Toshima City, Tokyo, Japan"},
		{"Cafe Einstein Stammhaus, Berlin", "Kurfurstenstrasse 58, 10785 Berlin, Germany"},
		{"Hotel Adlon Kempinski, Berlin", "Unter den Linden 77, 10117 Berlin, Germany"},
	}

	var agreed, ambiguous, oneSided int
	t.Logf("%-34s %9s %8s %10s", "place", "apart", "chose", "ambiguous")
	for _, p := range places {
		osm := a.locateViaOSM(context.Background(), p.name, p.address, log)
		google := a.locateViaPlaces(context.Background(), p.name, p.address, log)

		if osm == nil || google == nil {
			oneSided++
			t.Logf("%-34s %9s %8s   only one source answered (osm=%v google=%v)",
				p.name, "-", "-", osm != nil, google != nil)
			continue
		}
		apart := metresBetween(osm.Lat, osm.Lng, google.Lat, google.Lng)
		chosen := choosePosition(osm, google, log)
		if chosen.Ambiguous() {
			ambiguous++
		} else {
			agreed++
		}
		t.Logf("%-34s %7.0f m %8s %10v  (osm precise=%v)",
			p.name, apart, chosen.Source, chosen.Ambiguous(), osm.Precise)
	}
	t.Logf("both answered: %d agreed, %d ambiguous at %dm; only one source: %d",
		agreed, ambiguous, ambiguousMetres, oneSided)
}
