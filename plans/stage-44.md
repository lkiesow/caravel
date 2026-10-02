# Stage 44: a fullscreen trip map

## Context

The trip's Map tab is a 60vh (85vh on a phone) map inside a page: the trip
header and tabs above it, the credit and the category legend below. Because
there is page to scroll, touch is cooperative: one finger scrolls the page, two
fingers move the map, and a plain mouse wheel scrolls the page (Ctrl/Meta+wheel
zooms). That is right on the page, but awkward when you just want to *use the
map*.

This stage adds a **fullscreen mode**: a button on the map makes it fill the
screen, map only, and a button in the same place ends it. In fullscreen there is
nothing else to scroll, so **one finger pans the map** and **a plain wheel
zooms**. The category filters still apply. You set them before entering,
because the legend is not part of the fullscreen view.

### Decisions already taken

- **Trip Map tab only.** The single-location map (location view) and the
  editor's coordinate picker are unchanged. Opt-in by attribute on `<map-view>`.
- **Button top-right**, over the map. Bottom-left is the locate button; the
  other corners are free (the "zoom control top left" comment in map-view.js is
  stale: there is no zoom control).
- **No filters in fullscreen.** Exit, change them, re-enter. A compact
  in-fullscreen filter toggle goes to `todo.md` as a possible follow-up.
- **Not remembered.** Back from a location's page returns to the map at the
  saved camera (`history.state.mapView`, unchanged), in the normal view.
- **Native fullscreen where the browser has it, in-page otherwise.** The
  Fullscreen API (desktop, Android) hides browser chrome; Esc / Android back
  exit it for free. iPhone Safari has no element fullscreen, so there the map
  fills the viewport with `position: fixed` (MapLibre calls this "pseudo"
  fullscreen) and Safari's own bars stay.
- **Our own button, not MapLibre's `FullscreenControl`.** The control does the
  gesture switch for us, but brings a background-image icon, English-only
  strings, and its own control box. That is the opposite of the locate button
  next to it. The gesture switch is two calls (`map.cooperativeGestures.
  disable()/enable()`), so owning the button costs little and keeps one style,
  the Lucide sprite and our i18n.

### What fullscreen contains

Not just `.map-wrap`. The **locate status** line (`p.locate-status`) and the
**credit** (`div.attribution`) both sit *outside* `.map-wrap`, below it
(`render()`, map-view.js ~1161). Fullscreening `.map-wrap` alone would hide
locate errors and drop the OSM/OpenMapTiles credit the licence wants visible.
So a new wrapper, **`.map-stage`**, holds `.map-wrap` + `.locate-status` +
`.attribution` (everything except the legend), and *that* element goes
fullscreen. In fullscreen it is a column: map `flex: 1`, status and credit
beneath on the surface colour. The legend stays outside, so it is excluded
without special-casing.

## 1. The fullscreen toggle

**`web/js/components/map-view.js`**

- New observed attribute `fullscreen-toggle` (opt-in). `render()` wraps
  `.map-wrap`, `.locate-status`, `.attribution` in `<div class="map-stage">`
  (always, so the layout model is one shape; only the button is conditional),
  and inside `.map-wrap` renders
  `<button type="button" class="fullscreen" data-action="fullscreen">` with
  `icon("maximize")` and `aria-label`/`title` from `map.fullscreen.enter`.
- `toggleFullscreen()`:
  - native: `stage.requestFullscreen()` when `document.fullscreenEnabled`,
    else in-page: set `this.toggleAttribute("data-fullscreen", true)` and add
    an Escape keydown listener. Exit is the mirror (`document.exitFullscreen()`
    only if `this.shadowRoot.fullscreenElement === stage`).
  - one `applyFullscreenState(on)` both paths go through: reflect
    `data-fullscreen` on the host (tests and CSS read it), swap icon to
    `minimize` and label to `map.fullscreen.exit`, `map.resize()` (the
    ResizeObserver usually catches it; explicit is cheap insurance).
  - native state comes from a `fullscreenchange` listener on `document`,
    reading `this.shadowRoot.fullscreenElement === stage` (shadow-aware the
    same way MapLibre's control is), so Esc / back keep the button honest.
- `destroyMap()` (and disconnect): exit fullscreen if ours, remove the
  `fullscreenchange` and Escape listeners. Native fullscreen also ends by
  itself when the element leaves the DOM (popup link → location page), but
  the listeners must not leak.
- Fix the stale "zoom control sits top left" comment at `.locate`.

**Shadow CSS (`styles` in map-view.js)**

- `.fullscreen` button: top-right, same chrome and `--tap-min` size as
  `.locate`, icon-only.
- `.map-stage:fullscreen` and `:host([data-fullscreen]) .map-stage` (in-page:
  `position: fixed; inset: 0; z-index: …`): column flex, surface background,
  `.map-wrap { flex: 1; height: auto; min-height: 0 }`, `#map` border-radius 0.
- Check stacking for in-page mode: `.trip-tab-content` is `z-index: 0`
  (base.css ~1606) and caps everything inside it. The header is not positioned,
  so a fixed stage should still paint over it. Verify. If anything paints on
  top, lift the panel with `.trip-tab-content:has(map-view[data-fullscreen])`
  in base.css rather than changing the cap for everyone.

**`web/js/pages/trip-detail-page.js`** — add `fullscreen-toggle` to the
Map tab's `<map-view>` (~141).

**Icons** — add `maximize`, `minimize` to `ICONS` in
`scripts/gen_icon_sprite.py`, regenerate per CLAUDE.md, and diff: existing
symbols must be byte-identical.

**i18n** — `map.fullscreen.enter` ("Show map fullscreen" / "Karte im
Vollbild anzeigen"), `map.fullscreen.exit` ("Exit fullscreen" / "Vollbild
beenden") in `en.json` and `de.json`.

Gestures are deliberately unchanged in this milestone: still two fingers and
Ctrl+wheel in fullscreen. It is reviewable as "the view works" on its own.

## 2. One finger and a plain wheel in fullscreen

**`web/js/components/map-view.js`**

- `applyFullscreenState(on)`: `map.cooperativeGestures.disable()` on enter,
  `enable()` on exit. With it off, MapLibre's dragPan takes one finger and
  `touch-action` on the canvas container stops handing it to the page; the
  `cooperativegestureprevented` hint stops firing on its own.
- `bindGestureGate`: when `this.hasAttribute("data-fullscreen")`, a plain
  wheel goes the Ctrl path (`preventDefault()` + `this.zoomByWheel(e)`) and
  shows no hint. The library's `scrollZoom` stays off: `zoomByWheel` is still
  the one place a wheel zooms (see its comment for why).
- `noteUserMovedMap` already covers mousedown/touchstart/keydown, so the
  follow-camera behaviour of the locate feature is unchanged.
- Update the long comments at the `cooperativeGestures: true` option and
  `bindGestureGate` to mention the fullscreen exception.

## Build order

1 → 2. Milestone 2 depends on 1's `applyFullscreenState` and `data-fullscreen`.

## Workflow

Per milestone: implement → `make ci` green plus the relevant Playwright specs
(below) plus a manual pass at 324×756 against `make dev` via the Playwright MCP
tools → add a **Done.** paragraph here and update `plans/todo.md` (add the
"filter toggle inside fullscreen" idea; remove nothing unless implemented) →
one commit → `make dev` running → stop and wait.

**If the Playwright MCP has no browser installed (or one fails to launch),
stop and say so rather than skipping the manual pass. It can probably be
installed.**

## Verification

Milestone 1:
- `tests/ui/map.spec.js`: the button exists on the trip map only (absent on
  the location view and the editor picker), has its accessible name; clicking
  it sets `data-fullscreen` on the host; the stage's
  `getBoundingClientRect()` equals the viewport; legend not inside it; credit
  and locate button visible inside it; clicking again (accessible name now
  "Exit fullscreen") restores the old map height. Esc exits (native in
  Chromium/Firefox headless; force the in-page path once by stubbing
  `document.fullscreenEnabled = false` in `addInitScript` and assert the same
  plus Escape).
- Filters apply: uncheck a category, enter fullscreen, count markers equals
  the filtered count.
- Popup link while fullscreen navigates to the location; Back shows the map
  at the saved camera, not fullscreen.
- `make test-ui` in full: `map-view` has three mounts, and the new
  `.map-stage` wrapper must not shift layout for the other two (sweeps check
  overflow and heights).
- Manual at 324×756, both locales and both map themes.

Milestone 2:
- `tests/ui/map.gesture.spec.js` (chromium-gestures, real CDP touches): in
  fullscreen a **one-finger** drag moves the map centre and shows no hint;
  after exit, one finger scrolls the page again and leaves the map alone.
  This is the existing test, re-run after the toggle.
- `map.spec.js`: `map.cooperativeGestures.isEnabled()` false in fullscreen,
  true after; a plain wheel in fullscreen changes zoom with no hint, outside
  it the page scrolls and the hint shows (existing assertion).
- Manual at 324×756: one-finger pan and pinch in fullscreen; locate button
  still works and tracks.
- `make ci` both milestones (i18n parity catches a missing `de.json` key).
