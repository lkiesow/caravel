# Stage 26 — Tags, and a locations list you can filter and sort

## Context

On a long trip the locations tab stops being browsable. Today it offers a
search box and two filters — category (`site`/`stay`/`transport`) and distance
from the device — and both live as their own button in a toolbar that is
already a deliberately non-wrapping row sized to fit 324px
(`web/js/pages/locations-tab.js:18-30`). There is no way to ask "what is in
Reykjavik", "what is this week", or "what have I not scheduled yet".

Two things are missing and one thing is in the way.

**Missing: tags.** A location should carry a free list of keywords whose
meaning the user chooses — a kind of site, a city, a region, whose idea it was.
The app very nearly has this already: `items.type` is `TEXT NOT NULL DEFAULT
''` with **no validation anywhere**, and `internal/db/domain.go:402` describes
it in so many words as `free-text tag, e.g. "mountain", "hotel" — not a rigid
enum`. It is a tag list that can only hold one tag. Adding tags beside it would
leave the app with two overlapping free-text classification fields forever, so
this stage ends by retiring `type` into the tag set. Unlike Stage 25's dates,
that migration is lossless and mechanical: one non-empty `type` becomes one tag.

**Missing: date filtering.** Since Stage 25 a location knows which itinerary
days it is on, but the locations tab shows and uses none of it — the list
endpoint carries no dates at all (`dates` exists only on `itemDetailResponse`,
`internal/httpapi/items.go:84`). `plans/todo.md` already carries this as
**Dates on the locations list**, including the correct approach: one trip-wide
query bucketed by item in Go following `ListItemCoordinates`
(`internal/httpapi/items.go:108-125`), never a query per card. This stage does
that and spends the data twice — on a filter and on the cards.

**Missing: any order but the one things were added in.** The list comes back
`ORDER BY sort_order, created_at` (`internal/db/sqlc/queries/items.sql:9-19`)
and there is no reorder UI on the tab, so that is insertion order and it is the
only order there is. The same `todo.md` entry names the fix in the same breath
as the cards — *a "5-7 Sep" line on the card, **or sorting the list by date**,
is the obvious next use* — so closing only the card half would leave a stale
entry, which that file's own header calls worse than a missing one.

**In the way: the toolbar.** Two more buttons do not fit. All four filters move
behind a single funnel trigger, which frees exactly the room the sort button
needs: the row goes from search + category + distance + New to search + filter
+ sort + New, and stays two controls wide.

### Decisions taken before planning

- **The filter popup drills down, it does not fly out.** A row reading `All
  categories ▸` replaces the panel's contents in place with that filter's
  options plus a back row. A second panel opening *sideways* has nowhere to go
  at 324px, and flyouts are poor touch targets. Each top-level row doubles as
  the current-value display, which is the second sketch in `plans/notes.md` and
  the better one.
- **The tag filter is single-select.** It stays inside the `menuitemradio`
  model every other menu in the app uses. Multi-select is where tags eventually
  earn their keep, but it needs checkbox rows, a different trigger-label rule
  and a clear affordance; it goes to the backlog rather than into this stage.
- **Sorting is its own button, not a fifth group in the filter drawer.**
  Sorting is not filtering, and `trips-page.js` already established a separate
  sort trigger with the `arrow-down-up` icon. Consistency between the app's two
  list screens is worth more than one fewer control. It also keeps sorting on
  the plain `renderMenu` path, so it needs none of Milestone 4's new component.
- **Filtering stays client-side.** The tab already fetches the trip's locations
  once and filters in memory (`locations-tab.js:24-30`). Tags and dates need to
  reach the *list payload*; they need no new query parameters. The unused
  backend `?category=` filter stays unused.
- **Tags belong to the item, not to the trip.** No `tags` table, no ids — a
  join table of `(item_id, tag)` text rows, written replace-the-set like
  `item_links`. Renaming a tag globally is then an `UPDATE`, not a schema
  feature, and there is no orphan-row lifecycle to own.
- **Tags are stored as typed, compared exactly.** Trimmed, inner whitespace
  collapsed, deduped case-insensitively *within one item*. The editor suggests
  tags already used on the trip, so casing converges by use rather than by
  rule. Accepted cost: someone determined to can create both `Museum` and
  `museum` on two different locations.
- **`type` is retired in this stage** (Milestone 6), not deferred. Deferring
  means doing the card, view-page and assistant work twice.

### What exploring the code changed about the plan

- **`type` and `category` are unrelated fields, and only `category` is
  constrained.** `category` has a SQL `CHECK` and a Go allowlist
  (`internal/httpapi/items.go:16`); `type` has neither and is passed straight
  through (`items.go:340`, `items_create.go:319`). The notes' "drop the
  explicit `type` field" reads as though it might mean the category enum. It
  must not — the category filter is the one filter that works well today.
- **Retiring `type` reaches the assistant.** The model proposes one
  (`internal/assist/schema.go:24,63,74`) and the agent diffs it as a plain
  string (`internal/assist/agent.go:647`), the panel allowlists the field name
  and places the suggestion in a `[data-assist-field="type"]` slot
  (`web/js/components/assist-panel.js:71,363,498`). Because `Field` is
  `{Name, Current, Proposed string}`, the cheap move is to keep the field a
  **string** named `tags` holding a comma-joined list, and split it on apply.
  Every existing mechanism survives untouched; making `Field` polymorphic would
  not be worth it.
- **`menu.js` is single-layer by construction and already carries a lot.** It
  serves the tab bar, the user menu, the settings dropdown and nine row menus,
  with options for each (`chevron`, `triggerClass`, `triggerPrefixHtml`,
  `label`). Bending it into a drill-down container would make it a monolith.
  But its header comment correctly boasts that it is *the only popup
  implementation in the tree*, so a second one must not simply copy the
  outside-click/Escape/`aria-expanded` mechanics. Milestone 4 extracts those
  ~30 lines into `popup.js` and builds `filter-menu.js` on the same base.
- **`neutralValue` already implements the notes' blue-when-narrowing idea.**
  `menu.js:137` toggles `menu__trigger--active`, styled at `base.css:721-725`.
  The filter menu keeps exactly this rule, applied when *any* group is
  off-neutral.
- **The trips list is a near drop-in template for sorting**
  (`web/js/pages/trips-page.js:25-97`), and it has already solved the three
  things that are easy to get wrong: it sorts a **copy**, because the fetched
  order *is* the default answer and sorting in place destroys it; it compares
  titles with `Intl.Collator(getLocale(), {sensitivity:"base", numeric:true})`
  so umlauts sort where a German reader expects; and it sorts undated entries
  **last**, because something unscheduled is not imminent. Locations differ in
  one respect only — the default is "Added", not "Newest", since `sort_order`
  is insertion order rather than recency. `arrow-down-up` is already in the
  committed sprite, so no regeneration.
- **A dirty flag is not needed for tags.** Stage 25's `datesDirty` exists
  because itinerary entries are shared with another screen and a co-editor.
  Tags are owned solely by the item, so the editor can always send them.
- **`readJSON` rejects unknown fields** (`internal/httpapi/json.go:29`). This
  bit Stage 25 M2. It fixes the ordering here: the backend must accept `tags`
  before the editor sends it (M1 → M2), and M6 must remove `type` from the
  request struct and from `location-form.js` in the *same* commit.
- **`ListExpenseSharesByTrip`** (`internal/db/sqlc/queries/expenses.sql:49-68`,
  fanned out at `internal/httpapi/expenses.go:391-410`) is a closer precedent
  than the coordinates one for a join table bulk-loaded per trip. Both are
  cited below.

---

## 0. Land the plan, and reconcile the backlog

Commit this file as `plans/stage-26.md`. Fold `plans/notes.md` into it and
empty the notes file. In `plans/todo.md`, mark **Dates on the locations list**
as claimed by this stage — it names two things, the card line and sorting by
date, and it is removed for real only once Milestone 5 lands the second — and add the
entries this plan defers — multi-select tags, tag management, filter state in
the URL — each citing Stage 26.

**Verify.** `make ci` green, no behaviour change.

**Done.** Landed as `plans/stage-26.md`; `plans/notes.md` emptied, which is what
previous stages did with it rather than deleting the file. In `plans/todo.md`,
**Dates on the locations list** is marked claimed by this stage with a note that
it names two things and so is removed only once Milestone 5 lands the second,
and three entries were added for what this plan defers: multi-select tag
filtering, tag management across a trip, and list view state living only in
memory.

One deviation, and it is a correction to the plan rather than to the code. The
**Out of scope** section justified leaving server-side filtering alone by citing
an existing pagination entry in `todo.md`. There is no such entry -- the
2026-08-29 review dropped it, and that file's header explicitly forbids
reconstructing a deliberately-deleted entry without asking. So the entry was
*not* re-added; the plan now states the situation instead, and records that
`web/js/pages/locations-tab.js:29-30` still tells the reader that a `q`
predicate plus pagination is a `todo.md` item when it is not. That stale comment
is corrected in Milestone 4, which rewrites that header anyway.

Verified: `make ci` green -- build, vet, JS syntax, i18n parity, the env-var,
screenshot and migration checks, and `go test ./...` (all packages cached or
ok). Nothing outside `plans/` was touched, so there is no behaviour to re-test.

---

## 1. Tags: the table, the store, the wire

**Migration `0005_item_tags`**, both dialects:

```sql
CREATE TABLE item_tags (
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    PRIMARY KEY (item_id, tag)
);
CREATE INDEX idx_item_tags_tag ON item_tags(tag);
```

Composite PK and cascade follow `expense_shares`
(`internal/db/migrations/sqlite/0001_init.up.sql:330-338`). The reverse index
serves the trip-wide listing. Down drops both.

**Queries** — a new `internal/db/sqlc/queries/item_tags.sql`. Keep every
comment in plain prose: no backticks, no double quotes, no apostrophes
(`CLAUDE.md`'s sqlc-lexer trap).

- `ListItemTagsByItem :many` — `WHERE item_id = ... ORDER BY tag`
- `ListItemTagsByTrip :many` — join `items` on `trip_id`, returning
  `item_id, tag`, `ORDER BY item_id, tag`. Serves both the list payload and the
  distinct tag set.
- `CreateItemTag :exec`
- `DeleteItemTagsByItem :exec`

**Store.** `ItemTag{ItemID, Tag}` in `internal/db/domain.go` beside
`ItemLink`; four methods on the `Store` interface (`internal/db/store.go:275-289`)
and hand-written adapters in **both** `sqlite_store.go` and `postgres_store.go`.
Run `sqlc generate` by hand from `internal/db/sqlc/` and read the generated
files rather than only diffing them.

**Wire.** `tags []string` (never null — `[]` when empty) on `itemResponse`
(`internal/httpapi/items.go:18-46`), so it is present on both the list and the
detail. `Tags *[]string` on `itemRequest` beside `Links`, pointer-valued with
the established meaning: absent leaves tags untouched, present replaces the set,
empty clears it. Written in `writeItemNested` (`items.go:190-233`) as
delete-all-then-insert inside the existing transaction, shaped like
`writeShares` (`internal/httpapi/expenses.go:328-340`).

`handleListItems` gains one `ListItemTagsByTrip` call bucketed into a
`map[string][]string`, exactly like the coordinates map at `items.go:108-125` —
and, like it, **non-fatal**: a failure costs the tag filter, not the list.

**Normalize and validate**, in a new `internal/httpapi/item_tags.go`:
trim, collapse inner whitespace, drop empties, dedupe case-insensitively
keeping the first spelling; 400 on a tag over 40 runes or more than 20 tags on
one item.

**New endpoint.** `GET /api/trips/{tripId}/tags` → `["museum","reykjavik"]`,
distinct and sorted, for the editor's suggestions (the locations tab derives
its own from the list it already holds). Viewer role, registered beside the
other trip-scoped routes at `internal/httpapi/router.go:322-323`.

**Verify.** `make ci` **and** `make test-postgres`. New Go tests alongside
`items_test.go`: tags round-trip on create and update; absent-versus-empty
mirroring `TestItemDatesAbsentVersusEmpty`; the list endpoint carries tags in
one query; normalization and the two limits; a cross-trip tag does not leak
into `GET /trips/{id}/tags`.

---

## 2. Tags in the editor and on the location page

**`web/js/components/tag-field.js`** — a chip list plus a text input.
Enter or comma commits a chip, Backspace on an empty input removes the last,
each chip has a remove button with an accessible name. Suggestions come from
`bindSuggestInput` (`web/js/components/suggest-input.js`) with a **synchronous**
search over the trip's tags fetched once from the new endpoint; already-chosen
tags are filtered out of the suggestions.

**`location-form.js`** gains the field below Category, `draft.tags` is
initialised from `item.tags ?? []` and always submitted (no dirty flag — see
the exploration note). **`location-view-page.js`** renders the chips read-only;
**`location-card.js`** adds a `tags` observed attribute and a chip row under
the title, both taking the existing muted `.type` styling as their starting
point.

New i18n keys in **both** `en.json` and `de.json` — `location.form.tags`,
`location.form.tagsPlaceholder`, `location.tags.remove`, `location.tags.add`.

**Verify.** `make ci` (i18n parity is the easy thing to forget here). A
Playwright pass in `tests/ui/locations.spec.js`: add two tags to a location,
save, reload, assert both chips by accessible name; assert the second
location's editor *suggests* those tags; remove one and assert it is gone after
save. Manual pass at 324×756 that a location with six tags does not break the
card.

---

## 3. Dates on the locations list, and on the cards

Closes the **Dates on the locations list** backlog entry.

**Query.** `ListItemDatesByTrip` appended to
`internal/db/sqlc/queries/itinerary_entries.sql`, the trip-wide sibling of
`ListItineraryDatesByItem` (`:56-61`) — entries joined to days joined to items,
`ORDER BY i.id, d.date, e.sort_order`, projecting into the existing
`db.ItemItineraryDate`. Both dialect adapters, and mind the type split Stage 25
M1 already hit: `itinerary_days.date` is `TEXT`/`string` on SQLite and
`DATE`/`time.Time` on Postgres, `sort_order` `int64` vs `int32`. Reuse the
conversion Stage 25 wrote rather than inventing a second one.

**Handler.** `handleListItems` buckets the rows by item and runs the existing
`collapseDateRanges` (`internal/httpapi/item_dates.go:51`) per item, so `dates`
is on the list rows with the same shape the detail endpoint already publishes.
Non-fatal on error, like the other two bulk loads. `itemDetailResponse` keeps
its own per-item read; the field simply moves up to `itemResponse` and the
detail stops setting it separately.

**Card.** `location-card.js` gains a `dates` attribute carrying the JSON
ranges, rendered through `formatDateRange` (`web/js/format.js:14`) as a muted
line — first range only, with a `+N` when there are more, so a location on
three separate stretches does not grow a paragraph.

**Verify.** `make ci` and `make test-postgres`. A Go test that the list
endpoint returns collapsed ranges for an item on three consecutive days **in a
single query** (assert against a trip with several dated locations). Playwright:
put a location on 5–7 Sep via the editor, return to the tab, assert the card
shows the formatted range.

---

## 4. One filter menu: a drill-down popup, holding the filters we already have

Deliberately **no new filters in this milestone** — it moves category and
distance, unchanged, into the new container, so the checkpoint is "the toolbar
was rebuilt and nothing about filtering changed".

**`web/js/components/popup.js`** — extract from `menu.js:145-173` the
open/close mechanics: `hidden` toggling, `aria-expanded`, the
`menu__trigger--open` class, and the outside-click/Escape listeners added on
open and removed on close. `menu.js` becomes its first caller with no
behavioural change; `tests/ui/menu.spec.js` passing unmodified is the proof.

**`web/js/components/filter-menu.js`** — one funnel trigger, one
`.menu__dropdown.menu--filter`. Signature:

```js
renderFilterMenu(container, {
  groups: [{ key, label, neutralLabel, items, activeValue, neutralValue, onSelect,
             renderPanel }]
})
```

Root view: one `role="menuitem"` row per group showing that group's current
label — the neutral label when neutral, the selected option's label otherwise,
accent-coloured when off-neutral — with a chevron. Activating a row swaps the
panel contents for a back row plus that group's `menuitemradio` items (or its
`renderPanel` output, which the date group needs); choosing an option returns
to the root view. Escape closes from either level; the panel returns to the
root view on close so it never reopens mid-drill. Focus moves to the panel's
first row on each swap.

The trigger takes `menu__trigger--active` when **any** group is off-neutral,
reusing `base.css:721-725` untouched. New CSS is limited to a `.menu--filter`
block for the back row and the row chevrons.

**`locations-tab.js`** collapses `.locations-filter-slot` and
`.locations-distance-slot` into one `.locations-filter-slot`. The distance
group is simply omitted when `!canLocate()`, replacing today's "render the
whole menu or not"; its async `getCurrentPosition` failure path still needs to
undo a selection, so `renderFilterMenu` returns `{ setActive(groupKey, value) }`
mirroring `menu.js`'s `setActive`.

New keys, both locales: `locations.filter.title`, `locations.filter.back`,
`locations.filter.category`, `locations.filter.distance`.

**Verify.** `make ci`. `tests/ui/menu.spec.js` green **unmodified**. The
existing distance-filter specs (`tests/ui/map.spec.js:1249-1360`) updated only
where they name the old trigger, and still asserting the same outcomes. New
specs at 324×756: the toolbar is one row and does not overflow; drilling in and
back; Escape closing from the second level; the trigger accent-coloured with a
category chosen and neutral again after picking All.

---

## 5. Sort the locations list

Closes the other half of the **Dates on the locations list** entry, and spends
the toolbar space Milestone 4 just freed. Plain `renderMenu` — none of the
drill-down component is involved.

**`locations-tab.js`.** A `.locations-sort-slot` between the filter slot and
the New button, and the trips-page shape transplanted:

```js
const SORTS = ["added", "title", "date"];
const DEFAULT_SORT = "added";
```

rendered with `iconName: "arrow-down-up"`, `ariaLabel: "locations.sort.label"`,
and `neutralValue: DEFAULT_SORT` so a non-default order tints the trigger — the
same cue the funnel uses, and the only one left when the label collapses under
640px.

A `sorted(items)` function returning a **copy**, applied in `applyFilters()`
after `matches()`:

- `added` — return the fetched order untouched. It is the server's
  `sort_order, created_at` and must survive being sorted away and back.
- `title` — `Intl.Collator(getLocale(), { sensitivity: "base", numeric: true })`,
  as `trips-page.js:84-86`.
- `date` — earliest `dates[0].start_date` first, and a location with **no**
  dates goes last rather than first: it is unscheduled, not imminent. ISO
  date strings compare directly. The ranges arrive already sorted from
  `collapseDateRanges`, so `dates[0]` is the earliest without re-sorting.

New keys, both locales: `locations.sort.label`, `locations.sort.added`,
`locations.sort.title`, `locations.sort.date`. Deliberately *not* reusing
`trips.sort.*` — the option set differs and `trips.sort.newest` would be a lie
here.

**Verify.** `make ci`. Playwright: seed a trip whose insertion order is neither
alphabetical nor chronological, then assert the rendered card order under each
of the three options by reading the titles out of the list; assert an undated
location sorts last under By date and that switching back to Added restores the
original sequence exactly. Assert the trigger is accent-coloured under a
non-default sort and neutral again under Added. Manual pass at 324×756 that
search + filter + sort + New still fit one non-wrapping row.

---

## 6. Filter by tag, and by date

Both groups plug into Milestone 4's container; both extend `matches()`
(and compose with Milestone 5's sort, which runs after them)
(`locations-tab.js:87-97`) and nothing else.

**Tag group.** Options are the distinct tags across `allItems`, sorted, with
`Any tag` neutral at the top — derived from the list already in memory, so no
extra request. The group is omitted entirely when the trip has no tags yet,
rather than offering a menu with one row in it. Predicate: `item.tags.includes(
selected)`.

**Date group.** A `renderPanel` group rather than a plain option list:

```
‹ Back
✓ Any date
  Not scheduled
  Scheduled
  ──────────────
  [ from ] [ to ]   (native <input type="date">, Apply)
```

`Not scheduled` — `item.dates.length === 0` — is the one most worth having
while planning, and is why this is a small preset list rather than only a range
picker. The range matches when **any** of the item's ranges overlaps
`[from, to]`; an empty `from` means open-ended before `to` and vice versa;
both empty is the neutral state. `to` gets `min = from` on change, as the
editor's date form already does (`location-editor-page.js:890-910`). The
group's row label shows `formatDateRange(from, to)` while a range is active.

Search also starts matching tags in this milestone, since `type` is on its way
out: the predicate becomes title plus `type` plus the joined tags, and drops
`type` in Milestone 6.

New keys, both locales: `locations.filter.tags`, `locations.filter.anyTag`,
`locations.filter.date`, `locations.filter.anyDate`,
`locations.filter.unscheduled`, `locations.filter.scheduled`,
`locations.filter.from`, `locations.filter.to`, `locations.filter.apply`.

**Verify.** `make ci`. Playwright, against a seeded trip: filtering to a tag
narrows the list to the right cards and the trigger goes accent; `Not
scheduled` hides every dated location; a range covering 5–7 Sep shows the
location placed there in Milestone 3 and hides one placed a month later;
clearing every group restores the full list and the neutral trigger. Manual
pass at 324×756 that the date panel's two inputs fit side by side.

---

## 7. Retire `type`

**Migration `0006_type_becomes_a_tag`**, both dialects. Up:

```sql
INSERT INTO item_tags (item_id, tag)
SELECT id, TRIM(type) FROM items WHERE TRIM(type) <> ''
ON CONFLICT DO NOTHING;
ALTER TABLE items DROP COLUMN type;
```

Check SQLite's `DROP COLUMN` support against the bundled driver's version
before relying on it (it needs 3.35+); the fallback is the create-copy-drop-rename
rebuild, which must also recreate `idx_items_trip_id_category`. Down re-adds
`type TEXT NOT NULL DEFAULT ''` **empty**, with a comment saying which tag was
the type is not recoverable — the shape Stage 25's down migration used.

**Go.** Drop `Type` from `db.Item`, `CreateItemParams`, `UpdateItemParams`, the
`items.sql` queries and `ItineraryEntryDetail.ItemType`; regenerate **both**
dialects and delete anything `sqlc generate` leaves behind (it adds and
rewrites but never deletes). Then **grep for `Type` by name** — an unused
exported field or type compiles perfectly well, which is the gotcha `CLAUDE.md`
records from Stage 25 M4. Remove `type` from `itemResponse` and `itemRequest`.

**Assistant.** `schema.go`'s `Type string` becomes `Tags string` — still a
string, holding a comma-separated list, so `agent.go:647`'s field-diff table
and the whole `Field{Name, Current, Proposed string}` machinery survive
unchanged; the prompt's vocabulary guidance is reworded to ask for a few tags.
`assist-panel.js`'s `FIELD_NAMES` (`:71`) swaps `type` for `tags`, the slot
moves from `location-form.js:49` to sit under the tag field, and the editor's
`applyField` splits the proposal on commas into chips.

**Frontend and the rest.** Remove the `type` input (`location-form.js:47,77,212`),
the `.type-label` on the view page (`location-view-page.js:80,167`), the `type`
attribute on the card, and `type` from the search predicate. Remove
`location.form.type` and `location.form.typePlaceholder` from both locales.
Update the seeder, `scripts/gen_screenshots.mjs` if it sets a type, and the
feature docs under `docs/`.

**Verify.** `make ci`, `make test-postgres`, and the migration round-tripped
up/down/up on a real database of each dialect with rows present — asserting a
location whose type was `hotel` comes out of the up migration carrying a
`hotel` tag. `make docs`. Playwright: run the assistant against the stub
provider (`internal/assist/stub.go`) and assert the proposal lands as chips in
the tag field. Update `internal/assist/agent_test.go`'s several `fieldNamed(p,
"type")` assertions (`:68,200,359,504,527`).

---

## Build order

`0 → 1 → 2 → 3 → 4 → 5 → 6 → 7`, strictly. 1 before 2 because `readJSON`
rejects unknown fields, so the editor cannot send `tags` until the API accepts
them. 3 before 5 and 6 because neither sorting by date nor filtering by date has
anything to work with until the list carries dates. 4 before 5 for a physical
reason: until the two filter buttons collapse into one, adding a third control
makes the toolbar three buttons wide and it stops fitting 324px — the row would
be broken for the length of a whole checkpoint. 4 before 6 so the container is
built and checkpointed against filters whose behaviour is already known, rather
than debugging a new popup and a new predicate at once. 7 last because it
depends on tags existing, being editable, and being filterable — until all
three are true, dropping `type` removes a capability.

## Files this touches

- `internal/db/migrations/{sqlite,postgres}/0005_item_tags.*`, `0006_type_becomes_a_tag.*`
- `internal/db/sqlc/queries/item_tags.sql`, `itinerary_entries.sql`, `items.sql`; both generated dialect packages
- `internal/db/domain.go`, `store.go`, `sqlite_store.go`, `postgres_store.go`
- `internal/httpapi/items.go`, `items_create.go`, `item_tags.go` (new), `router.go`, `assist.go`
- `internal/assist/schema.go`, `agent.go`, and the prompt
- `web/js/components/popup.js` (new), `filter-menu.js` (new), `tag-field.js` (new), `menu.js`, `location-form.js`, `location-card.js`, `assist-panel.js`
- `web/js/pages/locations-tab.js` (the toolbar, the filter menu, the sort menu and both predicates), `location-view-page.js`, `location-editor-page.js`
- `web/css/base.css`, `web/locales/en.json`, `web/locales/de.json`
- `tests/ui/locations.spec.js`, `map.spec.js`, `menu.spec.js`; Go tests beside `items_test.go`, `item_dates_test.go`, `internal/assist/agent_test.go`
- the seeder, `scripts/gen_screenshots.mjs`, `docs/`
- `plans/stage-26.md`, `plans/notes.md`, `plans/todo.md`

## Out of scope, deliberately

- **Multi-select tag filtering.** Where tags eventually pay off, but it breaks
  the radio model every menu in the app shares. → `todo.md`.
- **Tag management: rename, merge, delete across a trip.** With tags stored as
  text this is an `UPDATE`, not a schema problem, so it can wait for evidence
  that drift actually happens. → `todo.md`.
- **Server-side tag and date filters, and pagination.** Client-side filtering
  is correct at realistic per-trip counts. Note that `locations-tab.js:29-30`
  claims a `q` predicate plus pagination is a `todo.md` item and it is **not**:
  the 2026-08-29 review dropped it, and that file forbids reconstructing a
  deliberately-deleted entry without asking. The stale comment is corrected in
  Milestone 4, which rewrites that header anyway; whether pagination comes back
  to the backlog is a separate decision.
- **Filter state in the URL.** Nothing in the app puts view state in the URL
  today; doing it for one tab would be inconsistent. → `todo.md`.
- **The identifier sweep, `item` → `location`.** Tempting, because this stage
  is already inside `renderItemsTab`, `<item-card>`, `data-action="new-item"`
  and the `item.category.*` keys. Declined: `todo.md` marks it a
  choose-your-depth rename spanning the API, the schema and both locales, and
  folding it in would put a mechanical rename through every diff in the stage
  and hide the real changes. It wants a stage of its own, as Stage 11 M1 was.
- **Making date formatting follow the app's locale rather than the browser's.**
  Worth flagging rather than doing: this stage puts `formatDateRange` output on
  every card *and* in the filter menu's trigger, so it makes that existing bug
  markedly more visible. Its `todo.md` entry says it needs a decision first, and
  that decision is not this stage's to take — but it is a strong candidate for
  the next one.
- **Times on itinerary entries**, and **a link from a location into the
  itinerary** — both already in `todo.md` from Stage 25, both untouched here.

## Verification

Every milestone ends with `make ci` green; Milestones 1, 3 and 7 also run
`make test-postgres`, since each changes `internal/db` or the queries.
Milestone 7 additionally round-trips its migration up/down/up on a populated
database of both dialects and runs `make docs`. Milestones 2 through 6 each
end with a Playwright pass and a manual look at 324×756, which is the width
that motivated the redesign.

**End-to-end proof of the whole stage.** On a trip of thirty-odd locations:
tag several of them `reykjavik`, put three of those on 5–7 September, then from
the locations tab open the single funnel, drill into Tags and choose
`reykjavik`, drill into Date and set 5–7 September, and see exactly those three
cards — each showing its date range — with the funnel accent-coloured; sort By
date and see them in itinerary order; drill into Date, choose `Not scheduled`,
and see only the locations with no days yet, still alphabetical under By name.
Nowhere in that flow does the word "type" appear, and the toolbar never wraps.

## Workflow

One milestone at a time. Implement, verify, then add a **Done.** paragraph to
that milestone's section in `plans/stage-26.md` describing what actually landed
— including any deviation from this plan — and how it was verified. Reconcile
`plans/todo.md` in both directions: remove what the milestone implemented, add
what it deferred. One commit per milestone; a milestone needing a follow-up fix
gets its own `"... follow-up: ..."` commit. Make sure `make dev` is running,
then stop and hand back control. Do not start the next milestone until told to
continue; feedback given at a checkpoint is fixed and re-verified before moving
on, not folded into the next milestone.
