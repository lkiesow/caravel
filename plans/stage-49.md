# Stage 49: "item" → "location", all the way down

## Context

User-visible copy says "location". Below the copy, almost everything still says
"item": the `items` table and its satellites, `/api/items/{id}`, `item_id` JSON
fields, `Item*` Go types and store methods, the `item.*` i18n namespace (27
keys), `renderItemForm`/`renderItemsTab`, `<item-card>`,
`data-action="new-item"`, and `.item-*` CSS. This was decided on 2026-10-03: go
all the way, including the API routes and a table-rename migration. The
precedent is Stage 11 M1 ("documents" → "files"), which dropped the old URL
outright with no alias.

**Why a stage rather than one patch.** The sweep touches ~155 files across four
layers (schema, Go store, HTTP API, frontend). One commit would mix a migration,
an API break and a frontend sweep, and nobody can review that diff. Split by
layer, each commit is mechanical within itself, green on its own, and can be
reverted on its own. The backlog entry already says this should not hide inside
another diff, and that it should land before the location summary and the
visited flag.

**Decided in planning:**
- `item_locations` is the lat/lng/address/OSM satellite. Its identifiers become
  **geo**: table `location_geo`, Go `LocationGeo`, JSON `"geo": {...}`, route
  `PUT /api/locations/{id}/geo`, i18n key `location.detail.geo`. This avoids
  "the location of a location".
- **No visible change.** The section heading keeps its text,
  "Location"/"Standort", under its new key. "Screens are identical" is part of
  how we verify. A copy change, if wanted, goes in the backlog.
- Old routes are dropped, not aliased. This follows the Stage 11 precedent; the
  frontend is the only client.

**Not renamed** (not this "item"): checklist items (`checklist_items`,
`/checklists/{id}/items`, `ChecklistItem`), menu items in `menu.js` and
`suggest-input.js`, CSS `align-items`, JSON-Schema `items`/`maxItems` in
`internal/assist/schema.go`, `tags.MaxPerItem`, Go `.items()` in scripts,
`role="menuitem"`. Stored note mentions already link `/trips/{t}/locations/{id}`,
so no user data contains "item".

## 0. Land the plan

Commit this as `plans/stage-49.md`. Update the todo.md entry to point at Stage
49.

## 1. Schema and `internal/db`

**Migration `0013_rename_items_to_locations`**, both dialects, up and down:
- Tables: `items`→`locations`, `item_locations`→`location_geo`,
  `item_links`→`location_links`, `item_tags`→`location_tags`.
- Columns: `item_id`→`location_id` on `location_geo`, `location_links`,
  `location_tags`, `files`, `itinerary_entries`, `expenses`.
- Indexes: `idx_items_trip_id_category`, `idx_item_links_item_id`,
  `idx_files_item_id`, `idx_expenses_item_id`, `idx_itinerary_entries_item_id`,
  `idx_item_tags_tag` get their `location` names. SQLite has no
  `ALTER INDEX ... RENAME`, so it uses DROP + CREATE; Postgres uses
  `ALTER INDEX ... RENAME`.
- Postgres also renames the implicit constraints (`items_pkey`,
  `*_item_id_fkey`, `item_locations_item_id_key`, `item_tags_pkey`) with
  `RENAME CONSTRAINT`, so that `\d` reads consistently.
- SQLite relies on `ALTER TABLE ... RENAME TO` rewriting FK references in the
  other tables (modernc ≥ 3.26, `legacy_alter_table` off). The migration test
  must prove this, not assume it. Check that there are no triggers or views
  (there are none today).
- Update `scripts/check_migrations.py` expectations if it pins labels.

**sqlc**: rename `queries/items.sql`, `item_links.sql`, `item_locations.sql`
and `item_tags.sql` to `locations.sql`, `location_links.sql`, `location_geo.sql`
and `location_tags.sql`. Rewrite the SQL and query names (`ListItemsByTrip` →
`ListLocationsByTrip`, `GetItemLocationByItemID` → `GetLocationGeoByLocationID`,
…). Also edit the `item_id` uses in `files.sql`, `expenses.sql`,
`itinerary_entries.sql` and `stats.sql`. Run `sqlc generate`, then **delete the
stale `gen/item*.sql.go` by hand in both dialects**. Follow the CLAUDE.md sqlc
traps: plain comment prose, and read the generated files.

**`internal/db`**: rename the domain types (`Item`→`Location`,
`ItemLocation`→`LocationGeo`, `ItemLink`, `ItemTag`, `MapItem`→`MapLocation`,
`ItemCoordinate`, `ItemItineraryDate`, and every `ItemID` field →
`LocationID`). Rename the `Store` interface methods, both adapters
(`sqlite_store.go`, `postgres_store.go`, including the
`*ItemLocationToDomain` mappers), and the db tests. Fix the httpapi, seed and
assist call sites **only** as far as compilation needs. The handler, route
and JSON renames belong to M2. Grep `domain.go` afterwards for orphaned names.

New `migration_0013_test.go`, in the style of `migration_0012_test.go`: migrate
to 12 and seed a trip with one location carrying geo, a link, a tag, a file, an
expense and an itinerary entry. Then migrate to 13 and assert that every row is
present under the new names. Delete the location and assert that the cascade
reaches `location_geo/links/tags/files/itinerary_entries`, and that
`expenses.location_id` becomes NULL. That proves the FK references were
rewritten. Finally migrate down to 12 and up again.

**Verify:** `make ci`, `make test-postgres`, and `make dev` starts against the
existing dev DB (the real migration path). Click through a location view, the
map, itinerary and expenses: everything still loads.

**Done.** Migration `0013_rename_items_to_locations` renames the four tables,
the six `item_id` columns and the six indexes in both dialects. SQLite does
this in place with `RENAME TO`/`RENAME COLUMN`; it has no index rename, so the
indexes are dropped and recreated. Postgres also renames all fourteen
constraints that carried an item name. Before writing those I listed them from a
scratch schema migrated to 12, rather than guessing Postgres's naming.
`items_category_check` is the one rename that matters: 0010 and 0011 drop it by
name, and the next change to the category list will drop
`locations_category_check`.
On that scratch schema, up leaves no name containing "item" outside the
checklist tables, and down restores the catalog exactly.

The Go side was renamed type-aware with gopls (`gopls rename`, built into a
scratch directory, not installed), not with sed. `ItemID` is a field on the db
structs *and* on httpapi's own request structs, and only the first belonged in
this milestone. That covered 49 identifiers: the domain types (`Location`,
`LocationGeo`, `LocationLink`, `LocationTag`, `MapLocation`,
`LocationCoordinate`, `LocationItineraryDate`), the params structs, every
`ItemID`/`ItemTitle`/`ItemCategory`/`ItemImageID` field on them, and the 20
`Store` methods. gopls carried the references into httpapi and `cmd/seed`, so
those files change only where they call the store. Their handler names, JSON
tags and file names are untouched and wait for M2. The query files were renamed
(`locations.sql`, `location_geo.sql`, `location_links.sql`,
`location_tags.sql`), with query names following the store methods
(`ListItemLocationsByTrip` became `ListLocationCoordinatesByTrip`, which is
what it returns). Table aliases changed from `i`/`l` to `loc`/`g`. sqlc is not
installed here either, so v1.31.1, the version stamped in the generated
headers, was built into the scratch directory. The eight stale
`gen/item*.sql.go` files were deleted by hand, and the generated SQL has no
unsubstituted `sqlc.arg`.

**Deviation: the older migration tests now pin their own version.** The tests
for 0006, 0010, 0011 and 0012 seeded under the old names and then ran `Up()` to
head, so 0013 broke all four by renaming the tables they query afterwards. Each
now runs `Migrate(N)` for its own N, which is what it was testing all along, so
a future rename cannot break it again.

`migration_0013_test.go` seeds one location under the old names, with geo, a
link, a tag, a file, an itinerary entry and an expense. It then migrates to 13
and checks:
- every row is present under its new name;
- no schema object outside checklists still names an item;
- the six indexes exist.

It then goes down to 12 and back up. Finally it deletes the location and checks
that all five satellites cascaded, that the expense survived with
`location_id` NULL, and that `foreign_key_check` is clean. The test catches a
real failure: with the migration sabotaged to run under
`legacy_alter_table=ON` and `foreign_keys=OFF` (the one combination where SQLite
leaves references pointing at the old name), it fails seven assertions. With
only `legacy_alter_table=ON` it still passed, because foreign keys being on is
enough for SQLite to rewrite the references. So the migration comment now says
exactly that, instead of the "since 3.26" shorthand the plan used.

Verified: `make ci` and `make test-postgres` green, and `make test-ui` green
(run early, since every store write path was renamed). The dev DB was backed up,
then migrated 12 → 13 by `make dev` at startup. Counts are unchanged
(14 locations, 11 with geo, 2 files on a location, 10 itinerary entries) and
`foreign_key_check` is clean. In the browser at 324×756 (Playwright), the
locations, map, itinerary, expenses and files tabs, a location view and its
editor all load, with no console errors. Every read endpoint returned 200 across
all 7 trips, all 10 itinerary entries carry their location title, and both files
on a location carry theirs.

## 2. HTTP API (and the JS fetch calls that must move with it)

- Routes in `internal/httpapi/router.go`: `/trips/{id}/items[/batch]` →
  `/trips/{id}/locations[/batch]`, `/items/{itemId}/…` →
  `/locations/{locationId}/…`, and `/location` → `/geo`. The checklist
  `/items` routes stay.
- JSON: `item_id`/`item_title`/`item_category`/`item_image_url` →
  `location_*` in files, expenses and itinerary. The nested `"location"` becomes
  `"geo"`. The batch body becomes `{"locations": [...]}`.
- Go: handler names (`handleGetItem`→`handleGetLocation`, …), the
  request/response types (`itemLocationRequest`→`geoRequest`, …) and helpers
  (`loadOwnedItem`, …). Rename the files: `items.go`→`locations.go`,
  `items_create.go`, `items_batch.go`, `item_dates.go`, `item_tags.go`, and
  every `*item*_test.go` (`expense_item_test.go`→`expense_location_test.go`,
  `map_items_test.go`, …). Update the route table in `ownership_test.go`.
- JS: every `api.*` call and every response-field read in the same commit
  (`mention-picker.js`, `expenses-tab.js`, `itinerary-tab.js`,
  `location-*-page.js`, `file-list.js`, `image-field.js`, `url.js` comments,
  …). Also update `tests/ui` specs that hit `/api/items` or read `item_*`
  fields, and `cmd/seed`.
- Add a test that `GET /api/items/{id}` is now 404, so that the drop is
  deliberate.

**Verify:** `make ci`, `make test-postgres`, `make test-ui`. Grep
`git grep -nE "item_(id|title|category|image_url)|/items\b" -- web internal cmd tests`
and check that only checklist hits remain. Do a manual pass at 324×756:
create, edit (set geo by search and by pin), delete a location, attach a file,
link an expense, and set dates.

## 3. Frontend identifiers, i18n, CSS, and the long tail

- i18n: `item.*` → `location.*` in every `web/locales/*.json`.
  `item.detail.location` → `location.detail.geo`, and the other
  `item.detail.*` → `location.detail.*`; `item.category.*` →
  `location.category.*`. There are no collisions with the existing
  `location.form/editor/view.*`. The values stay unchanged. Run
  `scripts/i18n.py unused` to catch dead keys (e.g. `item.deleteConfirm`,
  "Delete this item?", if it has no user, gets dropped rather than renamed).
- JS: `renderItemForm`→`renderLocationForm`, `renderItemsTab`→
  `renderLocationsTab`, `ItemCard`/`<item-card>`→`LocationCard`/
  `<location-card>`, `data-action="new-item"`→`"new-location"`, and the local
  `item`/`items` variables. **Trap:** naming a local `location` shadows
  `window.location`. In any function that also navigates or reads the URL, use
  `loc`, or write `window.location` explicitly. Check each touched file for a
  bare `location.` that means the global.
- CSS: the `.item-*` selectors in `base.css` (~24 lines) and the `item-card`
  element selectors.
- `tests/ui/*.spec.js` selectors, `scripts/gen_screenshots.mjs` selectors
  (read its header traps first).
- The long tail: comments in Go, SQL, JS and the seed, including the "UI calls
  a whole item a location" comment on `category`. Also
  `internal/assist` comments that point at `items.go`/`items.category`, and the
  `internal/markdown` package doc. Leave historical migration files alone.
- `plans/todo.md`: remove the sweep entry. Rewrite the other entries that cite
  old names (7 hits). Note any copy follow-up.

**Verify:** `make ci`, `make test-ui`, and `make screenshots`. The output must
come out pixel-identical, or differ only by noise; that proves "no visible
change". Then run the closing grep
`git grep -nwiE "items?" -- internal web cmd tests scripts ':!**/migrations/**'`
and review the remainder: it should be only the "not renamed" senses above.
Do a manual pass at 324×756 and 1280×800 in both locales.

## Build order

0 → 1 → 2 → 3. Each one compiles and passes on its own. M1 touches httpapi only
for compilation, and M2 leaves JS-internal names for M3.

## Workflow

The usual milestone loop: implement; `make ci` green, plus the per-milestone
checks above; add a **Done.** paragraph and update todo.md; commit one per
milestone; make sure `make dev` is running;
stop and wait.

## Verification (stage-wide)

- `make ci`, `make test-postgres` and `make test-ui` all green.
- The existing dev DB migrates forward and back (`0013` down/up) without data
  loss.
- Screenshots are unchanged.
- The closing grep is clean apart from the documented non-location senses.
