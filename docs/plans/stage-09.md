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
