package webbundle

import (
	"strings"
	"testing"
	"testing/fstest"

	webassets "caravel"
)

// The real tree has to bundle, cleanly. This is the test that stands between a
// broken import and a release: the server falls back to unbundled serving
// when the build fails at startup, which keeps the app up but would quietly
// give back everything Stage 45 bought.
func TestRealTreeBundles(t *testing.T) {
	b, err := Build(webassets.FS(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Warnings) > 0 {
		t.Errorf("bundling the real tree warned:\n%s", strings.Join(b.Warnings, "\n"))
	}
	for _, e := range EntryPoints {
		out, ok := b.Entries[e]
		if !ok {
			t.Fatalf("no output for %s", e)
		}
		f, ok := b.Files[out]
		if !ok || len(f.Body) == 0 {
			t.Fatalf("%s -> %s, which is not among the files", e, out)
		}
		if !strings.HasPrefix(out, "/assets/") {
			t.Errorf("%s built to %s, want it under /assets/", e, out)
		}
		if _, ok := b.Files[out+".map"]; !ok {
			t.Errorf("%s has no source map", out)
		}
	}

	js := string(b.Files[b.Entries["/js/app.js"]].Body)
	// MapLibre stays a separate, lazily loaded file: it is already minified,
	// and its worker URL is derived from its own location. Its import goes
	// through assetURL with an absolute path; a relative one would resolve
	// against /assets/ and miss.
	if !strings.Contains(js, `"/js/vendor/maplibre/maplibre-gl.mjs"`) {
		t.Error("the MapLibre path is not in the bundle as an absolute URL")
	}
	// Two entries and their two maps, and nothing else: no split chunk, which
	// is what a bundled-in MapLibre (or any second dynamic import) would add.
	if len(b.Files) != 4 {
		names := make([]string, 0, len(b.Files))
		for n := range b.Files {
			names = append(names, n)
		}
		t.Errorf("built %d files, want 4: %v", len(b.Files), names)
	}
	css := string(b.Files[b.Entries["/css/base.css"]].Body)
	if !strings.Contains(css, "/fonts/inter-400.woff2") {
		t.Error("the font url() was rewritten or dropped")
	}
}

func TestBundleFollowsContent(t *testing.T) {
	tree := func(src string) fstest.MapFS {
		return fstest.MapFS{
			"js/app.js":   {Data: []byte(`import { v } from "./lib/v.js"; console.log(v);`)},
			"js/lib/v.js": {Data: []byte(src)},
		}
	}
	a, err := Build(tree(`export const v = 1;`), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(tree(`export const v = 2;`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Entries["/js/app.js"] == b.Entries["/js/app.js"] {
		t.Errorf("a changed import kept the bundle name %s; the hash must follow the content", a.Entries["/js/app.js"])
	}
	again, _ := Build(tree(`export const v = 1;`), nil)
	if again.Entries["/js/app.js"] != a.Entries["/js/app.js"] {
		t.Error("the same tree built to two different names")
	}
	if _, ok := a.Entries["/css/base.css"]; ok {
		t.Error("an entry that is not in the tree was reported as built")
	}
}

func TestBuildWithNoEntriesIsEmpty(t *testing.T) {
	b, err := Build(fstest.MapFS{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Files) != 0 || len(b.Entries) != 0 {
		t.Errorf("empty tree built %d files", len(b.Files))
	}
}

func TestBuildReportsABrokenImport(t *testing.T) {
	_, err := Build(fstest.MapFS{"js/app.js": {Data: []byte(`import "./missing.js";`)}}, nil)
	if err == nil {
		t.Fatal("a missing import bundled without error")
	}
}

// The URL table reaches both places it is meant to: url()s in the stylesheet
// are rewritten at build time, and assetURL() in the code looks paths up in
// the same table at runtime, passing unknown ones through.
func TestBuildAppliesTheURLTable(t *testing.T) {
	fsys := fstest.MapFS{
		"js/app.js":       {Data: []byte(`import { assetURL } from "./asset-url.js"; console.log(assetURL("/locales/en.json"), assetURL("/nope.json"));`)},
		"js/asset-url.js": {Data: []byte(`export function assetURL(path) { return path; }`)},
		"css/base.css":    {Data: []byte(`@font-face { font-family: X; src: url("/fonts/x.woff2"); } a { mask: url("/brand/other.svg"); }`)},
	}
	urls := map[string]string{
		"/locales/en.json": "/v/aaaa/locales/en.json",
		"/fonts/x.woff2":   "/v/bbbb/fonts/x.woff2",
	}
	b, err := Build(fsys, urls)
	if err != nil {
		t.Fatal(err)
	}
	css := string(b.Files[b.Entries["/css/base.css"]].Body)
	if !strings.Contains(css, "/v/bbbb/fonts/x.woff2") {
		t.Errorf("font url() not versioned: %s", css)
	}
	if !strings.Contains(css, "/brand/other.svg") {
		t.Errorf("an absolute url() with no version was not left as written: %s", css)
	}
	js := string(b.Files[b.Entries["/js/app.js"]].Body)
	if !strings.Contains(js, `"/v/aaaa/locales/en.json"`) {
		t.Errorf("the generated asset-url module does not carry the table: %s", js)
	}

	// The table is part of the bundle's content, so a new version of a file it
	// names gives the bundle a new name too -- an old bundle must never point
	// at a URL that is now served revalidating.
	urls["/locales/en.json"] = "/v/cccc/locales/en.json"
	b2, err := Build(fsys, urls)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Entries["/js/app.js"] == b.Entries["/js/app.js"] {
		t.Error("a changed URL table kept the bundle name")
	}
}
