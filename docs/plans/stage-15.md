# Stage 15 — The everyday screens get their controls

## Context

Caravel is feature-complete for one person's trip and, since Stage 14, for
several people's. What it is not is *comfortable*. Four entries under
`todo.md`'s "Planned features" share one shape of complaint: the data is all
there, and the control that would let you get at it is missing.

- **You cannot find a trip.** `web/js/pages/trips-page.js` is 53 lines: an
  `h1`, a "New trip" button, and a straight loop over `GET /trips` in
  `created_at DESC`. No search, no sort, and since Stage 14 other people's
  trips land in that same list.
- **You cannot reuse a packing list.** Stage 14 Milestone 8 gave checklists
  renaming, in-place item editing and the ⋮-menus to hold them. Copying a list
  was the one part left out, because nobody had decided what happens to the
  ticks.
- **You write note formatting blind.** Notes are a plain `<textarea>`
  (`web/js/components/location-form.js:35`) and only rendered after saving, by
  the view page's `item.notes_html`.
- **Itinerary entries have no order at all.** The backlog asks for reordering.
  Checking it turned up the bug underneath: `handleCreateItineraryEntry`
  (`internal/httpapi/itinerary.go:209`) never sets `SortOrder`, so every row is
  `0` and `ListItineraryEntriesByTrip`'s `ORDER BY e.sort_order` is an
  undefined tie. Entries within a day render in whatever order the database
  felt like. Reordering has to fix that first.

None of this is architectural — no new tables, one new endpoint, and one
migration-free `sort_order` fix. Four independent milestones plus a sweep-up,
ordered cheapest-first so the visible wins land early.

Decisions taken with the user up front:

1. **Duplicating a checklist resets the ticks**, one "Duplicate" menu item.
   The copy's `owner_user_id` becomes the duplicator.
2. **The trips toolbar is search + sort**, applied **in memory** — the API
   already returns every trip, so no backend change. Copies
   `locations-tab.js`'s toolbar shape exactly.
3. **The notes preview is server-rendered, behind an Edit/Preview toggle** — a
   small `POST /api/markdown/preview` reusing `internal/markdown.ToSafeHTML`,
   so the preview is byte-identical to what the view page will show and there
   is no second markdown implementation to drift.
4. **Reordering ships up/down buttons, not drag-and-drop**, and fixes the
   `sort_order` bug above first.
5. **Four milestones, not five.** Two candidates the user declined outright are
   in "Out of scope" below.

---

## 0. Land the plan

Commit this plan as `docs/plans/stage-15.md` before any code, then three
`todo.md` edits:

- **Delete two entries as declined, not built.** "A way to jump to 'today' in a
  long itinerary" and "No progress while a large file uploads" are both wanted
  by nobody — say so in the commit message so a future reader does not
  resurrect them as oversights. The collapse half of the first one landed in
  Stage 10 Milestone 4 and stays as it is.
- **Fix one piece of drift** found while planning: the trips search/sort entry
  cites `ListTripsByOwner`, which Stage 14 replaced with `ListTripsForUser`
  (`internal/db/sqlc/queries/trips.sql:16`).

**Also in this milestone: the sprite, once.** Three Lucide icons are needed
across the stage and `web/icons/lucide-sprite.svg` is committed, so add them
all in a single regeneration rather than re-running the script in three
milestones. Add to `ICONS` in `scripts/gen_icon_sprite.py`: `copy`
(Duplicate), `arrow-down-up` (sort menu), `chevron-up` (move entry up;
`chevron-down` and `eye` already exist). Then follow CLAUDE.md's procedure and
**diff the result** — the existing symbols must come out byte-identical, or an
upstream Lucide revision has silently restyled icons already in use.

---

## 1. Duplicate a checklist

No new SQL. `GetChecklistByID` + `ListChecklistItemsByChecklist` read;
`CreateChecklist` + `CreateChecklistItem` already take explicit `ID`,
`SortOrder`, `Checked`, `Visibility` and `OwnerUserID`.

Backend — `internal/httpapi/checklists.go`:
- `POST /checklists/{checklistId}/duplicate`, routed beside the existing
  `PATCH`/`DELETE` (`internal/httpapi/router.go:190-202`).
- Authorization: `loadChecklist(w, r, db.RoleEditor)` for reachability, then
  the *read* rule rather than the write one — you may copy any list you can
  see, including a shared list you did not author. `canModifyChecklist`
  (`checklists.go:82`) is the write rule and is the wrong one here; the read
  rule is what `ListChecklistsByTrip` encodes
  (`visibility <> 'personal' OR owner_user_id = @user_id`).
- The copy: same `Visibility`, `OwnerUserID: &me.ID`, `SortOrder:
  len(existing)` (recomputed by re-listing, as `handleCreateChecklist` does),
  every item copied with `Checked: false` and its original `SortOrder`.
- Title: the client sends it. The server emits no user-facing copy today and
  should not start — the frontend builds it from
  `checklists.duplicateTitle` (`"{title} (copy)"`).
- Respond 201 with `writeChecklist`, so the client gets the same shape create
  does.
- Go test: duplicate a shared list as a non-author editor (allowed), a personal
  list as its author (allowed, copy stays personal and mine), someone else's
  personal list (404 — it is not visible), and as a viewer (403). Assert the
  copy's items are all unchecked and the source is untouched.

Frontend — `web/js/components/checklist-list.js`: one more `action: true` entry
in `renderCardMenu` with `iconName: "copy"`, between the visibility moves and
`rename`. `onSelect` POSTs, pushes the returned list, re-renders. Offered
whenever `!readOnly` — copying is a create, so it needs editor, not authorship.

**Done.** `POST /api/checklists/{checklistId}/duplicate`, plus one `copy` row in
`renderCardMenu` and the two locale keys. Built as planned, with two things
worth recording.

*The authorization rule turned out to need no new code.* `loadChecklist`
already answers 404 for somebody else's personal list and 403 for a viewer, so
"the read rule" is simply what you get by calling it and **not** calling
`requireChecklistWrite`. The handler carries a comment saying that omission is
deliberate, because it looks like a missing check.

*One consequence that was not in the plan.* The card menu was only rendered
`if (canWrite || canMove)`, so a non-author's view of a trip-visible list had no
⋮ at all. Duplicate is offered to any editor, so the condition gained
`|| canDuplicate` — and that list, the one most worth copying, now has a ⋮
holding exactly one item. `menu.spec.js` gained a test for precisely that view,
driven as `other` through `openAs`.

Two seams inside the handler that are decisions, not defaults: the copy is
wrapped in `Store.WithTx` (a list plus N items is inherently multi-write, and a
half-copy would silently disagree with its source), and the title comes from the
client, because "(copy)" is translated copy and `internal/httpapi` emits no
user-facing strings.

Verified: `make ci` green. Seven new Go tests in
`internal/httpapi/checklist_duplicate_test.go`, and **break-checked** rather
than merely passing — the todo.md lesson about a test that asks the code what to
expect. Adding `requireChecklistWrite` to the handler fails
`TestDuplicateChecklistSomeoneElsesTripVisibleList` (403); copying
`item.Checked` fails `TestDuplicateChecklistResetsTicks`; keeping
`source.OwnerUserID` fails two. All 80 UI specs pass, including the Duplicate
row asserted in the menu list in both locales and the new non-author test. And
a real click at 324px on the `full` scenario: "Packing (copy)" appeared in the
shared section with all four items in source order and **0/4 ticked** where the
source has 1/4, the server-stored row read `shared · mine=true · ticked=0/4`,
and the source was unchanged. The copy was deleted afterwards, so the seed is
back as it was.

---

## 2. The trips list gets a toolbar

`web/js/pages/trips-page.js`. Copy `locations-tab.js`'s pattern, including its
reasoning: one non-wrapping row, filtering in memory over a list fetched once,
two distinct empty states.

- Markup: `.trips-toolbar` holding a `.locations-search`-shaped search input, a
  `renderMenu` sort control (`iconName: "arrow-down-up"`,
  `ariaLabel: "trips.sort.label"`), and the existing "New trip" button moved
  into the row.
- Search matches `title` and `subtitle`, lowercased substring — the same
  `matches()` shape as locations.
- Sort options: `newest` (the current `created_at DESC`, and the
  `neutralValue`/`activeValue` default), `title` (A–Z via `localeCompare` under
  the active locale), `start` (earliest start date first). **Trips with no
  `start_date` sort last** under `start` rather than first — a trip with no
  dates is unscheduled, not imminent.
- Two empty states, as locations has: `trips-empty` (the existing "no trips at
  all" paragraph) and a new `trips.noMatches`.
- Re-render on `input` and on select; no refetch.

CSS: `.locations-toolbar`/`.locations-search` in `web/css/base.css` are named
for one caller. Prefer generalising *those two rules* into a shared
`.list-toolbar`/`.list-search` used by both pages over copy-pasting a third
near-identical block — `base.css` already carries a `todo.md` entry about three
drifting input rules, and this is the same mistake one step earlier.

Verify: a spec asserting the row does not overflow at 324px with three
controls, that typing narrows the grid, that each sort order rearranges
`<trip-card>` titles as expected, and that the no-matches state appears. The
seed has seven scenario trips with varied dates, including `no-dates`, which is
exactly the sort edge case above.

---

## 3. A preview for location notes

Backend:
- `POST /api/markdown/preview`, authenticated but **not** trip-scoped (it
  renders text the caller already has). Body `{markdown}`, response `{html}`,
  via `markdown.ToSafeHTML` — the same call `renderNotesHTML`
  (`internal/httpapi/trips.go:86`) makes, so output cannot diverge from the
  view page.
- Cap the input length and return 400 above it, so this cannot be used as a
  general-purpose renderer.
- Go test: a request whose response HTML equals `renderNotesHTML`'s for the
  same input, and that a `<script>` is stripped (the sanitizer itself is
  already covered in `internal/markdown`; this asserts the endpoint uses it).

Frontend — `web/js/components/location-form.js`:
- Two-state toggle above the textarea (Edit / Preview), labels
  `location.form.notesEdit` / `location.form.notesPreview`, `icon("eye")` on
  the preview side, `aria-pressed` carrying the state.
- Preview swaps the textarea for a `div` given **the view page's own class**
  (`.location-view__notes`), so the styling is shared rather than
  reimplemented.
- One request per toggle into preview, not per keystroke. Skip the request when
  the text is unchanged since the last preview, and when it is empty (show
  `location.form.notesEmpty`). A failed request shows the standard inline error
  and returns to Edit rather than showing a blank preview.
- The toggle must not submit the form (`type="button"`) and must not lose the
  textarea's autogrown height when toggling back — `autoGrowNotes` runs on
  `input`, so call it after restoring.

Verify: a spec that types `# Heading` plus `**bold**`, toggles to Preview, and
asserts a real `h1` and `strong` exist inside `.location-view__notes`; then
toggles back and asserts the textarea still holds the source. Plus the a11y
sweep already covering every button on the route.

---

## 4. Itinerary entries get an order — and one, first

The bug first, because reordering on top of it would be building on sand.

- **`sort_order` is never set.** `handleCreateItineraryEntry`
  (`internal/httpapi/itinerary.go:209`) omits `SortOrder`, so every row is `0`
  and `ListItineraryEntriesByTrip`'s `ORDER BY e.sort_order` decides nothing.
  Set it to the count of entries already on that day. A Go test that adds three
  entries and asserts the returned order is insertion order is the one that
  would have caught this.
- No migration: `itinerary_entries.sort_order` exists in `0001_init`.
- New query in `internal/db/sqlc/queries/itinerary_entries.sql`:
  `SetItineraryEntrySortOrder` (id + itinerary_day_id + sort_order), plus
  whatever per-day list the handler needs to validate and renumber. Then
  **`sqlc generate` by hand from `internal/db/sqlc/`** and add the method to
  `Store` (`internal/db/store.go`) and *both* adapters (`sqlite_store.go`,
  `postgres_store.go`). Keep the query comments in plain prose — no backticks,
  no double quotes, avoid apostrophes (CLAUDE.md's three sqlc traps; bisect
  comments, not SQL, if it reports a syntax error on correct-looking SQL), and
  read the generated file rather than only diffing it for churn.
- Endpoint: `PUT /trips/{tripId}/itinerary/{dayId}/entries/order` taking the
  full ordered list of entry ids for that day and renumbering inside
  `Store.WithTx` — one transactional write, and it validates that the set of
  ids matches the day exactly. Sturdier than a per-entry "move up" endpoint,
  which needs two writes and can interleave.
  Authorization: `loadItineraryDay(w, r, db.RoleEditor)`, as its siblings do.
- Frontend (`web/js/pages/itinerary-tab.js`): **up/down buttons, not
  drag-and-drop.** The entries are a `<ul>` of real links inside a `<details>`
  on a 324px phone; native HTML5 drag does not work on touch, and a
  pointer-events reorder is its own piece of work. Two `btn-icon`s per entry
  (`chevron-up` / `chevron-down`), disabled at the ends, hidden when
  `!editable`. Reorder locally, render, then PUT — and on failure re-render
  from the server rather than leaving the optimistic order showing.
- Each button needs a distinct accessible name (`itinerary.moveUp` /
  `itinerary.moveDown`), or the a11y-names sweep will fail — correctly.

Verify: Go tests for insertion order, the reorder endpoint's happy path, a
mismatched id set (400), and a viewer (403). Playwright on the `full`
scenario's two-entry day: click down, assert the titles swapped, reload, assert
it stuck. Tap targets on the two new buttons at 324px — the sweep covers them,
but the `<details>` must be open for it to see them, which the seed's
relative-to-today dates arrange for the current day only. Worth checking rather
than assuming.

---

## 5. Sweep-up

The habit from every stage's last milestone: ask what the previous four left.

- **Which scenario renders this?** — for every element added above. The known
  shape of that gap: the trips no-matches state, and the disabled ends of the
  reorder buttons. Anything only reachable behind an interaction stays a
  `todo.md` entry, but the *empty and edge* states should be seeded or driven.
- **German.** Three new control clusters, and German is the longer language.
  Run the overflow sweep in `de` at 324px for the trips toolbar and the
  Edit/Preview toggle specifically, whatever the suite does by default.
- **i18n parity** (`scripts/check_i18n.py` in `make ci`) plus
  `scripts/i18n.py unused` by hand — the new keys are all static strings, so
  the report should stay clean.
- **`todo.md` in both directions**: delete the four entries this stage
  implemented (trips search/sort, checklist duplication, notes preview,
  itinerary reordering — the two declined ones went in Milestone 0), and add
  what it deferred: drag-and-drop reordering, backend `q`/`sort` for very large
  trip lists, and a side-by-side preview.
- Update each milestone's section here with its **Done.** paragraph as it lands
  (per the workflow), not in one pass at the end.

---

## Build order

`0 → 1 → 2 → 3 → 4 → 5`. Cheapest-first: 1 is a handler and a menu row, 2 and 3
are medium, 4 is the only one that regenerates sqlc and edits both dialect
adapters. A stall in 4 blocks nothing but the sweep-up.

## Workflow

One milestone at a time. For each: **implement**, **verify** (`make ci` green,
plus a Playwright or `go test` assertion proving the behaviour actually
changed), **update `docs/plans/stage-15.md`** with a **Done.** paragraph and
`todo.md` in both directions, **commit** (one commit per milestone; a follow-up
fix gets its own "... follow-up: ..." commit), **make sure `make dev` is
running**, then **stop and hand back control**. Do not start the next milestone
until told to continue; feedback at a checkpoint gets fixed and re-verified
before moving on.

## Verification (stage level)

- `make ci` green at every commit.
- New Go tests: checklist duplicate (four authorization cases), the markdown
  preview endpoint, entry insertion order, the reorder endpoint (happy path,
  mismatched ids, viewer).
- New/extended Playwright coverage: the trips toolbar (search, three sort
  orders, no-matches, 324px overflow), the notes preview toggle, and the
  reorder buttons including persistence across a reload. `menu.spec.js` gains
  the checklist Duplicate row in both locales, which is where that spec already
  asserts the card menu.
- A manual pass at 324×756 against `make dev`: the phone is where every one of
  these controls is tightest.

## Out of scope

Declined outright by the user and **removed from `todo.md`** in Milestone 0, so
they are not resurrected later as oversights:

- **Jump to "today" in a long itinerary.** The collapse half (Stage 10
  Milestone 4) stays; no control is wanted.
- **Upload progress for large files.** The drop zone's `.file-drop--busy`
  dimming is feedback enough.

Staying in `todo.md`, untouched:

- **Drag-and-drop reordering** — Milestone 4 ships up/down buttons; a
  pointer-events reorder is separate work with its own touch testing.
- **"Add to every day of this stay"** and **per-category Type suggestions** —
  both were offered as a fifth milestone and both declined in favour of a
  tighter stage.
- **Backend `q`/`sort` on `GET /trips`** — deliberately not built; the API
  returns every trip already, and the in-memory version is what
  `locations-tab.js` settled on for the same reason.
- **The Postgres CI job and the migration squash** — the strongest entry in the
  backlog and the obvious candidate for Stage 16. Milestone 4 regenerating sqlc
  and hand-editing both adapters is one more instance of exactly the risk that
  entry describes.
