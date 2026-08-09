# Stage 02 — UI/UX Review Fixes

> **Status: in progress.** Built one milestone at a time per the Workflow
> section below, each with its own commit and a manual-testing checkpoint.

## Context

Stage 01 delivered a fully working Caravel v1 (trips, items, media, map,
itinerary, documents, PWA/i18n, hardening — see `docs/plans/stage-01.md`).
This is the first hands-on user review of that build. The review surfaced a
consistent architectural problem across the app — **editors embedded inline
alongside read-only content, with inconsistent/incomplete field coverage** —
plus a broken map (real bug), a couple of scope gaps (item date ranges,
document notes), a naming complaint, a general styling/consistency pass, and
one thing Stage 01's own plan called for but never actually implemented:
[Lucide icons](https://lucide.dev/). Stage 02 fixes all of it, organized so
it can be built and tested incrementally, milestone by milestone, the same
way Stage 01 was.

Grounding for this plan came from two Explore passes over the current
codebase (trip/item editors & detail views; itinerary/documents/map/CSS) —
file:line references below reflect the actual current state, not
assumptions.

**Decisions locked in with the user before writing this plan:**
- Rename category `location` → `site` **in the database**, not just the
  label (tab "Items" → "Locations", category "Location" → "Sites", filter
  pills become All/Sites/Stays/Transport). This needs a real migration.
- The new multi-file "Add document" flow gets **one note field per file**,
  not one shared note for the whole batch.
- **The rename happens before the editor-unification work**, not after.
  Section 2 below introduces several brand-new frontend files and routes
  for items (an item detail view, an item editor, item-related navigation)
  that don't exist yet in Stage 01 — if the rename landed second, that new
  code would be written with "item" naming and then immediately need
  reworking. Doing the rename first means the new files/routes in Section 2
  get "location" naming from the moment they're created, not as a
  follow-up edit.

**Scope of the rename** — to keep this from ballooning into a full-codebase
refactor for a cosmetic change: the backend's Go identifiers, DB table name
(`items`), and HTTP API routes (`/api/items/...`) **stay as `item`** —
purely internal, never user-visible, and renaming them is all risk (touching
every handler, query, and struct) for zero visible benefit. What *does*
change: the category's stored **value** (`location` → `site`, since that
value round-trips through the API and is genuinely part of the data model
the rename is about), user-facing labels, and — because they're being
touched heavily in Section 2 regardless — a few frontend file names so the
codebase reads consistently going forward (see Section 1).

**A bug found during exploration, fixed as part of this stage:** SQLite
connections never set `PRAGMA foreign_keys=ON` (`internal/db/db.go`), so
`ON DELETE CASCADE` has silently been a no-op on SQLite this whole time —
deleting a trip orphans its items/documents/etc. instead of cascading.
Postgres wasn't affected (it enforces FKs by default). Fixing this pragma
also matters for the category-rename migration below, which needs to
recreate the `items` table (SQLite can't `ALTER` a `CHECK` constraint).

## 1. Naming: "Items" → "Locations", category "Location" → "Site"

- **Backend**: new migration renaming the stored category value. SQLite
  can't `ALTER` a `CHECK` constraint — recreate the table (create
  `items_new` with `CHECK (category IN ('site','stay','transport'))`,
  `INSERT INTO items_new SELECT ... CASE WHEN category='location' THEN
  'site' ELSE category END ...`, drop old, rename, recreate the
  `idx_items_trip_id_category` index), wrapped with
  `PRAGMA foreign_keys=OFF` / `=ON` around the swap (do the FK-pragma fix
  first so this migration runs under correct semantics). Postgres:
  `UPDATE items SET category='site' WHERE category='location';` then
  drop/recreate the `CHECK` constraint. Also update
  `internal/httpapi/items.go`'s `validCategories` map. Go identifiers, the
  `items` table name, and `/api/items/...` routes are otherwise unchanged
  (see Context above).
- **Frontend rename** — mechanical, done now so Section 2's new code is
  written against the final names instead of needing rework:
  - `web/js/pages/items-tab.js` → `locations-tab.js`
  - `web/js/components/item-form.js` → `location-form.js`
  - `web/js/components/item-card.js` → `location-card.js`
  - (`item-detail.js` is *not* renamed here — Section 2 replaces it
    entirely with two new files, so there's nothing to rename.)
  - Every `location|stay|transport` category array/color map in the above
    files plus `itinerary-tab.js` and `leaflet-map.js` (`CATEGORY_COLORS`
    keys): `location` → `site`.
  - Translation keys: `item.category.location` → `item.category.site`
    ("Sites"), `items.*` → `locations.*` (`locations.new`,
    `locations.empty`, `locations.filter.all`), `item.form.*` →
    `location.form.*`, `trip.tabs.items` label → "Locations" (rename the
    key too, e.g. `trip.tabs.locations`, for consistency with the above).
    Do this renaming in both `web/locales/en.json` and `de.json` together
    so neither file is ever missing a key.

## 2. Unify create/edit into one full editor, separate from read-only views

Today, "New Trip" and "New Item" open an **inline form** that pushes
existing content down (`web/js/pages/trips-page.js` lines 13, 44–54;
`items-tab.js` — now `locations-tab.js` per Section 1 — lines 21, 57–68) —
a different, *smaller* form than what you get when editing (missing the
image field entirely). Worse, `item-detail.js` currently **is** the item's
create/edit form, location editor, links editor, dates editor, and document
manager all rendered live, simultaneously, with no read-only mode at all
(confirmed: every section in `web/js/components/item-detail.js`, lines
10–96, renders as an always-active form) — so clicking an item never gives
you a clean "just show me the details" view, and clicking "New Item" while
one is already open leaves two editors on screen at once. Trip editing has
the same split-personality problem: the image is editable live on the
Overview tab (`trip-detail-page.js` lines 62–63, 80–87) while title/dates/
notes require clicking "Edit" to reveal yet another inline form (lines 73,
90–100) that duplicates `trip-form.js`.

**Fix: one full editor per entity, reached by navigation, separate from a
read-only view.** Concretely, add real routes (the router already
generically supports this — `web/js/router.js` — it's just new entries in
the `routes` array in `web/js/app.js` lines 10–13). These routes and files
are new (nothing to rename — Stage 01 never had item-level routes at all),
so they're written directly with the post-Section-1 naming:

| Route | Page | Purpose |
|---|---|---|
| `/trips/new` | `trip-editor-page.js` | Create a trip |
| `/trips/:tripId/edit` | `trip-editor-page.js` | Edit trip (title, dates, notes, image) + Delete |
| `/trips/:tripId/locations/new` | `location-editor-page.js` | Create an item |
| `/trips/:tripId/locations/:itemId/edit` | `location-editor-page.js` | Edit item (core fields, image, location, links, dates, documents) + Delete |
| `/trips/:tripId/locations/:itemId` | `location-view-page.js` | Read-only item detail |

Router ordering note: `/trips/new` vs `/trips/:tripId` (and
`/trips/:tripId/locations/new` vs `/trips/:tripId/locations/:itemId`) have
the same segment count, so the literal `new` pattern must be listed
**before** the `:param` pattern in the `routes` array, or `match()`
(`router.js` lines 5–19) will swallow "new" as the param value.

**Trip editor** (`trip-editor-page.js`, new): one page, two modes based on
whether `:tripId` is present. Reuses `trip-form.js`'s fields (title, dates,
notes) plus adds the image field (`image-field.js`, currently only reachable
from Overview) and, in edit mode, the Delete action (moved off Overview).
In create mode there's no image field yet — a trip needs to exist before an
image can attach to it — so on first successful save, client-side
`history.replaceState` to `/trips/:newId/edit`, which now shows the full
editor including the image field. This is the one unavoidable asymmetry
between create and edit and is worth a one-line code comment explaining why,
not a design flaw to work around.

**Item editor** (`location-editor-page.js`, new): same create→edit-in-place
pattern. Core fields (category/type/title/notes/show_on_map, from
`location-form.js`) are available immediately; image, location, links,
dates, and documents (today's always-on sections in `item-detail.js`) only
become available once the item exists, so create-mode saves and redirects
to the edit route exactly like the trip editor.

**Item view** (`location-view-page.js`, new): read-only — title/category
badge, notes, image, location as address text **plus an embedded map**
(reuse `<leaflet-map>` in a single-marker mode — add a small variant/
attribute path rather than a second component) with a "View on Google
Maps" link, links as a clickable list, dates displayed as ranges (see
Section 4), documents listed with download links. One "Edit" button → the
edit route.

**Trip detail page changes** (`trip-detail-page.js`): Overview tab becomes
pure read-only (image thumbnail, dates, notes — no live `image-field`, no
inline `trip-form`); its "Edit" action navigates to `/trips/:tripId/edit`
instead of rendering a form. Add a small edit/settings icon-button near the
page header (next to the back link) as a second entry point to the same
route, reachable from any tab. Default active tab changes from `"overview"`
to `"items"` (Locations) per the review — that's the landing view users
actually want.

**Locations tab changes** (`locations-tab.js`): "New item" navigates to
`.../locations/new` instead of rendering `location-form` inline (drop the
inline form slot). Clicking a `<location-card>` navigates to
`.../locations/:itemId` via `history.pushState` + synthetic `popstate` (the
same pattern `trip-card`'s `trip-open` handler already uses —
`trips-page.js` lines 39–42) instead of rendering `item-detail` into an
in-page slot (drop that slot too — `item-detail.js` is fully replaced by
the two new pages above and can be deleted once they're in place).

## 3. Map: fix the OpenStreetMap Referer error

Root cause found: `internal/httpapi/security.go`'s `securityHeaders`
middleware sets `Referrer-Policy: same-origin` (line ~28), which suppresses
the `Referer` header on **all** cross-origin requests — including the
browser's tile requests to `tile.openstreetmap.org`, which OSM's usage
policy requires a `Referer` for. Fix: change the value to
`strict-origin-when-cross-origin` (the modern browser default — still sends
at least the origin cross-origin over HTTPS, satisfying OSM, while staying
reasonably private). One-line change, registered globally in
`internal/httpapi/router.go`.

## 4. Item dates: support ranges

The backend already fully supports `end_date`/`all_day`/`start_time`/
`end_date` (`internal/db/domain.go` `ItemDate` struct) — this is a pure
frontend gap. The date form (currently in `item-detail.js` lines 53–59, to
be moved into `location-editor-page.js`'s dates section) only has a
`startDate` input; add an optional `endDate` input, send `end_date` in the
`POST /items/{id}/dates` body, and display existing dates as `start – end`
when `end_date` is set (both in the editor's date list and in
`location-view-page.js`).

## 5. Itinerary: entry thumbnails + click-through

Today each day's entries show a plain colored dot + title + note
(`itinerary-tab.js` `renderEntries()`, lines 102–116) with no image and no
click behavior. Add:
- **Backend**: `ListItineraryEntriesByTrip` (query +
  `ItineraryEntryDetail`) needs the item's `image_id` so the handler can
  resolve an image URL, matching the existing pattern in `tripToResponse`/
  `itemToResponse` (`internal/httpapi/trips.go`, `items.go`) — worth
  extracting that repeated "resolve media asset → URL" snippet into one
  shared helper in `media.go` and using it in all three response builders
  instead of duplicating it a third time.
- **Frontend**: entry row shows a small thumbnail (fallback to today's
  colored dot when no image) and is clickable, navigating to
  `/trips/:tripId/locations/:itemId` (the new item view page from
  Section 2).

## 6. Documents: redesign the upload flow, add per-file notes

- **Backend**: new migration adding a nullable `note TEXT` column to
  `documents` (both dialects — this is a genuinely new migration on top of
  the existing one, unlike Stage 01 where amending 0001 in place was safe
  because no real data existed yet; that's no longer true). Update
  `Document`, `CreateDocumentParams`, the `CreateDocument` query,
  `documentResponse`, and `uploadDocument` in `internal/httpapi/documents.go`
  to accept a `note` form field.
- **Frontend** (`document-list.js`): today, picking a file and clicking
  "Upload" are the only two steps, no note field at all (lines 13–16,
  43–58) — replace with an "Add document" button that opens a native
  `<dialog>` modal (no need for a custom modal component — `<dialog>` covers
  `showModal()`/backdrop/focus-trapping natively, fitting the "plain JS, no
  framework" approach) supporting multi-file selection (drag-and-drop is a
  nice-to-have, not required), rendering **one note input per selected
  file**. Confirming the dialog uploads each file with its own note; the
  document list itself becomes purely read-only rows (filename, size,
  **note**, download link, delete) — rename the "Upload document" trigger
  to "Add document".

## 7. Icons: vendor Lucide (deferred from Stage 01)

Stage 01's own plan named [Lucide](https://lucide.dev/) as the icon set but
never actually wired it in — every icon-shaped thing in the app today is
either absent, a raw `&times;`/`×` character (remove buttons throughout
`item-detail.js`, `document-list.js`, `itinerary-tab.js`), or plain text.
Doing this now, before the styling pass in Section 8, means that pass can
give buttons a real icon+label treatment (e.g. a trash icon on delete
buttons, a pencil on Edit, a plus on the various "Add ..." buttons, an
arrow on "Back") instead of text alone.

**Vendoring approach** (mirrors how Leaflet was vendored in Stage 01 —
`web/js/vendor/leaflet/`): `npm install lucide-static` in a scratch
directory (it ships each icon as a standalone, dependency-free SVG file —
no React/JS wrapper, exactly what a no-framework app needs), then build a
single SVG sprite containing only the icons this app actually uses, each
wrapped as `<symbol id="lucide-{name}" viewBox="0 0 24 24">...</symbol>`,
committed at `web/icons/lucide-sprite.svg`. A small script
(`scripts/gen_icon_sprite.py` or a `Makefile` target, following the
precedent of `scripts/gen_icons.py` for the app's own PWA icons) generates
it from a curated name list, so adding an icon later is a one-line addition
and a re-run — not a manual copy-paste. Starter icon list (extend as
needed while implementing the styling pass): `pencil` (edit), `trash-2`
(delete), `plus` (add), `x` (close/remove — replacing the raw `×`
characters), `arrow-left` (back), `upload` (upload/add document), `image`
(image field), `map-pin` (location/site), `link` (links), `calendar`
(dates), `file-text` (documents), `log-out` (user menu), `chevron-down`
(dropdown affordance), `map` (map tab), `list` (locations tab).

**Usage**: reference the sprite by file path, not by same-document ID —
`<svg class="icon"><use href="/icons/lucide-sprite.svg#lucide-trash-2"></use></svg>`.
This matters because several buttons live inside Shadow DOM components
(`trip-card`, `location-card`); a `<use href="#id">` referencing a
`<symbol>` elsewhere in the *same* document does not pierce shadow
boundaries, but a `<use>` pointing at an external file (even
`/icons/lucide-sprite.svg#id`) is a resource fetch and works identically
inside or outside a shadow root. Add a tiny helper — a plain function (e.g.
`icon(name)` in a new `web/js/icon.js` returning the `<svg>...</svg>`
markup string) rather than a full custom element, since every call site
already builds markup via template strings and a helper function composes
into that directly with no extra machinery. Style `.icon` in `base.css`
(e.g. `width/height: 1em`, `vertical-align`, `fill: currentColor` so icons
inherit the button's text color automatically in both themes without
per-icon color rules).

## 8. Styling pass

- **Links**: no bare `a { color: ... }` rule exists — only specific classes
  (`.link-button`, `.back-link`) use `--color-accent`; plain anchors (the
  document download link, popup "View on Google Maps" link, item links
  list) fall back to browser-default blue/purple, clashing with dark mode.
  Add a base `a, a:visited { color: var(--color-accent); }` in
  `web/css/base.css`.
- **Text selection**: no `::selection` rule exists anywhere — add a
  theme-aware one (`::selection { background: var(--color-accent); color:
  white; }`, working in both light/dark since `--color-accent` is already
  themed).
- **Buttons**: introduce a small shared class system in `base.css` —
  `.btn` (base: padding, border-radius, border, cursor, consistent
  `box-sizing`/height, `display: inline-flex; align-items: center; gap:
  0.4em` so an icon and label sit together cleanly) with
  `.btn-primary`/`.btn-secondary`/`.btn-danger` modifiers — and apply it to
  every button currently missing explicit styling (confirmed unstyled: the
  "New item" button in `locations-tab.js`, `image-field.js`'s "Set" and
  "Remove" buttons — the exact height mismatch the user saw, since the
  adjacent URL `<input>` has explicit padding/border and these buttons
  don't). Apply the same classes consistently to the *already-styled*
  buttons too (trip/item editor save/cancel, document dialog actions,
  itinerary add-day/add-item, login page) so everything matches one system
  instead of N one-off rules. Pair each with its Section 7 icon where one
  fits (Delete → `trash-2`, Edit → `pencil`, the various "Add ..." buttons
  → `plus`, Back → `arrow-left`), and swap every raw `×`/`&times;`
  remove-button character for the `x` icon for a consistent look.
- **File input styling**: give the documents dialog's file picker the same
  styled-label-wrapping treatment `image-field.js` already uses for its
  upload control, instead of a bare `<input type="file">`.

## 9. Header: user menu

Currently `web/js/app.js` (lines 16–23) renders the app title, then a plain
`<span>${user.display_name}</span>`, then the logout button as flat
siblings — no avatar, no menu, username sits right next to the app title
instead of the far right. Replace with a small user-menu (new
`web/js/components/user-menu.js`, or a plain render function in `app.js` —
either is fine): an initials avatar (first letter of `display_name`,
uppercased, circular, `--color-accent` background/white text) at the far
right of the header, with a small `chevron-down` icon (Section 7) next to
it as an affordance; the name shown next to it on desktop, hidden at
narrow/mobile widths (avatar alone remains tappable); clicking the avatar
toggles a small dropdown containing a "Log out" item (paired with the
`log-out` icon) — structured so more items can be added later — this is
explicitly a "for now" scope per the review, admin options come later.

## Build order

1. **Naming** (Section 1) — category-rename migration + FK pragma fix,
   `validCategories`, and the mechanical frontend file/translation-key
   renames. Done first so every later milestone builds on final names.
   **Done.**
2. **Vendor Lucide icons** (Section 7) — sprite generation script, `icon()`
   helper, `.icon` CSS. Small, self-contained, and needed by nearly every
   milestone after this one. **Done.**
3. **Backend groundwork (remaining)** — documents `note` column migration,
   itinerary entry image resolution (+ shared media-URL helper),
   Referrer-Policy fix. Independently testable via curl. **Done.**
4. **Routing + trip editor unification** (Section 2, trip half) — new
   routes in `app.js`, `trip-editor-page.js`, Overview becomes read-only,
   default tab → Locations. **Done.**
4.5. **Trip editor follow-up** (hands-on review of Milestone 4) — cover
   photo available directly in create mode via client-side staging
   (`image-field.js` staging mode; upload+attach folded into the same save
   that creates the trip; image stays fully optional), plus three
   consistently-styled bordered cards (Basic Info / Cover Photo / Delete)
   replacing the previous inconsistent styling. **Done.**
5. **Item view/editor split** (Section 2, item half) — `location-view-page.js`,
   `location-editor-page.js`, `locations-tab.js` navigation + card
   thumbnails, item date ranges (Section 4). **Done.**
6. **Itinerary entry thumbnails + click-through** (Section 5).
7. **Documents redesign** (Section 6) — add-document dialog, per-file
   notes, read-only list.
8. **Styling pass** (Section 8) — links, selection, button system + icons,
   input/button height consistency.
9. **Header user menu** (Section 9).

## Workflow: one milestone at a time, with a manual-testing checkpoint

Unlike Stage 01 (built straight through, reviewed only at the end), Stage 02
is a direct response to hands-on user review — so each of the 9 milestones
above gets its own review checkpoint, not just a final one. For each
milestone, in order:

1. Implement that milestone's changes.
2. Verify it myself (build/vet/curl for backend pieces; a Playwright
   click-through for UI pieces) — the same baseline checks used throughout
   Stage 01, so obviously broken states never reach the user.
3. Commit just that milestone's changes (one commit per milestone, not one
   giant commit at the end — mirrors how Stage 01 was committed as a whole
   only because it was reviewed as a whole; here each piece is reviewed
   separately, so each piece is committed separately).
4. Start the dev server (`make dev`, live-reload against `web/`) and hand
   back control — **stop and wait** for manual testing rather than
   continuing to the next milestone automatically.
5. Resume only once told to — either "continue" (moves to the next
   milestone) or with feedback/bugs to fix first (addressed, re-verified,
   and committed as a follow-up before moving on).

This applies to all 9 milestones, including the last — Stage 02 isn't
"done" until milestone 9 has been through this same review loop.

## Verification

- Backend: `go build ./... && go vet ./...`, plus curl smoke tests for the
  category rename (create an item, confirm `category: "site"` round-trips,
  confirm the old value is rejected) and the documents note field.
- Migrations: run against a **non-empty** SQLite DB (one with existing
  trips/items from Stage 01 testing) to confirm the category-rename table
  swap preserves data and existing rows migrate from `location` → `site`;
  re-run the Postgres parity smoke test from Stage 01's Milestone 10 for the
  same migration.
- Frontend: browser click-through per milestone (Playwright, as in Stage
  01) — particularly confirm no double-editor state is reachable anymore,
  confirm the map actually renders tiles (not just that the error is gone),
  confirm icons render correctly *inside* Shadow DOM components
  (`trip-card`/`location-card`, not just in plain-DOM pages — this is the
  one place the sprite-reference approach could silently fail), and confirm
  mobile-width header/user-menu layout.
