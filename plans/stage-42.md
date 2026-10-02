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

**Done.** As planned, with one structural note. `geolocation.js` passes
`heading` and `speed` through on every fix via a new `courseOf()`, which turns
NaN, negative and "heading without speed" into `null`. `showPosition()` now
creates the marker once and `setLngLat()`s it afterwards. `destroyMap()` still
nulls it, and also calls the new `clearCourse()`, so a rebuilt map starts from
the dot with no poll left running. The deviation: the here marker is now styled
by class (`.here`, `.here__dot`, `.here__arrow` in the shadow stylesheet)
rather than inline like the other markers, because which shape shows is a
`[data-moving]` selector and inline styles cannot express one. The arrow is an
inline SVG chevron in the dot's colours, rotated on the inner SVG so it cannot
collide with the `transform` MapLibre writes on the outer element. The angle is
relative to `map.getBearing()`. `showCourse()` applies the 1.5 / 0.8 m/s
hysteresis. While an arrow shows, a one-second poll compares `Date.now()`
against the last direction and reverts to the dot after 10 s without one.
Comparing against the clock rather than counting timer ticks is what lets the
suite's clock offset drive it.

Verified: `make ci` green. Six new tests in `map.spec.js` ("the marker shows
the direction of travel"): dot at rest including a heading with zero speed;
arrow with `data-course` and a computed `rotate(90deg)` matrix; hysteresis in
both directions; reverting to the dot after the clock passes 10 s; the same
element across fixes and only one `.here`; the picker never shows an arrow.
Run against the previous `map-view.js`, five of the six fail. The picker test
is the negative case and passes on both, as it should. The 34 existing
geolocation, tracking and locate-control tests stay green, after
`tests/ui/helpers/geolocation.js` learned to pass `heading`/`speed` through.
Looked at once at 324x756 and 3x DPR on the dark map: dot at rest, arrow at
60 deg when moving. The images were not kept.

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

**Done.** As planned, with two corrections to what the plan assumed.
`web/js/heading.js` has `requestCompassPermission()` (asks at most once per
page; a call the platform refuses, such as one outside a gesture, does not
count as an answer), `compassAllowed()`, and `watchCompass()`. That function
smooths readings as a unit vector, delivers at most once a frame and only for
changes of 1 deg or more, adds `screen.orientation.angle` for landscape, and
withdraws the direction after 5 s without a reading, checked against
`Date.now`. In `map-view.js` the locate click asks for the compass first and
synchronously. `startCompass`/`stopCompass` sit on the position watch's
lifecycle: started with tracking, stopped on hidden, on a failed watch and on
disconnect. On iOS the compass starts once the permission promise answers yes.
The cone is a `.here__cone` span inside the marker: a radial fade cut to a
wedge by a conic `mask-image`, so its width is one custom property (`--spread`,
30 deg each side for now). That replaced the SVG wedge the plan named, because
an SVG gradient `url(#id)` inside a shadow root is fragile and the CSS needs no
id. `showFacing()` keeps the reading in `_facing` too, so a marker created
after the compass answered still gets it.

The corrections: **Firefox exposes `deviceorientationabsolute`**, so it takes
the same path as Chrome. The plan, and this file's first draft, said Firefox
used `deviceorientation` with `absolute: true`. That remains the fallback for
an engine with neither event, and the tests exercise it by deleting the
property. The first visual check found this, by dispatching the wrong event.
Second, the plan's "count add/removeEventListener" landed as a wrapper in the
new `tests/ui/helpers/compass.js`, which also fakes the three platforms, iOS
`requestPermission` and a landscape `screen.orientation`.

Verified: `make ci` green; full UI suite green. Ten new tests in `map.spec.js`
("the marker shows which way the phone faces"):
- an absolute reading turns the cone, checked by `data-facing` and the
  computed rotation;
- relative and null-alpha readings are never drawn;
- crossing north never passes through anything outside 340-20 deg and settles
  at 10;
- the cone is withdrawn after 5 s;
- the listener count goes 1 -> 0 hidden -> 1 visible -> 0 removed;
- the picker never listens;
- only the absolute event is read where it exists;
- iOS is asked once, inside the click, and reads `webkitCompassHeading`;
- an iOS refusal leaves no cone, no listener and no message;
- landscape adds the screen angle.

Against Milestone 1's `map-view.js`, nine of the ten fail, the picker one being
the negative case. Removing the absolute check from `heading.js` makes the
relative-reading test fail, so that test guards the filter rather than merely
the cone's absence. Looked at once at 3x DPR on the dark map: the cone faces
east, alone and together with an arrow pointing northeast. The images were not
kept. Real-device behavior is Milestone 3's check.

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

**Done**, except the real-device check, which is handed to the user and
tracked in `plans/todo.md` until it is made. `watchCompass()` now passes iOS's
`webkitCompassAccuracy` to `onUpdate` as a second argument, and delivers again
when only the accuracy changed. A reported -1 (uncalibrated) withdraws the
direction at once instead of waiting 5 s for it to go stale; the reading is
still arriving, it is just worthless. `showFacing()` sets `--spread` on the
marker and mirrors it to `data-spread`: the reported accuracy clamped to
15-45 deg, or 30 where nothing is reported. The default moved from the cone's
own rule to a `var(--spread, 30)` fallback, so the value set on the marker
reaches the cone instead of being shadowed by it. `docs/features/the-map.md`
gains a "Where you are" section: the button and HTTPS, what the dot, ring,
arrow and cone each mean, the two iOS prompts, and the figure-eight
recalibration. The first draft gave the two iOS prompts in the wrong order (the
compass is asked first); the sentence now does not depend on the order. In
`plans/todo.md` the "no heading" entry is gone, replaced by the outstanding
device check and a heading-up map, which was deliberately left out.

Verified: `make ci` green; `make docs` green (strict). The 47 location,
tracking and heading UI tests pass. Three new tests read the angle actually
drawn from the computed `mask-image`, which resolves to plain degrees in
Firefox:
- 30 with no accuracy;
- 25 -> 25, 5 -> 15 and 80 -> 45 on iOS;
- a -1 withdraws the cone within a second.

Against Milestone 2's code the two iOS ones fail. The default-width one passes
on both, because Milestone 2 already drew 30; it is there to keep it so.

**Follow-up: the arrow flickered on a real walk, and tracking felt slow.**
The user's first walk with Milestones 1-3 confirmed the arrow and cone work in
general. Two problems showed up. The marker kept switching between dot and
arrow while walking, and the position updated slowly. There were three
causes, and all three are fixed in one follow-up:
- **The thresholds sat on top of walking pace.** On at 1.5 m/s and off at
  0.8 m/s put the switch just above ordinary walking (1.2-1.4 m/s). They are
  now on at 1.0 and off at 0.5.
- **One fix that disagreed ended the arrow.** A single fix with no heading, or
  one speed dip, dropped straight back to the dot. Now the arrow keeps its last
  angle until nothing has supported it for 5 s (`COURSE_HOLD_MS`). That one
  rule also replaces the 10 s staleness timeout: no fixes at all is just the
  limiting case of no supporting ones.
- **Tracking showed one fix every 3-4 s.** `TRACK_THROTTLE_MS` was 3000, and
  fixes inside the window were dropped rather than deferred. At walking pace
  the 10 m skip rule never fired, so a ~1 Hz platform was shown at a third of
  its rate. Now 900 ms: about the platform's own pace, with slack so that a
  fix arriving at 990 ms is not dropped (that would halve the rate again). This
  changes Stage 36 behavior; the user decided it.

Animating the marker between fixes was proposed too, and deferred until the
user has walked with this. It is in `plans/todo.md`.

Verified: `make ci` green; full UI suite green. Changed and new tests:
- the hysteresis test, at the new numbers: 0.8 from rest is a dot, 1.2 starts
  an arrow, 0.8 keeps it;
- "a fix that disagrees is ridden out": a null heading and a 0.2 m/s dip each
  hold the arrow at its last angle, and the next good fix picks it up again;
  four seconds stopped is still held, 5.5 s is the dot;
- the staleness test, now at 6 s;
- in `geolocation.spec.js`, the throttle's first test: 400 ms is dropped,
  980 ms delivered, a further 1000 ms delivered.

All three fail against the code before this follow-up. Re-walking it is the
user's.

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
