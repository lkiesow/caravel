# Stage 34 — The map credit is text under the map

## Context

On a 324px-wide phone the map's attribution is clipped: the screenshot
that opened this stage shows "OpenFreeMap (c) OpenMapTiles Data from
OpenStreetMap" running off the left edge of the map with its first words
cut away.

The mechanism is not a missing `max-width`. MapLibre puts its
attribution control inside `.maplibregl-ctrl-bottom-right`, which the
vendored CSS makes `position: absolute; right: 0; bottom: 0` — a
shrink-to-fit box — and then floats the control right inside it
(`.maplibregl-ctrl-bottom-right .maplibregl-ctrl{margin:0 10px 10px 0;
float:right}`). Nothing constrains the control's *left* edge, so the
one-line credit simply extends past the map's left edge, where
[`.map-wrap { overflow: hidden }`](web/js/components/map-view.js#L356)
cuts it off. There is no `white-space: nowrap` anywhere; the box is just
allowed to be wider than the map it sits in.

Constraining the width in place would fix the clipping and leave a
two-line translucent plate covering the bottom of a phone-sized map. The
better answer is that a credit line is not really a map control: it is a
sentence about where the cartography came from, and it can be a sentence
*under* the map, in the page's own type, where it wraps like any other
text and needs no plate behind it to stay readable.

That is what this stage does, and it is deliberately one milestone.

## 1. The attribution control mounts below the map

Today the map is constructed with `attributionControl: { compact:
false }` and MapLibre owns the whole arrangement. Instead:

- construct with `attributionControl: false`,
- instantiate `new maplibre.AttributionControl({ compact: false })` by
  hand and append `ctrl.onAdd(map)` — which returns a *detached*
  element — into a `.attribution` box rendered as a sibling after
  `.map-wrap`, the same shape [`.locate-status`](web/js/components/map-view.js#L905)
  already has,
- call `ctrl.onRemove()` from `destroyMap()`.

Reading the vendored implementation first, because the approach depends
on it: `onAdd` builds a `<details>`, wires `styledata`, `sourcedata`,
`terrain`, `resize` and `drag` on the map, and returns the element
without ever parenting it. The only place it looks at the map's geometry
is `_updateCompact`, reading `getCanvasContainer().offsetWidth` — and
with `compact: false` that takes the branch that just sets `open`. So
nothing in the control assumes it is inside the map container.

The credit therefore stays *live*: a theme swap does `setStyle`, which
re-fires `styledata`, and the control re-reads the loaded sources. That
property is the whole reason Stage 30 Milestone 6 deleted
`CARAVEL_TILE_ATTRIBUTION`, and reimplementing the credit as our own
static string would give it straight back.

`onRemove()` in `destroyMap()` is the part that is easy to miss: a
control added by hand is not in `map._controls`, so `map.remove()` will
not tear it down and its five listeners would outlive the map.

Styling, now that it is text on the page rather than a plate on a map:

- **no background.** The vendored `background-color: hsla(0,0%,100%,.5)`
  exists to keep the credit readable over cartography; under the map
  there is nothing to be readable over, and a translucent white bar on
  the page background would be wrong in both themes.
- **the app's own colours.** The vendor gives links `rgba(0,0,0,.75)`
  with no underline, which is invisible in dark mode. Muted text with an
  underline on the links instead: the credit should read as a footnote,
  not as three accent-blue calls to action, but the links still have to
  look like links.
- **an explicit font size.** `.maplibregl-map` sets `font: 12px/20px
  Helvetica…` and that inheritance is gone outside the map container, so
  without one the credit jumps to the app font at body size.
- **`max-width: 100%` and ordinary wrapping.** Out from under
  `.map-wrap`'s `overflow: hidden`, an over-wide credit can widen the
  document — which `routes.spec.js`'s page-level overflow sweep would
  catch. This is the one new failure mode the move introduces, and the
  guard is a line.

Layout: `:host` is already a column flex with `.map-wrap { flex: 1 }`,
so on the desktop path the footer takes its own height and the map
shrinks by a line. Under `@media (max-width: 640px)`, `:host` is
`height: auto` and `#map` takes `--map-height` directly, so there the
credit adds to the total instead — 85vh of map plus a credit line, still
short of the fold.

## Build order

One milestone, so: implement, verify, record, commit.

## Workflow

The loop from `CLAUDE.md`: implement, verify with `make ci` plus a real
browser pass, add a **Done.** paragraph here, update `plans/todo.md` in
both directions, commit, leave `make dev` running, hand back.

## Verification

`make ci`, and then the part `make ci` cannot do. The UI suite blocks
every request for map data, so no source loads and the control has
nothing to list — `map.spec.js:282-310` records at length why there is
no attribution assertion and why three routes round it were rejected.
That does not change here.

What can be asserted is structural and worth asserting, because it is
exactly what this stage changes: the attribution element exists, it is a
sibling *after* `.map-wrap` rather than a descendant of the map, and it
carries no background of its own.

The credit text itself is checked by hand against live tiles at 324x756
and on a desktop width: three links, all live, nothing clipped, readable
in both themes.

**Done.** The control is instantiated by hand in `mountAttribution`
([map-view.js](web/js/components/map-view.js)) and its element appended
to a `.attribution` box rendered after `.map-wrap`; the map is built with
`attributionControl: false` and `destroyMap` calls `onRemove()`. Styling
as planned: no background, muted text with underlined links, an explicit
0.7rem/1.4, `max-width: 100%`, and `.attribution:empty { display: none }`
so a map that never constructed does not leave a margin behind.

One thing the plan did not see. The two fixed-height mounts overflowed
their own host on a phone: the mobile block sets `--map-height` for
`[lat]` and `[pick]` and leaves `#map` to take it literally, but the
desktop `:host([lat]) { height: 16rem }` still applied, so the host was
exactly as tall as its map and the credit hung out of it -- measured at
324px on the location view, 39px of overlap across the "View on Google
Maps" links, which sit 8px below the host. Both mobile rules now say
`height: auto`, which makes the number in that block mean the map
consistently. After the fix, at 324px: map 256px, host 295px, nothing
past the host, 8px to the next element. The desktop path is unchanged and
still means "16rem is the whole component", so the map there loses a line
to the credit (296px inside a 320px picker).

Verified: `make ci` green. By hand at 324x756 against live tiles -- the
credit wraps to two lines under the map, reads "OpenFreeMap (c)
OpenMapTiles Data from OpenStreetMap" with all three links live and
nothing clipped, `document.scrollWidth` equal to the viewport. Measured
on all three mounts at 324px and at 1280px, none overflowing its host.
Colours read from `getComputedStyle` in both themes: `#52525b` on white
and `#a1a1aa` on `#18181b`, links underlined, background
`rgba(0, 0, 0, 0)` in both. A `setStyle` -- what a map-theme change does
-- leaves exactly one control with the credit intact, and three in-app
navigations in and out of the map tab leave one control, not three.
`make test-ui` green for `map` (77) and for the route sweeps including
the overflow and tap-target ones (21).

A structural assertion went into `map.spec.js` next to the long note
about why the *text* cannot be asserted: the control is mounted in the
credit box, is not inside `.map-wrap`, follows it in the flow, has a
transparent background and does not reach past the map's width.
Confirmed it fails on the old arrangement by putting `attributionControl`
back and dropping the mount call -- one failure, restored afterwards.
