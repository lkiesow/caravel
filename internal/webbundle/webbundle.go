// Package webbundle turns the frontend's source tree into the files a
// production browser loads: one minified, content-hashed JavaScript bundle and
// one stylesheet, built in memory with esbuild when the server starts.
//
// Why at startup and not as a build step: the tree is already inside the
// binary (see embed.go), so bundling it on the way up keeps `go build`, the
// Dockerfile and `go install` exactly as they were, and there is no generated
// directory to forget to refresh. It takes a fraction of a second, once.
//
// Why at all: the source is 46 modules three import levels deep, and a browser
// only discovers each level after fetching the one above it. From far away
// that is a round trip per level before the app can run, on every load, since
// each module is revalidated. A bundle is one request, and a content-hashed
// name means the browser can keep it without asking again (see Stage 45).
//
// Dev mode does not come here: `make dev` serves the source straight off disk,
// which is what makes an edit visible on refresh.
package webbundle

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// EntryPoints are the source files a page loads by name. Everything else is
// reached from these by import (or url(), for the stylesheet).
var EntryPoints = []string{"/js/app.js", "/css/base.css"}

// namespace marks the paths the plugin below owns. esbuild reads from disk by
// default, and the embedded tree is not on disk.
const namespace = "webfs"

// File is one built output, ready to serve.
type File struct {
	Body        []byte
	ContentType string
}

// Bundle is the result of a build.
type Bundle struct {
	// Files maps a URL path ("/assets/app-AB12CD34.js") to its contents. The
	// source maps are in here too, next to what they describe.
	Files map[string]File
	// Entries maps an entry point's source URL ("/js/app.js") to the URL of
	// the file built from it -- what the shell has to point at instead.
	Entries map[string]string
	// Warnings are esbuild's, formatted. A clean tree has none; the test on
	// the real tree holds it to that.
	Warnings []string
}

// Build bundles whichever EntryPoints exist in fsys. A tree with none of them
// -- a test fixture, typically -- yields an empty Bundle and no error, so a
// server built over it simply serves the source as it always did.
func Build(fsys fs.FS) (*Bundle, error) {
	var entries []string
	for _, e := range EntryPoints {
		if _, err := fs.Stat(fsys, strings.TrimPrefix(e, "/")); err == nil {
			entries = append(entries, e)
		}
	}
	b := &Bundle{Files: map[string]File{}, Entries: map[string]string{}}
	if len(entries) == 0 {
		return b, nil
	}

	result := api.Build(api.BuildOptions{
		EntryPoints:       entries,
		Bundle:            true,
		Write:             false,
		Metafile:          true,
		Format:            api.FormatESModule,
		Target:            api.ES2022,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Charset:           api.CharsetUTF8,
		Sourcemap:         api.SourceMapLinked,
		// Outputs land under /assets/ with esbuild's content hash in the name,
		// which is what lets them be served immutable: a changed file is a
		// different URL, never different bytes at the same one.
		AbsWorkingDir: "/",
		Outdir:        "/",
		EntryNames:    "assets/[name]-[hash]",
		LogLevel:      api.LogLevelSilent,
		Plugins:       []api.Plugin{fsPlugin(fsys)},
	})
	for _, m := range result.Warnings {
		b.Warnings = append(b.Warnings, formatMessage(m))
	}
	if len(result.Errors) > 0 {
		msgs := make([]string, len(result.Errors))
		for i, m := range result.Errors {
			msgs[i] = formatMessage(m)
		}
		return nil, fmt.Errorf("bundling the frontend: %s", strings.Join(msgs, "; "))
	}

	for _, out := range result.OutputFiles {
		p := "/" + strings.TrimPrefix(out.Path, "/")
		b.Files[p] = File{Body: out.Contents, ContentType: contentType(p)}
	}

	// The metafile is the only place that says which output came from which
	// entry; the names alone would have to be guessed from [name].
	var meta struct {
		Outputs map[string]struct {
			EntryPoint string `json:"entryPoint"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(result.Metafile), &meta); err != nil {
		return nil, fmt.Errorf("reading the bundle metafile: %w", err)
	}
	for out, o := range meta.Outputs {
		if o.EntryPoint == "" {
			continue
		}
		b.Entries[strings.TrimPrefix(o.EntryPoint, namespace+":")] = "/" + strings.TrimPrefix(out, "/")
	}
	for _, e := range entries {
		if _, ok := b.Entries[e]; !ok {
			return nil, fmt.Errorf("bundling the frontend: no output for %s", e)
		}
	}
	return b, nil
}

// fsPlugin resolves and loads every path from fsys.
//
// Relative specifiers are joined to the importer's directory. Absolute ones --
// "/fonts/inter-400.woff2" in a url(), the MapLibre import() -- are URLs the
// browser fetches on its own, so they are left exactly as written. Nothing in
// web/ uses a bare specifier, and the plugin refuses one rather than guessing
// where it should come from.
func fsPlugin(fsys fs.FS) api.Plugin {
	return api.Plugin{
		Name: "webfs",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				switch {
				case args.Kind == api.ResolveEntryPoint:
					return api.OnResolveResult{Path: args.Path, Namespace: namespace}, nil
				case strings.HasPrefix(args.Path, "/"):
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				case strings.HasPrefix(args.Path, "./"), strings.HasPrefix(args.Path, "../"):
					return api.OnResolveResult{Path: path.Join(path.Dir(args.Importer), args.Path), Namespace: namespace}, nil
				case strings.HasPrefix(args.Path, "data:"):
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf("%q is neither relative nor absolute; web/ has no package imports", args.Path)
			})
			build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: namespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := fs.ReadFile(fsys, strings.TrimPrefix(args.Path, "/"))
				if err != nil {
					return api.OnLoadResult{}, err
				}
				contents := string(data)
				loader := api.LoaderJS
				if path.Ext(args.Path) == ".css" {
					loader = api.LoaderCSS
				}
				return api.OnLoadResult{Contents: &contents, Loader: loader}, nil
			})
		},
	}
}

func contentType(p string) string {
	switch path.Ext(p) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".map":
		return "application/json"
	}
	return "application/octet-stream"
}

func formatMessage(m api.Message) string {
	if m.Location == nil {
		return m.Text
	}
	return fmt.Sprintf("%s:%d:%d: %s", m.Location.File, m.Location.Line, m.Location.Column, m.Text)
}
