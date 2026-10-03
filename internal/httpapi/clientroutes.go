package httpapi

import "strings"

// clientRoutes mirrors the route table in web/js/app.js, in its own ":param"
// syntax, so the SPA fallback can tell a deep link from a typo: both get the
// shell (the client renders its own not-found page), but only the deep link
// gets a 200. TestClientRoutesMatchAppJS holds the two lists to each other.
//
// What this cannot know is whether the record behind a valid shape exists:
// /trips/<missing id> is still a 200, and only the API call the page makes
// finds out.
var clientRoutes = []string{
	"/",
	"/trips",
	"/settings",
	"/admin",
	"/trips/new",
	"/trips/:tripId/suggest",
	"/trips/:tripId/locations/new",
	"/trips/:tripId/locations/:itemId/edit",
	"/trips/:tripId/locations/:itemId",
	"/trips/:tripId",
}

// clientTripTabs mirrors TRIP_TABS in web/js/trip-tabs.js: app.js turns each
// key into a /trips/:tripId/<key> route.
var clientTripTabs = []string{
	"locations", "map", "itinerary", "notes",
	"checklists", "files", "expenses", "members", "settings",
}

// isClientRoute reports whether the client router has a route for p, matching
// the way match() in web/js/router.js does: empty segments are dropped (so a
// trailing slash is ignored), the segment counts must agree, a ":param"
// segment matches anything and a literal must match exactly.
func isClientRoute(p string) bool {
	parts := splitSegments(p)
	for _, route := range clientRoutes {
		if segmentsMatch(splitSegments(route), parts) {
			return true
		}
	}
	for _, tab := range clientTripTabs {
		if segmentsMatch([]string{"trips", ":tripId", tab}, parts) {
			return true
		}
	}
	return false
}

func splitSegments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func segmentsMatch(pattern, parts []string) bool {
	if len(pattern) != len(parts) {
		return false
	}
	for i, seg := range pattern {
		if !strings.HasPrefix(seg, ":") && seg != parts[i] {
			return false
		}
	}
	return true
}
