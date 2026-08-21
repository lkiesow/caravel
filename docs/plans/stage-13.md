# Stage 13 — The map grows up

## Context

`web/js/components/leaflet-map.js` has not changed shape since Stage 01: it is
attribute-driven and strictly **read-only**. It renders markers from either
`GET /trips/{id}/map` or a `lat`/`lng` pair, and that is all it does — no click
handler, no way to report a position back, no notion of where the user is.
Three `todo.md` entries are blocked on that one limitation; a fourth is a
confirmed bug in the same file's mobile CSS; and a fifth thing is simply
missing from its marker popups:

- **A marker popup is a dead end.** Clicking a marker on the trip map shows the
  title and a "View on Google Maps" link — and no way to reach the location
  *inside Caravel*. The popup at `leaflet-map.js:277` renders only
  `item.title` and `item.google_maps_url`, even though the payload already
  carries `item.id`.
- **The mobile map swallows vertical scrolling.** (Bug, Stage 07.) The
  `@media (max-width: 640px)` block at `leaflet-map.js:104` gives `#map`
  `height: 50vh; min-height: 16rem` with the legend moved *below* it. At
  324×756 that leaves almost no page under the map, so a one-finger drag
  anywhere in the lower half pans the map instead of the page, and the legend
  sits just below the fold with nothing hinting it exists.
- **Click-to-pick coordinates.** (Stage 06.) `location-editor-page.js:84-95`
  takes latitude and longitude as two raw `<input type="number" step="any">`.
  Fine for pasting, unpleasant on a phone.
- **Use the device's location.** (User's notes.) Show your own position, centre
  on it, and filter the locations list by distance from it.

Doing these separately would mean touching the same 300-line component five
times. Doing them together means one deliberate rework: `leaflet-map.js` gains
an interaction surface, and the features are then thin callers of it.

**Decided with the user up front:**

1. **An external geocoder is acceptable**, opt-in per search — no per-keystroke
   requests. OSM's Nominatim, the same project the tile layer already comes
   from.
2. **`localhost` is enough to verify geolocation.** `navigator.geolocation`
   needs a secure context; `http://localhost` counts, so desktop Playwright and
   a local browser can prove it works. Phone testing over plain HTTP will
   silently do nothing — that stays the existing `todo.md` deployment entry
   about HTTPS, and the feature must degrade honestly rather than hang.
3. **No new dependency.** Leaflet is vendored (`web/js/vendor/leaflet/`) and
   image-free on purpose; gesture handling and the locate control are written
   here rather than pulled in as plugins.

---

## 0. Land the plan

Copy this document to `docs/plans/stage-13.md` before any code, per `CLAUDE.md`.

---

## 1. The mobile map stops swallowing the page

The confirmed bug, and it goes first because Milestones 2–6 all add controls to
this component — sizing them against a layout that is about to change would be
wasted work.

Three decisions, all inside `leaflet-map.js`'s `styles` block and `render()`:

- **Height.** Cap the mobile map instead of letting it take `50vh` of a 756px
  screen: `height: min(50vh, 20rem)` in the ≤640px block, so there is real page
  below it to grab. `:host([lat])`'s 16rem single-marker mode already behaves
  and stays as it is.
- **Legend above the map, not below.** It is currently `position: static` after
  `#map`, which puts it off-screen. Moving it before the map in the mobile
  layout (`flex-direction: column-reverse` on `.map-wrap`, so the markup order
  and the desktop absolute positioning are both untouched) makes it visible
  without scrolling and gives the page a non-map strip to start a drag in.
- **Two-finger pan on coarse pointers.** Construct the map with
  `dragging: !isCoarsePointer` (`matchMedia("(pointer: coarse)")`), then add a
  `touchstart`/`touchend` pair on the container that enables `map.dragging`
  only while `e.touches.length >= 2`. A one-finger drag then scrolls the page,
  which is what the user expects; two fingers pan the map. Add a translated
  hint line under the map on coarse pointers only
  (`map.twoFingerHint`, "Use two fingers to move the map"), so the changed
  gesture is discoverable rather than mysterious.

**Verify:** `make ci`; a new `tests/ui/map.spec.js` at 324×756 asserting the
things the bug is made of — the map's bounding box height is ≤ 320px, the
legend's `y` is *above* the map's, page content exists below the map's bottom
edge, and (with `hasTouch: true`) that `document.scrollingElement.scrollTop`
actually changes after a one-finger drag starting inside the map. That last
assertion is the bug itself and should fail on today's code.

**Done, with one simplification the plan did not anticipate.** The gesture half
needed no `touchstart`/`touchend` juggling at all. Constructing the map with
`dragging: !isCoarsePointer()` is the whole fix: with Leaflet's Drag handler
off, a one-finger touchmove is never consumed and the page scrolls normally,
while two fingers still **pan as well as zoom**, because the touchZoom handler
(left enabled) applies the pinch centre's delta even when the pinch scale is
exactly 1 — `TouchZoom._onTouchMove` in the vendored `leaflet.esm.js`. The
plan's approach would in fact have been wrong: enabling `map.dragging` during a
`touchstart` is too late for Leaflet's own listener to see that same gesture.

The layout half landed as planned but via `order: -1` on the legend rather than
`column-reverse` on `.map-wrap` — same result, and it does not silently reverse
anything else added to that container later. `:host` became a column flex box
so the new hint line takes its own height instead of being pushed past the
component's height at widths above 640px where a coarse pointer is still
possible. Measured at 324×756: the map is **320px** (was 424) and the legend
now sits at **y=549, above** the map at y=619 — it used to render at y=769,
past the fold. 138px of the visible 756px screen is map, down from 373px.

`map.twoFingerHint` is the one new key per locale (155 in sync).

Verified: `make ci` green; `make test-ui` green, 32 tests (was 29). The new
`tests/ui/map.spec.js` asserts the three faults separately — the map's cap, the
legend being above it, and how much of the visible screen is map; then that
`dragging` is off while `touchZoom` stays on and the hint renders; then a
desktop regression guard that a fine pointer keeps click-and-drag panning and
gets no hint. `scripts/without.sh` confirms both mobile tests fail on the
pre-change file (the desktop one passes, correctly — it guards, it does not
accuse).

One thing to know about that spec: Playwright drives **Firefox only** here, and
Firefox device emulation cannot flip `(pointer: coarse)` — only Chromium's
`isMobile` does. So the touch device is emulated by stubbing `window.matchMedia`
for that one query via `addInitScript`. That stubs the *input*; everything
asserted afterwards is the component's real response to it. Noted in
`todo.md` as a genuine limit rather than papered over.

---

## 2. A marker popup links into the app

Small, self-contained, and deliberately placed before the pick-mode rework so
it lands on today's popup code rather than tangling with it. **No backend
change** — `mapItemResponse` (`internal/httpapi/map.go:11`) already sends `id`.

- The trip-wide popup (`plotMarkers`, `leaflet-map.js:277`) gains a
  `/trips/{tripId}/locations/{item.id}` link above the existing Google Maps
  one, so the primary action is "open this in Caravel" and the external map is
  the secondary. `tripId` is already on the element as the `trip-id`
  attribute. The single-marker mode (`:262`) is untouched — it is *already* on
  that location's page, so a link to itself would be noise.
- **`data-link` cannot work here, and this is the part to get right.** The
  router intercepts navigation with a `document`-level listener doing
  `e.target.closest("[data-link]")` (`router.js:56`), but a click inside a
  shadow root retargets `e.target` to the `<leaflet-map>` host, so the link
  inside would never be found and the click would fall through to a **full
  page reload**. Instead the component binds the popup link itself and
  dispatches the existing `item-open` contract — `bubbles: true, composed:
  true, detail: { itemId }`, exactly as `location-card.js:76` does — which
  `trip-detail-page.js` handles by calling `navigate()`, mirroring
  `locations-tab.js:96`. One event shape for "open a location", not two.
- Keep it a **real `<a href>`** and intercept only unmodified left-clicks, for
  the reason `itinerary-tab.js:187` already spells out: middle-click,
  open-in-new-tab, "copy link address" and the status bar all come free with a
  link and none of them with a button. A ctrl/cmd/shift/middle click falls
  through to native navigation, which resolves fine because the SPA serves that
  route.
- Tap target: Leaflet's popup content is inside the shadow root, so the 44px
  floor has to come from the component's own `styles` block, the same way the
  legend's did in Stage 09 Milestone 6 (`--tap-min` pierces the shadow
  boundary; the box-sizing reset does not).

**Verify:** `make ci`; a test in the new `tests/ui/map.spec.js` — open the
`full` trip's Map tab, click a marker, click the in-app link in the popup, and
assert `window.location.pathname` became the location's view route **and** that
no page load happened (stamp a sentinel on `window` beforehand and assert it
survived — a full reload is exactly the failure mode `data-link` would have
caused, and a pathname assertion alone would not catch it).

**Done.** No backend change, as planned — `item.id` was already in the payload.
The trip-wide popup is now the title, then **Open location**, then View on
Google Maps: two labelled destinations, in-app first. The single-marker mode is
untouched, since it is embedded on the very page it would link to.

The click handler is **delegated on the map container** rather than bound per
popup, which the plan did not specify but which the DOM forces: Leaflet builds
and destroys popup markup on demand, so at bind time there is nothing to bind
to. It dispatches the existing `item-open` contract (`bubbles`, `composed`,
`{ itemId }`) that `location-card.js` already uses, and `trip-detail-page.js`
turns it into a `navigate()` exactly as `locations-tab.js` does — one event
shape for "open a location", not two. The link stays a real `<a href>` and only
an unmodified left-click is intercepted, so ctrl/⌘/shift/middle-click keep
working. `map.openLocation` is the one new key per locale (156 in sync).

Two keys per locale and one CSS block: the popup links are `display: block` so
each is its own row, and carry `min-height: var(--tap-min)` at ≤640px. That
last part is **not** something the sweep would have caught — `routes.spec.js`
skips anything under a `leaflet-*` *ancestor*, and Leaflet renders popup
content inside `.leaflet-popup-content`, so markup we own is invisible to it
there. `map.spec.js` measures the two links directly instead, and `todo.md`'s
entry about that exclusion has been amended to say so.

Verified: `make ci` green (156 keys); `make test-ui` green, 35 tests (was 32).
Three new tests: the client-side navigation, a ctrl-click falling through
without `preventDefault`, and both links measuring 44px at 324px.
`scripts/without.sh` fails all three on the pre-change files.

The negative check worth recording is the second one. The plan predicted that
a pathname assertion alone would not catch a `data-link` implementation, and
that is exactly what happened when it was tried deliberately: with the shadow-
root handler removed and `data-link` on the anchor, the URL assertion **passed**
— the browser did a full document load and landed on the right route — and only
the `window.__caravelNoReload` sentinel failed, with the message naming the
cause. Measured in German at 324px as well: the popup is 192px wide, no
document overflow, both links 44px.

---

## 3. `leaflet-map.js` learns a pick mode

Component work only — no page uses it yet, which keeps the diff reviewable.

- New boolean attribute `pick` (added to `observedAttributes`). In pick mode
  the component: shows a single draggable marker, moves it on map click, and
  dispatches a bubbling, composed `location-picked` `CustomEvent` with
  `{ lat, lng }` on every change. `composed: true` matters — the event has to
  cross the shadow boundary to reach the editor page.
- The existing `lat`/`lng` attributes keep driving the marker's position, so
  the editor's number inputs and the map stay two-way bound through attributes
  rather than through a second state copy. Guard the resulting loop:
  `attributeChangedCallback` currently calls the full `load()`, which tears
  down and rebuilds the whole map — in pick mode a lat/lng change must only
  move the marker.
- With no coordinates yet, pick mode opens on the world view (`setView([20,0],
  2)`), same as the empty trip map.
- Precision: round emitted coordinates to 6 decimals (~11 cm) so a click does
  not write a 17-digit float into a form field.

**Verify:** `make ci`; extend `map.spec.js` — mount a `<leaflet-map pick>` on a
scratch page, click at a known offset inside it, and assert a `location-picked`
event fired with plausible finite numbers; then set `lat`/`lng` attributes and
assert the marker moved without the map being re-created (e.g. the tile pane
element identity survives).

**Done.** `pick` is a boolean attribute on `observedAttributes`, settled at the
top of `load()` before anything else, since it also reads `lat`/`lng` — but as
a starting point meant to be moved rather than as the fixed embed the
single-marker branch renders. It never fetches. Clicking the map emits
`location-picked` (bubbling, composed, `{ lat, lng }`); the marker is
`draggable` and emits the same on `dragend`. The component deliberately does
**not** move its own marker on click — the page owns the coordinates and feeds
them back as attributes, which is what makes Milestone 4's two-way binding a
single source of truth rather than two copies.

The rebuild guard the plan asked for is `if (this._map) { syncPickMarker();
return; }`: once the map exists a coordinate change only moves the marker,
because the inherited behaviour is a full `load()` and a fresh `innerHTML`, and
the editor rewrites those attributes as its number fields are typed in.

Four things the plan did not specify, each a decision rather than a detail:

- **Recentring.** On first render the map centres on the point at
  `SINGLE_MARKER_ZOOM`; afterwards it moves only if the point has left the
  visible bounds. Recentring on every change would yank the map per keystroke;
  never recentring would let the marker silently vanish off the edge.
- **`wrapLatLng` on every emission.** Panning the world sideways yields
  longitudes past ±180, which no tile server or database column wants.
- **Empty is not zero.** `readCoordinate()` treats an absent, blank or
  unparseable attribute as `null` and removes the marker, because `Number("")`
  and `Number(null)` are both `0` — a real place in the Gulf of Guinea. An
  emptied field means "nothing chosen", not "chosen: null island".
- **The pick marker is a ring, not a fourth category dot**, in amber
  (`#ea580c`) — not `--color-accent`, which is the same `#2563eb` transport
  already uses. `:host([pick])` fixes the height at 20rem regardless of whether
  coordinates exist yet, so the editor card will not jump on first click; it is
  placed after `:host([lat])` because equal specificity makes source order the
  tie-breaker. One new key per locale, `map.pickMarkerLabel`, since a draggable
  marker is a control and Leaflet gives it focus but no name (157 in sync).

Verified: `make ci` green; `make test-ui` green, 40 tests (was 35). Five new
tests: the click path (world view and no marker when empty, one finite wrapped
coordinate at ≤6 decimals, marker appearing once fed back), a real
`page.mouse` drag reporting a point south-east of where it started, the
no-rebuild assertion, clearing a coordinate, and the absence of the legend and
empty-state. The drag test uses real input rather than dispatched `MouseEvent`s
— Leaflet's `Draggable` ignored the synthetic ones, and a genuine drag is the
claim anyway.

Negative check: deleting the four-line rebuild guard fails **exactly** the
no-rebuild test ("the Leaflet map was re-created") with the other four still
passing — a precise assertion rather than a blanket one.

**Verifying this milestone turned up two suite-level faults**, both of which
cost more time than the milestone itself and are worth recording in full.

*One is a genuine race, diagnosed and fixed.* `settings.spec.js`'s German
324px sweep began failing intermittently, asserting the `h1` read
"Kontoeinstellungen" and getting "Anmelden" — the login page. Nothing to do
with the map: that test logged in as `OTHER_USER`, and the password-change test
in the same file changes `other`'s password, which by design deletes **every**
session that user has. Whenever the two overlapped across workers, the sweep
was logged out mid-test. It was always a race; the suite growing from 35 to 40
tests changed the scheduling enough to expose it. The sweep needs nothing from
`other` — only an account whose `/settings` shows all three cards — so it now
uses the demo user and `auth.setup.js`'s shared session, which also spends no
login attempts against the rate limiter. The reasoning is a comment in the
test, since the next person to reach for `OTHER_USER` there would reintroduce
it.

*The other is only partly understood, and the fix is a coverage decision rather
than a diagnosis.* `routes.spec.js`'s clipped-content sweep intermittently
reported `.map-wrap` holding content wider than its box — 1163 vs 744 on
desktop, 414 vs 292 on mobile, the text being Leaflet's own zoom control and
attribution. It appeared roughly once in three **full** runs and never once in
six isolated runs of `routes.spec.js` alone, and a probe that reloaded the map
route 25 times under a concurrent full-suite run failed to reproduce it at all.

What is certain is that the sweep should not have been measuring that box.
`.map-wrap` is ours, so the existing `[class*="leaflet-"]` exclusion does not
cover it — but its entire purpose, per the comment that has been on it since
Stage 07, is to clip panes Leaflet parks at enormous offsets *by design*
(measured at right=1825757). Its content width therefore reports the library's
internals one level up from where they are already excluded. So the sweep now
also skips any element that **contains** a `.leaflet-container`, matched that
way rather than by our class name so it cannot quietly grow to cover something
else. Five consecutive full runs are green afterwards.

Two honesty notes on that. First, this removes a class of false failure; it
does not explain the timing, and a bisect was inconclusive — the pre-Stage-13
component ran four clean rounds, but that comparison is confounded, because at
that revision `map.spec.js`'s newer tests fail and change the load the rest of
the suite runs under. Second, a single "view location: no headings at all"
failure was also seen once and is **not** addressed by any of this; it is
recorded in `todo.md` rather than being quietly folded in here.

The exclusion was checked for over-reach rather than assumed safe: forcing the
legend — our markup, a child of `.map-wrap` — to 500px inside a 290px box still
fails the sweep, naming `div.map-wrap > div.legend` exactly.

**`leaflet-map.js` also gained a `data-ready` attribute**, set on itself once
the map has laid out and cleared while it rebuilds, with `gotoRoute` waiting on
it. This was written as a fix for the flake above and **did not fix it** — said
plainly because the commit would otherwise imply it did. It is kept on its own
merits: Leaflet is lazily imported *after* a route's fetches settle, so
"fetches quiet plus two frames" genuinely does not mean "the map is up", and
this is the small, component-scoped version of the ready signal `todo.md` has
been asking for app-wide.

---

## 4. Picking coordinates in the location editor

The user-visible half of Milestone 3.

- In the Location card (`location-editor-page.js:81-102`), keep the two number
  inputs — pasting coordinates must stay possible — and add a
  `<leaflet-map pick>` below them, plus a `location.form.pickHint` line.
- `renderLocationForm()` (`location-editor-page.js:291`) already syncs the
  "Show on map" hint from `input` events on `lat`/`lng`; extend that one
  function in both directions: inputs → map attributes, and `location-picked` →
  input values (then fire the existing `syncHint`, so ticking coordinates in by
  map lights up the same hint).
- `readLocationForm()` (`:281`) is unchanged — it reads the inputs, which
  remain the single source of truth. This deliberately keeps the picker out of
  the save path.
- The editor page is already in the route sweeps
  (`tests/ui/helpers/scenarios.js`'s `buildRoutes`, "edit location" and "new
  location"), so the new map is swept for overflow and tap targets from this
  milestone on — including the 324px case Milestone 1 just fixed.

**Verify:** `make ci`; a mutating UI flow in `map.spec.js` following the
`files.spec.js` / `settings.spec.js` shape — create a trip in `beforeEach`,
delete it in `afterEach` — that opens a new location, clicks the picker map,
asserts both number inputs became non-empty, saves, and asserts the persisted
item's `location.lat`/`lng` match what the inputs showed.

**Done.** The Location card keeps its two number inputs — pasting coordinates
had to stay possible — and gains a `<leaflet-map pick>` under them with a
`location.form.pickHint` line (158 keys in sync). `readLocationForm()` is
untouched: it still reads the fields and nothing else, so the map cannot
contribute to a save except by writing into them first. That is what makes the
fields authoritative rather than there being two copies of the value.

One deviation, and it removes a race rather than being cosmetic: the initial
coordinates are rendered **onto the element in the page template**, not pushed
from `renderLocationForm()` after mount. Pushing them afterwards would have the
map open on the world view and then jump to the point, and would also collide
with the component's own `connectedCallback` load — survivable, since
`leaflet-map.js`'s generation counter discards the losing render, but pointless
when the attribute can simply be there from the start. There is a test for the
resulting behaviour ("opens on its own point, not the world view").

The fields→map direction removes the attribute for a blank field rather than
setting an empty one, matching `readCoordinate()` on the other side: "no
coordinate" and "the coordinate 0" have to stay different answers. No loop
between the two directions — setting the attributes only moves the marker, and
`location-picked` is only ever emitted by a click or a drag.

Verified: `make ci` green; `make test-ui` green, 43 tests (was 40), three
consecutive full runs. Three new tests, in the mutating-flow shape
`files.spec.js` established (own trip created in `beforeEach`, deleted in
`afterEach`): a map click filling both fields and the **saved item** carrying
those exact coordinates, typing moving the marker and clearing removing it, and
an existing location opening zoomed on its own point. `scripts/without.sh`
fails all three on the pre-change page.

Two things worth recording. The persistence assertion first read
`/trips/{id}/items`, which returns a summary **without** the nested location —
so it read `undefined.lat` rather than a wrong coordinate; it now reads the
item's own route, which is where `location` actually lives. And the editor's
two routes were already in the sweeps, so the picker has been swept from this
milestone on: measured in German at 324×756, no document overflow, the hint
wrapping to three lines inside 258px and the map 258×320.

---

## 5. Address search, through a backend geocode proxy

The user approved Nominatim, opt-in per search. It goes through **our** server,
not straight from the browser, for three reasons that are worth stating: OSM's
usage policy wants an identifying User-Agent and ≤1 req/s, which a browser
cannot promise; a self-hosted app should not silently hand a user's typing to a
third party the moment a page loads; and it gives one place to turn the feature
off.

- **Config:** `CARAVEL_GEOCODER_URL` in `internal/config/config.go`, defaulting
  to `https://nominatim.openstreetmap.org/search`. Empty string disables the
  feature outright.
- **Handler:** `GET /api/geocode?q=` in a new `internal/httpapi/geocode.go`,
  behind `auth.RequireAuth` and its own rate limiter (`router.go:46`'s
  `rateLimitLogin` is the shape to copy — a separate, stricter limiter, since
  the budget being protected is an external service's). Sets a `User-Agent`
  identifying Caravel, applies a short `context.WithTimeout`, and maps the
  upstream response down to `[{display_name, lat, lng}]` rather than proxying
  it verbatim.
- **Disabled state:** expose the flag so the frontend can hide the search box
  instead of rendering a control that 404s. `userResponse.has_password`
  (Stage 12 Milestone 5) is the precedent for gating a control on a
  server-reported capability; a `geocoding` boolean on the same payload keeps
  it to one fetch the app already makes.
- **Frontend:** a search input + button above the picker map, submitting on
  Enter or button press only. Results as a short list; picking one sets the
  coordinates *and* pre-fills the empty `address` field. New `search` icon
  already exists in the sprite.

**Verify:** `make ci` with Go tests in `internal/httpapi` against an
`httptest.Server` standing in for Nominatim — a result list mapped correctly, a
timeout surfacing as a clean error rather than a 500 stack, the rate limiter
returning 429, an empty `q` rejected, and the endpoint 404/disabled when
`CARAVEL_GEOCODER_URL` is empty. No test may hit the real Nominatim. Then one
manual search against the live service.

---

## 6. Where am I

`navigator.geolocation` at last, in the two places it is worth having.

- **`leaflet-map.js` gains a "here" layer:** a `showPosition(lat, lng,
  accuracy)` method drawing a distinct marker (not a category colour) plus an
  `L.circle` accuracy ring, and a locate button rendered inside the map when a
  new `locate` attribute is present. New icon `locate-fixed` — needs the
  `ICONS` + `gen_icon_sprite.py` recipe in `CLAUDE.md`, diffing for
  byte-identical existing symbols.
- **Trip map tab** (`trip-detail-page.js:126`) gets `locate`: press it to show
  and centre on your position.
- **Picker** (Milestone 4) gets a "Use my location" button that sets the
  coordinates directly — the single most useful case, standing somewhere and
  recording it.
- **Failing honestly is the requirement, not an edge case.** Insecure context,
  permission denied, and timeout are three different messages
  (`map.locate.insecure` / `.denied` / `.timeout`), and the insecure-context
  one names the cause, because over plain HTTP on a phone the API does not
  error — it does nothing. Feature-detect `navigator.geolocation &&
  window.isSecureContext` and disable the button with an explanatory line
  rather than offering a control that cannot work.

**Verify:** `make ci`; `map.spec.js` using Playwright's geolocation
override (`context.grantPermissions(["geolocation"])` +
`setGeolocation`) — assert the here-marker appears at the mocked coordinates
and the map centred there; then the denial path with permissions revoked,
asserting the translated error is shown and no marker appears.

---

## 7. Filter the locations list by distance

Cheap, because `locations-tab.js` already loads every item and filters entirely
client-side (`matches()` at `:61`) — no backend change at all.

- A second `renderMenu` in the toolbar next to the existing category filter
  (`locations-tab.js:49` is the pattern to copy), with radii
  1 / 2 / 5 / 10 / 25 km and an "any distance" neutral value.
- Needs coordinates, so the menu is only enabled once a position is known —
  reuse Milestone 6's guarded helper, extracted to `web/js/geolocation.js` so
  the map component and this tab share one code path and one set of error
  messages.
- A haversine in that module; **locations with no coordinates stay visible**
  when a radius is active, because hiding them would make an unrelated data gap
  look like a distance result. Say so in a hint line rather than in a comment.
- The toolbar is explicitly a single non-wrapping row that fits 324px (see the
  comment at `locations-tab.js:11`). A third control does not fit as a labelled
  button — it must be icon-only like the funnel, and the 324px sweep in
  `routes.spec.js` is what proves it.

**Verify:** `make ci`; `map.spec.js` with a mocked geolocation near one seeded
location — pick 5 km, assert the visible `item-card` count drops and that the
near location survives while a far one does not. Plus the existing 324×756
overflow sweep, which now has to pass with three controls in that row.

---

## 8. Sweep-up

- German pass at 324×756 over every surface this stage touched — the picker
  card, the geocode results, the locate errors, the distance menu. German is
  the longer language and the locate error strings are long sentences.
- `python3 scripts/i18n.py unused` clean.
- `tests/ui/contrast.js --min 4.5` over the map tab and the location editor in
  both schemes — the new markers, the here-ring and the locate button are all
  new colours on a photographic background.
- `docs/plans/stage-13.md`: a **Done.** paragraph per milestone.
- `docs/plans/todo.md`, both directions: delete the *mobile map swallows
  scrolling* bug, the *click-to-pick coordinates* entry and the *use the
  device's location* entry; sharpen the *HTTPS in deployment* entry from
  "becomes a concern if" to "is now a concern, because", since the feature now
  exists; add whatever this stage defers.

---

## Build order

1 → 2 → 3 → 4 → 5 → 6 → 7 → 8. One commit per milestone; a same-day fix on a
milestone gets its own "… follow-up: …" commit.

## Workflow

Per `CLAUDE.md`: implement → verify (`make ci` green **plus** a
Playwright/manual pass proving behaviour changed, assertions preferred over
screenshots) → update `docs/plans/stage-13.md` and `docs/plans/todo.md` →
commit → leave `make dev` running → **stop and wait** for the go-ahead before
the next milestone.

Note from Stage 12 Milestone 5: Milestone 5 is a backend change, so
`make dev-restart` is required before testing it — a stale binary reads exactly
like a missing feature.

## Verification (stage level)

- `make ci` green: build, vet, JS syntax, i18n key parity, `go test` (now
  including the geocode handler).
- `npx playwright test` green, including the new `tests/ui/map.spec.js` and the
  existing route sweeps across desktop + 324×756 × light + dark.
- Manual: on a phone-sized window, one finger scrolls the page over the map and
  two fingers pan it; a marker popup opens the location in-app without a page
  reload, and middle-clicking that link still opens a new tab; clicking the
  picker fills both coordinate fields and saving persists them; an address
  search finds a real place; the locate button puts you on the map on
  `localhost` and explains itself when denied.

## Out of scope

Reverse geocoding (coordinates → address text), a self-hosted Nominatim,
Leaflet's own sub-44px zoom controls (a standing `todo.md` entry — restyling a
dependency's internals to satisfy our sweep is still the tail wagging the dog),
server-side distance filtering, and HTTPS in deployment.
