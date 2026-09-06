package geocode

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The stub geocoder.
//
// # Why it exists
//
// Until Stage 33 the browser suite left CARAVEL_GEOCODER_URL at its default,
// so every run of assist.spec.js and locations.spec.js asked the public
// Nominatim -- a volunteer-run service with its own rate limits and its own
// opinion about automated traffic -- for the same three coordinates. Stage 27
// made that materially worse: a trip-level suggestion run geocodes every
// candidate it proposes, serialised precisely because of that policy, so one
// spec was six lookups and a slower run for no gain.
//
// # Why a fixture host rather than a canned struct
//
// Same shape as internal/wikimedia/stub.go and the assistant's fixture host,
// and for a better reason than symmetry: a canned struct would replace
// Client.get, which is where the timeout, the identifying User-Agent, the
// non-200 handling and the JSON decoding all live. Those would then be
// unexercised by every test that uses the stub. A loopback server answering
// Nominatim-shaped JSON leaves the whole package live and fakes exactly one
// thing -- who is on the other end of the socket.
//
// It also means the /search and /reverse paths are real siblings, so
// ReverseURL derives one from the other exactly as it does against a real
// instance and ReverseAvailable stays true. A sentinel that was not
// URL-shaped would silently turn the reverse-geocoding control off in the
// suite that is meant to be testing it.
//
// # What it answers
//
// A small table keyed on the query, covering the three cases the resolution
// algorithm has to tell apart:
//
//   - a PRECISE match -- the OSM element somebody actually mapped, which is
//     the point a user expects to see;
//   - a COARSE match -- a street, which is what a postal address usually
//     resolves to and is the pin that lands a couple of hundred metres out;
//   - a MISS -- an empty array, so "searched, found nothing" is reachable.
//
// The queries are the ones the stub assistant proposes (internal/assist,
// stub.go), because the browser suite reaches this through a stubbed run
// rather than by typing.

// StubURL is the sentinel endpoint that starts the fixture. Same idea as
// assist.LLMStub and wikimedia.StubURL, and spelled as a bare word because
// that is what an operator types.
const StubURL = "stub"

// stubPlace is one row of the fixture table.
type stubPlace struct {
	// queries are matched case-insensitively after trimming. Several spellings
	// map to one place because the resolver tries the place name and the
	// postal address separately, and they are different strings for the same
	// building.
	queries []string
	name    string
	lat     float64
	lng     float64
	// class, kind and addressType are Nominatim's `category`, `type` and
	// `addresstype`. They are what says whether this is a building or the road
	// it stands on, and the resolution algorithm reads them.
	class       string
	kind        string
	addressType string
	osmType     string
	osmID       string
}

// The fixture table. Coordinates are the real ones, to within the precision a
// pin needs: a stub that puts Reykjavik in the sea would make every map
// assertion in the suite unreadable.
var stubPlaces = []stubPlace{
	{
		// The precise case: the hostel itself, as an OSM node with an
		// identity, which is what the feature link is built from.
		queries:     []string{"kex hostel, reykjavik", "kex hostel"},
		name:        "Kex Hostel, 28, Skulagata, Reykjavik, 101, Iceland",
		lat:         64.146590,
		lng:         -21.925350,
		class:       "tourism",
		kind:        "hostel",
		addressType: "hostel",
		osmType:     "node",
		osmID:       "1370624482",
	},
	{
		// The coarse case, and the whole point of the stage: the postal
		// address of the place above resolves to the street, 150m north of
		// the door. Whichever query the resolver sends first is visible in
		// which of these two answers comes back.
		queries:     []string{"skulagata 28, 101 reykjavik, iceland", "skulagata 28"},
		name:        "Skulagata, Reykjavik, 101, Iceland",
		lat:         64.147930,
		lng:         -21.923410,
		class:       "highway",
		kind:        "residential",
		addressType: "road",
		osmType:     "way",
		osmID:       "23553640",
	},
	{
		queries:     []string{"hallgrimskirkja, reykjavik", "hallgrimskirkja"},
		name:        "Hallgrimskirkja, 1, Hallgrimstorg, Reykjavik, 101, Iceland",
		lat:         64.141900,
		lng:         -21.926500,
		class:       "amenity",
		kind:        "place_of_worship",
		addressType: "place_of_worship",
		osmType:     "way",
		osmID:       "23553642",
	},
	{
		// The disagreement case. The stub places backend (internal/assist,
		// search.go) answers "Harpa" with a point 3.4km east of this one, so a
		// resolver asking both sources has to notice that they cannot both be
		// right. Nothing in this file needs to know that; it just has to be a
		// real place with a real position, and this is Harpa's.
		queries:     []string{"harpa, reykjavik", "harpa concert hall, reykjavik", "harpa"},
		name:        "Harpa, 2, Austurbakki, Reykjavik, 101, Iceland",
		lat:         64.150470,
		lng:         -21.932100,
		class:       "amenity",
		kind:        "arts_centre",
		addressType: "arts_centre",
		osmType:     "way",
		osmID:       "23553646",
	},
	{
		queries:     []string{"hallgrimstorg 1, 101 reykjavik, iceland", "hallgrimstorg 1"},
		name:        "Hallgrimstorg, Reykjavik, 101, Iceland",
		lat:         64.142430,
		lng:         -21.927220,
		class:       "highway",
		kind:        "pedestrian",
		addressType: "road",
		osmType:     "way",
		osmID:       "23553644",
	},
}

// stubReverseName is what every reverse lookup answers with.
//
// One answer rather than a table, because Reverse keeps the coordinates it was
// asked about and takes only the address text (see its doc comment) -- so the
// interesting variable is whether an address comes back at all, and a second
// row would say nothing the first does not. A query nobody could mean answers
// with nothing; see stubReverseNowhere.
const stubReverseName = "Vonarstraeti 4, 101 Reykjavik, Iceland"

// stubReverseNowhere is the latitude past which the fixture reports no
// address, so the ErrNoResult path -- the middle of an ocean, honestly -- is
// reachable from a test without needing a second table.
const stubReverseNowhere = 85.0

var startStubFixture = sync.OnceValue(func() string {
	// Port 0: the kernel picks. One listener for the life of the process, for
	// the same reason the other two fixtures are singletons -- every test that
	// constructs a stub client would otherwise leak one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("geocode: starting the stub geocoder: " + err.Error())
	}
	base := "http://" + ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubSearchAnswer(r.URL.Query().Get("q")))
	})
	mux.HandleFunc("/reverse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubReverseAnswer(r.URL.Query().Get("lat")))
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return base + "/search"
})

// stubSearchAnswer is the /search reply for one query: a list of zero or one
// candidates, in Nominatim's own shape.
//
// One rather than several deliberately. The thing worth stubbing is which
// place a query resolves to, and a fixture that returned five would invite the
// resolver to be tested on how it ranks them -- which is a real question, and
// one that belongs to the live service rather than to a table written here.
func stubSearchAnswer(query string) []map[string]any {
	key := strings.ToLower(strings.TrimSpace(query))
	for _, p := range stubPlaces {
		for _, q := range p.queries {
			if q != key {
				continue
			}
			return []map[string]any{{
				"display_name": p.name,
				// Strings, as Nominatim sends them, so the parsing in
				// toResult is exercised rather than bypassed.
				"lat":         strconv.FormatFloat(p.lat, 'f', -1, 64),
				"lon":         strconv.FormatFloat(p.lng, 'f', -1, 64),
				"category":    p.class,
				"type":        p.kind,
				"addresstype": p.addressType,
				"osm_type":    p.osmType,
				// A number, again as Nominatim sends it.
				"osm_id": json.Number(p.osmID),
			}}
		}
	}
	// Not in the table: searched, found nothing.
	return []map[string]any{}
}

// stubReverseAnswer is the /reverse reply. An object rather than an array, and
// on a miss an object with nothing in it -- which is what a real instance
// sends, and why Reverse checks for an empty display name rather than trusting
// the status code.
func stubReverseAnswer(lat string) map[string]any {
	if parsed, err := strconv.ParseFloat(lat, 64); err == nil && (parsed > stubReverseNowhere || parsed < -stubReverseNowhere) {
		return map[string]any{"error": "Unable to geocode"}
	}
	return map[string]any{"display_name": stubReverseName}
}
