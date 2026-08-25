package wikimedia

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The stub encyclopaedia.
//
// # Why a fixture host rather than a canned struct
//
// The image picker is a grid of pictures the browser has to actually load,
// and the thing most likely to be wrong about it is not the Go code -- it is
// whether a thumbnail renders, whether picking one reaches POST /media/url
// with the right full-size URL, and whether a picture that fails to load
// leaves a hole in the grid. None of that is reachable from a test that stops
// at the JSON.
//
// So StubURL starts a server on loopback that answers the two API calls
// Search makes and serves real PNGs from the URLs it returns. It is the same
// shape as the assistant's fixture host (internal/assist/stub_fixture.go) and
// exists for the same reason, with one difference worth noting: nothing here
// weakens a security control. The image fetcher has no address policy to
// relax, so this is a fixture and nothing more.
//
// # What it deliberately gets wrong
//
// One of the three results points at a path the fixture does not serve. A
// dead thumbnail is a real and common case -- hotlink-blocked hosts, moved
// files -- and "the grid keeps an invisible empty cell" is exactly the bug
// image-field.js already had to learn about for its own preview. A fixture
// where everything loads could not catch it coming back.

// StubURL is the sentinel endpoint that starts the fixture. Same idea as
// assist.LLMStub, and spelled as a URL because that is what the field it goes
// in holds.
const StubURL = "stub"

// stubPNG is a small solid-colour PNG in the requested tint, so the three
// results are visibly different pictures rather than three copies of one.
func stubPNG(tint color.RGBA, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for x := range size {
		for y := range size {
			img.Set(x, y, tint)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("wikimedia: encoding a stub image: " + err.Error())
	}
	return buf.Bytes()
}

// stubImages are the pictures the fixture offers, in the order Search returns
// them. The third has no file on the server: see the note above.
var stubImages = []struct {
	file    string
	tint    color.RGBA
	width   int
	height  int
	title   string
	credit  string
	licence string
	dead    bool
}{
	{file: "Stub_harbour.png", tint: color.RGBA{R: 20, G: 120, B: 200, A: 255}, width: 800, height: 600,
		title: "Stub Harbour", credit: "A. Photographer", licence: "CC BY-SA 4.0"},
	{file: "Stub_square.png", tint: color.RGBA{R: 200, G: 90, B: 40, A: 255}, width: 640, height: 640,
		title: "Stub Square", credit: "B. Photographer", licence: "Public domain"},
	{file: "Stub_missing.png", tint: color.RGBA{}, width: 900, height: 500,
		title: "Stub Missing", credit: "C. Photographer", licence: "CC BY 3.0", dead: true},
}

// stubBase is the fixture's root URL, remembered so StubImageURL can name a
// file it really serves.
var stubBase string

var startStubFixture = sync.OnceValue(func() string {
	// Port 0: the kernel picks. One listener for the life of the process, for
	// the same reason the assistant's fixture is a singleton -- every test
	// that constructs a stub client would otherwise leak one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("wikimedia: starting the stub fixture host: " + err.Error())
	}
	base := "http://" + ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/w/api.php", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubAnswer(base, r.URL.Query()))
	})
	for _, img := range stubImages {
		if img.dead {
			continue
		}
		body := stubPNG(img.tint, 64)
		mux.HandleFunc("/img/"+img.file, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(body)
		})
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	stubBase = base
	return base + "/w/api.php"
})

// StubImageURL is a picture the fixture really serves, for other packages
// stubbing something that has to hand the browser a loadable image.
//
// internal/assist is the caller: its stub image search offers one live result
// and one dead one, so the browser suite can see both a group that renders and
// a thumbnail that removes itself. Without a live URL from somewhere, every
// stubbed web result would be dead and the group could never be looked at.
func StubImageURL() string {
	startStubFixture()
	return stubBase + "/img/" + stubImages[0].file
}

// stubAnswer replies to whichever of Search's calls this is. Which one it is
// can be read off the generator, exactly as a real endpoint would.
func stubAnswer(base string, q map[string][]string) any {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	// A search nobody could match answers with nothing, so the "no results"
	// path is reachable from a test.
	if strings.Contains(strings.ToLower(get("gsrsearch")), "nothing") {
		return map[string]any{"query": map[string]any{"pages": []any{}}}
	}

	switch get("generator") {
	case "search":
		// One article with a lead image. The remaining fixtures arrive
		// through the second call, which is the one the plan was unsure was
		// worth making.
		img := stubImages[0]
		return map[string]any{"query": map[string]any{"pages": []any{map[string]any{
			"index": 1, "title": "Stub Article",
			"original":  map[string]any{"source": base + "/img/" + img.file, "width": img.width, "height": img.height},
			"thumbnail": map[string]any{"source": base + "/img/" + img.file},
			"fullurl":   base + "/wiki/Stub_Article",
		}}}}
	case "images":
		pages := []any{}
		for _, img := range stubImages[1:] {
			pages = append(pages, map[string]any{
				"title": "File:" + img.file,
				"imageinfo": []any{map[string]any{
					"url":            base + "/img/" + img.file,
					"thumburl":       base + "/img/" + img.file,
					"descriptionurl": base + "/wiki/File:" + img.file,
					"mime":           "image/png",
					"width":          img.width,
					"height":         img.height,
					"extmetadata": map[string]any{
						"LicenseShortName": map[string]any{"value": img.licence},
						"Artist":           map[string]any{"value": "<a href=\"/x\">" + img.credit + "</a>"},
					},
				}},
			})
		}
		// An icon, so the filter has something to drop and a test can assert
		// that it did. Exactly the shape that made the real call doubtful:
		// small, SVG, and nothing to do with the place.
		pages = append(pages, map[string]any{
			"title": "File:Commons-logo.svg",
			"imageinfo": []any{map[string]any{
				"url": base + "/img/Commons-logo.svg", "mime": "image/svg+xml",
				"width": 48, "height": 48,
			}},
		})
		return map[string]any{"query": map[string]any{"pages": pages}}
	}

	// The batched licence lookup for lead images.
	pages := []any{}
	for _, name := range strings.Split(get("titles"), "|") {
		for _, img := range stubImages {
			if !strings.Contains(name, img.file) {
				continue
			}
			pages = append(pages, map[string]any{
				"title": "File:" + img.file,
				"imageinfo": []any{map[string]any{
					"descriptionurl": base + "/wiki/File:" + img.file,
					"extmetadata": map[string]any{
						"LicenseShortName": map[string]any{"value": img.licence},
						"Artist":           map[string]any{"value": img.credit},
					},
				}},
			})
		}
	}
	return map[string]any{"query": map[string]any{"pages": pages}}
}
