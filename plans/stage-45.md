# Stage 45: bundled, minified, immutable frontend

## Context

From Japan against the European server (~250 ms round trip) the app is slow to
come up, and sometimes a deploy still needs Shift+F5. Two causes, both in how
the frontend is delivered rather than in its size:

- **Round trips.** `index.html` loads `/js/app.js`, which statically imports 46
  modules three levels deep; the browser discovers each level only after
  fetching the previous one. MapLibre is a further dynamic import, `i18n.js`
  fetches `/locales/xx.json` after that, and `map-view` fetches a style JSON.
- **Revalidation.** Since Stage 23 every asset is `Cache-Control: no-cache` +
  ETag and the service worker is network-first for code, so even an unchanged,
  fully cached reload asks the server about every file. Roughly 6–7 sequential
  round trips (≈1.5–2 s) before the app runs, with nothing changed.

Measured in scratch: esbuild bundles + minifies our 60 files (682 KB raw /
215 KB gzip) to one 233 KB / **69 KB gzip** file in 0.4 s.

The fix: bundle the app into content-hashed files served `immutable`, give every
other file on the load path a versioned URL, and leave only `index.html` (and
`sw.js`) to revalidate. A warm reload becomes one round trip; a deploy can no
longer mix old and new code because a hashed URL's bytes never change, which is
the structural fix for the hard-reload problem.

### Decisions already taken

- **esbuild via its Go API** (`github.com/evanw/esbuild/pkg/api`), pinned in
  `go.mod`. No Node, no npm, no `node_modules` in the build.
- **Bundled in-process at server startup**, from the same `fs.FS` the server
  already serves (`webassets.FS()`), into memory. `go build`, the Dockerfile,
  `embed.go`, CI's build steps and `go install` are unchanged. A Go test
  bundles the real tree so `make ci` catches a broken bundle.
- **Dev stays unbundled.** When `NoCache` is set (`CARAVEL_WEB_DIR`, i.e.
  `make dev`) nothing is bundled and files are served live from disk exactly
  as today. No new environment variable.
- **A bundle failure at startup is not fatal**: logged loudly, and the server
  serves the unbundled tree as it does today. The test is what stops it
  reaching a release.
- **MapLibre is not re-bundled.** It is already minified (the trial gained
  ~nothing) and its three filenames are load-bearing (vendor README: the
  worker URL is derived from `import.meta.url`). It gets a versioned URL
  instead.
- **No code splitting.** The only dynamic import is MapLibre, which stays
  external, so the app is one JS file and one CSS file.

## 1. Bundle the app and serve it immutable

- New package `internal/webbundle`: `Build(fsys fs.FS) (*Bundle, error)`.
  esbuild `api.Build` with `Write: false`, entry points `js/app.js` and
  `css/base.css`, `Bundle`, `MinifyWhitespace/Identifiers/Syntax`,
  `Format: ESM`, `Target: ES2022`, `EntryNames: "assets/[name]-[hash]"`,
  `Sourcemap: SourceMapLinked`. A small plugin resolves relative paths and
  loads file contents from `fsys` (namespace `webfs`), because the embedded
  tree is not on disk. Absolute URLs (`/fonts/…`, `/brand/…` in CSS) are
  external and left alone in this milestone. Result: published path → bytes +
  content type, plus the hashed URLs of the two entries.
- The MapLibre `import("../vendor/maplibre/maplibre-gl.mjs")` in
  `web/js/components/map-view.js` must not break: once bundled the code runs
  from `/assets/`, so a relative path would resolve wrongly. Mark it external
  and make it absolute (`/js/vendor/maplibre/maplibre-gl.mjs`) in the source,
  which works identically in dev.
- `internal/httpapi`: `Options` gains the bundle (built in `NewServer` next to
  `buildAssetETags`, skipped when `NoCache`, startup time logged).
  `serveStatic` (`internal/httpapi/staticassets.go`) answers `/assets/*` from
  memory with `Cache-Control: public, max-age=31536000, immutable`; an unknown
  `/assets/` path is a 404 via the existing `isAssetRequest` (add `/assets/` to
  `assetDirs`). `handleShell` rewrites `src="/js/app.js"` and
  `href="/css/base.css"` to the hashed URLs, the same plain-string substitution
  it already does for the origin. Its ETag is computed from the output, so it
  follows the bundle automatically.
- The unbundled `/js/…` files stay embedded and served (stale tabs, fallback).
- Tests (`staticassets_test.go` style, plus `internal/webbundle`):
  the real `web/` tree bundles without error and without warnings; the shell
  references the hashed URLs; `/assets/…` is immutable with the right content
  type and the old ETag behaviour is unchanged elsewhere; dev mode does not
  bundle; and a guard test that the real `index.html` still contains the two
  exact strings being substituted (like
  `TestRealServiceWorkerCarriesThePlaceholder`).

**Done.** `internal/webbundle.Build` bundles `js/app.js` and `css/base.css`
from the served `fs.FS` through a resolve/load plugin, with no Node involved.
It builds whichever entries exist, so the test servers' empty `MapFS` yields
nothing and costs nothing. Absolute URLs and `data:` stay external, and a bare
specifier is refused. The output is one JS file, one CSS file and their two
maps under `/assets/[name]-[hash]`. `NewServer` builds it next to the ETags
when not `NoCache`, logs the duration (77 ms on the real tree) and falls back
to the source on error. `serveStatic` looks built files up *before* the
missing-file 404, because they are in memory, not in the tree; that ordering
was a failing test first. `handleShell` swaps `"/js/app.js"` and
`"/css/base.css"`, quotes included, so a mention in an HTML comment is left
alone. The MapLibre `import()` is now absolute in the source. Costs: the binary
grows 5.9 MB (31.5 → 37.4 MB unstripped).

Verified: `make ci` green. New tests cover the real tree bundling with no
warnings into exactly four files (so no chunk and MapLibre not inlined), the
hash following content and staying stable for the same content, a broken import
erroring, the shell pointing at the hashed URLs, `/assets/` being `immutable`
with the right type and a map, an unknown `/assets/` file 404ing, the source
still served `no-cache`, dev not bundling, and `index.html` keeping the spelling
the substitution matches. Firefox could not emulate latency over CDP, so a
small proxy added 250 ms to every request in front of a bundled build and a
`main` build, both on the dev database. Loading the Iceland trip's Map tab:

| | `map-view` attached | map `data-ready` |
|---|---|---|
| `main`, cold / warm | 4.3 s / 4.5 s, 4.3 s | 7.9 s / 7.6 s, 7.3 s |
| bundled, cold / warm | 2.1 s / 1.4 s, 1.4 s | 5.5 s / 4.4 s, 4.3 s |

According to the server log, `main` fetched 66 code files per load (198 over
three), while the bundle reached the server once over three loads. What still
reached it on every load is M2's list: the MapLibre files, the locale, the
style JSON, the sprite and the fonts. No console errors. Deploy simulation: a
page under service-worker control on the old bundle, a rebuild with a marker
line, a restart, then plain `page.reload()`. That loaded `app-ETBZBXMR.js` with
the marker, and the unchanged stylesheet kept its name.

## 2. Version every other file on the load path

What still revalidates after M1: MapLibre (3 files + its CSS in the map's
shadow root), the locale JSON, the two map style JSONs, the fonts (CSS
`url()` + the preload in `index.html`), the icon sprite, `/brand/mark.svg`.

- **Versioned URL scheme `/v/<hash>/<path>`**, where `<hash>` is a hash of the
  files in that path's directory (non-recursive, built from the existing
  `assetETags` map). One hash per directory is what lets MapLibre's relative
  sibling import and its `import.meta.url`-derived worker URL resolve inside the
  same versioned prefix untouched.
- Serving: `/v/<h>/<path>` serves `<path>`; when `<h>` matches the current
  directory hash it is `immutable`; when it doesn't (a stale tab after a deploy)
  it serves the current file with `no-cache` rather than 404ing, so an old tab
  that lazily opens the map still gets a map.
- **JS side:** a new `web/js/asset-url.js` exporting `assetURL(path)`, the
  identity function on disk (dev). The bundle plugin substitutes a generated
  module for it that maps known paths to their versioned URLs. Used by the
  MapLibre `import()` and its stylesheet link in `map-view.js`, the locale fetch
  in `i18n.js`, and the sprite URL in `icon.js`.
- **CSS side:** the plugin's `onResolve` rewrites `url("/fonts/…")` and
  `url("/brand/…")` to their versioned URLs. `handleShell` does the same for the
  font `preload` in `index.html`.
- **Map styles:** the default `style_url`/`dark_style_url` the server hands out
  in the map config become versioned; an operator-configured URL is untouched.
- Favicons and the manifest keep stable URLs (they are not on the load path).
- Tests: version hash follows directory content; matching hash is immutable,
  mismatched is `no-cache` with current bytes; the generated `asset-url` module
  covers each path the source passes to `assetURL` (grep the source in the test
  so a new call site cannot silently miss the map).

## 3. Service worker and testing the production path

- `web/sw.js`: `/assets/` and `/v/` requests become **cache-first** (their
  bytes cannot change). The shell and `sw.js` itself stay network-first. The
  hashed bundle URLs join `SHELL_URLS` through a second server-substituted
  placeholder inside a string literal (same mechanism and `check_js.sh`
  constraint as `__CARAVEL_BUILD__`), so offline works after the first visit.
  `activate` already drops old caches.
- **The UI suite runs the bundled build.** `scripts/with_server.sh` (and
  `gen_screenshots.sh`) already `go build` a binary from the working tree, so
  dropping `CARAVEL_WEB_DIR=web` there makes it embed and bundle the working
  tree, without testing a stale build. Fix whatever surfaces, then run `make
  test-ui` and `make check-contrast` green.
- Docs and notes: the "no bundler" claims in `package.json` and
  `web/js/vendor/maplibre/README.md`; `CLAUDE.md` (dev is unbundled, `make run`
  or the UI suite shows the bundled path, `/v/` URLs and `assetURL`); the
  operator docs under `docs/` if they mention caching/proxies (a proxy must not
  strip `Cache-Control`). `make docs` if anything under `docs/` changes.
- `plans/todo.md`: add precompressing bundles at startup (gzip now happens per
  request in `middleware.Compress(5)`) and brotli; note that API round trips
  are the remaining latency from far away.

## Build order

1 → 2 → 3. M1 alone already removes the module waterfall and most
revalidation; M2 removes the rest; M3 makes the worker exploit it and moves
the test suite onto the production path.

## Workflow

Per milestone: implement → `make ci` green + a Playwright pass against a
**bundled** server (the built binary without `CARAVEL_WEB_DIR`, or the UI
suite after M3) proving behaviour changed → "**Done.**" paragraph in
`plans/stage-45.md` and `plans/todo.md` updated both ways → one commit (no
attribution trailer) → `make dev` running → stop and wait for review.

## Verification

- `make ci` (includes the new bundle tests on the real tree).
- `make test-postgres` is not needed (no DB changes).
- Playwright against a bundled server, by assertion rather than screenshot:
  - Cold load: `browser_network_requests` shows one `/assets/app-*.js` and
    one `/assets/base-*.css`, no `/js/*.js` module requests.
  - Warm reload (after M2/M3): the only requests that reach the server are the
    shell, `sw.js` and `/api/…`. Every `/assets/` and `/v/` response is from
    cache.
  - Headers: `/assets/…` and current `/v/…` carry `immutable`; a mismatched
    `/v/` hash gets `no-cache`.
  - Latency: CDP `Network.emulateNetworkConditions` with 250 ms latency and
    time-to-`data-ready` on the Map tab, before (current `main`) vs after,
    recorded in the Done paragraph.
  - Deploy simulation: load the app, rebuild with a changed JS string, restart,
    plain F5. The new string appears without Shift+F5.
  - Map tab works (MapLibre worker loads from `/v/…`), German locale loads,
    fonts and icons render, offline reload works after the SW precache (M3).
- `make test-ui` and `make check-contrast` green on the bundled build (M3).
