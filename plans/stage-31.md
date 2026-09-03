# Stage 31 — Trip notes: a markdown scratchpad per trip

## Context

A trip has structured places for structured things — locations, itinerary
days, checklists, expenses — and nowhere at all for prose. Anything that is
not a place or a task (visa paperwork, a packing rationale, restaurant
hearsay, "ask Anna about the car") currently gets wedged into a location's
notes field or an itinerary day it does not belong to.

This stage adds a **Notes** tab: one markdown document per trip. Empty trip
opens straight into the editor; once there is text and you press Save, the
tab shows the rendered markdown and the editor is behind an Edit button.

Four decisions were taken up front:

- **Placement.** Notes goes between Itinerary and Checklists as a *primary*
  tab, and Checklists moves into the More menu. The phone row fits exactly
  four primary tabs plus More, and `trip-tabs.js` requires every overflow tab
  to come after every primary one, so this is the only arrangement that puts
  Notes where it was asked for without retuning the mobile grid.
- **Shape.** One document per trip, not a list of named notes. `GET`/`PUT`
  on a single resource — no create, delete, rename or reorder.
- **Concurrency.** Last write wins, as itinerary day notes already do.
- **Process.** A short stage, three milestones, one commit each.

Nothing here invents a markdown stack: `internal/markdown` (goldmark +
bluemonday) and `renderNotesHTML` already exist and are reused verbatim.

---

## Milestone 1 — Schema, store, and the API

**Migration** `internal/db/migrations/{sqlite,postgres}/0008_trip_notes.{up,down}.sql`
(0007 is the current head). One row per trip, so `trip_id` is the primary key:

```sql
CREATE TABLE trip_notes (
    trip_id TEXT PRIMARY KEY REFERENCES trips(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by TEXT REFERENCES users(id) ON DELETE SET NULL
);
```

Match `0001_init`'s dialect conventions for the postgres twin (timestamp and
id column types) rather than copying the SQLite text verbatim. Down file
drops the table. `scripts/check_migrations.py` enforces the pairing and
dialect parity; it runs in `make ci`.

**Queries** — new `internal/db/sqlc/queries/trip_notes.sql`:
`GetTripNote`, `UpsertTripNote`, `DeleteTripNote`. Then run `sqlc generate`
**by hand** from `internal/db/sqlc/` and read the generated
`{sqlite,postgres}/gen/trip_notes.sql.go` rather than only diffing it.
Two traps from CLAUDE.md apply directly here:

- named args are **not** substituted inside `ON CONFLICT ... DO UPDATE` —
  use `excluded.body`, `excluded.updated_at`, `excluded.updated_by`;
- keep the comment prose in the `.sql` plain (no backticks, no quotes, avoid
  apostrophes) or the SQLite lexer misreports a syntax error on correct SQL.

**Domain and store** — `TripNote{TripID, Body string; UpdatedAt time.Time;
UpdatedBy *string}` in [domain.go](internal/db/domain.go) beside the
`Checklist` block; `UpsertTripNoteParams` in
[store.go](internal/db/store.go) with the other param structs; three methods
on the `Store` interface; adapters plus a `sqliteTripNoteToDomain` /
`postgresTripNoteToDomain` converter in
[sqlite_store.go](internal/db/sqlite_store.go) and
[postgres_store.go](internal/db/postgres_store.go), following the checklist
adapters exactly.

**Handlers** — new `internal/httpapi/trip_notes.go`, modelled on
[checklists.go](internal/httpapi/checklists.go):

```
GET /api/trips/{tripId}/notes   RoleViewer
PUT /api/trips/{tripId}/notes   RoleEditor   body: {"body": "..."}
```

Registered in [router.go](internal/httpapi/router.go) in the trip-scoped
block immediately after the two itinerary routes (~line 349). No
`/notes/{noteId}` block — there is no note id. `s.loadTrip(w, r, db.Role…)`
does the membership check; no new `authz.go` loader is needed.

Response shape, reusing `renderNotesHTML` from
[trips.go:91](internal/httpapi/trips.go#L91) so the tab and the location view
page cannot drift in how they render markdown:

```json
{ "body": "...", "body_html": "<p>…</p>", "updated_at": "..." }
```

Two behaviours worth stating in the handler comments:

- **A missing row is not a 404.** GET on a trip with no note returns 200 with
  `body: ""` — the client then has one shape to render and "no note yet" is
  just an empty string.
- **Saving an empty body deletes the row**, so a cleared note is genuinely
  absent rather than an empty string on disk, and the tab goes back to
  opening in the editor.

**Size cap.** Cap the body at `maxPreviewMarkdownBytes` (64 KiB, already
defined in [markdown.go](internal/httpapi/markdown.go)) — reuse the constant
rather than picking a second number, because a note larger than the preview
endpoint accepts would be a note the editor cannot preview. Note the coupling
in a comment.

**Seed** — give the `full` scenario a short markdown note in
[cmd/seed/main.go](cmd/seed/main.go) so the tab has content to look at and
the screenshot run has something to capture.

**Verify.** `make ci`; `make test-postgres` (this stage touches
`internal/db` and adds an `ON CONFLICT`, exactly the shape Stage 18 found
Postgres rejecting while every SQLite test stayed green). New
`internal/httpapi/trip_notes_test.go`: empty trip returns 200 with an empty
body; PUT then GET round-trips and renders `body_html`; empty PUT clears it;
over-cap PUT is a 400. Add rows to the tables in
[roles_test.go](internal/httpapi/roles_test.go) (GET viewer, PUT editor) and
[ownership_test.go](internal/httpapi/ownership_test.go).

**Done.** Landed as planned, with one deliberate deviation: `UpsertTripNote` is
an update-then-insert pair rather than the `ON CONFLICT` upsert the plan
sketched. That is the idiom `UpsertItineraryDayNotes` already uses in both
stores, and it sidesteps the documented sqlc trap (named args are not
substituted inside `ON CONFLICT ... DO UPDATE`) rather than working around it —
so the queries file needed no `excluded.col` at all. Migration
`0008_trip_notes` in both dialects (SQLite `TEXT`, Postgres `TIMESTAMPTZ` for
`updated_at`, the only line that differs); `db.TripNote` plus three `Store`
methods with adapters in both stores; `internal/httpapi/trip_notes.go` with
`GET`/`PUT /api/trips/{tripId}/notes` registered after the itinerary routes.
`updated_by` is written from the first save but nothing reads it yet — recorded
in `todo.md`, because a column added later could not be backfilled.

Verified: `make ci` green, and `make test-postgres` green (worth the run — the
new query pair is exactly the dialect-parity shape Stage 18 got burned by).
New `trip_notes_test.go` covers the empty-before-first-write 200, the verbatim
source round-trip alongside real rendered HTML, sanitization, clearing back to
empty, last-write-wins, the size cap, and the FK cascade on trip delete; rows
added to the tables in `roles_test.go` (GET viewer, PUT editor) and
`ownership_test.go` (stranger denied, unauthenticated 401). Then live against a
restarted dev server, confirmed as the new binary with
`make dev-restart MARKER=handleSetTripNote`: GET on an unwritten trip gives
`{"body":"","body_html":"","updated_at":null}`; saving
`## Ferry\n\n- book by *May*\n- <script>alert(1)</script>` returns the source
byte-for-byte with `<h2>Ferry</h2>`, an `<em>` and the script tag stripped;
saving `"  "` returns it to empty.

---

## Milestone 2 — The Notes tab

**Icon.** No suitable symbol is in the sprite (`file-text` is Files). Add
`notebook-pen` to `ICONS` in
[scripts/gen_icon_sprite.py](scripts/gen_icon_sprite.py) and regenerate per
CLAUDE.md, diffing to confirm the existing symbols come out byte-identical.

**New page** `web/js/pages/notes-tab.js`, exporting
`renderNotesTab(container, trip)` — the page-module convention
(`(container, trip)`) that itinerary and expenses use, not the component one.
Imported and dispatched from
[trip-detail-page.js](web/js/pages/trip-detail-page.js) with a new arm beside
the itinerary one.

`renderLoading(container)` → `api.get(\`/trips/${trip.id}/notes\`)` → one
closure-scoped `render()` over two modes:

- **View mode** — used when `body` is non-empty. A
  `.trip-notes__rendered` div whose `innerHTML` is `body_html` (trusted: the
  server sanitized it, same justification as
  [location-view-page.js:229](web/js/pages/location-view-page.js#L229) —
  say so in a comment), plus an Edit button, hidden for viewers.
- **Edit mode** — used when `body` is empty and the user can edit, and
  whenever Edit is pressed. A `<textarea>` with the auto-grow handler lifted
  from [location-form.js](web/js/components/location-form.js) (the
  `offsetHeight - clientHeight` border correction matters), a Save button and
  — only when there is already saved text — a Cancel that returns to view
  mode with the unsaved text dropped.

Save goes through `guard()` from [busy.js](web/js/busy.js), PUTs, takes
`body` and `body_html` from the response, and re-renders into view mode.
Failure shows an inline `role="alert"` and leaves the text in the box. A
viewer on a trip with no note sees a muted empty line, no textarea.

Deliberately **not** in scope: the Edit/Preview toggle from the location
form. Here Save *is* the transition to the rendered view, and a second
preview mechanism inside the editor would be a third place that renders
markdown. Worth revisiting once the tab is in use — record it in
`plans/todo.md`.

**CSS** — a `.trip-notes` block in [base.css](web/css/base.css) near the
checklist block (~2724). The rendered container needs the same first-child /
last-child margin reset the `.notes-field__preview` rule already carries,
since goldmark emits real headings and lists.

**i18n** — `trip.tabs.notes` plus a **`tripNotes.*`** namespace in both
`web/locales/en.json` and `de.json`. Not `notes.*`: `location.form.notes*`
and `itinerary.notesPlaceholder` already exist and a bare `notes.` prefix
makes every future grep ambiguous. Keys: `tripNotes.edit`, `.save`,
`.cancel`, `.placeholder`, `.empty`, `.saveFailed`. Labels: "Notes" /
"Notizen".

**Verify.** `make ci` (i18n parity is a gate). Manually against `make dev` at
1280×800 and 324×756: a fresh trip opens in the editor; typing a heading and
a list and saving renders them; reload lands in view mode; Edit shows the
source back; clearing and saving returns to the editor.

**Done.** Landed, and it absorbed the tab reshuffle that the plan had put in
Milestone 3. That was not optional: `app.js` derives the route table from
`TRIP_TABS`, so `/trips/{id}/notes` does not exist as a route until the array
has the entry — the plan's line about the tab being "reachable by URL while
still absent from the bar" was simply wrong about this codebase. Rather than
add a throwaway route and delete it a milestone later, the array change came
here, and with it the three hard-coded lists that assert tab order
(`scenarios.js`, `menu.spec.js`'s `TAB_ORDER` and `OVERFLOW_LABELS`, and the
`Makefile`'s `CONTRAST_ROUTES`), so no milestone ends with a red suite.
Milestone 3 is correspondingly smaller: the notes-specific UI spec, docs and
screenshots.

What landed: `notebook-pen` added to the sprite (diffed — a pure addition, no
upstream restyling of the 38 existing symbols); `web/js/pages/notes-tab.js`
with the two modes; the dispatch arm in `trip-detail-page.js`; a `.trip-notes`
block in `base.css`; nine `tripNotes.*` keys in both locales. Three
course-corrections while building, all toward reusing what exists rather than
adding beside it: the save uses `guardForm` rather than a hand-rolled listener,
because `guard()` applies `preventDefault` *after* the busy check and would let
the dropped half of a double-tap submit the form for real; the error paragraph
joined the shared `.item-form__error, …` rule instead of getting a private one
(`--color-danger` is a background token, not a text colour); and Save/Cancel
reuse the existing `common.save`/`common.cancel` rather than adding
near-duplicate keys.

The reshuffle turned out to *relieve* the phone row rather than strain it.
Measured at 324px in German, the longest remaining row label is "Reiseplan" at
49.9px in a 58.4px cell, where the departing "Checklisten" was 59px — wider
than its own cell, which is what the `hyphens: auto` rule was there to absorb.
The comment at that grid rule quoted the old number and has been corrected;
the hyphenation stays, since nothing guarantees the new slack lasts.

Verified: `make ci` green; the full `make test-ui` suite green (226 passed),
which is what actually pins the reshuffle — `menu.spec.js` asserts the phone
row plus More menu reads in the desktop order in both locales, and
`routes.spec.js` now sweeps `/notes` for overlap, tap targets and field sizes
at every viewport and scheme. `make check-contrast` green with the new route
(702 elements). Then driven by hand in Firefox at 1280×800 and 324×756: an
unwritten trip opens in the editor with Save and no Cancel; saving
`## Ferry / Book by *May*. / - passport / - paper licence / [road](…)` renders
`H2, P, UL, P` with a real `<em>` and a real link and switches to the read
view; reload lands in the read view; Edit returns the source verbatim and
focuses the textarea, auto-grown to 272px; typing then Cancel discards the
draft and keeps the saved note; clearing to whitespace and saving deletes the
note and drops back to the editor, with the API confirming
`{"body":"","updated_at":null}`. The phone row measured 5 equal 58.4px cells
with no horizontal overflow, and the More menu reads Checklisten, Dateien,
Ausgaben, Mitreisende, Einstellungen. A viewer was checked against the
component directly, in both states: rendered note, no textarea, no buttons at
all; and on an empty trip the muted empty line rather than an editor.

---

## Milestone 3 — The notes UI spec, docs, screenshots

**Scope note.** The tab reshuffle and the three order-asserting lists moved
into Milestone 2 (see its Done paragraph) because the route table derives from
`TRIP_TABS`. What remains here is everything specific to the notes tab that
the generic sweeps cannot cover.

**New** `tests/ui/notes.spec.js`, assertion-led rather than screenshot-led,
pinning what was verified by hand in Milestone 2 so it cannot regress quietly:
a trip with no note shows a `textarea` and no Edit button; saving
`## Heading\n\n- one\n- two` produces a real `h2` and two `li` in
`.trip-notes__rendered`; a reload still shows them; Edit puts the original
source back in the textarea; Cancel discards an unsaved draft; clearing the
note returns to the editor; and a viewer sees the rendered note with no Edit
button. Check whether `sharing.spec.js` enumerates what a viewer is not
offered and add Notes if so.

**Docs** — `docs/features/itinerary-and-lists.md` gains a Notes section; check
`zensical.toml`'s nav if a new page is warranted instead. Run `make docs`
(`--strict` catches dead links).

**Screenshots** — `docs/assets/screenshots/` is committed and several shots
show the tab bar, which now reads differently. Check which are actually stale
before regenerating; `make screenshots` needs `scrollTo` for tab captures.

**Verify.** `make ci`, `make test-ui`, `make docs`.

**Done.** Landed as scoped after Milestone 2 absorbed the reshuffle.

`tests/ui/notes.spec.js` — four tests, owning their own trip like
`checklists.spec.js` does, because the seeded `full` trip's note is read by the
screenshot run. They cover the mode rule transition by transition (empty opens
the editor with no Cancel; saving renders `h2`/`li`/`em` and switches to the
read view; reload lands in the read view; Edit returns the source verbatim;
Cancel discards a draft without resurrecting it; clearing to whitespace returns
to the fresh-trip state) plus a viewer who reads the note and is offered no
editor. `sharing.spec.js` gained the same viewer check in its per-tab sweep,
including the promoted-to-editor half — and its fixture now writes a note for
the reason its own comment already gives about locations and checklists: on a
trip with *no* note everyone gets the editor, so an empty note would assert
nothing.

The specs were mutation-checked rather than merely observed passing: forcing
`editing = false` in `notes-tab.js` fails exactly the two tests that own the
mode rule, and the file was restored from a pre-mutation copy and diffed clean.

Docs: the page is retitled "The itinerary, notes and lists" (nav label
follows), with a Notes section between the itinerary and files. The home
page's feature card needed no change — its copy is thematic and never named the
page title.

Screenshots regenerated, and this was not optional: every trip-tab capture
shows the tab bar, which now reads differently. `notes.png` was added to
`gen_screenshots.mjs` after the itinerary shot. The seeded note needed one fix
found only by looking at the result — it had `--` where an em dash belonged,
which is a habit from the SQL-comment rule in CLAUDE.md that does not apply to
a Go string, and it was rendering literally on the project website.

Verified: `make ci` green (16 screenshots, all shown by a page), `make test-ui`
green at 230 passed — up from 226, the four new tests — and `make docs` green
under `--strict`.

One gap found and recorded in `todo.md`: `--strict` does **not** catch a
missing image. `make docs` passed cleanly while the new page referenced
`notes.png` before it existed, and a deliberate reference to a made-up filename
also exits 0. Only `check_screenshots.py` looks at these, and it checks the
other direction — that every committed file is used, not that every reference
resolves.

## Build order

1. Milestone 1 — schema through API, backend-only, independently testable
   with `curl`/`go test` before any UI exists.
2. Milestone 2 — the tab, plus the reshuffle that gives it a route at all and
   the lists that assert tab order, in one commit so the app and its tests
   never disagree. (The plan originally split these; see the Done paragraph
   for why they cannot be.)
3. Milestone 3 — the notes-specific UI spec, docs and screenshots.

## Workflow

Per CLAUDE.md, for each milestone in order: implement → verify (`make ci`
green plus real evidence the behaviour changed) → add a **Done.** paragraph
to this plan and reconcile `plans/todo.md` in both directions → one commit
saying what changed, why, and how it was verified → make sure `make dev` is
up → stop and hand back control. Do not start the next milestone until told
to continue.

## Verification (whole stage)

- `make ci` and `make test-postgres` green.
- `make test-ui` green, including the updated `menu.spec.js` order
  assertions at both viewports and in both locales.
- `make check-contrast` green with the new route.
- `make docs` green.
- By hand at 324×756: the phone row reads Locations · Map · Itinerary ·
  Notes · More, with Checklists first in the More menu, and the desktop bar
  reads in the same relative order.

## How complex is this, really

**Moderate — no new concepts, but a wide diff.** Around 20 files. The
feature itself is small: markdown rendering, sanitization, the size cap and
the edit/preview idiom all already exist and are reused. What makes it a
stage rather than a commit is breadth — a migration in two dialects, `sqlc`
regeneration, two hand-written store implementations, two locale files, and
a tab-order change that four separate hard-coded lists assert against.

The two places time will actually go: `sqlc` on the `ON CONFLICT` upsert
(the named-arg trap, and a Postgres-only failure that SQLite tests will not
catch — hence `make test-postgres`), and the screenshot regeneration in
Milestone 3, which the tab reshuffle probably forces.
