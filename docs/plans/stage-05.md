# Stage 05 — Trip navigation restructure, location-editor fixes, documents inline

> **Status: in progress.** Built one milestone at a time per the Workflow
> section below, each with its own commit and a manual-testing checkpoint.

## Context

A second manual UI/UX pass (mobile, 324×756, plus spot-checks on desktop)
surfaced one real bug, several UX rough edges, and a navigation restructuring
that fixes the header-overflow issue Stage 04 couldn't fully solve with CSS
alone: a long trip title (e.g. "Demo Trip: Iceland Ring Road") still pushes
the header's Edit button onto its own row, which reads oddly once that
button is icon-only.

The chosen fix removes the button rather than better-wrapping it: the trip's
"Overview" tab and the page-header Edit button both go away, replaced by a
new **Settings** tab (last position) that hosts what used to be the separate
`/trips/:tripId/edit` page — title, dates, cover photo, delete — inline, the
same way Locations/Map/Itinerary already work. The trip's markdown "notes"
field is retired in favor of a plain-text "subtitle", shown together with
the dates directly beneath the title. This block belongs to the page's
persistent shell (rendered once alongside the back-link/header/tab-bar,
above the per-tab content area that swaps underneath it), **not** to any one
tab — it must stay visible no matter which tab is active, including
Checklists, Itinerary, etc., not just the default Locations tab.

Separately, a real bug was found and root-caused: clicking "Add link" in the
location editor adds 1, then 2, then 4 links on successive clicks — an exact
doubling pattern from a submit listener that gets re-attached to the same
persistent `<form>` node on every render instead of being bound once (so by
the Nth click, N−1 stacked listeners all fire at once). The identical
pattern exists in "Add date" too (unreported by the user, but confirmed
present). Both get fixed with the same technique.

**Decisions from clarifying questions:**

- **Documents**: convert the "Add document" dialog to a single-file inline
  row everywhere (trip-level Documents tab *and* location editor's Documents
  card) — same file+note+button shape as Links/Dates, dropping multi-file
  select. Sequential single uploads are an acceptable tradeoff.
- **Cover photo**: dropped from the default trip view for now — visible/
  editable only inside the new Settings tab. Revisit later (→ `todo.md`).

## 1. Backend: trip `notes` → `subtitle`

Plain-text replacement for trip's markdown notes field. Follows the Stage 02
"add a nullable TEXT column" precedent (`0003_add_document_note`) as a
drop-one/add-one pair, next migration number **`0005`** in both dialects:

- `internal/db/migrations/{sqlite,postgres}/0005_trip_subtitle.{up,down}.sql`:
  `ALTER TABLE trips DROP COLUMN notes; ALTER TABLE trips ADD COLUMN subtitle TEXT;`
  (down does the reverse). SQLite's `ALTER TABLE ... DROP/ADD COLUMN` is
  already relied on by `0003`, same simple case here.
- `internal/db/sqlc/queries/trips.sql`: rename `notes` → `subtitle` in
  `CreateTrip`/`UpdateTrip`; regenerate both dialects' `gen/trips.sql.go` and
  `gen/models.go` via `sqlc generate` (manual step, no Makefile target —
  confirmed by Stage 02's precedent commit).
- `internal/db/domain.go`: `Trip.Notes *string` → `Subtitle *string`.
- `internal/db/store.go`, `sqlite_store.go`, `postgres_store.go`:
  `CreateTripParams`/`UpdateTripParams`.`Notes` → `Subtitle`; update the
  three mapping functions per dialect (`CreateTrip`, `UpdateTrip`,
  `sqliteTripToDomain`/`postgresTripToDomain`).
- `internal/httpapi/trips.go`: `tripResponse` drops `Notes`/`NotesHTML`
  (`json:"notes"`/`json:"notes_html"`), adds `Subtitle *string
  json:"subtitle"`; `tripToResponse` drops the `renderNotesHTML(t.Notes)`
  call for trips; `tripRequest.Notes` → `Subtitle`, wired through
  `CreateTripParams`/`UpdateTripParams`. **Leave `renderNotesHTML` and
  `markdown.ToSafeHTML` untouched** — both are still used by
  `internal/httpapi/items.go` for item notes, a completely separate field.
  Only update `internal/markdown/markdown.go`'s doc comment (currently says
  "trip/item notes") to say "item notes".
- `cmd/seed/main.go`: replace the seeded trip's markdown `notes` string with
  a short plain-text subtitle, rename the `CreateTripParams` field.

No frontend dependency for this milestone — verify standalone via
`go build && go vet` + curl (create/update a trip, confirm `subtitle`
round-trips and `notes`/`notes_html` are gone from the response).

## 2. Frontend: trip navigation restructure

`web/js/trip-tabs.js` (the single source of truth consumed by both
`app.js`'s route table and `trip-detail-page.js`'s nav rendering — no
duplication to worry about): remove the `overview` entry, add `{ key:
"settings", icon: "settings" }` at the end. Net tab count stays 6 (−1 +1) —
**explicitly verify** `web/css/base.css`'s mobile tab grid
(`grid-template-columns: repeat(6, 1fr)`, hardcoded, not derived from
`TRIP_TABS.length`) still matches after the edit; it should coincidentally
still be correct, but confirm rather than assume.

**New icon**: `settings` doesn't exist in `web/icons/lucide-sprite.svg` yet.
Follow Stage 04's exact process — extend `scripts/gen_icon_sprite.py`'s
`ICONS` list, `npm install lucide-static --prefix /tmp/lucide-scratch`,
regenerate, diff to confirm the existing symbols come out byte-identical.

**Remove the header Edit button** — `web/js/pages/trip-detail-page.js`:
delete the `<button data-action="edit-trip">` element and its click
listener entirely (no icon-only fallback, just gone). `.page__header` then
holds only `<h1>`; no CSS change needed (it's a widely-shared class —
`trips-page.js`, `trip-editor-page.js` — so only the trip-detail template
changes, not the rule).

**Remove the Overview tab**: delete the `renderOverview` function and its
`tab === "overview"` branch from the content switch. Its cover-image
display is dropped per the cover-photo decision above (`todo.md` note to
revisit). Its dates + notes rows are repurposed into the new under-title
block below. Delete the now-dead `trip.tabs.overview` i18n key from both
locale files (`scripts/check_i18n.py` only checks parity, not unused keys,
so this is cleanliness, not a build requirement) — but `trip.form.startDate`/
`trip.form.endDate` are reused elsewhere and must stay.

**New "subtitle + dates" block**, inserted between `.page__header` and
`<nav class="trip-tabs">` (as its own sibling — *not* nested inside
`.page__header`, to avoid fighting that class's `flex`/`space-between`
row-layout semantics, which stay unchanged for the other pages sharing it).
Placing it here — outside `.trip-tab-content`, the div whose contents
actually get swapped per tab — means it's part of the outer template that
`render()` rebuilds identically on every tab click, so it stays visible
regardless of which tab is active (Checklists, Itinerary, etc.), not just
Locations: a `<p>` for the subtitle (only rendered if `trip.subtitle` is
set — plain text via `textContent`, no markdown) followed by a `<dl
class="trip-overview">` for start/end dates, reusing the existing
`.trip-overview` grid CSS (`base.css`, currently the Overview tab's
date-pair styling, freed up once that tab is deleted) minus the notes row.
**Desktop gotcha**: add this new block's selector to the existing
`grid-column: 1 / -1` rule (`base.css`, the `@media (min-width: 768px)`
block that already spans `.back-link`/`.page__header` full-width above the
sidebar) — otherwise it mis-places itself next to the tab sidebar instead
of spanning above it.

**New Settings tab** — new file `web/js/pages/settings-tab.js`, exporting
`renderSettingsTab(content, trip)`, wired into `trip-detail-page.js`'s tab
switch (`else if (tab === "settings") { renderSettingsTab(content, trip); }`).
Reuses the exact same three `.editor-card`s `trip-editor-page.js`'s
edit-mode branch has today — Basic Info (`renderTripForm`), Cover Photo
(`renderImageField`), Delete trip — both `web/js/components/trip-form.js`
and `web/js/components/image-field.js` are already plain "render into a
container" functions with no page-coupling, so they drop in unchanged. Two
behavioral adjustments since there's no "page" to navigate away from:
`onSaved` merges the returned trip into the closure's `trip` object and
re-renders (no `navigate()`); `onCancel` re-renders the tab from the
last-saved `trip` (discarding in-progress edits) instead of navigating.
Delete keeps its existing `window.confirm` + `api.delete` +
`navigate("/trips")` logic unchanged (a real navigation is correct there,
the trip no longer exists).

**Shrink `trip-editor-page.js` to create-mode only**: strip the `if
(tripId)` branch (the fetch-existing-trip logic and the three edit-mode
cards) — `/trips/new` (no `tripId`) is all that's left, and it's unaffected
by any of this (its own image-field runs in staging mode, unrelated to
Settings-tab's non-staging usage). **One surviving cross-reference to
fix**: the create flow's staged-image-upload-failure fallback currently
navigates to `/trips/${saved.id}/edit` as a retry landing spot — repoint to
`/trips/${saved.id}/settings`.

**Route table** (`web/js/app.js`): remove `{ pattern:
"/trips/:tripId/edit", render: renderTripEditorPage }`. `/trips/new`'s
precedence over `/trips/:tripId`-shaped patterns is unaffected (unrelated
route, no ordering dependency on the one being removed).

**Done.** Also removed the now-dead `trip.editor.editTitle` i18n key (its
only call site was the edit-mode `<h1>` just deleted) and `trip.tabs.overview`
— both cleaned from both locale files. Added the `settings` Lucide icon to
the sprite (existing 19 symbols confirmed byte-identical after regeneration).

Verified end-to-end via Playwright at 324×756 and 1200px: the header no
longer has an Edit button at all (the original overflow complaint is gone
by construction, not just re-wrapped); the subtitle+dates block renders
under the title and *stays visible* across every tab (confirmed by
switching to Checklists and taking a fresh accessibility snapshot); the
Settings tab's Basic Info form is correctly prefilled including the new
Subtitle field; saving it updates the persistent header in place with
**no navigation** (URL stays on `/settings`) — confirmed via snapshot
before/after; the six-tab mobile grid still renders one row, evenly sized.
Re-ran the full route sweep (12 real routes incl. the two location pages)
asserting the landed-on URL: zero overflow, no sub-44px targets anywhere.
Confirmed the removed `/trips/:tripId/edit` route now falls through to the
router's unmatched-path fallback (`/trips`), as expected. Exercised
`/trips/new` end-to-end through the actual UI (fill title, Create trip,
lands on the new trip's Locations tab) — create-mode is fully intact.
Zero console errors throughout. `make ci` green.

**Follow-up refinement** (same milestone, after initial manual testing):
the subtitle+dates block, as first built, stacked three separate margins
(the header's, the subtitle paragraph's, and the dt/dd date grid's),
reading as noticeably too much dead space between the title and the tab
bar. Reworked into a single flex-wrap line — subtitle and a compact
human-readable date range (`Aug 18 – Aug 21, 2026`, year shown once,
replacing the raw ISO strings and the "Start date"/"End date" labels)
joined by a `·` that only appears between two pieces that both exist. The
`·` is hidden below the app's one mobile breakpoint (640px) rather than
left to flex-wrap alone, so it can't dangle at the start of a wrapped
second line — matching the two explicit mockups (desktop: one line with
`·`; mobile: subtitle then dates, no `·`) given during review. The whole
block is omitted entirely when neither subtitle nor dates are set (no more
bare "—, —" placeholders). `.trip-overview`'s old dt/dd grid CSS is now
fully dead and removed along with it.

Verified via Playwright at 324×756 and 1200px against three cases: both
subtitle and dates set (desktop renders "Subtitle · Aug 18 – Aug 21, 2026"
on one line; mobile stacks subtitle then dates with no dot, confirmed via
a computed-style check that the dot is `display:none` on all six tabs),
subtitle-only (no dangling dot or empty date span), and neither set (no
`.trip-summary` element in the DOM at all — confirmed via a fresh
accessibility snapshot). Zero overflow across all six tabs at mobile
width. `make ci` green throughout.

## 3. Location editor: fix the link/date duplicate-listener bug

**The bug** (confirmed root cause): `renderLinks()`/`renderDates()` in
`web/js/pages/location-editor-page.js` each re-query the same persistent
`<form>` node and call `form.addEventListener("submit", ...)` again on
every invocation — including from inside their own submit handler, which
calls `renderLinks()`/`renderDates()` again at the end. The listener count
doubles on every submit (1 → 2 → 4 → 8 …), and each submit fires *all*
currently-bound listeners at once, so the number of links actually created
by a single click equals however many listeners had accumulated *before*
that click: click 1 fires 1 listener (1 link created, then 2 are now
bound); click 2 fires those 2 (2 links created, then 4 are bound); click 3
fires those 4 (4 links created) — exactly the reported 1, then 2, then 4
pattern. (Deletes also re-invoke the render function, so they accumulate
listeners the same way even though they don't themselves create links.)

**The fix** (same shape for both Links and Dates): split each into a
list-only render (rebuilds just the `<ul>` and its per-item delete buttons
— safe, since those buttons are on fresh nodes every call) and a
**one-time** form-binding step called exactly once from the page's initial
`render()`. The submit handler posts, pushes into `item.links`/
`item.dates`, resets the form, and calls the *list-only* render — never
re-binds. (Documents, in Section 5, does *not* need this same technique —
see that section for why its existing full-rebuild approach already
avoids the bug by construction.)

This is a self-contained, easily-isolated fix — land and verify it on its
own before touching anything else in this file, so the UX changes in
Section 4 build on a page that's already known-good.

## 4. Location editor: heading, field order, and copy fixes

**Heading + title-field reordering**: `location-editor-page.js`'s `<h1
data-i18n="location.editor.editTitle">` becomes dynamic ("Edit {title}") —
`translatePage()` never passes interpolation params (confirmed:
`i18n.js`'s `translatePage` calls `t(key)` with no second argument), so
drop `data-i18n` from this specific `<h1>` and set `textContent` manually
after the initial `translatePage(container)` call:
`h1.textContent = item ? t("location.editor.editTitle", { title: item.title }) : t("location.editor.newTitle")`.
Update the key's value in both locales to include `{title}` (e.g. en:
`"Edit {title}"`, de: `"{title} bearbeiten"`). Since the user asked that
saving Basic Info update this heading too: after the existing `onSaved`'s
`Object.assign(item, saved)` in edit mode, re-set `h1.textContent` the same
way. Also rename `location.editor.newTitle`'s value ("New item" → "New
location") for consistency while this file is already being touched.

**Title field to the top**: `web/js/components/location-form.js`'s field
order (Category, Type, Title, Notes, Show on map) becomes (Title, Category,
Type, Notes, Show on map) — pure reorder, no logic change. Applies to both
create and edit mode since it's the same shared form.

**"New item" → "New location"**: `web/js/pages/locations-tab.js`'s
`locations.new` key value only (en: "New item" → "New location", de
equivalent) — a copy change, not a key rename, minimal footprint. (A
broader sweep of `item.*`-namespaced i18n keys and JS identifiers like
`renderItemForm`/`renderItemsTab` surfaced during exploration as a real
but separate inconsistency — out of scope here, → `todo.md`.)

## 5. Documents: inline single-file form, everywhere

Per the decision above, `web/js/components/document-list.js` drops its
`<dialog>` entirely (used by both the trip-level Documents tab and the
location editor's Documents card — same component, both converted
together) in favor of one inline row, shaped like Links/Dates and reusing
their existing flex-wrap CSS group (`.location-form, .link-form,
.date-form` in `base.css` — add `.document-form` to that selector list
rather than writing new rules): a hidden file input behind the existing
`.image-field__upload`-styled label (same pattern already used twice for
image upload — reuse it, don't invent a third), an optional text input for
a note, and a submit button (icon `upload`, `.btn-collapse` for
consistency with Add Link/Add Date — this is *why* the original "don't
collapse, there's room" complaint disappears: a 3-control inline row has
exactly as little spare room as Links/Dates, unlike the old standalone
full-width "Add document" trigger).

**Keep, don't change, `document-list.js`'s existing render discipline**:
its `render()` already does a full `container.innerHTML` rebuild on every
call (confirmed by reading the current file) — which is *why* it has no
listener-stacking bug today, unlike `renderLinks`/`renderDates`'s *partial*
rebuild that reuses a persistent form across calls. Build the new inline
form inside that same full-rebuild `render()`, not as a separately-bound
persistent node — this sidesteps the Section 3 bug class entirely rather
than needing its fix technique (the one built in Section 3 for Links/Dates
is *not* needed here, precisely because this file was never broken that
way). Don't introduce a partial-rerender-plus-persistent-form pattern here;
that's the exact shape that caused the original bug.

Submit: single `FormData` (`file`, optional `note`) via the existing raw
`fetch` upload path (unchanged — still needed since `api.js`'s wrapper
doesn't handle multipart), push the returned doc, reset the form, re-render
just the list. Drop the multi-file file-row generation and the dialog
open/close/cancel wiring. Retire the now-dead dialog-only i18n keys
(`documents.dialogTitle` and friends); reuse `documents.upload` for the
button label and the existing `documents.notePlaceholder` key for the note
input's placeholder (already present, previously used per-file inside the
dialog). Remove the now-dead `.document-dialog*` CSS rules from `base.css`
— **keep `.document-note`**, which is unrelated (renders an existing
document's saved note in the read-only list, not dialog markup).

No signature change needed on `renderDocumentList(container, path)` — since
there's no more separate "open the dialog" trigger button, both call sites
(`trip-detail-page.js`'s Documents tab, `location-editor-page.js`'s
Documents card) keep invoking it exactly as they do today.

## 6. Trip card equal-height fix

`.trip-grid` (`base.css`) is already `display: grid` — grid's default
`align-items: stretch` already stretches each `<trip-card>` `:host` to the
row height, but the shadow-DOM-internal `.card` div inside `web/js/
components/trip-card.js` has no height rule of its own, so it just sizes
to its own content — meaning two cards in the same row can have different
`.card` heights (a `.dates` div renders conditionally) even though their
stretched `:host`s are equal. Fix entirely inside `trip-card.js`'s shadow
styles: `:host { height: 100%; ... }`, `.card { height: 100%; display:
flex; flex-direction: column; ... }`, and `flex: 1` on `.body` so the text
area (not empty space) absorbs the difference when `.dates` is absent. No
`.trip-grid` container change needed.

## Build order

1. **Backend subtitle field** (Section 1) — standalone, curl-verifiable,
   nothing else depends on it existing to build correctly, but frontend
   milestones need it to actually persist a value.
2. **Trip navigation restructure** (Section 2) — the biggest milestone;
   depends on Section 1 for the subtitle field to bind to.
3. **Location editor: duplicate-listener bug fix** (Section 3) —
   independent of 1/2; a small, isolated, easily-verified fix on its own
   before the same file gets touched again in the next milestone.
4. **Location editor: heading/field-order/copy fixes** (Section 4) —
   depends on Section 3 landing first (same file, want it starting from a
   known-good state), otherwise independent of 1/2.
5. **Documents inline form** (Section 5) — independent of 3/4's fix
   technique (see that section for why), but sequenced after them since
   it's the same general "inline add-row" family of change, good to land
   once that pattern's been re-proven correct earlier in the stage.
6. **Trip card height fix** (Section 6) — small, fully independent, could
   move anywhere; last for low risk plus this stage's `todo.md` additions
   (checklist edit/duplicate/⋮-menu deferral, cover-photo-on-default-view
   revisit, the broader item→location terminology sweep).

## Workflow: one milestone at a time, with a manual-testing checkpoint

Same loop as prior stages:

1. Implement that milestone's changes.
2. Verify — `go build ./... && go vet ./...`, the CI script checks, and a
   Playwright pass at 324×756 (plus a desktop spot-check for anything
   touching layout).
3. Update `todo.md` where that milestone adds a deferred item.
4. Commit just that milestone's changes (one commit per milestone).
5. Start the dev server (`make dev`) and hand back control — **stop and
   wait** for manual testing rather than continuing automatically.
6. Resume only once told to.

## Verification

- **Backend**: `go build ./... && go vet ./...`; curl create/update a trip,
  confirm `subtitle` round-trips and `notes`/`notes_html` are gone from the
  response; re-run the Postgres migration parity check used in prior
  stages.
- **Bug fix, most important**: in the browser, open a location editor, add
  a link, confirm exactly one link appears (not more); submit a second
  link, confirm exactly two total (not three or four); submit a third,
  confirm exactly three total (not seven) — repeat for Dates. This is the
  concrete regression test for the reported 1, then 2, then 4 doubling
  pattern.
- **Frontend, mobile (324×756) and desktop (1200px)**: full route sweep
  (asserting the landed-on URL, not just absence of overflow — Stage 04
  found this matters) across all tabs including the new Settings tab and
  the now-tabless-Overview trip detail page; confirm the header no longer
  wraps a button on a long trip title (`Demo Trip: Iceland Ring Road` from
  the seed data is exactly the reported repro case); confirm the mobile tab
  grid still shows exactly 6 evenly-sized tabs; confirm the subtitle+dates
  block spans full width on desktop, not just squeezed beside the sidebar.
- **Documents**: upload a file with a note via the new inline form on both
  surfaces (trip Documents tab, location Documents card); confirm the note
  displays correctly in the list; confirm uploading twice in a row doesn't
  duplicate (same regression check as the link bug, applied to the new
  form).
- **Trip cards**: view `/trips` with at least one dated and one undated
  trip at ≥960px (3-column grid) — confirm equal card heights.
- CI stays green throughout: `go build`, `go vet`, `node --check` on
  `web/js/`, `python3 scripts/check_i18n.py` (will catch any locale added
  to only one file — several keys are being added/removed/reworded this
  stage).
