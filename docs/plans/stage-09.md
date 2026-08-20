# Stage 09 — Don't lose the user's input

## Context

Stages 07 and 08 built the machinery to *find* problems: a Playwright pass over
desktop/mobile/dark mode (Stage 07) and a checked-in UI suite, seed scenarios,
non-vacuity tooling and cross-user ownership tests (Stage 08). What they found
and deliberately deferred has piled up in `docs/plans/todo.md`, and the largest
cluster in there is not cosmetic — it is **input the app silently throws away**:

- The location editor has **five independent save paths**. The visually primary
  "Save" in the Basic info card ([location-form.js:85](web/js/components/location-form.js#L85))
  sends only `title/category/type/notes/show_on_map`. Coordinates typed into the
  Location card two cards below are submitted by *their own* button
  ([location-editor-page.js:297-300](web/js/pages/location-editor-page.js#L297-L300));
  press the big Save and they are discarded with no warning and no
  unsaved-changes guard.
- The **create** form has the opposite shape — one button that commits
  everything — but non-atomically: `flushStaged`
  ([location-editor-page.js:195-261](web/js/pages/location-editor-page.js#L195-L261))
  fires up to six follow-up requests after the item POST and, on failure, reports
  a raw `window.alert` and dumps the user on the edit page with a
  half-populated location. So the two forms teach contradictory habits, and
  neither is safe.
- `show_on_map` gates the map **in Go**
  ([sqlite_store.go:452](internal/db/sqlite_store.go#L452),
  [postgres_store.go:510](internal/db/postgres_store.go#L510)) but its checkbox
  sits in Basic info, several cards above the coordinates it gates. Saving
  coordinates with it off yields an empty map and no explanation.
- There is **no modal component at all** — 5 `window.confirm` + 2 `window.alert`
  sites, which on mobile render as a "localhost:8080 says" system dialog, and
  the image-URL failures leak untranslated Go error text.
- There is **no not-found route**: `router.js:31-38` silently redirects
  unmatched paths to `/trips`, and the three per-page fetch-failure fallbacks
  render bare unstyled text with no `.page` wrapper and no `<h1>`.
- Nothing shows a **loading state** — `common.loading` has exactly one caller
  ([app.js:62](web/js/app.js#L62)) — so every navigation flashes an empty or
  partial page. The UI suite had to add a fetch counter to work around it.
- **Nothing meets the 44px tap-target guideline**; the suite pins the measured
  40px floor as a regression guard, not the guideline.

Outcome: after this stage a location is edited through **one Save that commits
everything atomically**, destructive actions and errors are in-app and
translated, routes say what they are doing while loading, unknown URLs look
deliberate, and touch targets meet the guideline with the suite's constant
raised to match.

Deliberately **out of scope** (staying in `todo.md`): the map click-to-pick
picker and geocoder, the ⋮ contextual menu / checklist editing, the
`user-menu.js` → `menu.js` refactor, multi-user sharing, the `item` → `location`
*identifier* rename, `sw.js` syntax checking, build-SHA stamping, migration
squashing.

---

## 1. Nested, transactional item create/update (backend)

Make one request able to commit an item plus its location, links and dates.

- Extend `itemRequest` ([internal/httpapi/items.go:112](internal/httpapi/items.go#L112))
  with optional nested blocks, all **pointer/slice-pointer** typed so "absent"
  and "present but empty" stay distinguishable:
  `Location *itemLocationRequest`, `Links *[]itemLinkRequest`,
  `Dates *[]itemDateRequest`. Reuse the existing request structs
  ([items.go:292](internal/httpapi/items.go#L292), `:324`, `:372`) rather than
  inventing parallel shapes.
- `handleCreateItem` ([items.go:130](internal/httpapi/items.go#L130)) and
  `handleUpdateItem` ([items.go:237](internal/httpapi/items.go#L237)) wrap their
  writes in `s.Store.WithTx` — already on the `Store` interface
  ([internal/db/store.go:250](internal/db/store.go#L250)) and implemented for
  both dialects (`sqlite_store.go:770`, `postgres_store.go:833`), with **no
  `httpapi` caller yet**, so this is the first one. No migration, no `sqlc`
  regeneration, no new endpoint.
- Semantics: `location` absent → untouched, present → `UpsertItemLocation`,
  present with all-null fields → the existing explicit-null upsert.
  `links`/`dates` absent → untouched; present → **replace the set** (delete the
  item's rows, insert the given ones in array order as `sort_order`). Replace,
  not merge, because the frontend edits them as a list and has no per-row PATCH
  endpoint anyway (editing a link today means delete + re-add).
- Both handlers return `buildItemDetail` ([items.go:212](internal/httpapi/items.go#L212))
  instead of `itemToResponse`, so one round trip returns the whole item and the
  editor can re-render from the response.
- The existing standalone sub-resource endpoints (`PUT /items/{id}/location`,
  `POST|DELETE .../links`, `.../dates`) stay as they are — additive change,
  nothing else in the tree has to move in the same commit.

**Verify:** `go test ./...` with new cases in the `httpapi` tests — create with
nested location+links+dates in one POST; PATCH omitting them leaves them intact;
PATCH with `"links": []` clears them; a deliberately invalid nested date rolls
the *whole* create back (no orphan item row — the point of the transaction),
asserted against both dialects the way the existing store tests do. Prove
non-vacuity of the rollback test with `scripts/without.sh`.

**Done.** `itemRequest` gained `Location *itemLocationRequest`,
`Links *[]itemLinkRequest`, `Dates *[]itemDateRequest`, reusing the existing
sub-resource request structs; `validate()` now also checks the nested blocks
(blank link URL, unparseable start/end date) so bad nested input is a 400 before
any write rather than a rolled-back 500. A new `writeItemNested` helper takes the
`db.Store` to use — that's what lets it run inside a transaction — and applies
location as an upsert, links and dates as replace-the-set. Both
`handleCreateItem` and `handleUpdateItem` now wrap their writes in
`s.Store.WithTx` (the interface's first `httpapi` caller) and return
`buildItemDetail`, so one round trip returns the item with its generated
sub-resource IDs.

Deviation from the plan: replace-the-set is implemented as list-then-delete-each
inside the transaction rather than a new "delete all by item" query, which kept
the promise of no `sqlc` regeneration and no migration. No dialect-specific code
was touched, so both dialects get this for free.

Verified: new `internal/httpapi/items_test.go` (5 tests, 3 subtests) covers the
one-request create, PATCH-without-nested-keys leaving them intact,
PATCH-with-empty-list clearing them, array order becoming `sort_order`, an
explicit `"address": null` clearing, the three 400 cases, and the rollback. The
rollback test uses a new `failingStore` decorator (`db.Store` embed whose
`WithTx` re-wraps the transaction-bound store, so the injected failure is visible
inside the transaction) and a new `newTestServerWithStore` hook in
`testing_test.go`; the sub-resource tables have no constraints to violate, so
injection was the only way to make a real failure happen mid-transaction.
Non-vacuity: `scripts/without.sh internal/httpapi/items.go -- go test ...` fails
without the change, but for the wrong reason (the old handler rejects the nested
body outright, 400), so the rollback was proved separately by replacing just the
`WithTx` call with a direct call against `s.Store` — the test then fails on its
own assertion, "got 1 items after the failed create". Also smoke-tested through
the real `make dev` server with curl: nested create returns everything with IDs,
a basic-fields-only PATCH leaves location/links/dates untouched, and
`"links":[],"dates":[]` clears them while the location survives.

## 2. One Save in the location editor (frontend)

Collapse the five save paths into one, in **both** modes.

- `renderItemForm` ([location-form.js:16](web/js/components/location-form.js#L16))
  stops owning the request. It exposes `readValues()` (and keeps its inline
  `.item-form__error` for validation display); the page composes the single body.
- [location-editor-page.js](web/js/pages/location-editor-page.js) gets one
  Save/Cancel row at the bottom in *both* modes (create mode already has it at
  line 127). Remove the Location card's own submit button (line 86) and its
  submit listener (`renderLocationForm`, 297-300), and make links/dates
  add/remove **in-memory in edit mode too** — i.e. the `links()`/`dates()`
  accessors (lines 50-51) and their handlers stop calling the API and just mutate
  the local arrays, exactly as create mode does today. One
  `POST /trips/:id/items` or `PATCH /items/:id` carries
  `{...basic, location, links, dates}`.
- `flushStaged` shrinks to the two things that genuinely cannot ride in a JSON
  body: the **cover photo** (media upload then `PUT /items/:id/image`) and
  **documents** (multipart). Both keep their existing staging paths; the header
  comment (lines 9-32) gets rewritten to describe the new, much smaller
  non-atomic remainder.
- Unsaved-changes guard: a `beforeunload`/`navigate` check is *not* needed once
  there is one Save — note this explicitly in the Done paragraph so the todo
  entry can be closed rather than left ambiguous.

**Verify:** Playwright, against `make dev` seeded with `make dev-reset FORCE=1`.
Asserted, not screenshotted: on an existing location, type lat/lng **and** a new
link, press the single Save, reload, and assert both survive (the exact
regression that todo.md records as verified-broken); assert
`GET /api/items/{id}` returns them. Same for create mode in one shot. Plus
`make ci`.

**Done.** `renderItemForm` no longer saves anything: it renders the Basic info
fields and returns `readValues()` / `showError()` / `clearError()`, with the
request moved to the page. The editor now keeps one `draft` object
(`links`, `dates`, plus the `image`/`documents` upload slots) used identically in
both modes, so the `links()`/`dates()` mode-switching accessors are gone along
with the per-row `POST`/`DELETE` calls — adding or removing a link or date is a
`push`/`splice` on the draft and reaches the server only when Save does. Both
modes render the same `.editor-actions` row (labelled Save or Create location);
the Location card's own submit button and its listener are gone, and
`flushStaged` shrank to `flushUploads`, which handles only the cover photo and
documents. Its failure now reports inline in the Basic info card and stays on the
page instead of `window.alert` plus a redirect to the edit page.

Deviations from the plan, both deliberate:

- **Edit mode's Save navigates to the view page** (it used to stay put and only
  refresh the heading), matching create mode. With one Save committing
  everything, staying on the form gives no signal that anything happened.
- **The actions row sits above the Delete card**, so the danger zone stays last.
- **Enter needed an explicit handler.** The plan assumed the `submit` listener
  was enough. It isn't: with several fields and no submit button, the HTML
  implicit-submission algorithm does *nothing*, so Enter silently did nothing —
  caught by the verification, not by reading the code. Both the Basic info and
  Location cards now bind `submit` (the safety net against a native reload, which
  *does* fire if a form is ever left with a single field) plus `keydown` on Enter,
  excluding the notes textarea.

Also removed as this milestone orphaned them: the `item.detail.saveLocation` key
in both locales (its button is gone) and the `.item-form__actions` CSS rule
(`.editor-actions` carries the row now). No unsaved-changes guard was added, and
none is needed: with a single Save there is no half-committed state to warn
about — either Save was pressed and everything landed, or it wasn't and nothing
did.

Verified: `make ci` and `make test-ui` (9 tests) green. A Playwright script
asserted 20 behaviours against the seeded dev server: exactly one Save button and
no per-card save; adding *and* removing a link writes nothing until Save; Cancel
discards a removal; coordinates, address and the staged link all survive the
primary Save; the link count goes up by exactly one (no double-write); create
issues **exactly one** `POST /items` where it used to issue up to four, and the
created location comes back with its coordinates, link and date; Enter in a
coordinate field and in a Basic info field both save client-side with no page
reload and without losing the other card's input.

Non-vacuity: `scripts/without.sh` on the two JS files fails the main script, but
only because the old markup has no single Save button. So the data loss itself was
re-proved with a second, version-agnostic script that presses whichever button is
the page's *visually primary* Save — the Basic info submit on the old code, the
actions-row Save on the new. It reports `coordinates were DISCARDED by the
primary Save (location={"lat":null,...})` on the reverted frontend and `PASS` on
this one, which is the Stage 07 bug reproducing and then being fixed.

**Follow-up.** Spotted in testing: the Save/Cancel row sat flush against the
Delete card below it. `.editor-actions` only had `margin-top`, which was enough
while it was the last thing on the page — moving it above the Delete card
exposed the missing bottom half. Now symmetric `1rem`, matching
`.editor-card`'s own `margin-bottom`. Verified by measuring the rendered gaps
rather than by eye: above, below and card-to-card all come out at 16px, and
`scripts/without.sh web/css/base.css` fails that check (no gap below the row) on
the reverted CSS. `make ci` and `make test-ui` still green.

## 3. Couple `show_on_map` to the coordinates it gates

- Move the `showOnMap` checkbox out of Basic info
  ([location-form.js:38-41](web/js/components/location-form.js#L38-L41)) into the
  Location card, directly under the lat/lng inputs, so the control and the data
  it gates are adjacent. Possible now that Milestone 2 made the whole page one
  form.
- Keep default-on for new locations (matches the backend default,
  [items.go:147-150](internal/httpapi/items.go#L147-L150)) and add a hint when
  coordinates are empty, explaining that a location needs coordinates to appear
  on the map — the missing explanation todo.md calls out.
- i18n: `location.form.showOnMap` keeps its key; the new hint is a new key in
  `en.json` + `de.json`.

**Verify:** Playwright — set coordinates on a location that was previously off
the map, save, then assert `GET /api/trips/{id}/map` contains it, and that the
hint is visible with coordinates empty and hidden once they are filled. `make ci`
(i18n parity).

**Done.** The checkbox moved out of `location-form.js` into the Location card,
directly under the lat/lng/address row, and `readValues()` no longer returns
`show_on_map` — the page reads it from where it now lives and puts it in the same
single request as everything else. It stays checked by default for a new
location (matching the API default) and prefills from `item.show_on_map` when
editing. The new `location.form.showOnMapHint` (both locales) appears exactly
while the box is ticked and the coordinates are incomplete, updating live on
input.

Judgement calls: the hint keys off **both** lat and lng, because that is what
`ListMapItems` filters on — one coordinate alone still yields no pin. And the
checkbox stays **enabled** with no coordinates rather than being disabled:
unchecking isn't what's missing, and the user's intent should survive until they
fill the fields in.

Also cleaned up, both dead as a result of this stage: `.item-form__checkbox`
became `.location-form label.location-form__checkbox` (its only user moved
cards), which let the rule's two `!important`s go — `label.` outspecifies
`.location-form label` on its own — and the `.location-form button` rules (the
base one and its mobile override) went, orphaned by Milestone 2 removing that
card's Save button.

Verified: `make ci` and `make test-ui` (9 tests) green. A Playwright script
asserted 17 behaviours: the checkbox is in the Location card and not in Basic
info, and renders *below* the coordinate fields (measured, lat y=881 vs box
y=929); no hint while unchecked, hint on checking with empty fields, hint still
there with only a latitude, gone once both are filled, gone again on unchecking;
the stored value round-trips into the editor; a new location opens checked with
the hint already showing. End-to-end: a location that `GET /trips/{id}/map`
excluded appears in that response after one Save, carrying the right
coordinates — the exact "coordinates saved, map still empty" confusion from
`todo.md`. Separately checked in a German locale at 324×756: both strings render
translated and the row doesn't overflow its card. `scripts/without.sh` on the two
JS files fails the script without the change.

## 4. In-app dialogs, and translated errors

- New `web/js/components/dialog.js`: `confirmDialog({titleKey, bodyKey, confirmKey, danger})
  → Promise<boolean>` and `alertDialog({...}) → Promise<void>`, built on the
  native `<dialog>` element (`showModal()` gives focus trapping and Escape for
  free), translated via `t()`, with `.btn-danger` styling for destructive
  confirms. Styles go in `base.css` next to the existing popup rules.
- Replace all 5 `window.confirm` sites — `checklist-list.js:69`,
  `document-list.js:88`, `settings-tab.js:49`, `itinerary-tab.js:93`,
  `location-editor-page.js:175` — reusing their existing `*.deleteConfirm` keys
  as the dialog body.
- Replace the 2 `window.alert` sites (`trip-editor-page.js:74`,
  `location-editor-page.js:256`) with `alertDialog`, and **map image-URL
  failures to a translated message** (new key) instead of rendering the Go error
  verbatim — the raw text goes to `console.error`. This covers both facets
  todo.md records: the new-trip form's late alert and the existing-trip card's
  untranslated `dial tcp: lookup ... no such host`.
- The existing orphaned key `item.detail.close` gets a real caller here (the
  dialog's close/dismiss control) instead of being deleted.

**Verify:** Playwright — trigger a trip delete, assert a `dialog[open]` exists
with the right accessible name, that Escape cancels without deleting, and that
confirming deletes; assert no `window.confirm` override was needed (i.e. grep
`web/js` for `window.confirm|window.alert` returns nothing outside vendor).
`make ci`.

**Done.** New `web/js/components/dialog.js` exports `confirmDialog` and
`alertDialog`, both promise-returning so callers keep the `if (!(await ...))
return;` shape the blocking `window.confirm` had. Built on `<dialog>` +
`showModal()`, which supplies the focus trap, Escape-to-dismiss and the top
layer (hence no `z-index` anywhere in the CSS). One dialog is created per call
and removed on `close`, so there is no long-lived instance to keep in sync.
All five `window.confirm` sites and the one remaining `window.alert` are
converted; `grep` for either now matches nothing in `web/js` outside
`dialog.js`'s own comments.

Deviations from the plan:

- **No `titleKey`.** The existing `*.deleteConfirm` strings are already
  self-contained prompts ("Delete this trip? This cannot be undone."), so the
  dialog is message-only and the message doubles as the accessible name via
  `aria-labelledby`. That avoided inventing five title strings in two locales
  for no gain.
- **Cancel comes first in the DOM**, the reverse of the app's usual
  primary-first order, so `<dialog>`'s autofocus lands on it and Enter can't
  delete by accident. The row is right-aligned so the confirming action still
  reads last.
- **The second `window.alert` was already gone** — Milestone 2 replaced
  `flushUploads`'s alert with an inline error in the Basic info card — so only
  `trip-editor-page.js`'s remained.
- **No backdrop-click dismissal.** Every caller is destructive; an explicit
  choice is wanted.

Translated errors: two new keys, `image.fetchFailed` and `image.uploadFailed`,
now cover all three paths that used to render Go error text verbatim — the
existing-trip URL fetch (`dial tcp: lookup ... no such host`), the file upload,
and the create-form failure. Each logs the raw detail via `console.error` and
shows app copy instead. `item.detail.close` got its first real caller (the
alert dialog's dismiss button), so `scripts/i18n.py unused` is down to one
orphan (`common.edit`, Milestone 7's call). Two now-stale `t` imports were
dropped from `settings-tab.js` and `trip-editor-page.js`, which no longer call
it at all.

Verified: `make ci` and `make test-ui` (9 tests) green. A Playwright script
asserted 24 behaviours covering **all five** confirm sites (location, checklist,
itinerary day, document, trip) plus the error path: each opens a real
`dialog.dialog` matching `:modal` with its own translated copy; focus starts on
Cancel; Escape and the Cancel button both close without deleting; the element is
removed from the DOM on close; confirming actually deletes (checked via the API,
not the DOM). The itinerary day keeps its two-mode behaviour — a day with content
asks (with a "Remove" confirm label, not "Delete"), an empty day is still removed
with no dialog at all. A `page.on("dialog")` counter proves **no native dialog
fired anywhere in the run**. The image-URL failure shows the translated sentence
with no Go text in it, and the detail lands in `console.error`. A second script
checked the dialog in German at 324×756 in dark mode: both strings translated,
the box fits (16..308 of 324) with no internal overflow, both buttons 44px, and
the message measures 14.27:1 against the dialog surface.

Non-vacuity: `scripts/without.sh` on the six tracked call-site files fails the
script at `waiting for locator('dialog.dialog') to be visible` — the native
dialog fires instead — which is the right reason. (`dialog.js` itself is new and
untracked, so `without.sh` can't include it; reverting the call sites is the
equivalent test.)

## 5. Loading states and a real not-found route

- Small shared helper (e.g. `renderLoading(container)` in a new
  `web/js/components/loading.js`, or alongside the existing page helpers) reusing
  the `common.loading` key, plus a `.loading` CSS block. Call it at the top of
  every fetch-first renderer — `trip-detail-page.js:15`,
  `location-view-page.js:33`, `location-editor-page.js:33`,
  `itinerary-tab.js:12`, `checklist-list.js:10`, `document-list.js:20` — and for
  the shell-first ones (`trips-page.js`, `locations-tab.js`) inside the list
  container so the toolbar no longer sits over a void.
- Replace `router.js`'s silent `navigate("/trips")` on no-match
  ([router.js:31-38](web/js/router.js#L31-L38)) with a rendered not-found page: a
  proper `.page` wrapper, an `<h1>`, the explanatory copy and a Home link.
  Register it as an explicit fallback route in `app.js`'s `routes` array.
- Reuse that same renderer for the three per-page fetch-failure fallbacks
  (`trip-detail-page.js:20`, `location-view-page.js:38`,
  `location-editor-page.js:43`), which today emit a bare `<p>` + raw link with no
  `h1` — so they also become `headings.spec.js`-safe.
- Add the not-found path to `buildRoutes()` in
  [tests/ui/helpers/scenarios.js](tests/ui/helpers/scenarios.js) so the overflow,
  heading and accessible-name sweeps cover it. Note `gotoRoute()`'s
  `location.pathname === path` assertion (scenarios.js:174-199) gets *more*
  correct here, since the redirect it was written around is gone.

**Verify:** `make test-ui` green with the extra route in the matrix; Playwright
assertion that `/trips/00000000-0000-0000-0000-000000000000/locations` renders one
`h1` inside `.page` rather than redirecting. `make ci`.

**Done.** Two new modules. `components/loading.js` exports `renderLoading(target)`,
called by all six await-then-paint renderers (trip detail, location view, location
editor, itinerary tab, checklist list, document list) with their page container,
and by the two shell-first ones (`trips-page`, `locations-tab`) with just their
list container — so on those the heading and toolbar stay put and only the list
waits. `pages/not-found-page.js` exports `renderNotFoundPage(container, {href,
labelKey})`, used for both flavours: the router's catch-all, and the three
fetch-by-ID pages' failure path, which is why the back link is a parameter (Home
for an unknown URL, back to the trip for a location that's gone).

The router gained a `"*"` catch-all pattern rather than a `createRouter` option,
so the routes array in `app.js` says out loud what an unknown URL does; the
silent `navigate("/trips")` is gone. That exposed something the plan didn't
mention: `index.html` is served at `/`, so without a route for it the app's own
entry point would have reported itself as not found. `/` is now an explicit
route that canonicalizes to `/trips`.

i18n: `common.notFound` ("Not found.") was orphaned by this — it existed only for
those bare fallbacks, and its trailing period reads wrong as a heading — so it
was replaced by `notFound.title` + `notFound.body` in both locales. Two more
stale `t` imports went (`trip-detail-page.js`, `trips-page.js`).

The UI suite's `buildRoutes()` now yields **19 routes**, up from 17: one
unmatched URL and one missing trip, so both paths to the page get swept for
overflow, headings and accessible names. Two comments in `scenarios.js` that
warned about the `/trips` redirect were rewritten, since the behaviour they
describe no longer exists.

Verified: `make ci` and `make test-ui` (9 tests, 19 routes) green. A Playwright
script asserted 22 behaviours: an unmatched URL stays at its own path instead of
redirecting, renders `.page.not-found` with exactly one translated `h1` and real
explanatory copy, and — measured, since "renders unstyled" was the original
complaint — sits at x=176 inside the page padding rather than flush at x=0; all
three missing-resource routes render the same page with the right back link; `/`
still lands on `/trips`. For loading, the API response is held open mid-flight:
the locations list shows the translated `role="status"` line **while its heading
and toolbar stay rendered**, then is replaced by real cards; an await-then-paint
route shows the line instead of a blank container. `scripts/without.sh` on six of
the touched files fails at `waiting for locator('.not-found h1')`.

Not a regression from this milestone, but found by it and recorded in `todo.md`:
the suite can fail with **HTTP 429** instead of a real assertion. Login is rate
limited to 10/min per IP and the suite logs in once per spec (9 per run), so two
runs inside a minute — or one run alongside a hand-written script — trips it, and
the resulting message blames the seed rather than the limiter.

## 6. Raise touch targets to the 44px guideline

- CSS in [web/css/base.css](web/css/base.css): `.btn` min-height 40 → 44px;
  give block links, the icon+text `.back-link`/Home links (22px today) and
  checkbox rows enough padding to clear 44px; stack `.trip-form__dates` under the
  existing 640px breakpoint so the date inputs stop clipping the year at 324px;
  raise the mobile trip-tab label from 0.625rem to ~0.75rem (tap targets there
  are already fine).
- Convert `.itinerary-entry__link` from a `<button>` with a click handler to a
  real `<a href data-link>` in `itinerary-tab.js` — the router already
  intercepts `[data-link]` clicks (`router.js`), so this is a swap, and it buys
  middle-click, copy-link and focus semantics along with the size.
- Move the suite's constant with the CSS: `MIN_TAP_TARGET_PX` 40 → 44 in
  [tests/ui/routes.spec.js:25](tests/ui/routes.spec.js#L25), rewrite its comment
  (it currently documents the 40px floor as a deliberate pin), and widen the
  check past `looksLikeAButton` (lines 101-107) to include block links and the
  now-`<a>` itinerary entry link.

**Verify:** `make test-ui` green at the raised constant on both viewports and both
colour schemes — this is the milestone where the test *is* the verification.
Manual 324×756 pass on the trip settings date row. `make ci`.

**Done.** `base.css`'s `max-width: 640px` block went from sizing `.btn` and one
label to sizing *everything the user aims a finger at* — `button`, `input`
(except checkbox/radio), `select`, `textarea`, `.back-link`,
`.itinerary-entry__link`, `.link-list li a`, `.location-view__maps-link`,
`.checklist-item label`, `.location-form__checkbox` — plus `display: flex;
align-items: center` on the ones that needed their content centred for the
min-height to mean anything. Checkbox and radio inputs are deliberately
excluded: a native checkbox is ~14px and inflating it looks wrong, so the
*label* around it carries the height and is what the spec measures. The map
legend's own labels were fixed the same way inside `leaflet-map.js`'s shadow
styles (custom properties pierce the shadow boundary, so `var(--tap-min)` works
there). `MIN_TAP_TARGET_PX` is now **44** with a rewritten comment, and the spec
measures `button, a, input, select, textarea, label` with three documented
exclusions: prose links, Leaflet's vendored internals, and label-wrapped
checkboxes. `.itinerary-entry__link` became a real `<a href data-link>`, which
let its button-reset CSS go.

Three things the plan didn't anticipate, all found by the widened check rather
than by reading code:

- **Two more 22px links** — a location's external-link rows and its "View on
  Google Maps" link. My own measurement script had missed both by bucketing all
  bare `<a>` elements together, where the 14px OSM attribution hid them; the
  spec named them individually.
- **The seed was hiding one of them.** The `full` scenario created no item
  links, so the Links card only ever rendered its empty state and no sweep ever
  measured a row — the 22px target was found only because leftover manual test
  data happened to be in the database. `cmd/seed/main.go` now gives that
  location a link and a date (new `linkSpec`/`dateSpec`), so the coverage is
  real rather than accidental.
- **12px tab labels overflowed the viewport** (333px against 324px). A bare
  `1fr` grid track floors at its item's min-content width, so one unbreakable
  label ("Checklists") pushed the whole bar off-screen. Fixed with
  `minmax(0, 1fr)` plus `overflow-wrap: break-word`, and the label settled at
  **0.6875rem (11px)** rather than 12px — 12 needed mid-word breaking to fit,
  which reads worse than one point smaller.

Verified: `make ci` and `make test-ui` (9 tests) green at 44px, the tap-target
sweep now measuring ~200 controls per route set instead of buttons only. A
Playwright script asserted 14 more behaviours: the itinerary entry is an `<a>`
with a real href and `data-link`, is 44px (was 22), navigates client-side with
no page reload and lands on the location; the settings date fields stack at
324px, each 258px wide (was ~123) with neither clipping its own value; tab
labels are 11px with the bar fitting in the viewport (0px overflow) and all six
labels intact.

Non-vacuity: `scripts/without.sh web/css/base.css -- make test-ui GREP="tap
target"` fails with **90 controls below 44px**, naming exactly the classes
`todo.md` had recorded — the user-menu trigger at 40px, `.back-link` at 22px,
form inputs at 36–37px. (A first attempt at this proved nothing: the grep
pattern contained parentheses, Playwright found no tests, and `without.sh`
reported success off a non-zero exit for the wrong reason.)

**Follow-up: four tabs plus a "More" menu.** Testing found the enlarged labels
*overlapping*. The suite hadn't caught it because it measures page-level
overflow and cell height, not whether a label's ink stays inside its own cell —
at 324px each cell was 49px while "Documents" alone needed 60px, so the labels
ran into their neighbours. `overflow-wrap: break-word` didn't save them either:
the label span is a flex item, so `min-width: auto` kept it from shrinking to
the point where wrapping would kick in.

Rather than shrink the font back, the row was reduced to four content tabs —
Locations, Map, Itinerary, Checklists — plus a fifth "More" control (a
horizontal three-dot `ellipsis`, added to the committed sprite; every existing
symbol regenerated byte-identical) holding Documents and Settings.

**The breakpoint question was decided by measuring both locales.** Six labels
need ≥360px of label space in English but ≥426px in German ("Einstellungen" is
71px against "Settings"' 42px), so a width threshold tuned to English would
break German on exactly the devices it was meant to help — and a media query
cannot know the locale. The split therefore applies across the whole existing
`max-width: 640px` range rather than at some narrower "small phone" cutoff: one
phone layout, correct in both locales, and no third variant to maintain. Above
640px all six tabs stay in the row, which already scrolls there.

Which set is visible is a pure CSS decision — all six buttons and the menu are
always in the DOM — so there is no resize listener and no re-render on rotation.
The grid is `repeat(4, minmax(0, 1fr)) auto`, not five equal cells: "More" is
~28px against "Checklisten"'s 59px, and giving it an equal fifth left German's
longest label 1px short of fitting.

The menu is `renderMenu`, not a third hand-rolled popup. It grew five options for
this: `label` (a pinned trigger label — "More" must keep saying "More" while
Documents is open, so the *rows* carry the which-one-is-current signal),
`chevron: false`, `triggerClass` (so the trigger can be styled as a tab rather
than a secondary button), `className` (variant styling), and `items[].iconName`
so a row can show its section's own icon. It also now sets
`menu__trigger--open` on any open menu, which the locations filter gets too — an
open menu looking inert was never deliberate.

Three defects in the first cut of this, all found by review of the rendered
result rather than by the suite:

- **The dropdown was being styled as a tab bar.** The menu lives inside
  `<nav class="trip-tabs">`, so `.trip-tabs button` matched the dropdown's own
  rows: centre-aligned, muted, 11px, `flex-direction: column`, with a
  transparent 2px bottom border. `.menu__dropdown button` and `.trip-tabs
  button` have equal specificity, so source order decided it and the tab rules
  won. Every tab rule is now scoped to `.trip-tabs > button` (direct children);
  the trigger, which is nested, is styled explicitly. This is the kind of
  collision that follows from nesting a component inside a styled container, and
  the descendant selectors were the actual bug.
- **"More" got visibly less room than its neighbours.** The trigger sits three
  levels inside its grid cell (cell → `.trip-tabs__more-slot` → `.menu` →
  `.menu__trigger`) and `.menu` wasn't growing, so the trigger sized to its own
  short label: 45px inside a 58px cell, which read as tighter padding and an
  active underline that hugged the word instead of spanning the cell. `.menu`
  now takes `flex: 1` as well. Measured after: all five cells 58.4px, identical
  padding, identical underline width.
- **The rows lost their icons.** A section that has an icon in the row looked
  like a different destination once it moved into the menu. `items[].iconName`
  now takes the leading slot that would otherwise hold the check mark, and the
  current row is marked by styling instead (`aria-checked` still carries it for
  assistive tech). Those rows follow the row's own convention — other sections
  muted, the current one at full strength plus bold — rather than the accent the
  locations filter uses, since here the *trigger* has taken the accent.
- **Then the trigger's icon moved beside its label instead of above it** — a
  direct consequence of the first fix. Scoping the tab rules to
  `.trip-tabs > button` cut the trigger off from the `display: flex` it had been
  inheriting, so the mobile block's `flex-direction: column` had nothing to act
  on and it computed `display: block`. Two more rules had the same omission: the
  icon-size override (leaving the three dots at 1em against the tabs' 1.1rem,
  which also shifted the stack) and the `min-height: var(--tap-min)`. All three
  now name the trigger explicitly, and its own rule says why that repetition is
  necessary rather than leaving the next reader to rediscover it.

Fixing that exposed a smaller misalignment, in German only: with the stacks
vertically centred, a cell whose label hyphenates onto a second line
("Checklisten") has a taller stack than its neighbours, so its icon sat 6px
higher. The label box now reserves two lines at phone width, which lines every
icon and label up with its neighbours and — worth more than the alignment
itself — makes the bar's height the same in every language instead of depending
on whether some translation happens to wrap. It costs ~11px of bar height.

Equal cells also meant German's longest label ("Checklisten", 59px) no longer
had a 1px-wider cell to sit in, so the tab labels gained `hyphens: auto` with
`overflow-wrap: break-word` as fallback, plus `min-width: 0` on the label span —
as a flex item it defaults to min-content, which for an unbreakable word is its
full width, so it would have overflowed rather than wrapped whatever the
wrapping properties said. `i18n.js` already sets `<html lang>`, which is what
lets the browser hyphenate.

Behaviour, as specified: tapping More opens a menu directly below the row and
spanning it (not a modal); selecting switches section and closes; tapping More
again, tapping outside, or Escape all close. While open, More takes the active
tab colour, and when the current section lives inside More the trigger keeps
that active styling with the menu closed too — otherwise no tab in the bar would
look active. The current row inside the menu is marked with the strongest
foreground plus bold rather than the accent the locations filter uses for its
checked row, so two things in one open menu aren't both saying "active" in the
same colour.

One unrelated bug surfaced on the way: the UI suite began failing "desktop dark:
horizontal overflow, scrollWidth 1636 > 1280" on the location view page. It was
not the tabs and not dark mode — a shadow-DOM-piercing probe showed
`document.scrollWidth` settling at exactly 1280 while Leaflet parks internal
zoom-animation helpers at `right=1825757`. The check was racing the map's
animation. `.map-wrap` now has `overflow: hidden` so the component can never
widen the document whatever the library does mid-animation, and the
single-marker embed's `setView` passes `animate: false` — it is the only view
that embed ever takes, so there was nothing to animate from.

Verified: `make ci` green; `make test-ui` green on **two consecutive runs**
(chosen deliberately, since the thing being fixed was a flake). A Playwright
script asserted 33 behaviours across English/light/324px, German/dark/324px and
English/1280px: the row shows exactly the four content tabs and a labelled,
chevron-less `ellipsis` trigger; **no label's ink exceeds its own cell** in
either locale (the regression that started this) with zero page overflow; the
dropdown opens directly below the row (row bottom 395, menu top 394) and spans
it exactly (16..308 against 16..308); it is `role="menu"` with no `dialog[open]`
anywhere; it holds Documents and Settings; toggle, outside-click and Escape all
close it; selecting Documents closes the menu, updates the URL and renders the
section; with Documents current, More carries `active` and **no visible content
tab claims to be active**; the current row is the checked one, highlighted apart
from its sibling and at font-weight 600; returning to a row tab clears it; at
1280px all six tabs are in the row and no More trigger exists.

A second script (34 assertions, English/dark and German/light) covers what the
review found: all five cells the same width to 0.0px spread, the same padding
and the same height; every cell a flex container that stacks its icon above its
label, with all five icons the same size and all five icons *and* labels on the
same line as each other (the assertion that catches the trigger drifting from
the tabs, since it is styled separately); More's active underline the same width,
colour and weight as a row tab's; each dropdown row carrying its own section icon
(`lucide-file-text`, `lucide-settings`) rather than a check mark; and those rows
left-aligned, icon beside label, at menu text size rather than the 11px tab
size — i.e. no longer inheriting tab styling. A third checks eight width bands (639/640/641/700/767/
768/900/1280) for tab count, active-state marking and page overflow either side
of the breakpoint.

## 7. i18n copy pass

- Fix `itinerary.noDates` in `en.json` + `de.json` (line 98 in both): it points
  at the **Overview tab**, removed in Stage 05 — those fields live under
  **Settings** now.
- Grep all of `web/locales/` for other references to removed or renamed UI while
  in there (Overview/Übersicht was the known one; confirm there are no others).
- Fix `location.editor.createButton` — still reads "Create item" /
  "Eintrag erstellen" — to location wording. Copy only; the broader
  `renderItemForm`/`renderItemsTab`/`item.*`-namespace **identifier** rename
  stays a `todo.md` entry, to be done as one deliberate pass.
- Resolve the orphaned keys found by `scripts/i18n.py unused`: `item.detail.close`
  is now used by Milestone 4's dialog; `common.edit` gets deleted (re-addable
  when the ⋮ menu is actually built) — record the decision in the Done paragraph.

**Verify:** `scripts/i18n.py unused` shows the two keys resolved;
`scripts/check_i18n.py` parity green via `make ci`; render the Itinerary tab of
the `no-dates` seed scenario (available since Stage 08 Milestone 4) in both
locales and read the corrected sentence.

---

## Build order

1 → 2 → 3 must run in that order (backend contract, then the unified form, then
moving the checkbox into it). 4, 5, 6 and 7 are independent of each other and of
1-3; run them in that order so the dialog component exists before anything else
wants it.

## Workflow

Per `CLAUDE.md`: one milestone at a time — implement, verify with `make ci` plus
a real behavioural pass (assertions over screenshots), add a "**Done.**"
paragraph to `docs/plans/stage-09.md` and update `docs/plans/todo.md` in both
directions, commit (one commit per milestone; follow-ups get their own
"... follow-up:" commit), make sure `make dev` is running, then stop and wait for
the go-ahead.

## Verification (stage level)

- `make ci` green after every milestone.
- `make test-ui` green after 5 and 6, with the not-found route added to the sweep
  and `MIN_TAP_TARGET_PX` at 44.
- Go tests cover the nested/transactional create and update, including a rollback
  case proven non-vacuous with `scripts/without.sh`.
- End-to-end by hand at 324×756 and 1280×800, light and dark: create a location
  with coordinates, a link, a date and a document in one Save; edit it and change
  coordinates with the single Save; delete it through the new dialog; visit a
  bogus trip URL; confirm the German copy on a dateless trip.
