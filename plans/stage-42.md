# Stage 42: the location marker says which way you are going and facing

## Context

The trip map's "My location" control tracks you continuously (Stage 36), but
the marker is a dot. It says where you are and not which way you are pointing
or moving -- the half that matters when you are working out whether to turn,
or whether you have already walked past something. `plans/todo.md` has carried
this as "The location marker has no heading" since Stage 36.

Two sources can answer, and they answer different questions:

- **Direction of travel** -- `GeolocationCoordinates.heading` and `.speed`,
  already delivered by the watch `web/js/geolocation.js` runs and currently
  thrown away when the fix object is built (`handle({...})`, around line 233).
  No new permission. Meaningless when standing still, good from brisk walking
  upward, and not affected by magnetism. The web exposes no accuracy for it.
- **Direction the phone faces** -- the compass. `deviceorientationabsolute`
  on Chrome/Android (north-relative `alpha`, no prompt), and `deviceorientation`
  with `webkitCompassHeading` / `webkitCompassAccuracy` on iOS Safari, behind
  `DeviceOrientationEvent.requestPermission()`, which must be called from a
  user gesture. Works standing still; can be badly off indoors or near metal.
  Plain `deviceorientation` on Android is relative to an arbitrary zero and
  must never be read as a compass.

They are not fused into one number: that assumes the phone points the way you
walk, which is false when it is in a pocket. Each is shown as what it is:

- **Moving** -- the dot becomes an **arrow** along the direction of travel.
- **Compass available** -- a translucent **cone** around the marker shows the
  direction the phone faces, whether moving or not.
- **Neither** -- the dot exactly as today.

"Walking north, looking east" is then readable at a glance, and every device
degrades to the current behavior rather than to something wrong.

### Decisions already taken

- iOS asks for the compass on the **same locate press** that asks for location
  (two prompts in a row on iOS; nothing extra on Android).
- Heading is shown on the **trip map only** (the continuous watch). The editor's
  pick map records where a place is, not where you are going, and stays a dot.
- The map keeps **north up**: rotation stays disabled (`dragRotate: false`,
  `disableRotation()` in `map-view.js`). Marker angles are still computed
  relative to `map.getBearing()` so enabling rotation later would not break
  them.
- A refused or missing compass is **not an error message**. The cone simply
  never appears; the arrow and the dot still work. The marker not claiming a
  direction *is* the honest state.

### What exists and is reused

- `watchPosition()` in `web/js/geolocation.js`: the two-phase acquire/track
  logic and the 3 s / 10 m delivery throttle stay as they are; fixes just carry
  two more fields.
- `map-view.js`: `bindLocate()` (click handler, visibility pause/resume),
  `startLocateWatch()`, `showPosition()`, `hereMarkerElement()`,
  `--marker-here` / `--marker-ring` custom properties (light and dark), and
  `disconnectedCallback()` teardown.
- `tests/ui/helpers/geolocation.js`: the scripted fake geolocation and clock
  offset (`installFakeGeolocation`, `emitFix`, `advanceClock`).

## Milestone 1: an arrow while moving

- **`geolocation.js`** -- pass `heading` and `speed` through on every fix
  (normalised: `null` unless finite; `heading` also `null` when `speed` is not
  positive, since browsers report `NaN`/`0` there inconsistently). The one-shot
  `getCurrentPosition()` shape gains the fields harmlessly.
- **One marker, not one per fix.** `showPosition()` currently removes and
  recreates `_hereMarker` on every fix. Create it once and `setLngLat()` after,
  so state on the element (the arrow now, the cone in Milestone 2) survives
  fixes. Teardown paths that null `_hereMarker` keep working.
- **Marker element** becomes a small structure inside the element MapLibre
  positions: a dot and an arrow (inline SVG chevron, `--marker-here` fill,
  `--marker-ring` outline, the same footprint and shadow as the dot), with
  one shown at a time via a `data-moving` attribute. The arrow's angle is set
  as a CSS custom property on the element (`--course`), applied by `transform:
  rotate()`, and also mirrored to a `data-course` attribute for tests.
- **When it is an arrow** -- `speed` with hysteresis: on above 1.5 m/s, off
  below 0.8 m/s, so a walker near the threshold does not flicker. A finite
  `heading` is required. A fix without one, or no fix for 10 s (checked on a
  timer, since a lost signal delivers nothing), returns to the dot rather than
  freezing the last angle.
- Only for the continuous watch; pick mode never sets `data-moving`.
- **Tests** (`tests/ui/geolocation.spec.js` or a new `map-heading.spec.js`):
  extend `emitFix` to pass `heading`/`speed` through the fake. Assert: dot at
  rest; arrow with `data-course` matching after a fast fix; stays arrow at
  1.0 m/s (hysteresis), back to dot at 0.5 m/s; back to dot after
  `advanceClock` past 10 s with no fix; marker element identity unchanged
  across fixes (no recreate); pick map never shows the arrow.

## Milestone 2: the compass cone

- **New `web/js/heading.js`** -- owns the compass, mirroring the shape of
  `geolocation.js`:
  - `requestCompassPermission()` -- must be called **synchronously inside the
    click handler**, before any `await`, or iOS rejects it as not
    user-initiated. No-op resolving `true` where `requestPermission` does not
    exist. Remembers the answer for the page.
  - `watchCompass({onUpdate})` returning `{cancel}` -- listens to
    `deviceorientationabsolute` where supported, else `deviceorientation`
    only when it carries `webkitCompassHeading`. Readings with `alpha === null`
    (desktop Chrome fires these with no sensor) are ignored.
  - Heading = `webkitCompassHeading`, or `(360 - alpha) % 360`; then corrected
    by `screen.orientation?.angle` so landscape is right.
  - Smoothing as an exponential average of the unit vector (sin/cos), which
    handles the 359 -> 0 wrap without special cases. Delivery coalesced to one
    `requestAnimationFrame` per frame, skipped when the change is under 1 deg.
  - Stale after 5 s with no reading -> `onUpdate(null)`.
- **`map-view.js`** -- in the locate click handler, call
  `requestCompassPermission()` first, then the existing flow. Start
  `watchCompass` alongside the position watch when continuous; cancel it in
  the same places the position watch is cancelled (visibility hidden,
  `disconnectedCallback`) and restart it on resume. A press while tracking
  (recentre) re-requests only if the permission was never answered.
- **The cone** -- an inline SVG wedge behind the dot/arrow, about 60 px radius,
  `--marker-here` with a radial fade to transparent, rotated by `--facing`
  (minus `map.getBearing()`), mirrored to `data-facing`. Hidden when there is no
  reading. Fixed width in this milestone. Decorative: `aria-hidden`, no
  pointer events, so marker clicks and map gestures are unaffected.
- **Tests** -- synthetic `DeviceOrientationEvent('deviceorientationabsolute',
  {alpha, absolute: true})` dispatched on `window` (Chromium supports the
  constructor), and an iOS-shaped path with a stubbed `requestPermission` and
  an event carrying `webkitCompassHeading` via `defineProperty`. Assert:
  `data-facing` = 360 - alpha; landscape correction with a stubbed
  `screen.orientation.angle`; wrap smoothing (350 -> 10 settles near 10 rather
  than sweeping through 180); null-alpha ignored; cone hidden after 5 s with no
  reading; listener removed on visibility hidden and on disconnect (count
  `addEventListener`/`removeEventListener`); `requestPermission` called within
  the click (not after it), and a denial leaves no cone and no error text.

## Milestone 3: honest width, and the paperwork

- **Cone width shows uncertainty** -- on iOS from `webkitCompassAccuracy`
  (half-angle clamped to 15-45 deg; a negative value means uncalibrated, so no
  cone). Elsewhere a fixed default half-angle (about 30 deg), since Android
  exposes nothing. Tested by asserting the wedge's computed angle for a few
  accuracies.
- **Docs** -- `docs/features/the-map.md` has no section on "My location" yet.
  Add one: what the dot, ring, arrow and cone mean, that iOS asks twice, and
  that the cone can be wrong near metal or indoors (recalibrate with a
  figure-eight). `make docs` green.
- **`plans/todo.md`** -- remove "The location marker has no heading"; add
  anything deferred (e.g. heading-up map rotation, if still wanted).
- **Real-device check** by the user on their phone, recorded in the Done
  paragraph: arrow appears while walking and not while standing; the cone
  turns with the phone and is right in landscape; the cone points roughly the
  right way against a known street.

## Build order

1. Milestone 1 -- the arrow, because it needs no permission and ships value on
   every device; it also lands the single-marker change the cone depends on.
2. Milestone 2 -- the compass and cone.
3. Milestone 3 -- uncertainty width, documentation, backlog, device check.

## Workflow

One milestone at a time. For each: implement, verify with `make ci` plus a
Playwright pass that proves the behavior actually changed (assertions on
`data-course` / `data-facing`, computed transforms and element identity, not
screenshots), add a "**Done.**" paragraph to that milestone's section here,
update `plans/todo.md` in both directions, commit, leave `make dev` running,
and stop and hand back control. Do not start the next milestone until told to
continue; feedback at a checkpoint is fixed and re-verified before moving on.

## Verification

- `make ci` green at every milestone (no new user-facing strings are expected
  until the docs; if any are added, both `en.json` and `de.json`).
- The existing `tests/ui/geolocation.spec.js` and `map.spec.js` stay green --
  the acquire/track behavior is untouched.
- New UI tests cover arrow hysteresis and staleness, compass math, landscape
  correction, wrap smoothing, lifecycle cleanup and the permission call
  happening inside the gesture.
- At 324x756 the cone and arrow do not overlap the locate button or status
  line, and do not intercept taps on nearby pins.
- Real-device pass on the user's phone in Milestone 3.
