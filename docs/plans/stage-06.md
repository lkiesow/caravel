# Stage 06 — Locations: toolbar, view cleanup, full-featured create

> **Status: in progress.** Built one milestone at a time per the Workflow
> section below, each with its own commit and a manual-testing checkpoint.

## Context

A fresh round of manual testing (mobile 324×756 primarily, desktop
secondarily) on the locations area of a trip surfaced four problems, all
in `web/js` — no schema or API changes are needed:

1. **The locations tab header doesn't fit 324px.** The four filter pills
   (All / Site / Stay / Transport) still overflow their row — Stage 04
   only made them scroll internally (`base.css:1020-1037`) rather than
   fit — and the collapsed "New location" button is a ~32×44 rectangle,
   not a square, because mobile `.btn` gets `min-height: var(--tap-min)`
   (`base.css:946-948`) while `.btn-collapse` only shrinks padding
   (`base.css:1005-1008`) and never sets a matching `min-width`. The tab
   also has no way to find a location by name.
2. **The location view page is mostly empty scaffolding.** Every section
   renders unconditionally — a location with no coordinates, links, dates
   or documents still shows four cards saying "No location set." / "No
   links yet." / "No dates yet." / "No documents yet."
   (`location-view-page.js:41-105`). And its header still carries the
   `Edit` button shape that Stage 05 deliberately removed from the trip
   header for overflowing on long titles (`stage-05.md:8-25`) — this page
   was simply missed.
3. **The new-location form can't set most of a location.** Create mode
   offers only title / category / type / notes / show-on-map plus a
   staged cover photo (`location-editor-page.js:100-105`); coordinates,
   dates, links and documents are edit-mode-only, so every new location
   needs a second pass. Already logged at `todo.md:74-79`. Related: both
   create forms (location *and* trip) put the photo picker above the
   title; the title should come first.
4. **The notes textarea is too small and resizes the wrong way.** It's
   `rows="3"` with no CSS `resize`/`width` (`location-form.js:29`,
   `base.css:606-615`), so it inherits the UA default `resize: both` and
   can be dragged wider than its containing card.

Outcome: the locations tab fits and is searchable, the view page shows
only what exists, and a location can be fully described in one pass at
creation time.

Decisions taken with the user up front: search filters **client-side**
off a single fetch (title + type, case-insensitive) and the category
filter moves client-side with it; create mode gains **all** sub-resources
including documents; the filter button shows the **currently active
filter** as its label.

**Milestone 0 (not a commit of its own):** land this document as
`docs/plans/stage-06.md` before touching code, per `CLAUDE.md`.

---

## Milestone 1 — Locations toolbar: search + filter dropdown + square buttons

Replace `.items-tab__header` with a single-row toolbar:
`[ 🔍 search input (flex: 1) ][ filter button ▾ ][ + New location ]`.

**New reusable menu component — `web/js/components/menu.js`.**
Extract the popup pattern that today only exists inside
`user-menu.js:27-56`: `renderMenu(container, { triggerLabel, triggerIcon,
items, activeValue, onSelect })` rendering
`<div class="menu"><button class="menu__trigger btn btn-secondary
btn-collapse" aria-haspopup="menu" aria-expanded="false">…<ul
class="menu__dropdown" role="menu" hidden>`. Carry over verbatim: the
`hidden`-attribute visibility toggle, `aria-expanded` sync, and the
outside-click + Escape listeners added on open / removed on close. Mark
the active item with `aria-checked="true"` and a `check` icon (already in
the sprite). Leave `user-menu.js` alone — refactoring it onto this
component goes to `todo.md`.

**`web/js/pages/locations-tab.js`** — one fetch, client-side filtering:

- Fetch `/trips/${tripId}/items` once (drop the `?category=` query; the
  backend filter stays in place, just unused by this view) into an
  `allItems` array; add `applyFilters()` that renders
  `allItems.filter(byCategory).filter(bySearch)` into `.item-list`,
  called on every keystroke and menu selection — no network round trip.
- `bySearch`: `title` or `type` contains the trimmed, lowercased query.
- Search input: `<input type="search" name="q"
  data-i18n-placeholder="locations.searchPlaceholder">` inside a
  `.locations-search` wrapper holding the `search` icon; no debounce
  needed since nothing is fetched.
- Filter menu items: `all` + the three `CATEGORIES`, labels from
  `locations.filter.all` / `item.category.*`; the trigger's label is
  re-set to the chosen item's label on select.
- Two empty states instead of one: keep `locations.empty` for a trip with
  no locations at all, and add `locations.noMatches` for "0 of N match the
  current search/filter".

**Icons.** `search` and `funnel` are not in the sprite. Extend `ICONS` in
`scripts/gen_icon_sprite.py`, then regenerate per its docstring
(`npm install lucide-static --prefix /tmp/lucide-scratch`, run the
script, diff to confirm existing symbols are byte-identical). If
`funnel.svg` is absent in the installed lucide-static version, use
`filter` (the same funnel glyph under the older name) and note which was
used.

**CSS (`web/css/base.css`).** Replace the `.items-tab__header` /
`.items-filter` rules and their mobile counterparts
(`:548-584`, `:954-957`, `:1020-1037`) with `.locations-toolbar`
(flex row, `gap: 0.5rem`, `align-items: center`, no wrap),
`.locations-search { flex: 1 1 0; min-width: 0 }` with the icon absolutely
positioned and the input padded left, plus `.menu` / `.menu__dropdown`
generalized from `.user-menu__dropdown` (`:231-303`). And the square fix,
in the mobile block:

```css
.btn-collapse {
  padding: 0.5rem;
  gap: 0;
  min-width: var(--tap-min);   /* new — matches .btn's min-height */
}
```

**i18n.** Add `locations.searchPlaceholder`, `locations.noMatches`,
`locations.filter.label` (the menu's accessible name) to `en.json` +
`de.json`; also fix `locations.empty` ("No items yet." → "No locations
yet.").

**Verify.** `make ci`; Playwright at 324×756 on
`/trips/:id?tab=locations`: assert `document.documentElement.scrollWidth
<= window.innerWidth`; assert the collapsed `+` button's
`getBoundingClientRect()` width === height and both ≥ 44; assert the
filter trigger's accessible name reflects the active filter after
selecting "Stays" and that the rendered `item-card` count drops
accordingly; type a substring into search and assert the card count
matches; assert `window.location.pathname` is the trip route throughout
(the `todo.md:99-107` footgun). Repeat the filter/search assertions at
1280px wide.

**Done.** Landed as planned. `web/js/components/menu.js` is the new generic
single-select dropdown (`renderMenu`), carrying over user-menu.js's
`hidden`/`aria-expanded`/outside-click/Escape behavior and adding a
checkmark column (`role="menuitemradio"` + `aria-checked`); `user-menu.js`
itself is untouched, per plan. `locations-tab.js` now fetches
`/trips/:id/items` once into `allItems` and applies category + query in
memory via `applyFilters()`. Sprite regenerated with `search` and `funnel`
(both exist in lucide-static v1.31.0 and are byte-identical glyphs, so
`funnel` was used; the diff is purely additive — 7 lines, no existing
symbol changed). CSS: `.locations-toolbar`/`.locations-search` and a
generic `.menu`/`.menu__dropdown` block replace `.items-tab__header`/
`.items-filter` and its mobile scroller carve-out.

Three additions beyond the plan. Two are mobile tap-target fixes found
during verification: the collapsed filter trigger hides its chevron (it
otherwise doubled the icon-only button's width for no information), and
`.menu__dropdown button` gets `min-height: var(--tap-min)` under 640px
(menu rows measured 38px). The third came from testing at the checkpoint:
with the label visually hidden on mobile, nothing showed that a filter was
narrowing the list, so `renderMenu` gained a `neutralValue` option — while
the selection isn't that value, the trigger takes `.menu__trigger--active`
and tints text, icons and border in the same accent the checked dropdown
row uses (verified `rgb(37, 99, 235)` on Stay, back to default on All).
Menus that pass no `neutralValue` never highlight, so it's opt-in.

Verified with `make ci` green and Playwright assertions at 324×756 (no
screenshots committed): `scrollWidth === innerWidth === 324` with no
overflow; the toolbar is a single 44px row with search → filter → plus
left-to-right; **both the `+` and the filter button measure exactly
44×44** (the reported non-square button — it was 32×44), with accessible
names "New location" / "Filter by category" intact; selecting Stay leaves
one card and relabels the trigger to "Stay" with `aria-checked` moved;
search composes with the filter, is case-insensitive, and matches `type`
as well as `title` ("hotel" finds Foss Hotel Reykjavik); the no-matches
empty state appears only when the trip actually has locations, while a
brand-new empty trip keeps "No locations yet." even with a query;
`browser_network_requests` shows **exactly one** `/items` call across two
filter changes and five search changes (client-side filtering confirmed);
Escape and outside-click both close the menu and reset `aria-expanded`;
0 console errors; `location.pathname` stayed on the locations route
throughout. Re-verified at 1280×900: labels and chevron visible, search
477px wide, selection still filters.

## Milestone 2 — Location view: only real content, Edit at the bottom

**`web/js/pages/location-view-page.js`.**

- Wrap each of the four `.editor-card` sections in the condition that
  currently only picks the empty-branch: render the Location card only
  when `item.location?.lat != null && item.location?.lng != null` (or an
  address is set), Links only when `item.links.length`, Dates only when
  `item.dates.length`, Documents only when `docs.length`. Delete all four
  `<li class="empty">` branches. Image and notes are already conditional.
- Move the action: remove the `data-action="edit"` button from
  `.page__header` (leaving only `<h1>`, exactly as
  `trip-detail-page.js:48-50` does), and add at the very end of `.page
  .location-view` a full-width, never-collapsed
  `<button class="btn btn-secondary location-view__edit">` with
  `icon("pencil")` + a new `location.view.edit` string ("Edit this
  location"). Keep the same navigate handler.
- New CSS `.location-view__edit { width: 100%; margin-top: 1rem }`.
- Retire `item.detail.locationEmpty` from both locale files (its only
  caller disappears). `linksEmpty` / `datesEmpty` / `documents.empty`
  stay — the *editor* still uses them for its empty lists.

**Verify.** `make ci` (i18n parity catches a half-removed key); Playwright
at 324×756 against two locations from `make dev-seed` — a bare one
(assert `.editor-card` count === 0 and `leaflet-map` absent) and a fully
populated one (assert each expected card present); assert no `scrollWidth`
overflow with a deliberately long title, and that the trailing edit button
navigates to `…/edit`.

**Done.** Landed as planned, with one refinement the plan only parenthesized:
the Location card has two independent conditions rather than one, so an
address with no coordinates still gets a card (address text, no map, no
Google Maps link) instead of being hidden along with the map — `hasCoords`
and `hasAddress` are computed once and used separately. Links, Dates and
Documents cards render only when non-empty; all four `<li class="empty">`
branches are gone. The header is now `<h1>` alone and the Edit button moved
to the page's last element as a full-width, always-labelled
`.location-view__edit` ("Edit this location" / "Diesen Ort bearbeiten").
`item.detail.locationEmpty` retired from both locales;
`linksEmpty`/`datesEmpty`/`documents.empty` kept, since the editor still
uses them.

Verified with `make ci` green (i18n parity covers the retired key) and
Playwright assertions at 324×756 against three states of the seeded trip's
locations, driven through the real API so the page was re-read from the
server each time: a bare location renders **0 `.editor-card`s, 0
`leaflet-map`s, 0 `li.empty`** and reads only "Back / title / Site
landmark / Edit this location"; a fully populated one renders exactly
`["Location", "Links", "Dates", "Documents"]` with the address, one map,
the Google Maps link, both links (a labelled one and a bare URL), both
dates (a range with a label and a single date) and the uploaded document
with its size, and still 0 `li.empty`; an address-only location renders
just the Location card with the address and **no** map or maps link.
`.page__header` contains only `H1` in every case, and a 59-character title
wrapping to five lines produced no horizontal overflow (`scrollWidth ===
innerWidth === 324`) — the failure mode this milestone removes. The edit
button is 44px tall, is the page's last child, and navigating by it lands
on `…/edit`, where the editor's own "No links yet."/"No dates yet." rows
are confirmed still present. 0 console errors. Re-verified at 1280×900:
the button spans the content column exactly and keeps its label. Injected
test data was removed afterwards, leaving the seeded trip as it was.

## Milestone 3 — Form polish: notes textarea, and title before photo on new trip

Two small, independent fixes.

**Notes textarea** — `web/js/components/location-form.js:29` and
`base.css`. Mirror the pattern already used by
`.itinerary-day__notes` (`base.css:756-764`): add
`.item-form textarea { width: 100%; resize: vertical; min-height: 7rem }`
and bump the markup to `rows="6"`. Add auto-grow (it's cheap): an `input`
listener that sets `style.height = "auto"` then
`style.height = scrollHeight + "px"`, run once after prefill so an
existing long note opens at its full height; `resize: vertical` keeps a
manual drag possible. Note this form is shared by create *and* edit mode,
so both benefit.

**New trip title-first** — `web/js/pages/trip-editor-page.js:25-36`: swap
`.image-field-slot` and `.trip-form-slot` so the title/dates/subtitle
fields come first and the cover photo second. Since `renderTripForm`
renders its own Save/Cancel row, split the single card into two
`.editor-card`s (Basic info + Cover photo) so the actions still sit at the
bottom of the last thing above them rather than mid-page; this matches the
`settings-tab.js:19-31` edit layout (Basic Info → Cover Photo), making
create and edit consistent. Add `trip.editor.coverPhoto` if no existing
key fits (`settings-tab.js` already labels these cards — reuse its keys).
Update the stale header comment at `trip-editor-page.js:8-20`.

**Verify.** `make ci`; Playwright: on `/trips/:id/locations/:itemId/edit`
assert the notes textarea's computed `resize === "vertical"`, its
`clientWidth` ≤ its parent card's content width, and that its height grows
after typing several newlines; on `/trips/new` assert the title input's
`getBoundingClientRect().top` is less than the image field's, at both
324px and 1280px.

**Done.** Both halves landed, with one deviation from the plan's sketch of
the trip form. The plan said to split the create page into two cards "so
the actions still sit at the bottom of the last thing above them" — but
`renderTripForm` renders its own action row, so with fields first and photo
second, Save would have landed mid-page above the photo card. Instead
`renderTripForm` gained `showActions: false` (returning
`{ submit: () => form.requestSubmit() }`) and the page owns a `.editor-actions`
row below both cards. That's exactly the seam Milestone 4 needs for
`renderItemForm`, so it's built once here and reused there.
`settings-tab.js` passes nothing new and keeps its in-form Save/Cancel.
Cards are labelled Basic info / Cover photo, matching the Settings tab's
edit order.

Notes textarea: `rows="6"`, and `.item-form textarea` gets
`width: 100%; min-height: 7rem; resize: vertical` (the
`.itinerary-day__notes` pattern). Auto-grow is an `input` listener plus one
call after prefill. It adds `offsetHeight - clientHeight` to `scrollHeight`
because everything is `box-sizing: border-box` — without the borders the box
is 2px short, which is enough to leave a scrollbar.

Verified with `make ci` green and Playwright at 324×756 and 1280×900.
Textarea: computed `resize === "vertical"` (was the UA default `both`,
draggable wider than its card); 258px wide inside a 260px card content box;
132px tall at rest (was ~76px at `rows="3"`); grows to 246px on 12 typed
lines with no scrollbar and shrinks back to the 132px floor when cleared; a
15-line note loaded from the server opens at 303px fully expanded. Same on
the create form, which shares the component. Trip create: cards read
`["Basic info", "Cover photo"]`, the title input's `top` is above the image
field's, `.editor-actions` is the page's last child, and **zero**
`.trip-form__actions` rows remain inside the form. Most importantly the new
page-level button really creates: clicking Create with a title POSTs and
lands on the new trip (repeated from a fresh load to rule out a flake after
one inconclusive first attempt), an empty title still surfaces "title is
required" inside the form rather than navigating, and Cancel returns to
`/trips`. The Settings tab still shows exactly one in-form Save/Cancel, no
page-level row, and saving from it still persists (subtitle round-tripped
through the API). 0 console errors; all test trips created during
verification were deleted afterwards.

## Milestone 4 — New-location form: every field, at creation time

Restructure create mode in `web/js/pages/location-editor-page.js` to the
same multi-card shape as edit mode, in the order the user asked for, with
one `Create` action at the bottom that flushes everything:

1. **Basic info** — `renderItemForm` (title first, then category, type,
   notes, show-on-map) **without** its own action row.
2. **Cover photo** — `renderImageField` in staging mode (unchanged).
3. **Location** — the same lat / lng / address inputs as edit mode, but
   with no per-card submit button; values are read at create time. (A
   click-to-pick map picker is *not* in scope — deferred, see below.)
4. **Dates** — the existing `.date-form` + `.date-list`, appending to an
   in-memory array instead of `POST`ing.
5. **Links** — same treatment as dates.
6. **Documents** — the `document-list.js` form, holding picked `File`
   objects in memory and listing them with a remove button.
7. **Actions** — `Create location` + `Cancel` at the true bottom of the
   page.

**Implementation shape.** The cleanest split, given both modes now render
the same six cards:

- Add an optional `{ staged }` mode to the sub-resource renderers so each
  list works against an in-memory array with no network calls. Concretely:
  keep `renderLinksList`/`bindLinkForm`, `renderDatesList`/`bindDateForm`
  in `location-editor-page.js` (preserving the split-to-avoid-stacked-
  listeners discipline documented at `:209-217`) but have their submit
  handlers branch — `item ? await api.post(...) : pushLocal(...)` — with
  client-generated temp ids for the list keys.
- Extend `renderDocumentList` (`web/js/components/document-list.js`) with
  a staging mode: when called with `{ onStaged }` instead of a `path`, it
  keeps `File`s locally, echoes filenames + `formatSize`, and never
  fetches. Same rebuild-on-every-render discipline as today
  (`document-list.js:16-24`).
- Move `renderItemForm`'s action row out of the component (new option
  `{ showActions: false }`) so the page owns the single bottom Create
  button in create mode; edit mode keeps its per-card Save/Cancel exactly
  as today. The page's Create handler calls the form's exported
  `submit()`-equivalent — simplest is for `renderItemForm` to return
  `{ submit, readBody }` and for the page to wire its own button to it.
- **Flush order on create** (extending the staged-image sequence already
  at `:121-140`): `POST /trips/:id/items` → then, for whatever was
  staged, `POST /trips/:id/media` (+ `PUT /items/:id/image`),
  `PUT /items/:id/location`, `POST /items/:id/links` ×n,
  `POST /items/:id/dates` ×n, `POST /items/:id/documents` ×n (raw
  multipart `fetch`, as `api.js` can't do multipart). No backend change:
  every one of these endpoints already exists (`router.go:92,142-152`).
- **Partial-failure handling**, same policy as the existing staged image:
  the location itself is already created, so on any sub-resource error
  `window.alert` the message and navigate to `…/edit` (the only place to
  retry) instead of the view page; on full success navigate to the view
  page. Collect failures and report once rather than alerting per link.
- Fix the leftover `location.editor.createButton` = "Create item" →
  "Create location" (`en.json:58`, `de.json`) while here; the broader
  item→location sweep (`todo.md:177-190`) stays deferred.
- Rewrite the create-mode half of the header comment at `:9-24`, which
  currently documents the exact limitation this milestone removes.

**Verify.** `make ci`. Playwright at 324×756: fill a new location with
title, coordinates, two dates, two links, one document (and a cover
photo), submit once, assert it lands on `/trips/:id/locations/:newId`
(not `/edit`), and assert the view page then shows the map, both dates,
both links and the document — i.e. verify against the *server's* state via
a fresh page load, not just the form's DOM. Also assert staged rows can be
removed before submit, and that submitting with title only still works
(no empty sub-resource requests fired — check `browser_network_requests`).
Re-run the happy path at 1280px.

**Done.** Landed, with the two modes converged further than the plan
described. Rather than create mode growing a second set of cards, both modes
now render the *same* six cards from one template — Basic info, Cover photo,
Location, Links, Dates, Documents — and only the write timing differs. Two
deviations worth noting: the plan's card order put Dates before Links, but
Links-then-Dates was kept so create, edit and the read view all match; and
`renderItemForm` did not need a `readBody()`, just `showActions` plus the
same `{ submit }` return Milestone 3 added to `renderTripForm`. Small
supporting changes: `renderDocumentList` gained a staging mode
(`path: null` + a `staged` array; renders from `File` objects, no fetch, no
confirm on remove, "Add file" instead of "Upload"), the link/date list
renderers switched from server ids to array indices so one code path serves
both modes, and `location.editor.createButton` is finally "Create
location"/"Ort erstellen".

Verified with `make ci` green and Playwright at 324×756 plus 1280×900,
always asserting against a server re-read rather than form state. Create
with everything filled (title, category, type, a two-line note,
coordinates + address, two links — one labelled, one bare URL — two dates —
a range with a label and a single date — and a document with a note): a
single click lands on `/trips/:id/locations/:newId`, **not** `/edit`, and a
fresh load of that URL shows the map, the address, both links, both dates
and the document, with the API confirming every field including
`show_on_map`. Nothing is written before that click — the network log showed
zero `/items` requests while staging. Title-only create fires **exactly
one** POST and no empty sub-resource calls. Staged rows can be removed
before submit, and removing the *first* of two links/dates/documents removes
the right one (index handling). The partial-failure branch was exercised by
stubbing only the links POST to 500: one alert with the server's message,
landing on `…/edit`, and the date queued *after* the failed link still
written — failures are collected, not thrown, so one bad row doesn't drop
what follows. Empty title still shows "title is required" without
navigating; Cancel returns to the trip.

Edit mode re-verified as unchanged behavior on the shared code: its own
in-form Save/Cancel, the per-card "Save location" button, a Delete card, no
page-level action row, "Upload" (not "Add file"), and adds/removes hitting
the API immediately with the empty rows returning afterwards. Including the
Stage 05 duplicate-listener regression test, since these handlers were
rewritten: three sequential link adds gave 1 → 2 → 3 server-side and
rendered (not 1 → 2 → 4), same for dates. 0 console errors; all six
locations created during verification were deleted, leaving the seeded trip
with its original three.

## Milestone 5 (bonus) — Editor rows: aligned on desktop, full width on mobile

Added mid-stage after reviewing Milestone 4 on a real phone and on desktop.
Two complaints, both about the editor's sub-resource rows (Location, Links,
Dates, Documents, and the Image card), and both applying to create *and*
edit mode since they share one template now:

- **Desktop: the blue buttons don't line up.** Each row's inputs sat at
  their intrinsic widths, so its submit button stopped wherever those
  happened to end — four different horizontal positions down one page, which
  reads as clutter. Fix: every control in these rows grows (`flex: 1 1 8rem;
  min-width: 0`), which pushes each button flush against its card's right
  edge; the cards share a width, so the buttons line up with each other. The
  page-level "Create location" stays left, as the page's own action rather
  than a row's. The Location card's fields also move from label-beside-input
  to the stacked label-above-input shape `.item-form` uses in the card
  directly above it.
- **Mobile: rows wrapped badly and the add button became a stray "+".** At
  324px the controls wrapped at intrinsic widths, leaving half-width date
  inputs beside dead space, and the submit button wrapped onto a line of its
  own — where, collapsed to icon-only by `.btn-collapse`, it read as a stray
  button rather than that row's action. Fix: under 640px each control goes
  `flex-basis: 100%`, one per line, and the submit button goes full width
  *with* its label — a new `.btn-row` class replacing `.btn-collapse` on
  these four buttons (Add link / Add date / Add file-or-Upload / Set), since
  a full-width bare "+" would be worse than either option.

**Done.** Landed as described. `.btn-row` is the shared class for an
add-row's submit button: intrinsic width and flush right on desktop, full
width on mobile, labelled at both. One thing the plan above didn't
anticipate: `.image-field__controls` and `.image-field__url-form` needed
`flex-direction: column` on mobile, not just `align-items: stretch` — they're
row-direction flex containers, so stretch only equalizes heights, and with
the Set button going full width inside a row the URL input was squeezed to
an unusable 18px sliver. Caught by measuring, not by looking.

Verified with `make ci` green and Playwright at 1280×900 and 324×756, in
both create and edit mode. Desktop: all four row buttons in create mode
(Set / Add link / Add date / Add file) report an identical `right` of 1087px,
equal to the card's content edge; in edit mode all five (plus Save location)
match at 1087. Delete stays left. The Location card's three labels compute
`flex-direction: column`. Mobile: every control in all five forms measures
exactly the card's content width (borders included in the maths — the first
run's 258-vs-260 "failure" was my measurement forgetting the card's 1px
borders, not a layout bug), each form puts one control per row (3, 3, 3, 4, 3
controls in as many rows), every `.btn-row` is ≥44px tall and keeps a visible
label, and there's no horizontal overflow. The two other places these
components appear were re-checked at 324px for regressions: the trip
Documents tab and Settings' image field both stack into three full-width
controls. 0 console errors.

Two follow-ups from testing at the checkpoint, both folded into this
milestone's commit. **Copy:** the image button read just "Set" while its
neighbours read "Add link"/"Add date"/"Add file", so it became "Set image"
("Bild setzen") — verb-plus-noun like the rest, and long enough not to look
like a different kind of control. **Colour:** with five accent-blue buttons
stacked down one page the blue stopped meaning anything, so every row button
moved to `.btn-secondary` — the neutral outlined style the checklist's own
"Add" button already uses — including "Save location", which sits in a field
row and so reads as one of them. That leaves "Create location" as the only
blue button in create mode, and Basic info's "Save" as the only one in edit
mode (deliberately kept: it's that page's nearest equivalent to a primary
action, and an editor with no accent at all reads flat). Delete stays red.
Verified by enumerating every button on the edit page by computed
`backgroundColor` rather than by eye, and re-checking that all five row
buttons still share the 1087px right edge.

Noticed while verifying, not changed: `cmd/seed/main.go` never sets
`ShowOnMap`, so every seeded demo item is `show_on_map: false` (confirmed on
both demo trips, including one this session never touched) and the seeded
trip's Map tab is therefore empty. Logged to `todo.md`.

---

## Build order

1 → 2 → 3 → 4, one commit per milestone. Milestones 1–3 are
self-contained; 4 is the largest and benefits from 3's textarea fix
already being in place (it renders the same form). Follow the
`CLAUDE.md` loop for each: implement, verify (`make ci` + Playwright),
add a **Done.** paragraph to `docs/plans/stage-06.md`, update
`docs/plans/todo.md` in both directions, commit, make sure `make dev` is
running, then stop and wait.

## Workflow: one milestone at a time, with a manual-testing checkpoint

Same loop as prior stages (and as `CLAUDE.md` states):

1. Implement that milestone's changes.
2. Verify — `make ci` green, plus a Playwright pass at 324×756 (and a
   desktop spot-check for anything touching layout), preferring
   assertions over screenshots.
3. Add a **Done.** paragraph to this document's milestone section, and
   update `todo.md` in both directions (remove what landed, add what was
   deferred).
4. Commit just that milestone's changes (one commit per milestone).
5. Start the dev server (`make dev`) and hand back control — **stop and
   wait** for manual testing rather than continuing automatically.
6. Resume only once told to.

## Deferred to `docs/plans/todo.md`

New entries this stage adds:

- **Click-to-pick coordinates on a map.** Both create and edit still use
  raw lat/lng number inputs; `leaflet-map.js` is read-only (attribute-
  driven, no click handler). A picker (click the map, or search an address
  via a geocoder) is the natural next step and is what makes the location
  card actually pleasant on a phone — deliberately out of Stage 06 to keep
  Milestone 4 to plumbing already-existing endpoints.
- **Server-side search/sort for locations.** Stage 06 filters
  client-side off one fetch, which is right for realistic trip sizes but
  returns every location unconditionally; if a trip ever holds hundreds,
  `ListItemsByTrip` needs a `q` predicate and pagination. Pairs with the
  existing trips-list entry (`todo.md:133-140`).
- **Refactor `user-menu.js` onto the new `components/menu.js`**, and use
  the same component for the ⋮ contextual menus the checklist entry
  (`todo.md:159-169`) wants. Stage 06 built the generic component but
  wired only the locations filter to it, leaving two popup
  implementations in the tree.
- **Create-mode flush isn't atomic.** A location whose sub-resource
  writes partially fail is left half-populated, with the user bounced to
  the edit page to finish by hand. A transactional create (nested
  `location`/`links`/`dates` on `itemRequest`, `handleCreateItem`
  inserting them in one tx) would fix it properly; documents can't ride
  along regardless, being multipart.

Entries removed by this stage: `todo.md:74-79` (create mode lacking
location/links/dates/documents) — implemented by Milestone 4.

Entries left untouched and still open: the item→location terminology
sweep (`:177-190`), the Playwright suite + scripted mobile sweep
(`:85-107`), trip cover photo visibility (`:170-176`), Leaflet-vs-Google
re-evaluation (`:116-120`).

## Verification (stage-level)

- `make ci` green at every commit.
- A single 324×756 Playwright pass at the end walking
  `/trips` → trip → locations tab → search + filter → new location (all
  fields) → view page → edit page, asserting on each route:
  `window.location.pathname` is the intended route,
  `document.documentElement.scrollWidth <= window.innerWidth`, and every
  interactive control ≥ 44px in its constrained dimension.
- The same walk at 1280px to confirm the desktop toolbar keeps its
  labelled filter button and full-width search.
