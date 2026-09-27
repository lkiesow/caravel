# Stage 36 — The locate control on the water

## Context

Pressing **My location** on a boat at sea drops the "you are here" marker
on the nearest coastline instead of on the water. Reported from an actual
boat ride.

Nothing in Caravel snaps the coordinate. There is no land check, no
bounding box, no nearest-place lookup, and no rounding anywhere between
the browser's answer and the marker: `showPosition()` draws whatever it
is handed, `takeCoordinates` writes it into the editor's fields
unrounded, `internal/httpapi/items.go` stores coordinates pass-through,
and the only server-side validation is a −90..90 / −180..180 range check
in `parseLatLng`. The snapping happens *before* the app sees the fix.

**The cause is one options object**, `web/js/geolocation.js:105`:

```js
{ timeout: timeoutMs, maximumAge: 60000, enableHighAccuracy: false }
```

Two of those three fields are implicated.

`enableHighAccuracy: false` is not a precision hint — it decides *which
sensor answers*. On Android it maps to the fused provider's
`PRIORITY_BALANCED_POWER_ACCURACY`, which explicitly declines to power
GNSS; in WebKit it selects a coarse `CLLocationAccuracy`. So the answer
comes from the network location provider — wifi APs and cell towers.
Offshore the only reachable infrastructure is ashore, so trilateration
returns a point on the coast with a kilometre-scale `accuracy`. On land
that provider is accurate enough that nobody notices, which is why this
has survived since Stage 13.

`maximumAge: 60000` is a co-cause, not a footnote. It permits the
platform to answer *without acquiring anything at all* — returning the
coarse fix it already had cached. So flipping `enableHighAccuracy` alone
is partly a no-op: the cached coast fix still satisfies the request and
no GNSS acquisition is ever started.

The module's comment states the trade-off it thought it was making:

> enableHighAccuracy is deliberately off: it costs battery and seconds
> for a precision nothing here needs - a map view and a distance filter
> in kilometres.

That reasoning holds for *how precise* the answer is. It does not hold
for *where the answer comes from*, and outside cell coverage the two stop
being the same question. The sentence is true on land and false at sea.

**A second defect hid the evidence.** The accuracy ring exists precisely
so a 2 km fix and a 5 m fix do not look alike. But `showPosition()` ends
with an unconditional `jumpTo({ zoom: HERE_ZOOM })`, and `HERE_ZOOM = 15`
is street level — about 2.8 m per pixel at 54°, so a 3 km ring has a
radius near 1070 px and lies entirely outside the viewport. The ring was
computed correctly, drawn correctly, and off screen. All the user could
see was a confident 1 rem dot on a coastline. This is a camera bug, not
an opacity one; raising the fill opacity would not fix it.

Stage 13 set the standard for this control: *"failing honestly is the
requirement, not an edge case."* A coast pin with no visible ring is that
control failing dishonestly.

**The control is also one-shot, and should not be.** Pressing it takes a
single reading and leaves it on the map to go stale. While navigating —
the case where the map is worth looking at at all — the position needs to
keep up, and a live watch is also what lets a poor first fix correct
itself visibly rather than sitting there wrong.

**Intended outcome.** At sea the marker lands on the water; while the fix
is still coarse the map is zoomed out far enough that the ring reads as a
ring; once acquired the position keeps updating as you move; and whenever
the fix is coarse the interface says so in words.

## The three phases

The whole design turns on one distinction, so it is worth naming up
front. A press of **My location** moves through:

1. **Acquiring** — from the press until the fix settles (desired accuracy
   met, or the deadline passes). Only *improving* fixes are accepted, and
   the camera re-fits the accuracy ring as it improves. This is what makes
   a bad first fix correct itself visibly.
2. **Tracking** — steady state. Every fix is accepted, because on a moving
   boat each one is a genuinely new position. The camera holds the zoom
   the fit settled on and pans to follow.
3. **Detached** — the user has panned or zoomed. Marker and ring keep
   updating; the camera is theirs and never moves on its own again until
   the button is pressed.

**The only-improving rule must be dropped at settle.** During acquisition
it is what stops the marker snapping back to the coast. During tracking it
would freeze the marker at the first good fix and never move it again —
the exact opposite of the feature. Getting this boundary right is the
single most important detail in Milestone 3.

## 1. `watchPosition()` — high accuracy, first fix immediately, refine, then keep going

`web/js/geolocation.js` only. No caller changes; the two existing callers
must keep working untouched.

Flipping the flag is necessary but not sufficient, and risky alone.
`getCurrentPosition` resolves on the *first* position delivered and then
stops listening — and both Android's fused provider and CoreLocation
typically deliver the last network fix first, refining only seconds later.
So the flip alone can return the identical coast fix, having spun up the
GNSS chip for nothing. Where it *does* change behaviour (cold chip, no
cache) it converts a wrong answer into `map.locate.timeout`, because the
module's own 10 s `setTimeout` at line 83 is a hard reject.

A watch gives both halves for free: the first callback is the coarse fix
(instant, so land behaviour is unchanged), and later callbacks refine as
satellites lock. You never pay a timeout for the refinement, because you
already have an answer.

```js
export function watchPosition({
  onUpdate,                  // (fix) => void, every accepted fix
  onSettled,                 // (fix) => void, once: desired accuracy met or deadline
  onError,                   // (err) => void, non-fatal, after the first fix
  desiredAccuracyM = 50,
  settleDeadlineMs = 30000,
  firstFixTimeoutMs = 10000, // no fix at all by now => reject(LOCATE_TIMEOUT)
  continuous = false,        // keep watching past settle
} = {}) // -> { promise: Promise<fix>, cancel(): void }
```

Passing `{ enableHighAccuracy: true, maximumAge: 0, timeout: settleDeadlineMs }`.

`promise` resolves at settle regardless of `continuous`, so
`getCurrentPosition()` stays exactly as it is — a thin wrapper with
`continuous: false` returning `.promise`, leaving `locations-tab.js:397`
a one-word diff. Extend the fix object to
`{lat, lng, accuracy, timestamp, final}`; purely additive, so anything
destructuring `{lat, lng}` notices nothing.

**Acceptance rules, by phase.** Before settle, accept a fix only if it is
the first or `accuracy < best.accuracy`. After settle (continuous mode
only), accept every fix — with one bounded guard so a single garbage
reading cannot teleport the marker ashore: ignore a fix worse than
`max(3 × settledAccuracy, 500 m)`, but for at most 15 s. After that,
accept whatever arrives. You have genuinely lost GNSS at that point, and
the ring plus the coarse message tell the truth; a permanently frozen
marker would not.

**Render throttle.** Under GNSS the platform pushes about 1 Hz, which is
more DOM churn than anyone needs. Throttle `onUpdate` to at most one call
per 3 s, or immediately when the position has moved more than 10 m —
whichever comes first. Throttle the callback, never the watch: see below.

**Why a watch and not a timer.** Re-calling `getCurrentPosition` every few
seconds is the obvious alternative and is worse on both axes. Each call is
a fresh acquisition that may be cold, so it costs *more* battery than a
watch the platform is already servicing, and it adds latency to every
update. A single `watchPosition` keeps GNSS engaged and lets the platform
push cheap deltas.

Replace the platform's `maximumAge` with a cache we control: a
module-level `lastFix` plus a `maxAgeMs` option. Strictly better than
delegating, because our cached fix carries its own accuracy and timestamp
and is observable from a test. The locate button always passes
`maxAgeMs: 0`.

Keep the `permissionAlreadyDenied()` pre-check and the own-timer reasoning
at lines 69-83 — the comment about `PositionOptions.timeout` not being
honoured while the permission prompt is outstanding is still true for
`watchPosition`, and now governs `firstFixTimeoutMs`. Rewrite the comment
at lines 102-105, which will otherwise argue the opposite of what the code
does; keep its argument as the record of why it was wrong.

**Verification.** New `tests/ui/geolocation.spec.js` driving the module
via `page.evaluate(async () => { const m = await import("/js/geolocation.js"); ... })`
— the pattern already used in `map-theme.spec.js:83`. Install a scripted
fake `navigator.geolocation` with `page.addInitScript` emitting a
deterministic sequence (t=0, 2800 m, on the coast; t=400 ms, 300 m;
t=900 ms, 18 m, at sea), so accuracies and timings are exact rather than
at Chromium's discretion. Assert: the first `onUpdate` arrives before the
second fix is emitted; before settle a deliberately worse fix is *not*
delivered; the resolved fix is the 18 m one; a never-improving sequence
still resolves at the deadline rather than rejecting; an empty sequence
rejects with `reason === LOCATE_TIMEOUT`; `clearWatch` called exactly once
per case. **And the phase boundary:** in `continuous: true`, a fix after
settle that is *worse but plausible* (say 60 m) **is** delivered, proving
the only-improving rule was dropped — plus a 3000 m fix inside the 15 s
window is not, and one after it is. Plus a direct assertion that the
options object carries `enableHighAccuracy === true` and
`maximumAge === 0` — cheap, and it is the actual bug. `make ci` green.

**Done.** `web/js/geolocation.js` now exposes `watchPosition({onUpdate,
onSettled, onError, desiredAccuracyM, settleDeadlineMs, firstFixTimeoutMs,
continuous, maxAgeMs}) -> {promise, cancel}`, asking the platform for
`{enableHighAccuracy: true, maximumAge: 0}`. The two phases are in
`acceptFix()`: improving-only before settle, everything plausible after, with
the degraded-fix guard as planned (`TRACKING_ACCURACY_FLOOR_M` 500,
`DEGRADED_GRACE_MS` 15000, and `degradedSince` deliberately left set once the
grace expires so a persistent bad signal is believed rather than re-refused
every fifteen seconds). Throttling (`TRACK_THROTTLE_MS` 3000, `TRACK_MOVE_M`
10) applies only past settle -- while acquiring, every accepted fix is an
improvement and is what redraws a wrong marker, so pacing it would only delay
the correction. `rememberPosition()` fires on the first accepted fix and at
settle, never during tracking.

Three deviations from the plan, all deliberate. **`getCurrentPosition`'s
defaults became `desiredAccuracyM: 500, deadlineMs: 12000`** rather than
waiting for Milestone 5 to set them at the call site: with the planned 50m/30s
defaults, the distance filter would have waited up to thirty seconds for a
GNSS lock to answer "within 5 km" for the three commits between here and there.
Milestone 5 still makes the call site explicit and adds the cache. **A
`LOCATE_CANCELLED` reason was added** so `cancel()` can settle the promise
instead of leaving it pending forever; it is deliberately absent from
`LOCATE_ERROR_KEYS` because it is never rendered, and Milestone 2 must
recognise it and say nothing. **An error after the first fix is reported
through `onError` and does not tear the watch down** -- a boat loses and
regains signal constantly, and failing on the first `POSITION_UNAVAILABLE`
would make tracking useless exactly where it is wanted.

Verified by `make ci` green and a new `tests/ui/geolocation.spec.js` -- twelve
tests, all passing, driving the module through a scripted
`navigator.geolocation` fake with a movable `Date.now` (the tracking guard is
written in elapsed time, and a test that waited out fifteen real seconds is a
test nobody runs). It asserts the options object directly, that the first fix
is delivered before any better one arrives, that a worse fix is dropped while
acquiring **and delivered once tracking** (the phase boundary, which fails
outright if the rule leaks past settle), that a lone 2800m reading is ignored
but a persistent one is believed, that a never-improving sequence still
settles at the deadline, that an empty one rejects with `timeout`, and that
`clearWatch` is called exactly once on every exit path. Crucially the fifteen
existing locate and distance-filter tests in `map.spec.js` pass **unmodified**
against the real Playwright geolocation path, which is what proves the two
callers were not disturbed.

## 2. The map fits the accuracy, and redraws as the fix improves

`web/js/components/map-view.js`. **This is where the reported bug stops
happening.** Acquisition only — tracking is Milestone 3, so this milestone
is reviewable as the bug fix on its own.

`bindLocate()` (line 1424) switches from `await getCurrentPosition()` to
`watchPosition({ onUpdate })` with `continuous: false`, where `onUpdate`
calls `showPosition(...)`. The `position-found` event still fires exactly
once, on the final fix, so `location-editor-page.js` keeps its contract.
Store the handle on the element and `cancel()` it in
`disconnectedCallback()` (line 784), beside the existing `_generation`
bump.

In `showPosition()` (line 1469), replace the unconditional
`zoom: HERE_ZOOM` with a fit: compute the zoom that puts the accuracy
circle's diameter inside the viewport and clamp with
`Math.min(fitZoom, HERE_ZOOM)`, so a good fix still gets zoom 15 and a
coarse one zooms out until the ring reads as a ring. `accuracyRing()`
(line 252) already produces the geometry; feed its bbox to
`cameraForBounds`/`fitBounds` with padding.

**Verification.** Extend the "the locate control" describe in
`tests/ui/map.spec.js` (~line 1336). (a) With the scripted fake: click
locate, assert `_hereMarker` is at the coarse coast position with
`_hereAccuracy` about 2800, then assert it moves to the 18 m position and
`_map.getZoom()` rises to 15. (b) A coarse fix is not drawn as a confident
dot: a single 3000 m fix must leave the zoom low enough that the ring's
projected pixel diameter fits the map container — measure with
`_map.project()` over the ring's own coordinates, the technique the
existing ring test already uses at `map.spec.js:1420-1435`. (c) A fix
arriving worse than the current one does not move the marker. The three
existing ring tests must pass unmodified; they are the regression gate on
`showPosition`.

**Done.** `showPosition()` now ends with
`jumpTo({ center, zoom: this.zoomForAccuracy(lat, lng) })`, and the new
`zoomForAccuracy()` fits the accuracy ring's own geometry through
`cameraForBounds` with 32px of padding, clamped by `Math.min(camera.zoom,
HERE_ZOOM)`. It reuses `accuracyRing()` rather than doing the metres-to-degrees
arithmetic a second time, so the camera cannot disagree with the ring it is
framing. `cameraForBounds` answers undefined for a map that has not been laid
out -- reachable here, since the button can be pressed before the first style
loads -- and that falls back to `HERE_ZOOM`, which is the honest answer for a
map with no viewport to fit to. `HERE_ZOOM`'s comment now records that it is a
ceiling rather than the answer, and why.

`bindLocate()` drives `watchPosition({ onUpdate })` with `continuous: false`,
redrawing on every improvement; `position-found` still fires exactly once, on
the settled fix, so the editor is never handed a coordinate that is about to be
improved on. A second press cancels the first watch rather than racing it, a
`LOCATE_CANCELLED` rejection is swallowed without a message (it means this
element went away or was superseded, neither of which is a failure), and
`disconnectedCallback()` cancels any live watch.

One deviation worth noting: the scripted `navigator.geolocation` fake was
extracted to `tests/ui/helpers/geolocation.js` rather than living in
`geolocation.spec.js`, because Milestone 2 needs the same fake from
`map.spec.js` and two copies would drift. Milestone 1's spec was refactored
onto it in the same commit and still passes unchanged otherwise.

Verified by `make ci` green and 115 passing tests across `map.spec.js`,
`geolocation.spec.js`, `map-theme.spec.js` and `locations.spec.js` -- including
all fifteen pre-existing locate and distance-filter tests, and the three
pre-existing accuracy-ring tests, unmodified.

Five new tests in `map.spec.js` under "a coarse fix is not drawn as a confident
dot". The load-bearing one measures the ring's *projected pixel* extent against
the map container and requires it to fit while staying above 20px, so neither a
ring off the edge of the world nor a ring shrunk to a dot can pass. It was
confirmed as a real negative control by temporarily restoring
`zoom: HERE_ZOOM`: the ring then measures **4019px across in a 744px map** --
5.4 times the viewport, which is precisely why it could never be seen -- and
the test fails on that number rather than on a timeout or a missing element.
The other four cover a good fix still landing at zoom 15 (the ceiling did not
become a new framing bug), a wrong first marker visibly correcting itself from
the coast out to the water as GNSS answers, a worse fix not bouncing the marker
back ashore, and every watch started being cleared once the element goes away.

## 3. The position keeps up while you move

`web/js/components/map-view.js`, plus one locale key. The feature proper.

`bindLocate()` passes `continuous: true`. Lifecycle, camera and button:

**Lifecycle — nothing to build.** Tracking lives on the element instance.
`trip-detail-page.js:141` renders the map with
`content.innerHTML = ...`, so a tab switch disconnects the element and
`disconnectedCallback` cancels the watch. A reload obviously starts clean.
So "do not resume tracking on return, require a fresh press" is already
the behaviour once Milestone 2's cancel is in place.

**Follow, do not re-zoom.** A new `_followCamera` flag, set true on each
locate press and set false by the existing `noteUserMovedMap` (line 1113)
while tracking. It must be separate from `_userMovedMap`, which is set
once per render and never reset — tracking needs "interacted SINCE the
last press". The existing mousedown/touchstart/keydown listeners sit on
`#map`, and the locate button is a sibling overlay rather than a
descendant, so pressing the button does not trip the flag; verified.

While `_followCamera` and past settle, each accepted fix pans the camera
to the position at the **current zoom** — never re-fitting, because after
settle the zoom is the one the initial fit chose. Once `_followCamera` is
false, the camera never moves on its own again; the marker and ring still
update, which is the honest half.

**The ring follows the marker.** Its geometry is redrawn per accepted fix.
Freezing it at the initial accuracy would make it state something untrue
about the current fix; only the *camera* is pinned after settle.

**Hazard: `map-view-change` fires on `moveend`** (line 1142) and
`trip-detail-page.js:144` turns every one into a `replaceState`. A
following camera would write history state roughly once per update. Guard
it with a `_programmaticFollow` flag checked in the `moveend` handler,
suppressing the emit for follow-pans **only** — the existing comment at
lines 1134-1141 deliberately emits for our own `fitBounds` so the first
view is restorable, and that must keep working.

**Pause when hidden.** `document.visibilitychange`: on `document.hidden`,
`clearWatch` while keeping the tracking *intent*; on visible again,
restart the watch. About fifteen lines, and it is the honest answer to
battery — a watch running with the screen off is pure waste. On resume,
stay in the tracking phase: do not re-run acquisition and do not touch the
camera. The bounded staleness guard from Milestone 1 covers the coarse
first fix that may arrive after a resume. Remove the listener in
`disconnectedCallback`.

**The button always recentres, never stops.** Pressing while tracking sets
`_followCamera = true` and recentres on the current fix, re-applying
Milestone 2's fit clamp so a degraded fix still shows its ring. It does
not restart acquisition and does not stop the watch. Indicate the state
with a `data-tracking` attribute driving a CSS tint — the same
tinted-when-active convention the trips and locations toolbars already use
— and switch the accessible name to `map.locate.recentre` (Recentre on my
location), which tells a screen-reader user both that tracking is on and
what the press will do. No new sprite icon, so the `gen_icon_sprite.py`
recipe is not needed. `aria-pressed` is deliberately avoided: a pressed
state that never becomes unpressed on press is a lie.

**Recorded trade-off.** Because the button never stops tracking, the only
ways to end a watch are leaving the map tab, reloading, or the visibility
pause. That is a deliberate choice, made when this stage was planned; the
visibility pause is what makes it acceptable, since screen-off and
tab-away are where the battery would actually go.

**Verification.** New tests in `map.spec.js`, scripted fake throughout.
(a) After settle, a sequence of three moving fixes at constant accuracy
moves `_hereMarker` three times and leaves `_map.getZoom()` unchanged —
this is the regression gate on the dropped only-improving rule, and it
fails outright if the boundary is wrong. (b) Panning the map mid-track
stops the camera moving (`_map.getCenter()` constant across two further
fixes) while `_hereMarker` still moves. (c) Pressing locate again after
panning recentres on the latest fix. (d) Dispatching a `visibilitychange`
with `document.hidden` stubbed true calls `clearWatch`; restoring
visibility starts exactly one new watch. (e) Navigating away from the map
tab calls `clearWatch` and starts none. (f) Follow-pans emit no
`map-view-change` while the initial fit still does — count the events.

## 4. Say how accurate it is, in words

`web/js/format.js` gains `formatDistance(metres)` using
`Intl.NumberFormat(undefined, { style: "unit", unit: "meter"|"kilometer", unitDisplay: "short" })`
— matching the file's existing undefined-locale convention
(`formatDateRange`, `formatMoney`), and adding **zero** i18n keys for the
units, so German gets 3,2 km for free.

New keys after `map.locate.unsupported` (en.json/de.json line 260),
rendered into the `.locate-status` element that already exists:

- `map.locate.refining` — Finding your location… accurate to about
  {distance} so far.
- `map.locate.coarse` (fix worse than about 200 m) — This is a rough
  position, accurate only to about {distance}. Out of sight of land your
  device may place you on the nearest coast. That last clause is the one
  that actually helps the person on the boat: it names the failure they
  are looking at.
- Nothing for a good fix, as today. No permanent tracking line either —
  the tinted button is the indicator.

Spell these keys out literally at the `t()` call site — do **not** compose
them, per the unused-key-scan caveat documented at `geolocation.js:16-23`.

**Verification.** After a coarse-only fix the status is visible and its
text matches `/\d/` and `/km|m\b/`; after an 18 m fix it is hidden. During
tracking, a fix that degrades past 200 m brings the coarse line back and a
subsequent good fix hides it again. Direct `formatDistance` checks via
`await import("/js/format.js")` at the 35 m / 300 m / 2800 m / 12000 m
boundaries. `make ci` catches a missing German key.

## 5. The distance filter asks for less, and caches honestly

`web/js/pages/locations-tab.js:397` becomes
`getCurrentPosition({ desiredAccuracyM: 500, deadlineMs: 12000, maxAgeMs: 30000 })`.
The filter is radii in kilometres, so half a kilometre is already far
better than it needs and waiting 30 s for a GNSS lock to answer "within
5 km" is absurd. This is also the one caller that must stay
non-continuous. Drop the closure-scoped `devicePosition` (which never
expires — arguably worse than the old 60 s `maximumAge`) in favour of
Milestone 1's module cache.

**Verification.** The existing "distance filter on the locations list"
describe (`map.spec.js:1620` onwards) passes untouched, plus one new
assertion that choosing a radius twice does not start a second watch, and
one that it never requests `continuous: true`.

## 6. The editor flags a coarse fix

`web/js/pages/location-editor-page.js`. Inline, not a dialog at save: by
save time the coordinates may have been dragged or geocoded since, and a
blocking modal punishes the user furthest from the cause.

`takeCoordinates` already distinguishes its sources — `location-picked`
(a click or drag, no accuracy) from `position-found` (line 585, carries
`accuracy`). Have the `position-found` listener render a persistent line
beside the coordinate fields when `accuracy > 200`, alongside the existing
`.location-form__hint` / `.location-form__pick-hint` pair (lines 190,
199): Saved from a rough position, accurate to about {distance} — drag the
pin if you know better. `coordinatesChanged()` clears it, so an edit by
any route removes it. New key `location.form.coarseFix` in both locales.

Note the editor's picker stays **non-continuous**: it is recording where a
place *is*, not where you are going, so a live watch there would fight the
user's own pin drags.

**Verification.** Extend the existing editor locate test
(`map.spec.js` ~1540): a 3000 m fix fills lat/lng *and* shows the warning
naming a distance; a 20 m fix leaves it hidden; dragging the pick marker
afterwards hides it. Assert on `hidden`/`textContent`, no screenshot.

## Build order

1. **Milestone 1** — the primitive. The only milestone that can break
   every caller, and the only one worth testing in isolation.
2. **Milestone 2** — where the reported bug stops happening. Acquisition
   only, so it is reviewable as the fix without the new feature on top.
3. **Milestone 3** — continuous tracking. Depends on both; the largest.
4. **Milestones 4, 5, 6** — independent of each other; each could be
   reordered or dropped without leaving the tree inconsistent.

## Deliberately out of scope

`primeMapTheme()` (`map-theme.js:144-161`) is left exactly as it is. It is
permission-gated, silent, and exists only to tint a map; spinning up GNSS
for that would be indefensible, and its `maximumAge: 3600000` is correct
for its purpose. It does not import `geolocation.js`.

**One trap it does create, and continuous tracking sharpens it.**
`rememberPosition()` is called from `geolocation.js:97` on every
successful fix, and it calls `announce()`, which drives the map-theme
listeners and `scheduleNextTransition()`. A continuous watch would fire it
once per update indefinitely, potentially triggering repeated
`restyle()`/`setStyle` calls. Call it on the **first accepted fix and at
settle only**, and never during tracking. Day/night tinting does not care
about 3 km, let alone about where you are thirty seconds later.

Reverse geocoding over water is also out of scope. `Reverse()` sends no
`zoom`, so Nominatim uses its default zoom 18 (building level) and answers
a mid-sea point with an error object, which becomes `ErrNoResult`, a 404,
and "No address found for this point." Currently correct-by-design and
separately documented; a different complaint from the one reported. It
goes to `plans/todo.md` instead.

Heading and bearing (the direction cone Google Maps draws) is out of
scope: it needs the device-orientation permission, which is a separate
prompt and a separate set of failure modes. `plans/todo.md` if wanted
later.

## Testing risk to record

Playwright's `context.setGeolocation` does push to active `watchPosition`
handlers in Chromium, but delivery timing is not contractual and a
*sequence* of accuracies cannot be set without sleeping between calls.
Hence the scripted `navigator.geolocation` fake for Milestones 1-4 — and
keep exactly one end-to-end test on the real `test.use({ geolocation })`
path (the existing Reykjavik one) so the fake is never the only thing
under test.

## Workflow

The loop from `CLAUDE.md`: implement, verify with `make ci` plus a real
browser pass, add a **Done.** paragraph to the milestone's section here,
update `plans/todo.md` in both directions, one commit per milestone,
leave `make dev` running, hand back and wait.

## Verification

- `make ci` green at every milestone — build, vet, JS syntax,
  `scripts/check_i18n.py` proving both locales moved together, `go test`.
  No Go changes in this stage, so the Go half is a regression gate only.
- `npx playwright test tests/ui/geolocation.spec.js tests/ui/map.spec.js`
  green in both locales.
- Manual pass against `make dev` + `make dev-seed` at 324×756, using
  Chrome DevTools' sensor override: set a mid-sea coordinate with a large
  accuracy — the marker lands on the water, the map is zoomed out far
  enough that the ring is visibly a ring, and the status names the
  distance. Then move the override along a track and watch the marker
  follow without the zoom changing; pan the map and watch the camera stop
  while the marker keeps moving; press the button and watch it recentre.
- Battery and lifecycle check by hand: switch to another trip tab and
  confirm via a `clearWatch` counter that the watch stopped; background
  the tab and confirm the same.
- Land regression, same session: a Reykjavik fix still feels instant on
  first press and still settles at zoom 15 with a small ring.
