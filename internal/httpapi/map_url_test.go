package httpapi

import "testing"

// The outbound Google Maps link. Stage 29 Milestone 2 changed what it says:
// coordinates alone ask Google for a dropped pin, and a name asks for the
// place's own entry.
//
// The cases below are pinned strings rather than parsed URLs on purpose. This
// function has a twin in web/js/url.js that must produce the *same bytes*, and
// tests/ui/map.spec.js asserts the two agree at runtime for one seeded
// location; this table is what pins the exact form the twin is written against,
// including the choices a URL parser would happily normalise away -- "+" for a
// space rather than %20, and a literal comma rather than %2C, which is the form
// verified in a browser and the one Google itself emits.
func TestGoogleMapsURL(t *testing.T) {
	addr := func(s string) *string { return &s }

	cases := []struct {
		name    string
		lat     float64
		lng     float64
		title   string
		address *string
		want    string
	}{
		{
			name:    "title and address land on the place, biased by the coordinates",
			lat:     52.5161791,
			lng:     13.3805048,
			title:   "Hotel Adlon Kempinski",
			address: addr("Unter den Linden 77, 10117 Berlin"),
			want:    "https://www.google.com/maps/search/Hotel+Adlon+Kempinski,+Unter+den+Linden+77,+10117+Berlin/@52.5161791,13.3805048,17z",
		},
		{
			name:  "a title with no address still names the place",
			lat:   64.1,
			lng:   -21.9,
			title: "Hallgrimskirkja",
			want:  "https://www.google.com/maps/search/Hallgrimskirkja/@64.1,-21.9,17z",
		},
		{
			name:    "an empty address is not appended as a trailing comma",
			lat:     1.5,
			lng:     2.5,
			title:   "Somewhere",
			address: addr("   "),
			want:    "https://www.google.com/maps/search/Somewhere/@1.5,2.5,17z",
		},
		{
			// The fallback, and what every link in Caravel was before this.
			name: "no title falls back to the documented coordinate form",
			lat:  48.858844,
			lng:  2.294351,
			want: "https://www.google.com/maps/search/?api=1&query=48.858844,2.294351",
		},
		{
			name:  "a whitespace-only title is no title",
			lat:   1,
			lng:   2,
			title: "  \t ",
			want:  "https://www.google.com/maps/search/?api=1&query=1,2",
		},
		{
			// Searching for a coordinate as text is a tautology, so these take
			// the coordinate form even though there is a title.
			name:  "a title that is itself a coordinate takes the coordinate form",
			lat:   52.5,
			lng:   13.3,
			title: "52.5161791, 13.3805048",
			want:  "https://www.google.com/maps/search/?api=1&query=52.5,13.3",
		},
		{
			name:  "a title of digits with no comma is a name, not a coordinate",
			lat:   1,
			lng:   2,
			title: "42",
			want:  "https://www.google.com/maps/search/42/@1,2,17z",
		},
		{
			// The characters that would break the URL, and the ones that must
			// be left alone because the browser twin leaves them alone.
			name:    "only the dangerous characters are escaped",
			lat:     1,
			lng:     2,
			title:   `Bob's Café & Bar`,
			address: addr("Main St / 3 #b 100% ?x +1"),
			want:    "https://www.google.com/maps/search/Bob's+Café+%26+Bar,+Main+St+%2F+3+%23b+100%25+%3Fx+%2B1/@1,2,17z",
		},
		{
			// Coordinates are the shortest round-tripping form, matching what
			// JS gives a number in a template literal. %f gave 64.100000 here.
			name: "coordinates carry no trailing zeros",
			lat:  64.1,
			lng:  -21.9,
			want: "https://www.google.com/maps/search/?api=1&query=64.1,-21.9",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := googleMapsURL(tc.lat, tc.lng, tc.title, tc.address)
			if got != tc.want {
				t.Errorf("googleMapsURL()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// The bias must be the path segment, never a query parameter. Measured during
// Stage 29 planning: coordinates inside `query` are read as literal text, and a
// name plus a Paris coordinate pair returned results in San Francisco. A
// refactor that "simplified" this back into ?api=1&query= would silently
// restore that bug, so it is asserted rather than only commented.
func TestGoogleMapsURLBiasIsAPathSegment(t *testing.T) {
	got := googleMapsURL(48.8584, 2.2945, "Starbucks", nil)
	if want := "https://www.google.com/maps/search/Starbucks/@48.8584,2.2945,17z"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
