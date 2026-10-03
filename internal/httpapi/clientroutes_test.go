package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestIsClientRoute(t *testing.T) {
	for p, want := range map[string]bool{
		"/":                             true,
		"/trips":                        true,
		"/trips/":                       true,
		"/trips/new":                    true,
		"/settings":                     true,
		"/admin":                        true,
		"/trips/abc":                    true,
		"/trips/abc/map":                true,
		"/trips/abc/settings":           true,
		"/trips/abc/suggest":            true,
		"/trips/abc/locations/new":      true,
		"/trips/abc/locations/def":      true,
		"/trips/abc/locations/def/edit": true,
		// An escaped slash is one segment, as it is to the client router,
		// which matches against location.pathname.
		"/trips/a%2Fb/map": true,

		"/does-not-exist":            false,
		"/trip":                      false,
		"/settings/x":                false,
		"/trips/abc/nonsense":        false,
		"/trips/abc/locations/def/x": false,
		"/trips/abc/map/extra":       false,
	} {
		if got := isClientRoute(p); got != want {
			t.Errorf("isClientRoute(%q) = %v, want %v", p, got, want)
		}
	}
}

// The server's copy of the client route table has to agree with the real one,
// or a new page answers its deep links with a 404 (it still renders, which is
// exactly why nobody would notice) -- or a removed one keeps a 200.
func TestClientRoutesMatchAppJS(t *testing.T) {
	web := filepath.Join("..", "..", "web", "js")

	app := readSource(t, filepath.Join(web, "app.js"))
	var patterns []string
	for _, m := range regexp.MustCompile(`pattern: "([^"]+)"`).FindAllStringSubmatch(app, -1) {
		if m[1] != "*" {
			patterns = append(patterns, m[1])
		}
	}
	assertSameSet(t, "routes in web/js/app.js", patterns, "clientRoutes", clientRoutes)

	// The tab routes are built from TRIP_TABS rather than written out, so
	// the template is what ties the tab keys to a URL shape.
	if !strings.Contains(app, "pattern: `/trips/:tripId/${key}`") {
		t.Error("web/js/app.js no longer builds tab routes as `/trips/:tripId/${key}`; update isClientRoute to match")
	}

	tabs := readSource(t, filepath.Join(web, "trip-tabs.js"))
	start := strings.Index(tabs, "export const TRIP_TABS = [")
	end := strings.Index(tabs[max(start, 0):], "];")
	if start < 0 || end < 0 {
		t.Fatal("could not find the TRIP_TABS array in web/js/trip-tabs.js")
	}
	var keys []string
	for _, m := range regexp.MustCompile(`key: "([^"]+)"`).FindAllStringSubmatch(tabs[start:start+end], -1) {
		keys = append(keys, m[1])
	}
	assertSameSet(t, "TRIP_TABS in web/js/trip-tabs.js", keys, "clientTripTabs", clientTripTabs)
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertSameSet(t *testing.T, jsName string, js []string, goName string, goList []string) {
	t.Helper()
	if len(js) == 0 {
		t.Fatalf("found no entries in %s; has its shape changed?", jsName)
	}
	for _, v := range js {
		if !slices.Contains(goList, v) {
			t.Errorf("%q is in %s but not in %s (internal/httpapi/clientroutes.go)", v, jsName, goName)
		}
	}
	for _, v := range goList {
		if !slices.Contains(js, v) {
			t.Errorf("%q is in %s but no longer in %s", v, goName, jsName)
		}
	}
}
