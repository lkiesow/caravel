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

**Done.** The API now says "location" throughout:
- **Routes:** `/api/trips/{id}/locations` (and `/batch`), and
  `/api/locations/{locationId}/…` with `PUT …/geo` in place of `…/location`.
- **JSON:** `location_id`, `location_title`, `location_category` and
  `location_image_url` on files, expenses and itinerary entries. The nested
  object is `"geo"`, the batch body is `{"locations": [...]}`, and the multipart
  create part is `"location"`.
- **Error messages** say "location".

The checklist routes keep `/items/{itemId}`, since those really are items. The
old routes are gone, not aliased. `TestItemRoutesAreGone` asks every one of them
with a real trip and location id, so its 404s mean "no such route", not "no
such row". It also checks that nothing was written through them.

How it was done, in three passes over `internal/httpapi`:
1. **Struct fields, with gopls.** 13 fields, among them `ItemID`/`ItemTitle`
   and the request/response `Location` field, which became `Geo`.
2. **Identifiers, with a Go-aware script.** It split each file into code,
   comments and string literals and renamed by rule: every item identifier in
   code, plus compound ones in comments (`itemRequest` → `locationRequest`,
   `itemLocationRequest` → `geoRequest`, `handlePutItemLocation` →
   `handlePutLocationGeo`, the test names). `checklists.go` and the checklist
   tests were left out. Two locals that were not locations were renamed first so
   the rule passed them by: an assistant `candidate` in `assist.go` and a
   `checklistItemID` in `ownership_test.go`.
3. **String literals.** URLs, JSON keys and messages were rewritten, skipping
   any literal whose surrounding code names a checklist. `router.go` was edited
   by hand because its checklist routes are bare `"/items"` strings.

15 files were renamed (`items.go` → `locations.go`, `item_dates.go` →
`location_dates.go`, `expense_item_test.go` → `expense_location_test.go`, …).

**Deviation: httpapi comment prose is done here, not in M3.** Every file was
already in this diff, and a second pass over the same files later would only
split the review. The remaining "item" comments in httpapi are history
(`items.sort_order`, `item_dates`, `item_id IS NULL`) or the checklist sense.
`cmd/seed` got the same three passes. Its `seedID` namespace is now
`"location"`, so seeded locations get new deterministic ids on the next
`make dev-seed`. Nothing depends on the old ones, because every scenario's trip
is deleted and recreated anyway. The seeded file names followed
(`cascade-location.txt`).

**Not renamed, on purpose:**
- **The blob key prefix `{trip}/items/{id}/…`.** Every existing upload lives
  under it, each row stores its own `storage_path`, and nothing parses the
  layout. A new prefix would only split the tree on disk. `uploadFile` says so
  in a comment.
- **The client-side route parameter `:itemId` in `clientroutes.go`.** It
  mirrors `app.js` and moves with it in M3.

**Frontend, API surface only.** The URLs, the `location_*` field reads, and
`.location` → `.geo` on detail objects in ten `web/js` files, with the editor's
create and update bodies and the suggest page's batch body. Local names
(`item`, `itemId`, `renderItemForm`) stay for M3. The same edits went through
14 UI specs, `tests/ui/helpers/scenarios.js`, `contrast.js` and
`gen_screenshots.mjs`. One spec passed the geo block through shorthand property
syntax (`{ title, category, location }`), which no `location:` pattern can see.
The UI suite caught it as a 400 from the server, which refuses unknown fields.
That is reassuring: a client still sending the old key fails loudly instead of
losing coordinates quietly.

**Three snags, all caught before commit:**
- The `"/location"` → `"/geo"` string rule also rewrote the real
  `/assist/location` endpoint in nine assist tests. The tests failed, and those
  calls were reverted.
- The first string pass would have renamed the bare `"/items"` checklist
  routes. They were excluded before it ran.
- The comment reflow measured a tab as four columns, so it rewrapped 24
  paragraphs here, and three in M1's `store.go`, that never mentioned an item.
  All 27 are reverted, so the diff holds only renames.

Verified:
- `make ci` green.
- `make test-postgres` green (exit 0, `internal/httpapi` in 330 s). An earlier
  run with `make test-ui` going in parallel hit go test's 10-minute timeout
  mid-`TestRoleMatrix`, with requests still answering in 20–60 ms. That is load,
  not a hang; see todo.md.
- `make test-ui`: 349 passed, and the 2 shorthand failures pass after the fix.
- The closing grep for `item_(id|title|category|image_url)` and `/items` leaves
  only checklists, history and the new 404 test.
- Manual pass on `make dev` at 324×756 (Playwright, on a throwaway trip, since
  deleted). It created a location by pin with an address and a date, re-placed
  it by address search (geo came back with the OSM identity), uploaded a file
  (201, listed with `location_title`), and added an expense through the form
  (the POST body carries `location_id`, and the list links back). The itinerary
  linked the location on its day. Deleting through the editor gave 204, a
  follow-up GET 404, and the expense kept with `location_id` null. No API
  errors and no console errors throughout.

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

**Done.** The frontend now says "location" in its own names, and the stage is
complete.

Two decisions taken at this checkpoint changed the plan:
- **CSS classes went further than planned.** The shared form style `.item-form`
  (the location form *and* the expenses form) is now `.entry-form`. The
  editor's coordinates section, `.location-form*` (`__map`, `__hint`,
  `__checkbox`, …), is now `.geo-form*`, matching the API. The rename went in
  that order so the `location-form.js` file name was never touched.
  `.item-form-slot` → `.location-form-slot`, `.item-list` → `.location-list`,
  `.items-empty` → `.locations-empty`, `itinerary-day__add-item` →
  `__add-location`, `expenses__row-item` → `expenses__row-location`.
- **Visible copy changed after all.** Five strings said "item" where they meant
  a location, so the "no visible change" rule was dropped for them:
  - the delete dialog: "Delete this item?" → "Delete this location?", de
    "Diesen Ort löschen?";
  - the itinerary's "Add item", "Add an item to {date}" and "Choose an item…"
    → "Add location", "Add a location to {date}", "Choose a location…"
    ("Ort hinzufügen" …);
  - the itinerary day count: "{count} item(s)" → "{count} location(s)",
    "Ort/Orte";
  - the empty map: "No items with a location yet …" → "No locations with
    coordinates yet. Add coordinates to a location …".

  The first four were agreed explicitly; the last two are the same case,
  found by the closing grep. The heading the plan was about stays
  "Location"/"Standort".

**What landed:**
- **i18n.** `item.*` → `location.*`, with `item.detail.location` →
  `location.detail.geo`. `expenses.form.item[None]` →
  `expenses.form.location[None]`, and `itinerary.addItem[To]`/`selectItem` →
  `…Location…`. `item.detail.close` was only ever used by the generic alert
  dialog in `dialog.js`, so it became `common.close` rather than a location
  key.
- **The editing approach for the locale files.** Both were edited as text,
  because `de.json` is not in `i18n.py`'s canonical format (see todo.md). For
  the same reason `i18n.py unused` refuses to run, so it ran on a normalised
  scratch copy: all 482 keys are referenced. A separate scan checked the
  reverse: every key the JS names exists.
- **JS.**
  - `<item-card>`/`ItemCard` → `<location-card>`/`LocationCard`, with its
    `item-id` attribute → `location-id`.
  - The map popup's `data-item-id` → `data-location-id`, and the `item-open`
    event → `location-open` with `detail.locationId`.
  - `data-action="new-item"` → `"new-location"`.
  - `renderItemForm` → `renderLocationForm`, `renderItemsTab` →
    `renderLocationsTab`, `loadTripItemIds` → `loadTripLocationIds`.
  - The `:itemId` route parameter → `:locationId`, in `app.js` and
    `clientroutes.go` together.
  - The locals in the location pages, the map, the mention picker, expenses
    and itinerary.
  - Every "item" that is a menu option, a checklist entry, a suggestion pick,
    a `<li>` or a `localStorage` call kept its name.
- **Specs, the screenshot generator, and Go comments** in `internal/assist`,
  `internal/markdown` and `scripts/i18n.py` followed. In `assist/agent.go` the
  loop variable over model proposals became `proposal`.
- **todo.md.** The sweep entry is gone, and with it the now-empty "Consistency
  and cleanup" section. The summary entry cites `mapLocationResponse` and no
  longer waits on this stage. A new entry covers the `de.json`/`en.json`
  format.

**Not renamed:**
- `.location-search` and `.location-reverse`: they look up a place, which is
  the location sense.
- Comments about history (`items.sort_order`, `item_dates`, the
  `item.google_maps_url` field).
- `geocode.go`'s raw results.
- The `items/` storage prefix (see M2).

**JS has no type-aware rename, and these are the bugs that cost. All were caught
before commit:**
- **A hoisted name collision.** `location-editor-page.js` already had an inner
  `function renderLocationForm()` for the coordinates section. Once the
  imported `renderItemForm` took that name, the hoisted inner one would have
  won at the call that builds the main form. `node --check` cannot see this. It
  surfaced in review of the export/import list; the inner one is now
  `renderGeoForm` (with `readGeoForm`, and the editor's local
  `const location` → `geo`).
- **The menu API's `items`.** The script protected `items:` keys but not a
  property write, so `tagGroup.items = […]` became `tagGroup.locations`. The
  tag filter's options came back empty, and the UI suite caught it.
- **Other senses of "item".** Two checklist selectors (`li.dataset.itemId`, a
  `.checklist-item[data-item-id]` in a spec), and two menu-sense comments.
- **Comment wording.** About a dozen comments came out wrong: "a todo.md
  location", "an locations.sort_order column", "nested location" where geo was
  meant, "the location's location". All were found by reading every changed
  comment line, and fixed.
- **Shadowing `window.location`.** No JS file uses the bare global `location`,
  so naming locals `location` shadows nothing in use. Any future code in those
  functions has to write `window.location`.
- **Comment rewrapping.** Only paragraphs this milestone pushed past 80 columns
  were rewrapped, located from the diff, after M2's lesson.

Verified:
- `make ci` green.
- `make test-ui`: 349 passed and 2 failed on the first run. One was the
  `tagGroup` bug above, fixed. The other, the assistant's
  build-from-a-prompt spec, timed out at 60 s under full-suite load. Run alone
  it passes, and `--grep "AI assistant" --repeat-each 4` passed 24/24; its
  file changed only by an i18n key. A full rerun after the fix passed 351/351,
  the assistant spec included.
- **Screenshots.** Two baseline runs of the pre-M3 tree were pixel-identical
  except `assistant.png` (81 px of noise). Against that baseline, a run after
  M3 differs only in `itinerary.png` and `mobile-itinerary.png`, and viewed
  side by side those differences are exactly the new itinerary copy (the
  select is a few pixels narrower beside the longer button). `assistant.png`
  shows the same 81 px of noise. The committed screenshots had already drifted
  from a fresh run (seeded dates are relative to the run day), so only the two
  itinerary PNGs were replaced, from the post-M3 run. Their only other change
  is the dates. `make docs` is green.
- **Manual pass on `make dev`** (Playwright), in en and de at 324×756 and
  1280×800. Nine pages each: locations, map, itinerary, expenses, files, a
  location view, its editor, a new location, and an empty map on a throwaway
  trip, since deleted. 36 loads in all, with:
  - no raw i18n keys, and no "item"/"Eintrag" wording;
  - `<location-card>` count 7 and `<item-card>` 0 on the list;
  - `.entry-form` and `.geo-form` present where expected, and no
    `.item-form`/`.location-form` left;
  - no API errors and no console errors.

  The delete dialog reads "Delete this location? This cannot be undone." /
  "Diesen Ort löschen? Das kann nicht rückgängig gemacht werden."

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
