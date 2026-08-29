# Stage 25 — A location's dates *are* its place in the itinerary

## Context

Setting a hotel's dates to 5–7 September in the location editor does not put the
hotel on the 5th, 6th and 7th of the itinerary. Those days render empty. This
was not a bug in the strict sense — it is two date models that were never
connected — but it is not defensible behaviour either.

Today there are two independent representations:

- **`item_dates`** (`0001_init.up.sql:132`) — a 1:many table on an item holding
  `start_date`, `end_date`, `label`, `all_day`, `start_time`, `end_time`. Written
  by the location editor, read by exactly one place: the Dates card on the
  location detail page. Nothing else in the codebase consults it.
- **`itinerary_days` + `itinerary_entries`** — the day↔item join that drives the
  itinerary tab, carrying a per-day `sort_order` and `note`.

`plans/stage-01.md:207` anticipated the gap and proposed a one-shot convenience
action ("add to every day of this stay") that was never built. This stage takes
the stronger option instead: **`itinerary_entries` becomes the single source of
truth**, and a location's dates become a *derived view* of the itinerary days it
appears on, with contiguous runs collapsed into ranges. Editing the range in the
location editor creates and removes itinerary entries; editing the itinerary
changes what the location page shows. One fact, two windows onto it.

### Decisions taken before planning

- **Ranges are inclusive of both endpoints.** A stay of 5–7 September puts the
  location on days 5, 6 *and* 7 — checking out on the 7th still means being
  there on the 7th.
- **`item_dates` is dropped, not migrated.** Caravel has no published release;
  `docs/running/upgrading.md:48` already warns that pre-release databases cannot
  be carried forward. Existing rows are discarded. This removes the single
  largest piece of work from the stage.
- **`label` goes away.** The itinerary entry's per-day `note` is the only
  annotation. Keeping a range-level label would need a stable identity for a
  staged row the editor does not have.
- **`all_day` / `start_time` / `end_time` go away.** They have no UI today and
  no user has asked for one. `itinerary_entries` therefore needs **no new
  columns** — a large simplification. "A time on an itinerary entry" goes to the
  backlog.
- **Dates outside the trip's range are allowed.** `handleGetItinerary`
  (`itinerary.go:94-99`) already renders days outside `trips.start_date/end_date`,
  so a location dated the evening before the trip simply adds a visible day.

### What exploring the code changed about the plan

- **No new columns are needed anywhere**, once times and labels are dropped. The
  whole stage is one dropped table, one new index, two new read paths and one
  reconcile function.
- **`itinerary_entries` has no unique constraint on `(itinerary_day_id, item_id)`**
  — an item can legitimately sit on one day twice today. Both the derive and the
  reconcile have to cope rather than assume.
- **The dialect trap is real and is exactly the class CLAUDE.md warns about.**
  `itinerary_days.date` is `TEXT` in SQLite (`0001_init.up.sql:187`) and `DATE`
  in Postgres (`postgres/0001_init.up.sql:196`), so any new query selecting it
  generates `string` in one package and `time.Time` in the other.
  `postgresItineraryDayToDomain` (`postgres_store.go:1015`) already formats with
  `dateLayout` and is the pattern to copy. `sort_order` is likewise `int64` vs
  `int32`. A query that is green on SQLite proves nothing here.
- **The editor sends `dates` on *every* save**, including one that only changed
  the title (`location-editor-page.js:343`). Harmless while `item_dates` had a
  single writer; under the new model it makes every location save assert the
  item's complete itinerary membership, so an entry a co-editor added between the
  GET and the Save is silently deleted. This is the sharpest hazard in the change
  and it lives entirely in the frontend.
- **`writeItemNested` needs the trip id** for `EnsureItineraryDay`, and both call
  sites already hold the `db.Item` — `handleUpdateItem` (`items.go:355`) and
  `createItemTx` (`items_create.go:299`). Passing the row instead of the id costs
  nothing.
- **`WithTx` is not nestable** and `writeItemNested` already runs inside one, so
  the reconcile must use the `db.Store` it is handed and never `s.Store`.
- **The seeder writes item dates too** (`cmd/seed/main.go:335`) — and already has
  `addDay`/`addEntry` helpers (`main.go:351-356`), so it converts rather than
  grows.
- **`POST /items/{id}/dates` and `DELETE /items/{id}/dates/{dateId}`**
  (`router.go:433-434`) have no meaning in a derived model — there is no date id
  to address. The editor has never called them. They are deleted, along with the
  ownership and role matrix rows that cover them (`ownership_test.go:145`,
  `roles_test.go:205`).

---

## 1. The read path: days an item is on, collapsed into ranges

Purely additive — no behaviour changes, nothing is wired up yet. Its point is to
isolate the dialect trap in its own commit.

**SQL.** One query appended to
`internal/db/sqlc/queries/itinerary_entries.sql` (not a new file — `item_dates.sql`
is deleted in Milestone 4):

```sql
-- The days one location appears on. Duplicate rows for one date are possible
-- and are returned as they are, so the caller collapses to a set.
-- name: ListItineraryDatesByItem :many
SELECT e.item_id, e.id AS entry_id, e.itinerary_day_id AS day_id, e.sort_order, d.date
FROM itinerary_entries e
INNER JOIN itinerary_days d ON d.id = e.itinerary_day_id
WHERE e.item_id = sqlc.arg(item_id)
ORDER BY d.date, e.sort_order;
```

Comment prose stays plain per CLAUDE.md — no backticks, no double quotes, no
apostrophes. No `sqlc.narg`, so no `CAST` needed. Run `sqlc generate` by hand
from `internal/db/sqlc/` for both dialects and **read** the generated files.

Only the by-item variant is built. A trip-wide variant would be needed if the
locations *list* ever shows dates; that is out of scope, and the
`ListItemCoordinates` bucketing at `items.go:120-126` is the precedent to copy
when it happens.

**Store.** New `ItemItineraryDate` projection in `internal/db/domain.go` beside
`ItineraryEntryDetail` (`ItemID, EntryID, DayID, Date string, SortOrder int`),
one method on the `db.Store` interface, and implementations in
`sqlite_store.go` / `postgres_store.go` mirroring `ListItineraryEntriesByDay`
(sqlite:776, postgres:961) — the Postgres one formatting `Date` with
`dateLayout` and narrowing `sort_order`.

**Collapse.** New `internal/httpapi/item_dates.go` (`items.go` is already 591
lines):

```go
func collapseDateRanges(dates []string) []itemDateRangeResponse
```

Dedupe into a set (this is where two entries on one day become one date), sort
lexically — ISO dates are zero-padded, the same assumption `itinerary.go:103`
already makes — then walk, extending a run while the next date equals
`AddDate(0,0,1)` of the previous. Real date arithmetic, never string arithmetic:
month and year boundaries. Returns a non-nil empty slice so the JSON is `[]`.

**Verify.** `make ci`, plus **`make test-postgres`** — this milestone exists
because of the `DATE`/`TEXT` split, so a SQLite-only run does not prove it. Table
test for `collapseDateRanges` covering: empty, single day, contiguous run,
month boundary (31 Jan → 1 Feb), year boundary, gap, duplicates, unsorted input.

**Done.** Landed as planned, with nothing wired up: `ListItineraryDatesByItem`
appended to `internal/db/sqlc/queries/itinerary_entries.sql`, the
`ItemItineraryDate` projection in `domain.go`, one method on the `db.Store`
interface, and implementations in both dialect stores. `collapseDateRanges` and
`itemDateRangeResponse` live in the new `internal/httpapi/item_dates.go`. The
old `item_dates` path is untouched and still serves the location page, so this
commit changes no behaviour at all.

The dialect split the plan predicted is real and is now visible in the generated
code: `ListItineraryDatesByItemRow.Date` is `string` with `SortOrder int64` in
`sqlite/gen`, and `time.Time` with `int32` in `postgres/gen`. The Postgres store
formats the date back with `dateLayout`, the way `postgresItineraryDayToDomain`
already does for the same column.

Verified: `make ci` green; `make test-postgres` green (the full suite, 127s).
`TestCollapseDateRanges` is a twelve-case table — empty, single day, contiguous
run, gap, a day removed from the middle, month boundary, year boundary, leap
day, duplicates, unsorted input, and range ordering. The month and leap-day
cases are the ones that fail if the walk ever does string arithmetic instead of
`AddDate`. `TestListItineraryDatesByItem` drives the real query through the API
and was additionally run on its own against a live Postgres container to confirm
it executes rather than skips there: four appearances including a deliberate
duplicate on the 7th, returned in date order, filtered to one location, with the
date arriving as a plain `YYYY-MM-DD` on both dialects.

One deviation, small: a test case was named "a missing leap day splits the run"
while asserting a single range. The assertion was right — 2027 has no 29th, so
28 February is followed directly by 1 March — and the name was wrong. Renamed
rather than "fixed".

---

## 2. The write path: the API speaks ranges

The API switches over completely in one milestone, so it is never half-converted.

**Wire shape** (`internal/httpapi/items.go`), replacing `itemDateResponse`
(items.go:80) and `itemDateRequest` (items.go:483):

```go
type itemDateRangeResponse struct {
    StartDate string `json:"start_date"`  // never null now
    EndDate   string `json:"end_date"`    // equals StartDate for a single day
}

type itemDateRangeRequest struct {
    StartDate string  `json:"start_date"`
    EndDate   *string `json:"end_date"`   // absent means a single day
}
```

The JSON key stays `dates` so the frontend diff stays small.
`itemRequest.validate()` (items.go:164) gains: `start_date` required and
parseable; `end_date`, when present, parseable and **not before** start; and a
span cap (~370 days per range and across the expanded set) rejected as a 400.
The cap is not pedantry — see the transaction note below.

**Read.** `buildItemDetail` (items.go:312) swaps its `ListItemDatesByItem` block
for `ListItineraryDatesByItem` + `collapseDateRanges`, keeping the same tolerant
`err == nil` shape the location and links blocks use.

**Reconcile.** In `internal/httpapi/item_dates.go`:

```go
func reconcileItemDates(ctx context.Context, store db.Store, item db.Item, ranges []itemDateRangeRequest) error
```

Deliberately a **diff, not delete-all-then-recreate** the way links are handled
just above it — an entry carries a position and a note the location editor knows
nothing about, so a day that stays must keep its row untouched:

1. Expand the ranges into a desired date set (overlaps union naturally).
2. Read current state with the *same* query the read path uses, bucketed into
   `map[string][]db.ItemItineraryDate` — a slice, because duplicates are legal.
3. **Remove** each date not desired: delete *every* entry on that date, then
   `renumberItineraryDay` (`itinerary.go:462`) to close the gap. If the day is
   now empty **and** its `notes` is nil, `DeleteItineraryDay` — the exact inverse
   of lazy creation; the notes guard is what stops a location edit destroying
   somebody's day notes through the cascade.
4. **Add** each desired date not present, in ascending order:
   `EnsureItineraryDay` (never `UpsertItineraryDayNotes`, which would blank an
   existing day's notes — the same choice `handleMoveItineraryEntry` makes),
   then `CreateItineraryEntry` with `SortOrder: len(existing)`, appending at the
   end exactly as `handleCreateItineraryEntry` (`itinerary.go:216-222`) does.
5. **Dates in both sets: nothing happens.** No delete, no insert, no renumber.
   A save that did not change the dates writes zero itinerary rows.

Duplicates on a *kept* day are left alone — pruning them would mean a location
rename silently deleting an itinerary row somebody added on purpose.

`writeItemNested` (items.go:197) takes `item db.Item` instead of `itemID string`
so `item.TripID` is available; both call sites already hold the row. It must use
the `store` it is handed — `WithTx` is not nestable.

**Delete** `handleCreateItemDate`/`handleDeleteItemDate` (items.go:492, 537) and
their two routes.

**Verify.** `make ci` and `make test-postgres`. The test that matters is the
invariant one: create a range, reorder and annotate a day in the middle of it via
the itinerary API, PATCH the same range plus one extra day, then assert the
middle day's `sort_order` and `note` are unchanged and only the new day gained a
row. Plus: `dates` absent leaves the itinerary alone; `dates: []` clears it;
removing a day with notes keeps the day; a bad range is a 400 before any write.
Update `items_test.go`, `items_create_test.go`, and drop the deleted routes from
`ownership_test.go:145` and `roles_test.go:205`.

**Done.** The API switched over completely. `itemDateResponse` and
`itemDateRequest` are gone, replaced by `itemDateRangeResponse` (two non-null
strings, no id) and `itemDateRangeRequest` (`end_date` optional, absent meaning
a single day). `buildItemDetail` derives the ranges through
`ListItineraryDatesByItem` + `collapseDateRanges`; `writeItemNested` takes the
`db.Item` rather than an id, so `item.TripID` reaches `EnsureItineraryDay`, and
its dates block is one call to `reconcileItemDates`. `validateItemDateRanges`
rejects a missing or unparseable start, an end before the start, and anything
over `maxItemDateSpan` (370 days) both per range and in total -- all before a
write, so a bad range stays a 400. `POST /items/{id}/dates` and `DELETE
/items/{id}/dates/{dateId}` and their handlers are deleted, and the ownership
and role matrices now cover dates through the item PATCH that actually writes
them.

Verified: `make ci` and `make test-postgres` both green. Four new tests in
`item_dates_test.go`: the stage's own bug (set 5-7 September, find the location
on all three days, then remove the 6th in the itinerary and watch the location
report two ranges); the reconcile invariant; careful day removal; and
absent-versus-empty.

The invariant test was checked by mutation rather than trusted. Rewriting the
reconcile as delete-all-then-recreate makes
`TestReconcileItemDatesKeepsUntouchedDays` fail on the entry ids -- the hotel
comes back as a new row -- which is the evidence that the test can actually see
the difference. Worth noting what it did *not* catch on its own: the renumber
happened to restore the same visible order, so an assertion on order alone
would have passed a recreate.

Two deviations from the plan:

- **A two-line frontend change landed here rather than in Milestone 3.**
  `readJSON` refuses unknown fields, so the moment the request type lost
  `label` the editor's every date save became a 400. Leaving the checkpoint
  with a broken save was the worse trade, so `draft.dates` stops sending
  `label`. The label input is still on the form and is now ignored; Milestone 3
  removes it properly along with its i18n key.
- **An unrelated rough edge, recorded not fixed.** Confirming the deleted route
  was gone showed it answering 200 with the SPA shell. A route that never
  existed does the same, so this is the static fallback catching everything
  under /api and is pre-existing; it went to `plans/todo.md`.

---

## 3. The editor and the location page

**`web/js/pages/location-editor-page.js`** — `draft.dates` (line 97) drops
`label`; the add form (lines 187-197) loses its label input and keeps
`startDate` required with `endDate` optional and inclusive; `renderDatesList`
(line 849) uses `formatDateRange` from `web/js/format.js:14` instead of printing
raw ISO strings.

And the hazard: **`dates` must only be sent when the date card was touched.** A
`datesDirty` flag set by the add and remove handlers, and `commitSave` (line 334)
omits the key entirely otherwise — `absent means leave alone` is already the
documented contract (items.go:145-149). Without this, saving a retitled location
deletes itinerary entries a co-editor added minutes earlier.

**`web/js/pages/location-view-page.js:116`** — same `formatDateRange` treatment,
no label suffix.

**i18n**: remove `item.detail.dateLabel` from **both** `web/locales/en.json` and
`de.json` (`scripts/check_i18n.py` gates this in `make ci`).

**Verify.** `make ci`, then `make test-ui`. `tests/ui/locations.spec.js:85-116`
is rewritten: set 20–22 Aug on a location, save, then assert via
`GET /api/trips/{id}/itinerary` that the location is on all three days — which is
the actual bug this stage exists to fix, asserted rather than screenshotted. A
second case proves the diff: put a note on the middle day through the itinerary
tab, re-save the location unchanged, assert the note survived. Manual pass at
324×756 against `make dev`.

## 4. Drop the table and catch everything up

Migration `0004_item_dates_from_itinerary.{up,down}.sql` in **both**
`internal/db/migrations/sqlite/` and `.../postgres/`, in the house style of the
0003 pair (prose "why" block, then DDL):

```sql
DROP INDEX IF EXISTS idx_item_dates_item_id;
DROP TABLE IF EXISTS item_dates;

CREATE INDEX idx_itinerary_entries_item_id ON itinerary_entries(item_id);
```

The new index is needed: `ListItineraryDatesByItem` filters on `item_id`, and the
only index on that table today is on `itinerary_day_id`. The down migration
recreates `item_dates` verbatim from `0001_init` — **empty**, with the comment
saying plainly that the data cannot be reconstructed, and remembering
`all_day INTEGER`/`start_date TEXT` in SQLite versus `all_day BOOLEAN`/`start_date DATE`
in Postgres.

Then delete `internal/db/sqlc/queries/item_dates.sql`, regenerate both dialects
by hand, and remove `CreateItemDate` / `ListItemDatesByItem` / `DeleteItemDate`,
`CreateItemDateParams` and the `ItemDate` domain type from `store.go`,
`domain.go`, `sqlite_store.go`, `postgres_store.go`.

`cmd/seed/main.go:335` converts its `spec.dates` block to `addDay` + `addEntry`
(`main.go:351-356`), which also makes the seeded demo trip demonstrate the
unification instead of contradicting it.

Docs: `docs/features/trips-and-locations.md:44` ("links, its own dates, a photo")
and `docs/features/itinerary-and-lists.md` both need a sentence saying a
location's dates *are* its days on the itinerary. Regenerate screenshots only if
the seeder change visibly alters a captured page.

**Verify.** `make ci`, `make test-postgres`, `make dev-seed` against a fresh
database, and `make docs` for the documentation edits. Confirm migrating an
existing dev database forward and back does not error.

## Build order

1 → 2 → 3 → 4, strictly.

1 is additive groundwork 2 depends on. 3 cannot precede 2 (the form would post a
shape the server rejects). 4 must come last: the table can only be dropped once
nothing reads or writes it, and the seeder cannot be converted before the write
path exists.

## Files this touches

- `internal/db/`: `sqlc/queries/itinerary_entries.sql` (+ deleting
  `item_dates.sql`), `domain.go`, `store.go`, `sqlite_store.go`,
  `postgres_store.go`, the generated `sqlc/{sqlite,postgres}/gen/`, and
  `migrations/{sqlite,postgres}/0004_*`.
- `internal/httpapi/`: new `item_dates.go`, plus `items.go`, `items_create.go`,
  `router.go`, and `items_test.go`, `items_create_test.go`, `ownership_test.go`,
  `roles_test.go`.
- `web/js/pages/location-editor-page.js`, `web/js/pages/location-view-page.js`,
  `web/locales/{en,de}.json`.
- `cmd/seed/main.go`.
- `tests/ui/locations.spec.js`.
- `docs/features/trips-and-locations.md`,
  `docs/features/itinerary-and-lists.md`.
- `plans/stage-25.md`, `plans/todo.md`.

## Out of scope, deliberately

- **Times on itinerary entries.** `all_day`/`start_time`/`end_time` are dropped
  with the table; a real time UI goes to `plans/todo.md`.
- **Dates on the locations list or cards.** Nothing renders them today. If it
  ever should, it needs a trip-wide query and the `ListItemCoordinates` bucketing
  pattern, *not* a query per card.
- **A link from the location's dates card into the itinerary tab.** Worth having,
  not needed to fix the reported problem.
- **Mapping the `EnsureItineraryDay` unique-constraint race to a 409.** Two
  clients creating the same day concurrently currently surfaces as a 500. It is
  pre-existing, now reachable from a more common action; backlog it rather than
  widen this stage.
- **Deduplicating an item that appears twice on one day.** If that should be
  prevented, it belongs in the itinerary tab.

## Verification

Every milestone: `make ci` green, plus its own named proof above — an assertion
rather than a screenshot wherever one is possible. **`make test-postgres` for
Milestones 1, 2 and 4**, without exception: this stage adds a query over a column
whose Go type differs by dialect, which is precisely the failure that stays green
on SQLite (Stage 18 Milestone 3). `make test-ui` and a 324×756 manual pass for
Milestone 3. `make docs` for Milestone 4.

The end-to-end proof of the whole stage: set a location's dates to 5–7 September,
open the itinerary, and find it on all three days — then move it off the 6th in
the itinerary and find the location page showing "5 Sep" and "7 Sep" as two
ranges.

## Workflow

One milestone at a time, in the order above. For each: implement, verify
(`make ci` plus the milestone's own proof), add a **Done.** paragraph to that
milestone's section in `plans/stage-25.md` describing what actually landed —
including any deviation from this plan — and how it was verified, reconcile
`plans/todo.md` in both directions, commit (one commit per milestone; a same-day
follow-up fix gets its own "... follow-up: ..." commit), make sure `make dev` is
running, then stop and hand back control. Do not start the next milestone until
told to continue; feedback given at a checkpoint is fixed and re-verified before
moving on.
