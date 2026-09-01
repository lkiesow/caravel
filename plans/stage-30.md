# Stage 30 — Vector maps, in your language and your light

## Context

The map is the last part of Caravel that cannot follow the reader. Raster
tiles are drawn before anyone asks, so `CARAVEL_TILE_URL` can pick a provider
whose labels are Latin script or one language for the whole instance, but
never each user's own — the **(soon)** entry in `plans/todo.md:381-397`. And
the map is the last surface that cannot follow the app's light/dark choice
either: `web/js/components/leaflet-map.js` hardcodes three category colours as
hex and has no theme signal at all, so a dark app frames a bright white map.

This stage replaces Leaflet 1.9.4 with MapLibre GL JS against OpenFreeMap
(keyless, unmodified OpenMapTiles schema, carrying both `name:en` and
`name:de` — exactly the two locales `web/js/i18n.js` supports), which makes
both problems solvable at once: labels come from a style we own, and the style
can be swapped for a dark one.

The appearance half goes one step further than "follow the app". A map is
often the one thing on screen you look *at* rather than read, and a light map
under a dark UI is a legitimate preference. So the map gets its own four-state
setting — **follow app / day-night auto / light / dark** — independent of
`web/js/theme.js`'s three.

Decisions taken up front (see also *Out of scope*):

- **Provider**: OpenFreeMap's public instance as the keyless default,
  overridable by the operator. Same stance as today's OSM raster tiles.
- **Styles**: vendored in-repo, not fetched from a hosted URL — full control
  over the label `text-field`, no runtime dependency on a document we don't
  own, and cacheable by `web/sw.js`.
- **Full replacement**: `web/js/vendor/leaflet/` is deleted. One map engine.
- **Day/night auto** keys off sunrise/sunset at the *device's* coordinates,
  obtained **without a new permission prompt** — see Milestone 5.

Empirically verified during planning (headless Firefox 153 + Chromium 151 via
this repo's own Playwright, real shadow root, real OpenFreeMap style), so the
plan does not rest on recollection:

- maplibre-gl 6.6.0 is **ESM-only** and loads via a bare `import()` with no
  bundler — but ships **three** `.mjs` files whose filenames are load-bearing
  (the entry derives its worker URL from `import.meta.url`).
- The whole thing works **inside a shadow root**: `load` fires, the canvas
  sizes from the container, and `maplibre-gl.css` injected as a `<link>`
  styles the popup. Its `url()`s are inline data URIs — no image assets.
- Attribution HTML survives unescaped (`DOM.sanitize` strips only `<script>`,
  `on*` and `javascript:`/`data:` URLs), so the `map.spec.js` attribution
  claim holds.
- OpenFreeMap serves `/styles/positron` and `/styles/dark` (**not**
  `dark-matter` — that 404s), both already fully OpenFreeMap-pointed for
  sprite, glyphs and the `openmaptiles` TileJSON. positron has 55 layers, 19
  carrying `text-field`.
- A blocked tile source does **not** stall `load` (`vector_tile_source.ts`
  marks a failed source loaded on purpose), so the UI suite's
  `blockExternalRequests` yields a tile-less but geometrically correct map —
  which is what every assertion actually reads.

---

## 1. Vendor MapLibre and the two styles

Add, at a pinned 6.6.0, with a header note recording version, licence, and the
fact that the three filenames must not be renamed:

```
web/js/vendor/maplibre/maplibre-gl.mjs          (~568 KB, entry)
web/js/vendor/maplibre/maplibre-gl-shared.mjs   (~490 KB)
web/js/vendor/maplibre/maplibre-gl-worker.mjs   (~19 KB)
web/js/vendor/maplibre/maplibre-gl.css          (~83 KB)
web/js/vendor/map-styles/positron.json
web/js/vendor/map-styles/dark.json
```

The two style JSONs are `tiles.openfreemap.org/styles/{positron,dark}`
verbatim. Total ~1.16 MB vs Leaflet's 440 KB — the existing lazy `import()`
inside `render()` keeps it off every non-map page, which now matters more.

Teach the pipeline about `.mjs`:

- `web/sw.js` — `isCodeRequest()` matches `.js|.css|.json` only, so a `.mjs`
  would fall into `staleWhileRevalidate` and reproduce the "new build takes
  two reloads" bug the comment above it warns about. Add `.mjs`.
- `scripts/check_js.sh` walks `web/js -name '*.js'`, so vendored `.mjs` goes
  unchecked. Acceptable for vendored, unmodified code — note it rather than
  work around it.
- Nothing needed in `embed.go` or `internal/httpapi/staticassets.go`:
  `assetDirs` already covers `/js/`, and Go's `mime` returns
  `text/javascript` for `.mjs`.

No component change; Leaflet still runs the app after this milestone.

**Verify.** On the dev server: all four files served with the right content
types; `await import('/js/vendor/maplibre/maplibre-gl.mjs')` resolves and a
throwaway `Map` against the vendored positron style paints tiles and fires
`load` (this also proves the module worker loads same-origin). **Also confirm
headless Firefox on the CI runner has WebGL2** — v6 requires it and it is the
one unknown that could sink Milestone 2 (`.github/workflows/ci.yml`, the
`test-ui` job).

**Done.** maplibre-gl 6.6.0 (BSD-3-Clause) vendored as the four planned files
plus `LICENSE.txt`, byte-identical to the published npm tarball — nothing
rewritten, including the trailing `sourceMappingURL` comments, which 404 in
devtools exactly as `leaflet.esm.js`'s already do. `positron.json` (55 layers,
19 with `text-field`) and `dark.json` (**47** layers, **13** with `text-field`
— the plan's "19" was positron's count, not both) fetched verbatim from
OpenFreeMap. Two `README.md` files record version, licence, sha256 for every
file, the re-vendoring commands, and the reasons behind the two choices a
future reader would otherwise second-guess. `/styles/dark-matter` was confirmed
to 404 while `/styles/dark` serves; both styles reference only
`tiles.openfreemap.org`, so nothing further has to be vendored for them to
render. `web/sw.js`'s `isCodeRequest()` learned `.mjs` — note `.js` does *not*
cover it, since `endsWith(".js")` reads three characters and those are `mjs`.
`.json` was already covered, so the style documents were network-first
already.

Verified with `make ci` green and a Playwright probe against a live `make dev`
(script not committed — it is a one-off, and Milestone 2 replaces it with real
specs). All six files serve with the right content types
(`text/javascript; charset=utf-8` for `.mjs`, confirming Go's `mime` needs no
help). In **both headless Firefox and headless Chromium**: `webgl2` present;
`import()` of the entry from the app's own origin resolves to version 6.6.0
with `Map`/`Marker`/`Popup`/`LngLat`/`LngLatBounds`/`AttributionControl`
exported; `maplibre-gl-shared.mjs` **and**
`/js/vendor/maplibre/maplibre-gl-worker.mjs` both fetched same-origin, which
is the `import.meta.url` derivation working from the vendored location; `load`
fires, 27 tiles are requested, `areTilesLoaded()` is true and the canvas is
painted 640×400. Inside a real shadow root the canvas and the attribution
control land in the shadow root, and the vendored stylesheet applies there
(`.maplibregl-canvas` computes `position: absolute`, `.maplibregl-map`
`overflow: hidden`) — the `<link>`-into-shadow-root pattern carries over from
Leaflet unchanged. A Copenhagen view at zoom 11 rendered 23 symbol features of
real cartography.

Two things the probe settled that the plan had only predicted. The default
attribution really does credit MapLibre itself, so Milestone 2 must pass an
`attributionControl` options object; and our `customAttribution` came through
**unescaped** and joined with OpenFreeMap's own TileJSON credit, so the
`map.spec.js` attribution claim survives. The rendered label read
"Copenhagen" while the feature's own `name` is "København" — the stock
`text-field` coalesces to `name_en` for every reader on earth, which is
Milestone 4's whole subject, now demonstrated rather than assumed.

**Not verified, and deliberately carried into Milestone 2:** WebGL2 on the CI
runner. Both browsers have it on this machine, but GitHub's `ubuntu-latest`
cannot be probed from here — the first CI run of Milestone 2 is the test, and
if it fails there, that milestone is blocked rather than merely buggy.

## 2. Swap the component

One commit, all three modes of `<leaflet-map>` (trip-wide, single-marker,
pick), plus `rm -r web/js/vendor/leaflet/` — nothing else imports it.

In `web/js/components/leaflet-map.js`:

- **`buildStyle(tiles)`** — a new helper returning a style object. Vector
  positron for now; Milestone 5 adds the light/dark choice and Milestone 6 the
  raster path.
- **Construction** — MapLibre needs `center`/`zoom` up front (Leaflet
  tolerated a view-less map), so pass the existing world view
  `{center:[0,20], zoom:2}`. Attribution: `attributionControl: {compact: true,
  customAttribution: tiles.tile_attribution}` — the default credits MapLibre
  itself.
- **Handlers, explicitly** — `dragRotate:false, touchPitch:false,
  pitchWithRotate:false, rollEnabled:false`, then
  `touchZoomRotate.disableRotation()` and `keyboard.disableRotation()`.
  Rotation and pitch are new capabilities Leaflet never had; north stays up.
- **Markers** — the three `L.divIcon` builders become plain
  `document.createElement` spans passed as `Marker({element, anchor:'center'})`.
  Strictly simpler: the whole comment about `<img src>` resolving against the
  SPA URL evaporates. `keyboard`/`title`/`alt` marker options have no
  equivalent — set `tabIndex`, `role="button"` and `aria-label`
  (`t("map.pickMarkerLabel")`) on the element by hand.
- **Popups** — `marker.setPopup(new Popup({offset:12, focusAfterOpen:false})
  .setHTML(html))`. `setHTML` does not sanitize, so the existing
  `escapeHtml`/`escapeAttr` remain the only escaping — contract unchanged.
  MapLibre adds a close button; that is fine.
- **Camera** — `setView` → `jumpTo`; `fitBounds` padding is a number, not
  `[x,y]`; degenerate single-point bounds with `maxZoom:14` land exactly on 14
  (verified), so no special case. `fitBounds` now returns *fractional* zoom —
  nothing asserts an integer, and every wheel test measures a delta.
  `setZoomAround` → `easeTo({zoom, around: map.unproject(...), duration: 150})`
  (**≤200 ms**: `map.spec.js:336` waits 350 ms). `wrapLatLng` →
  `new LngLat(lng, lat).wrap()`. **Longitude first, everywhere.**
- **Gestures** — `cooperativeGestures: true` **plus** `scrollZoom: false`:
  - cooperative gestures give the touch semantics (`minTouches = 2`, plus
    `touch-action: pan-x pan-y` on the canvas container). This *replaces*
    `dragging: !isCoarsePointer()`, and is the only supported way to keep
    two-finger pan while releasing one finger to the page. `dragPan:false`
    would kill two-finger pan outright — `map.gesture.spec.js:155` asserts it.
  - the hand-written `zoomByWheel()` stays exactly as written. MapLibre's
    bypass key is `metaKey` only on a Mac UA, which would break the
    Meta-modifier assertion at `map.spec.js:365` on Linux; and the accumulator
    exists because of a reproducible-for-the-reporter failure that is not
    worth re-opening.
  - hide MapLibre's own overlay (`.maplibregl-cooperative-gesture-screen`,
    which is `aria-hidden` and UA-keyed) in the shadow CSS. Keep our
    `.gesture-hint` — it is the a11y contract both gesture specs assert on.
    Drive its touch branch from `map.on('cooperativegestureprevented')`,
    which drops the `isCoarsePointer()` dependency; keep the wheel branch in
    our capture listener, since `scrollZoom:false` emits no `wheel_zoom`.
- **`data-ready`** moves to `map.on('load')` — style loaded *and* first frame
  rendered, the honest analogue and strictly better than today's
  "constructed". **Guard it**: if construction throws (no WebGL2), still set
  `data-ready` and show a fallback message, or every route sweep in the UI
  suite hangs on a 15 s timeout.
- **Lifecycle** — `this._map?.remove()` in `render()` **and** a new
  `disconnectedCallback`. This is the biggest new correctness obligation:
  MapLibre holds a WebGL context and a worker, browsers cap simultaneous
  contexts (~16), and `render()` rebuilds on every attribute change.

Test port in the same commit — mostly mechanical
(`.leaflet-marker-icon` → `.maplibregl-marker`, `.leaflet-popup-content` →
`.maplibregl-popup-content`, `.leaflet-tile-pane`/`.leaflet-container` →
`.maplibregl-canvas` as the "shadow root was not re-rendered" sentinel, and
`[class*="leaflet-"]` → `[class*="maplibregl-"]` in `routes.spec.js:131,250`
and `map.spec.js:1436`). Five assertions change *meaning*:

| `map.spec.js` | Was | Becomes |
|---|---|---|
| :180 | `_map.dragging.enabled() === false` on coarse | `_map.cooperativeGestures.isEnabled() === true` |
| :258 | drag panning on a fine pointer | `_map.dragPan.isEnabled()` — now unconditionally true, which is correct |
| :441 | `scrollWheelZoom.enabled()` | `scrollZoom.isEnabled()`, same `false`, same reasoning |
| :205-248 | tile hosts from `img.leaflet-tile` | **re-designed** — no tile `<img>` exists. Assert both `_map.getStyle().sources` host *and* at least one outbound request to that host via `page.on('request')`. Closer to the actual claim, and survives raster and vector alike |
| :1129 | `_hereCircle.getRadius()` | Milestone 3 |

The attribution half of :205-248 survives near-verbatim (selector
`.maplibregl-ctrl-attrib-inner`); assert only the `customAttribution` half in
the suite, since OpenFreeMap's own TileJSON credit never resolves under
`blockExternalRequests`. Also update the *rationale* comments on the
`[class*=...]` exclusions: MapLibre parks nothing at ±1.8 M px, but the
exclusions are still wanted for the attribution `<summary>`, the popup close
button and the zoom controls. And `base.css:1317-1335`'s comment names
Leaflet's `z-index:1000` controls — the rule stays, the comment changes.

**One thing to verify before committing**, not assume: `clickPickerAt`
(`map.spec.js:574-589`) dispatches synthetic `mousedown/mouseup/click` with
`clientX/Y`. MapLibre's click comes from its own handler chain with a click
tolerance, so this may need to target `.maplibregl-canvas` and interleave a
`mousemove`. Six tests depend on it, including the editor save round-trip.

**Verify.** `make ci` plus `make test-ui` green **including the Chromium
gesture project** — run `map.gesture.spec.js` first, not last: real fingers,
one scrolls the page, two pan, hint appears and fades. Then by hand on the dev
server: trip map with markers and both popup links, the location-view embed,
and the editor picker (click / drag / type / clear).

**Done.** All three modes swapped in one commit and `web/js/vendor/leaflet/`
deleted. Everything the plan predicted about the API translation held; what
follows is what it did *not* predict, since that is the part worth reading.

**One deviation, taken deliberately.** The plan had `buildStyle()` return
vector positron unconditionally and left the operator's `CARAVEL_TILE_URL` to
Milestone 6. That would have shipped four milestones during which a documented
configuration option was silently ignored, and it would have made this
milestone's tile-conformance test assert something untrue. So the raster path
landed here instead: `rasterStyle()` synthesises a style from the tile fields,
and `/api/httpapi/map.go` grew one derived field, `style_url`, empty when the
operator has set a non-default tile URL. The rule lives on the server on
purpose — the browser inferring operator intent by string-comparing against its
own copy of the default would put the default in two languages. Milestone 6
extends this rather than rewriting it; its own setting (`CARAVEL_MAP_STYLE`)
and the docs are still its work. This also meant the raster path got exercised
for real by accident: the first spec run went against a dev server still
running the pre-change binary, so every map came up raster, and worked.

**Four things measured rather than assumed**, all before writing the component,
and one of them would have been a silent failure:

- A custom element handed to `Marker` *does* get `maplibregl-marker` (plus an
  anchor class), so the test selectors survive; inline styles survive too, with
  `transform` applied separately.
- Popups reach into the shadow root, `[data-item-id]` is queryable, and
  `customAttribution` renders unescaped.
- Degenerate `fitBounds` lands on exactly `maxZoom`, so no special case.
- **Synthetic `mousedown`/`mouseup`/`click` on `#map` produce zero map clicks.**
  MapLibre listens on the canvas, not the container it is handed. `clickPickerAt`
  had to be retargeted to `.maplibregl-canvas`; six tests depend on it, and the
  failure mode is a click that simply never happens.

**Three behavioural differences found by the suite**, each fixed on its merits
rather than papered over:

- **MapLibre will not pan vertically when the world already fits the
  viewport** — and at the trip map's `fitBounds` zoom it exactly does (measured:
  world 643px, container 643px). This is correct, and better than Leaflet,
  which would drag the world off the top of the screen. But it made
  `map.gesture.spec.js`'s two-finger test assert something the library rightly
  refuses, *and* silently hollowed out the one-finger test, whose "the map must
  not pan" was purely vertical and could no longer fail. The two-finger drag is
  now sideways and asserts longitude; the one-finger drag is now diagonal, so
  the vertical half still scrolls the page and the horizontal half keeps the
  assertion real.
- **`fitBounds` returns fractional zoom**, so the wheel test's `after - before`
  came out `1.0000000000000004` where Leaflet's integer snap gave exactly 1.
  The delta is rounded now, with the reason stated.
- **The wheel helper's flat 350ms wait** became a race against the 150ms ease
  under a full parallel run. It waits for the camera to stop instead, which is
  both faster and not a race. The same fix was needed where a test recorded
  "the view the person chose" mid-ease.

**A pre-existing failure fixed in passing.** `map.spec.js`'s three-way Google
Maps link test was already red on `main`: Stage 29 Milestone 3 added an
OpenStreetMap link under the same `.location-view__maps-link` class, making a
Stage 29 Milestone 1 locator a strict-mode violation. Confirmed unrelated —
`location-view-page.js` is untouched by this milestone. The locator now names
the Google link by its `data-i18n` rather than by position.

Also here: `data-ready` moved to the map's own `load` event (style parsed and
first frame drawn, a stronger claim than "the object exists"); a
`disconnectedCallback` and a `destroyMap()` before the shadow root is
rebuilt, because a WebGL context and a worker are now at stake where Leaflet
leaked nothing that mattered; a `map.unavailable` string and a fallback that
still sets `data-ready`, so a browser without WebGL2 gets a sentence instead of
a 15s timeout on every route with a map; rotation and pitch explicitly
disabled; MapLibre's own `aria-hidden` gesture overlay hidden in favour of the
app's `role="status"` hint, now driven by the library's own
`cooperativegestureprevented` event rather than by a `matchMedia` guess. The
accuracy ring came along too rather than waiting for Milestone 3 — leaving a
visible regression standing for one commit was not worth the tidier split, so
Milestone 3 is now the overlay *lifecycle* rather than the ring itself.

Verified: `make ci` green; `make test-ui` at 204 passed with the only failures
being the three `todo.md:308-319` names verbatim (both distance-filter tests
and, intermittently, `itinerary-order.spec.js`'s move-an-entry test), each
confirmed to pass in isolation — the documented shared-seed flake, not this
work. `map.gesture.spec.js` green on real fingers, 5/5. By hand against a live
`make dev` with **real tiles and no request blocking**: the trip map draws 15
markers over 55 rendered label features, the location-view embed and both
editor pickers render, the attribution reads
"© OpenStreetMap contributors | OpenFreeMap © OpenMapTiles Data from
OpenStreetMap" — our configured credit joined with the provider's own — and
there are zero console errors across all four mounts.

## 3. Locate: the overlay lifecycle

*Narrowed by Milestone 2, which brought the accuracy ring forward rather than
leave a visible regression standing for a commit. What is left here is the part
that only matters once something re-creates the style.*

The ring is already a GeoJSON source plus `fill` and `line` layers, drawn by
`drawAccuracyRing()` and asserted from the geometry it produced. What is not
yet done is making it survive a style change: `setStyle()` destroys sources and
layers (markers and popups are DOM and survive), so the re-add has to be a
single `_applyOverlays()` bound to `style.load`, which Milestone 5 then relies
on. Verify by restyling with a position shown and confirming the ring comes
back, and that it stays metrically correct across three zoom levels.

The original reasoning, kept because it is the *why* behind the shape:
`L.circle` has no MapLibre equivalent — there is no metre-radius circle.
Render the accuracy ring as a GeoJSON `Polygon` approximating the circle in
metres (64 vertices, `lat + (r/111320)·cos θ`,
`lng + (r/(111320·cos lat))·sin θ`) as a `geojson` source plus `fill` and
`line` layers in the existing colours. Exact at every zoom, no expression
arithmetic.

This brings an obligation Leaflet did not have: **`setStyle()` destroys
sources and layers** (markers and popups are DOM and survive). So introduce a
single **`_applyOverlays()`** bound to `style.load` — Milestone 5 reuses it.

Re-point the assertions: `_hereMarker.getLatLng()` → `getLngLat()`;
`_hereCircle.getRadius()` → assert `_map.getSource('here-accuracy')` exists
plus a radius carried on the component, keeping the assertion's spirit ("a
2 km fix and a 5 m fix must not look identical").

**Verify.** The locate specs pass; by hand with a faked position at two
accuracies the ring is visibly two sizes and stays metrically correct across
three zoom levels.

**Done.** `applyOverlays()` is now the one place that builds everything the
*style* owns, from state the component holds (`_hereRingAt`, `_hereAccuracy`)
rather than from whatever the caller happened to pass. `render()` binds it to
the map's `style.load`, which fires for the first style and for every
replacement, so a restyle re-adds the ring without the code doing the restyle
needing to know the ring exists — which is exactly what Milestone 5 needs.
`showPosition()` now only records where and how accurate, and calls it. The
geometry is recomputed rather than cached on re-add, because a degree of
longitude is a different distance at every latitude, so the ring is a function
of where it is and not only of how big it is.

Two tests, and both were checked by breaking the code to make sure they can
fail — a regression test that has never been red is a guess:

- **"the accuracy ring survives the style being replaced"** drives `setStyle()`
  onto the *other* vendored style directly, since nothing in the app restyles a
  map until Milestone 5. Removing the `style.load` binding turns it red on "the
  source must be re-added after a restyle". It also pins two things that should
  never have been at risk but would be silent if they broke: markers are DOM
  and must survive untouched, and the camera must be preserved, because a
  restyle must not read as a navigation.
- **"the accuracy ring is the size it claims, at any zoom"** replaced a weaker
  test I had written first. That one projected the ring's geometry and asserted
  the on-screen size doubles per zoom level — which sounds like it proves the
  ring is anchored to the ground, but is true of *any* fixed geographic
  coordinates by definition of Web Mercator. It was testing the projection, not
  the code. The version that landed measures the **east-west** span on the
  ground, which is the half that can actually be wrong: `accuracyRing()` has to
  divide the longitude offset by cos(latitude) or the ring is an ellipse too
  narrow everywhere but the equator. Deleting that correction makes the ring
  measure 30.5m where 70m is expected, and the test says so. The north-south
  span the earlier locate test checks would look perfect either way. The
  zoom-scaling assertions are kept alongside it, honestly labelled as the
  "on the ground, not on the glass" half.

Verified: `make ci` green; `make test-ui` at 207 passed with only the two
documented distance-filter flakes (`todo.md:308-319`), both of which pass in
isolation. By hand against a live `make dev` with a faked 400m fix over
Reykjavik: the ring paints as a filled cyan disc with an outline, is visibly
*circular* at 64°N rather than an ellipse — the cos(latitude) correction doing
its job where it is most visible — and comes back intact after a live
`setStyle()` onto the dark cartography. Worth noting from that screenshot,
since it is Milestone 5's problem and not a defect here: on the dark map the
legend and the locate button are still light-themed, and the category dot
colours are unchanged. That is the marker-and-chrome work Milestone 5 already
plans.

## 4. Labels in the reader's language

The payoff the todo entry asks for. Patch the 19 `text-field` layers of each
vendored style (currently a `["case", ["has","name:nonlatin"], …]` shape) to:

```json
["coalesce", ["get", "name:<locale>"], ["get", "name:latin"], ["get", "name"]]
```

`<locale>` from `getLocale()` in `web/js/i18n.js` (`en`/`de`); the `/planet`
TileJSON advertises `name:de`, `name:en` and `name:latin` on every labelled
layer. The patch runs inside `buildStyle()`, so it applies to every style
build regardless of which one Milestone 5 picks.

Locale changes need no `setStyle`: `app.js` already re-renders the route on
`locale-changed`, which rebuilds the component. Confirm that holds for all
three mount sites rather than assuming it.

**Do not** ship `setRTLTextPlugin` — it is a separate ~200 KB JS+WASM download
MapLibre fetches from unpkg. The coalesce already renders Arabic and Hebrew
places in Latin script for en/de readers; unshaped RTL is only reachable for a
place with no `name:latin`. Record the trade in the code comment and in
`plans/todo.md`.

New i18n keys, if any, go through `scripts/i18n.py set` so every locale file
is written at once.

**Verify.** Switch the app to German and confirm a place with a distinct
`name:de` changes on the map (Copenhagen → "Kopenhagen", Prague → "Prag"), on
all three mount sites; switch back and confirm it reverts. Assert it in
`map.spec.js` against the style object (`getStyle().layers`) rather than
against rendered glyphs, which the suite blocks.

## 5. Map appearance: four modes

The stage's second half, and the part not in the backlog.

**New module `web/js/map-theme.js`**, modelled on `web/js/theme.js` (same
guarded-`localStorage` shape, same "default is stored as absence of a key"
rule):

- `MAP_THEMES = ["app", "auto", "light", "dark"]`, default `"app"`,
  key `caravel.mapTheme`.
- `resolveMapTheme()` → `"light" | "dark"`:
  - `app` → `resolveTheme()` from `theme.js`
  - `light` / `dark` → themselves
  - `auto` → dark between sunset and sunrise (see below)

**Sunrise/sunset**, ~40 lines of NOAA solar-position maths, no dependency.
Coordinates come from a chain that **never triggers a permission prompt**:

1. a position already cached in `localStorage` by the locate control (add the
   write there — `web/js/geolocation.js` is the existing helper);
2. otherwise `navigator.permissions.query({name:'geolocation'})`, and
   `getCurrentPosition` **only when `state === "granted"`** — a granted
   permission returns silently, a prompted one does not;
3. otherwise the map's current viewport centre — free, no permission, and for
   a travel app usually the more interesting answer anyway;
4. otherwise fall through to `resolveTheme()`.

Recompute on a timer at the next transition rather than polling, and on
viewport change while on fallback 3.

**Theme signal.** `theme.js` currently dispatches nothing — the map is the
first consumer that needs to know. Add a `theme-changed` event on the existing
`eventBus` (mirroring `i18n.js`'s `locale-changed`), fired from
`applyTheme()`. The component subscribes and calls
`map.setStyle(buildStyle(...), {diff:false})`, then `_applyOverlays()` from
Milestone 3 re-adds the ring on `style.load`. Markers and popups are DOM and
survive; the camera is preserved by `setStyle`.

**Marker colours.** `CATEGORY_COLORS`, `PICK_MARKER_COLOR`,
`HERE_MARKER_COLOR` are hardcoded hex today and must stay legible on both
cartographies. Check each against both styles and give the dark variant its
own values where needed; the markers are DOM, so a `:host-context` rule or a
custom property is enough — no style rebuild.

**Settings UI.** A second field beside `renderThemeField` — new
`web/js/components/map-theme-field.js`, mounted in the same Appearance section
of `web/js/pages/settings-page.js:50`, same `role="radiogroup"` pattern. New
keys via `scripts/i18n.py`: `settings.mapTheme`, `settings.mapThemeHint`,
`settings.mapTheme.app|auto|light|dark`.

**Verify.** Toggle each of the four modes in Settings and watch the trip map
restyle **without losing the view or the markers**; with `app` selected,
change the OS theme and confirm the map follows live; with `auto`, fake the
clock (or the cached position's longitude) either side of a transition and
confirm the map flips. `make check-contrast` passes on
`/trips/{trip}/map` in both schemes. Assert the resolved style id in
`map.spec.js` for all four modes.

## 6. Operator configuration compatibility

`CARAVEL_TILE_URL` is documented (`docs/configuration/map-tiles.md`) and
deployments set it, so it must keep working. MapLibre renders raster fine —
synthesise a raster style when it is set:

```
{version:8, sources:{tiles:{type:'raster', tiles:[…], tileSize:256, maxzoom, attribution}},
 layers:[{id:'tiles', type:'raster', source:'tiles'}]}
```

Two sub-differences to handle in `buildStyle()`: **`{s}` is not supported** —
expand it into a three-URL array `a`/`b`/`c`, which is exactly Leaflet's
subdomain semantics *and* fixes the `d.tile.openstreetmap.org` bug the comment
at `leaflet-map.js:584-592` records, by construction. **`{r}` is not
supported** — strip it, or map it to `tileSize: 512`.

Server side (`internal/config/config.go:60-62,209-210,324-336`,
`internal/httpapi/map.go:24-72`): add `CARAVEL_MAP_STYLE`
(`positron` | `dark` | `auto` | a URL), extend `handleMapConfig`'s response,
keep the startup validation of `CARAVEL_TILE_URL`'s `{z}/{x}/{y}`, and update
the Go comments that name Leaflet. Then `docs/configuration/map-tiles.md`,
`docs/configuration/server.md`, `.env.sample`, and
`scripts/check_env_vars.py`'s parity.

Note the interaction to document: an operator-pinned raster provider has one
cartography, so the four-mode setting degrades to "whatever the tiles are".
Say so in the docs rather than pretending otherwise.

**Verify.** Dev server with `CARAVEL_TILE_URL` at OSM raster shows raster
tiles with the OSM credit; unset, the vector style. `make check-env` green.
The re-designed tile-host conformance test passes in both modes. Add a Go test
beside `internal/httpapi/map_config_test.go`.

## 7. Cleanup and the name

Rename `<leaflet-map>` → `<map-view>` and the file, across the three page
modules (`trip-detail-page.js:131`, `location-view-page.js:107`,
`location-editor-page.js:198`), the spec files, `tests/ui/helpers/scenarios.js`
and `tests/ui/contrast.js`. Sweep the remaining Leaflet prose in
`map.spec.js`, `routes.spec.js` and `CLAUDE.md`. Regenerate
`docs/assets/screenshots` (`make screenshots` — every map picture changes;
note the script's own tile-loaded wait needs re-pointing). Prune dead i18n
keys with `scripts/i18n.py unused`. Update `plans/todo.md` in both directions:
remove the vector-tiles entry, and add whatever this stage defers (RTL, the
Ctrl-vs-Cmd hint string if still unfixed, screenshot flakiness).

**Verify.** `grep -ri leaflet` returns only `plans/stage-*.md` history.
`make ci`, `make test-ui`, `make check-screenshots` green.

---

## Build order

1 → 2 is hard (2 needs the vendored files). 3 introduces `_applyOverlays()`
and 5 reuses it, so do not reverse them. 4 before 5 so the stage's stated
payoff — localised labels — lands early. 6 could move ahead of 5 to de-risk
the operator contract sooner, but the user-visible half is the better story
first. 7 last, always: renaming before the behaviour is settled makes every
intermediate diff unreadable.

## Workflow

Per `CLAUDE.md`: implement one milestone, verify with `make ci` green **plus a
real behavioural check** (assertions over screenshots), add a "**Done.**"
paragraph to `plans/stage-30.md` describing what actually landed and how it
was verified, update `plans/todo.md` in both directions, commit (one per
milestone; a same-day fix gets its own "... follow-up: ..." commit), make sure
`make dev` is running, then **stop and hand back control**. Do not start the
next milestone until told to continue.

## Verification (stage-wide)

- `make ci` and `make test-ui` green, including `map.gesture.spec.js` on the
  real-touch Chromium project.
- `make check-contrast` on the map route in both schemes.
- Manual pass at 324×756 (the mobile convention) against `make dev`: the three
  mount sites, all four appearance modes, both locales.
- `make screenshots` regenerated and `make check-screenshots` green.
- `grep -ri leaflet` clean outside `plans/`.

## Out of scope

- **Offline vector tiles.** `web/sw.js` deliberately skips cross-origin
  requests; a tile store is its own project.
- **Self-hosting OpenFreeMap.** Worth documenting eventually; this stage keeps
  the public instance as the default and the config as the escape hatch.
- **The RTL text plugin** (see Milestone 4) — recorded in `todo.md` instead.
- **The Ctrl-vs-Cmd hint string** (`todo.md:48-56`) — unchanged by this stage,
  since `zoomByWheel` is ported as-is.
- **The `map.spec.js` distance-filter flakiness** (`todo.md:308-319`) — a
  shared-seed problem, not a map-engine one.
