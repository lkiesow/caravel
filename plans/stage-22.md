# Stage 22 — Planning, day to day

## Context

Stages 19–21 went outward: the UI suite got a throwaway server, every mutating
control got a shared busy guard, and the assistant got logging, a run trace,
speed measurements and cover images. All useful, none of it about the thing the
app is for. Meanwhile the trip-planning loop itself still has gaps that a
person planning a real trip hits within a week:

- **An itinerary entry cannot be moved to another day.** Rescheduling means
  removing it from one day and adding it again on the other, *losing the note on
  the entry* in the process. `internal/httpapi/itinerary.go` has create, reorder
  and delete and nothing that reassigns `itinerary_day_id`; there is no client
  affordance either.
- **An expense cannot name the location it was for.** Stage 17 shipped the
  ledger and deliberately left out the one nullable column the original Stage 01
  sketch had. Looking at a row and asking "what was this, exactly" should be one
  click away from the location's picture and notes; today it is a memory test.
- **Coordinates are a one-way street.** `/api/geocode` turns a name into a
  point; clicking the map gives you a point and no address, and pasting the
  Google Maps link somebody sent you gives you nothing at all.
- **Two writes lie about their outcome.** A failed checklist tick leaves the box
  showing the click's state while the server holds the other one, saying
  nothing; a second reorder press while the first is in flight does nothing at
  all, silently.

This stage closes those four, and takes one backlog cleanup that this stage's
own work forces: `/auth/me` gains a fourth server-capability flag, which the
backlog already said was over the threshold at three.

Deliberately **not** in this stage: the trip journal (a stage of its own), the
mobile map height and the `base.css` input-rule consolidation (a UI-polish
stage of its own), trip-level AI suggestions, share links.

### What exploring the code changed about the plan

- **The move can be atomic, and it should take a *date*, not a day id.**
  `UpsertItineraryDayNotes` (`itinerary.go:137-165`) is already the only way an
  `itinerary_days` row comes into being, and the frontend already calls it with
  `notes=null` purely to materialise a day before adding an entry. So a move
  handler taking `{"to_date": "YYYY-MM-DD"}` can upsert the target day *inside*
  the same transaction. Taking a `day_id` instead would force the client to
  create the day first, in a second request that can succeed while the move
  fails — exactly the non-atomic shape Stage 09 spent two milestones removing
  from location creation.
- **`handleReorderItineraryEntries` is the pattern to copy, including its
  paranoia.** It validates the whole set before writing, renumbers from 0 inside
  `WithTx`, and answers 409 (`errItineraryEntryVanished`) if a row moves under
  it mid-transaction. A move is that shape across two days; renumbering *both*
  days is what keeps a day gap-free after an entry leaves it.
- **Migrations are at `0002`, not `0001`.** `0002_media_provenance.{up,down}.sql`
  landed in Stage 21 for both dialects, so the expense column is `0003`.
  `CLAUDE.md` still says the next one is `0002` — fix that line in Milestone 0.
- **The reverse endpoint has to be derived, and the derivation can fail
  honestly.** `config.GeocoderURL` defaults to
  `https://nominatim.openstreetmap.org/search` — a *search* URL. Nominatim's
  reverse endpoint is the sibling `/reverse`. Rather than add a second env var
  nobody wants to set, derive it by swapping a trailing `/search` segment, and
  when the configured URL does not end in `/search`, report reverse geocoding as
  *unavailable* rather than guessing at a URL. That makes the capability flag
  mean something.
- **There is already a hardened outbound fetcher, and it is unexported in the
  wrong package.** `internal/assist/fetch.go` refuses any non-public address at
  dial time *and* re-checks after every redirect (`guardIP`, `guardURL`,
  `checkDialAddress`, `CheckRedirect`) — precisely what following a
  `maps.app.goo.gl` redirect needs. Duplicating ~120 lines of SSRF guard for the
  link resolver would be the wrong kind of cheap, so Milestone 6 lifts the guard
  into `internal/safefetch` and leaves the HTML extraction in `assist`.
- **The pasted link needs no new control.** The location editor's address search
  (`location-editor-page.js:528-597`) already searches on Enter or on the
  button, never per keystroke. A pasted Google URL in that same field is
  recognisable on sight, so the existing button can resolve it as a link instead
  of sending it to Nominatim as a query.
- **Reverse geocoding must stay user-initiated.** The same field's
  search-on-Enter rule exists because every query costs a volunteer-run service
  a request. Firing a reverse lookup on every map click and marker drag would
  break that rule quietly, so the address arrives behind a "Look up address"
  button beside the coordinate hint, and it is *offered*, never auto-filled —
  the same decision the existing result handler already makes for
  `display_name`.
- **The failed-tick fix has a model in the repository.** The admin page's
  open-signup toggle puts the checkbox back and prints a message.
  `checklist-list.js:256-270` has the guard but no `try/catch`.

---

## 0. Land the plan, and reconcile the backlog

Commit this file as `plans/stage-22.md`. In the same commit, remove from
`plans/todo.md` nothing at all — entries come out as the milestone that
implements them lands — but do fix the two things exploring found stale:
`CLAUDE.md`'s "the next schema change is `0002_...`" (it is `0003`), and the
assistant-slowness entry, which still reads as untouched though Stage 21
Milestone 4 measured it and Milestone 4a landed a change; rewrite it to what is
actually left.

**Verify:** `make ci`. No behaviour change.

---

## 1. Move an itinerary entry to another day — the API

New endpoint, on the existing entry path so the addressing stays consistent:

```
PATCH /api/itinerary/days/{dayId}/entries/{entryId}
{ "to_date": "YYYY-MM-DD" }
```

The path names the *source* day (which is what authorizes the call, through the
existing `loadItineraryDay(w, r, db.RoleEditor)`); the body names the target
date. Response: the entry's new day (`{ "day_id": ..., "date": ..., "sort_order": n }`),
so the client never has to guess where it landed.

Files: `internal/httpapi/itinerary.go`, `internal/httpapi/router.go` (one line
in the `/itinerary/days/{dayId}` route block),
`internal/db/sqlc/queries/itinerary_entries.sql` (one
`SetItineraryEntryDay :execrows` — update `itinerary_day_id` and `sort_order`
`WHERE id = ? AND itinerary_day_id = ?`, the source-day predicate being the same
belt `DeleteItineraryEntry` wears), then `sqlc generate` by hand in
`internal/db/sqlc/` **for both dialects**.

Inside one `WithTx`:

1. Validate `to_date` parses as `2006-01-02`; reject otherwise (400).
2. If it equals the source day's date, answer 200 with the entry unchanged —
   a no-op move is not an error.
3. Upsert the target day via the existing `UpsertItineraryDayNotes` (id
   `uuid.NewString()`, `notes` nil) so a move to an untouched day works.
   **Careful:** it must not clear notes on a day that already has them —
   check the generated `ON CONFLICT` clause actually preserves `notes` when
   passed nil, and if it does not, add a plain `GetItineraryDayByTripAndDate`
   lookup first and only upsert when absent.
4. Append: `sort_order` = one past the target day's current count.
5. Renumber both days from 0, exactly as `handleReorderItineraryEntries` does,
   so the source day is left gap-free.
6. A zero-row update means the entry moved under us: roll back and answer 409,
   reusing `errItineraryEntryVanished`.

Tests in `internal/httpapi/itinerary_test.go` (or a new `itinerary_move_test.go`
beside `itinerary_order_test.go`): a move to an existing day, a move to a day
with no row yet, a move that keeps the entry's `note`, both days renumbered
contiguously, a viewer getting 403, a bad date getting 400, an entry id from
another day getting 404, and a same-day move being a no-op.

**Verify:** `make ci`, plus `make test-postgres` — this touches
`internal/db` queries, which is exactly the case CLAUDE.md says to run both
dialects for.

**Done.** `PATCH /api/itinerary/days/{dayId}/entries/{entryId}` with
`{"to_date": "YYYY-MM-DD"}`, as planned, answering
`{day_id, date, sort_order}`.

**The `UpsertItineraryDayNotes` worry the plan flagged was real**, and worse
than "check the ON CONFLICT clause" suggested: there is no `ON CONFLICT` at
all. The method is an update-then-insert pair in each store
(`sqlite_store.go:661`, `postgres_store.go:833`), and passing nil notes takes
the *update* branch on a day that already exists — setting `notes` to NULL.
Using it here would have silently wiped the target day's notes on every move
onto a day that had any. So the store gained `EnsureItineraryDay(ctx, newID,
tripID, date)` instead: get by (trip, date), insert with no notes only if
absent, never touch an existing row. `TestMoveItineraryEntryLeavesTargetDayNotesAlone`
pins it in both directions.

Also built: `SetItineraryEntryDay` (one `:execrows` query, both dialects
regenerated and read rather than diffed — all four args substituted), and
`renumberItineraryDay`, a helper the move needs twice. It compacts a stored
order rather than applying a supplied one, which is why
`handleReorderItineraryEntries` keeps its own loop instead of calling it — the
two are doing different jobs and merging them would have meant a flag.

Deviations from the plan, both small: the entry's membership of the source day
is checked *before* the transaction rather than discovered inside it, so a
wrong pairing is a plain 404 instead of a rolled-back conflict — and it gives
the same-day no-op branch a `sort_order` to answer with. And the no-op branch
answers before opening a transaction at all.

**Verified.** `make ci` green, and `make test-postgres` green (123s for
`internal/httpapi`) — which is what exercises the `int32`/`int64` split and the
Postgres `time.Parse(dateLayout)` path in `EnsureItineraryDay`. Seven new tests
in `itinerary_move_test.go`: note and entry id survive the move, both days come
back numbered 0..n-1, a date with no row yet is a valid destination, day notes
on both ends are untouched, a same-day move is a 200 no-op that writes nothing,
five bad inputs are rejected without moving anything, and a cross-trip attempt
is a 404. The role matrix (`roles_test.go`) gained the route and an `entryID`
fixture, so viewer-403 and stranger-404 come from the existing sweep.

**The renumber assertion was mutation-checked** rather than trusted: removing
the source-day renumber makes the day come back as `[0 2]` and the test fails
on it. Worth doing, because a test that reads the order through
`GET /itinerary` would pass on a gapped day if it only compared titles.

---

## 2. The move, in the browser

`web/js/pages/itinerary-tab.js`. The entry row's actions are currently three
icon buttons (`move-up`, `move-down`, `remove`); a fourth icon would be the
pile-up the checklist row and the file row both already solved with a `⋮`. So:
keep up/down (they are the common, one-tap action) and put **Move to another
day** and **Remove** behind a `renderMenu` trigger, matching
`checklist-list.js:276-290`.

Selecting it opens a dialog (`components/dialog.js`) with a `<select>` of the
itinerary's days — the dates the tab already holds, formatted with the tab's
own `formatDate`, current day excluded — and a confirm button. On confirm:
`api.patch(\`/itinerary/days/${day.id}/entries/${entry.id}\`, { to_date })`,
then re-read the itinerary and re-render. **Not optimistic**: two days change
and the target day may not have existed, so the server's answer is cheaper to
trust than to reconstruct.

New i18n keys in **both** `web/locales/en.json` and `de.json`
(`itinerary.moveToDay`, `itinerary.moveDialogTitle`, `itinerary.moveDayLabel`,
`itinerary.moveFailed`, …).

**Verify:** `make ci` (i18n parity is a gate). Extend
`tests/ui/itinerary-order.spec.js`: move an entry with a note from one day to
another, assert it is gone from the source list, present on the target with its
note intact, and that a reload shows the same. Then a manual pass at 324×756
against `make dev` — the select and dialog have to be usable at phone width and
carry the tap-target minimum.

**Done.** As planned: up and down stay as icon buttons, Remove and **Move to
another day** sit behind a `⋮` menu, and the move opens a dialog listing the
itinerary's other days. Not optimistic — the itinerary is re-read and
re-rendered, and the target day is added to `openDates` so the entry is visible
where it landed.

**`dialog.js` gained `selectDialog`** rather than the tab hand-rolling a modal.
It is `promptDialog` with a `<select>` in place of the input, resolving to the
chosen value or to null when dismissed — the same answer-or-null contract, so
"chose the first option" stays distinguishable from "changed their mind". A
select rather than a row of buttons because a fortnight of days is fourteen
buttons on a 324px phone, where a native select opens the picker people already
know.

**One real regression was introduced and caught by driving the page**, not by
any test: putting a menu in each row nests a second `<ul>` inside every entry
`<li>`, so `.itinerary-day__entries li:nth-child(2)` began matching the
*Remove* item inside row 1 before reaching row 2. That selector is the reorder's
focus restoration, so moving an entry would have put focus inside a closed
popup. Both the component and the spec now use the `> li` child combinator, with
a comment saying why. The same nesting broke four existing assertions in
`itinerary-order.spec.js` that counted rows as `.itinerary-day__entries li` —
all four failed loudly, which is the outcome to want.

Also updated: the spec's tap-target test now expects `move-up, move-down,
toggle` as the row's three controls and probes only the row's own buttons
(the dropdown's live inside the same `<li>`), removal goes through a new
`entryMenu()` helper, and the note at the head of the second describe saying
"there is no way to move an entry to another day" is no longer true and no
longer there.

**Verified.** `make ci` green (375 keys in sync across both locales); the full
UI suite green at **140 passed**, which is what proves the menu did not disturb
the a11y-name, heading and tap-target sweeps. Two new specs cover the move
end to end (note preserved, survives a reload) and a cancelled dialog writing
nothing. Driven by hand at 324×756: the trigger measures 44×44, the dialog is
260px wide inside a 324px viewport with no overflow, focus lands on the select,
the current day is absent from the options and the next day is preselected, a
one-day trip offers no move item at all, and the German pass reads correctly
throughout.

One thing seen and deliberately not fixed here: the day labels in the dialog
render as "Thu, 3 Sept 2026" even in German, because this tab's `formatDate`
calls `Intl` with an undefined locale. That is the existing browser-locale
decision `todo.md` already records for `format.js`, identical to the day
headings right beside it — a stage-level decision, not something to settle
inside a milestone about moving entries.

**Follow-up: the reorder buttons move into the menu on a phone.** Feedback at
the checkpoint, from a Galaxy Fold at 324px: up, down and the menu took 132px
of a 258px row, so a location with a thumbnail showed "Flight to K…" and the
title — the half anybody reads — lost to three controls, the rarest of which is
reordering.

Both reorder controls now exist **twice**: as buttons in the row and as items in
the menu, with CSS choosing which set a width shows. That is the trip tab bar's
arrangement for its "More" menu, copied deliberately
(`trip-detail-page.js:99-103` says why): both sets exist at every width, so
there is no resize listener, nothing to re-render on rotation, and no second
place that could disagree with the stylesheet about the breakpoint. It matters
more than usual on the device that raised it — a Fold changes width while the
page is open, and resizing across 640px flips the arrangement with no reload.

`renderMenu` gained a per-item `disabled` flag for the ends of the list, so the
menu keeps a stable shape between openings rather than growing and shrinking by
a row. The menu labels are their own keys ("Move earlier", not "Move Dinner
earlier"): the row's icon buttons name the entry because an icon in a list needs
disambiguating, and inside a menu that already belongs to one entry the name is
just the action.

Focus restoration had to learn the same lesson, and does it by asking the DOM
rather than `matchMedia`: it takes the first of same-direction button,
other-direction button, menu trigger that is enabled **and** has an
`offsetParent`, so a `display: none` control is skipped instead of silently
focusing nothing.

**Verified.** `make ci` green (377 keys), full UI suite green at **141 passed**.
The mobile reorder spec now drives the menu and asserts the ends are disabled
*there*; a new spec resizes to 1024 mid-test and asserts the buttons come back
into the row while the menu drops its copies, with no reload. The old
"three 44px controls" geometry test became "leaves the title room to be read at
324px" — it asserts the visible controls are exactly the menu trigger, that the
control column takes under a quarter of the row (it took over half before), and
that a title is not truncated. By hand at 324px the actions column measures
44px against the previous 132px, and "Foss Hotel Reykjavik" now renders in full.

---

## 3. An expense can name a location

The link is **one-directional and optional**: an expense may point at a
location, the row shows its name, and clicking it lands on the location page
with its picture and notes. There is deliberately no per-location cost and no
spending card on the location view — the question this answers is "what was
this expense", not "what did this place cost", and inventing a second aggregate
beside the trip total is work nobody has asked for.

Schema: `0003_expense_item.{up,down}.sql` for **both**
`internal/db/migrations/sqlite/` and `.../postgres/` — one nullable
`item_id TEXT REFERENCES items(id) ON DELETE SET NULL` on `expenses`, plus an
index on it (the location view queries by it). `SET NULL` rather than `CASCADE`:
deleting a location must not delete money from the ledger.

Queries (`internal/db/sqlc/queries/expenses.sql`): `item_id` into
`CreateExpense` and `UpdateExpense`; `SELECT *` carries it into the rest for
free. Then `sqlc generate` by hand, and **read** the generated files rather than
only diffing them (CLAUDE.md's warning about an unsubstituted `sqlc.arg`
compiling and failing at runtime). Keep the new comment prose plain — no
backticks, no quotes, no apostrophes.

API (`internal/httpapi/expenses.go`): `item_id` on `expenseResponse` and on the
request struct, nullable, validated to be an item **on this trip** (an id from
another trip is a 400, the same reasoning as the `trip_id` predicate on
`UpdateExpense`). Add `item_title` to the response alongside it, for the same
reason `payer_display_name` is there: the expenses tab does not load the items
list today and should not have to, and a location deleted after the fact leaves
the expense with a null `item_id` rather than a name the client cannot resolve.

Client (`web/js/pages/expenses-tab.js`): an optional `<select>` in the expense
form, first option empty and meaning "not tied to a location", populated from
`api.get(\`/trips/${trip.id}/items\`)` — loaded the way `members` already is,
in a `try/catch` that degrades to no select rather than failing the tab. The row
shows the location name as a real `<a href>` with `data-link` (the router
intercepts those, so navigation stays client-side while middle-click and
open-in-new-tab keep working — the same reasoning `itinerary-tab.js:252-258`
already spells out for the entry link). Read-only mode keeps the link and loses
the select.

One new i18n key per locale for the field label, plus whatever the empty option
is called.

**Verify:** `make ci` **and** `make test-postgres`. Go tests: create with an
`item_id`, patch one on and off, an id from another trip rejected, and deleting
the location leaving the expense with a null `item_id` and its amount intact.
Extend `tests/ui/expenses.spec.js`: set a location through the form, then follow
the row's link and assert `window.location.pathname` is the location page.

**Done.** Migration `0003_expense_item` for both dialects — one nullable
`item_id` with `ON DELETE SET NULL` and an index — plus `item_id` on
`CreateExpense`/`UpdateExpense`, a select in the form, and the location as a
link on the row. `check_migrations.py` confirms three pairs per dialect, both
agreeing.

**`payerNamer` became `expenseNamer`.** Resolving the location title is the same
job it already did for the payer — an id in a row, a name in the response,
cached per request so twenty rows naming two locations cost two lookups — and
giving it a second cache under a name that said "payer" would have been the
worse of the two options. Ten call sites, mechanical.

Validation went into `requireTripItem`, beside `requireTripMember` which does
exactly this for the payer, and it delegates to the existing
`requireSameTrip` (`authz.go:290`) rather than open-coding the comparison. A
location from another trip and a nonexistent id are both 400s naming the field,
which is what `requireSameTrip` already answers for a media asset.

Deviation from the plan, on a detail the plan got slightly wrong: the client
sends `item_id` **explicitly as null** when the select is empty rather than
omitting it. Omitting works — the server reads absent as none — but on a PATCH
that silently clears an existing link, and saying so out loud is worth two
characters.

**Verified.** `make ci` green (379 keys in sync); `make test-postgres` green
(118s for `internal/httpapi`), which is the run that matters for a schema and
query change. Five new Go tests in `expense_item_test.go`: create with a
location, create without one both ways (absent and explicit null), PATCH setting
and clearing it, a location from another trip and a nonexistent id both refused
on create *and* update with nothing written, and — the load-bearing one —
deleting the location leaving the expense with its 24000 intact and its link
cleared. A `CASCADE` typed by mistake would fail that test rather than quietly
changing what a trip cost.

Full UI suite green at **144 passed**, with three new specs: setting a location
from the form and following the row's link to the location page, clearing it
again from the same select (total unchanged, `item_id` null), and the expense
surviving the location's deletion. By hand at 324px: the row fits without
scrolling, the link reads as a link in accent colour, the German pass gives
"Ort (optional)" / "Kein Ort" with no overflow.

One thing worth recording because it cost time and was *not* a bug: a first
attempt to drive the form through the MCP tools appeared not to persist the
link, and capturing the request showed the client sending `item_id` correctly
all along. Repeating the same steps as ordinary UI clicks worked, and the
committed spec passes. It was harness noise between a scripted `selectOption`
and the following click, not a race in the page — nothing re-renders that form
between opening it and submitting it.

---

## 4. `/auth/me` grows a `capabilities` object

Backlog item, forced by this stage: Milestone 5 adds a **fourth** server flag
beside `geocoding`, `assist` and `image_search`, and the comment on the third
one already said three was over the threshold.

`internal/httpapi/auth.go`: nest them as
`"capabilities": { "geocoding": …, "assist": …, "image_search": …, "reverse_geocoding": … }`.
`web/js/session.js` and every reader of the three flat fields move with it —
`getCurrentUser()?.geocoding` at `location-editor-page.js:530` is one; grep for
the other call sites rather than trusting this list.

Its own commit, no feature riding along, which is the whole reason it is a
milestone and not a line in Milestone 5.

**Verify:** `make ci`. The UI suite is the real gate here: any missed reader
silently hides a control, and `assist.spec.js`, `image-search.spec.js` and
`locations.spec.js` each depend on one of these flags being read correctly.

**Done.** `/auth/me` now answers
`{id, username, display_name, has_password, capabilities: {geocoding, assist,
image_search}, is_admin}`. `is_admin` deliberately stayed a top-level user
field: unlike the other three it genuinely is a property of the account.

**Only three flags, not the four the plan wrote.** `reverse_geocoding` belongs
to Milestone 5 and does not exist yet; adding a flag for a capability the server
has no code for would be a lie the client could read. The reshape is the whole
commit, which is what this milestone was for.

**The client got `hasCapability(name)` rather than three call sites reaching
through two levels of optional chaining.** `web/js/session.js` is now the only
place that knows the payload's shape — which is exactly the lesson of the flat
version it replaced — and it answers false when the user is not loaded yet,
since a control that needs a capability should not render before the app knows
whether it exists. The three readers (`image-field.js`, `assist-panel.js`,
`location-editor-page.js`) each dropped their `getCurrentUser` import entirely.

**A missed reader was caught by a test, which is the outcome to want.** There
were *two* helpers in the Go suite asking `/auth/me` the same question, and
`assistCapability` in `assist_test.go` was still reading the flat field —
reporting the capability as absent while type-checking perfectly.
`TestAuthMeReportsAssistCapability` failed on it. It is now deleted and both
call sites go through the shared `ts.capability(cookie, name)`, so the tests
have one reader too.

**Verified.** `make ci` green. Full UI suite green at **144 passed**, and the
capability-gated specs were confirmed to have actually *run* rather than
skipped — `assist.spec.js` (4 tests) and `image-search.spec.js` (2) all
executed, including the one that fakes `capabilities.assist` off through a
route interception and asserts the panel disappears. That interception needed
its own fix: a shallow spread of the payload carries the original capabilities
through untouched, so the nested object has to be spread on its own. By hand
against `make dev`: the payload has no flat flags left, and with
`geocoding: true`, `assist: false`, `image_search: true` the address search and
image search controls render while the assist slot stays hidden.

No `make test-postgres` for this one: it touches no query and no schema.

---

## 5. Reverse geocoding: a point becomes an address you accept

`internal/geocode/geocode.go` gains `Reverse(ctx, lat, lng) (Result, error)`,
calling the derived `/reverse` endpoint with `format=jsonv2` and the same
`Timeout`, `User-Agent` and response mapping as `Search`. Derivation lives here
too — swap a trailing `/search` path segment for `/reverse` — with a
`ReverseAvailable()` (or a nil-ish equivalent) that is false when the configured
URL does not end in `/search`, so the capability flag added in Milestone 4 tells
the truth on a non-Nominatim endpoint.

`internal/httpapi/geocode.go`: `GET /api/geocode/reverse?lat=&lng=`, behind the
same `auth.RequireAuth` and the same `s.rateLimitGeocode` as
`/geocode` (`router.go:268`), with the same 501 / 400 / 502 shape the existing
handler establishes — an out-of-range lat/lng is a 400.

Client (`web/js/pages/location-editor-page.js`): a **Look up address** button
beside the coordinate hint, enabled only when both coordinates are set and only
when the capability is on. It offers the result as a line with an Accept button
— reusing the `.location-search__result` styling — and never writes the
`address` field without a press. Wire it through the existing `createGuard`
(`web/js/busy.js`) rather than a local `disabled` flag.

New i18n keys in both locales.

**Verify:** `make ci`. A Go test with an `httptest` upstream covering the
derivation, the mapping, a non-`/search` URL reporting unavailable, and the
502 path. A Playwright assertion in `locations.spec.js` — noting that the UI
suite currently reaches the *real* Nominatim (`todo.md`, "The UI suite reaches
the real Nominatim"); do **not** widen that dependency. Point
`CARAVEL_GEOCODER_URL` at a stub for this assertion, or if that turns out to
want the stub-geocoder work the backlog describes, keep the assertion to the
control's presence and enabled/disabled state and record the gap.

**Done.** `Reverse(ctx, lat, lng)`, `ReverseURL()` and `ReverseAvailable()` in
`internal/geocode`; `GET /api/geocode/reverse?lat=&lng=` behind the same auth
and the same limiter as the forward direction; a **Look up address** button in
the location editor that offers the answer with an Accept button and never
writes the field on its own; and `capabilities.reverse_geocoding` on
`/auth/me`, which is the fourth flag Milestone 4 reshaped for.

The derivation works as planned — swap a trailing `/search` for `/reverse`, and
report unavailable rather than guess when the URL does not end that way. Three
things came out differently or needed more care than the plan said:

- **`Search` and `Reverse` share one `get` helper.** The timeout, the
  identifying User-Agent and the non-200 handling are conditions of using the
  service rather than properties of an endpoint, and two copies of them was how
  one would eventually omit the User-Agent — the thing that gets anonymous
  traffic blocked. The body is read through an `io.LimitReader` while we are
  there.
- **`Reverse` returns the *queried* coordinates, not the ones upstream echoes
  back.** Nominatim answers with the location of whatever it matched, which for
  a click in a car park is the building next door. The caller asked about a
  point they chose; only the address is news. Both the package test and the
  handler test pin this, because a payload that invited the client to move the
  marker would be a bug nobody would notice until they looked at the map.
- **A miss is a 404, not an empty 200.** Nominatim answers a lookup in the
  middle of an ocean with `200 {"error":...}`, which decodes cleanly into a
  zero-valued struct — so without an emptiness check that reads as a successful
  lookup of a nameless place. `ErrNoResult` distinguishes it, and the client
  says "No address found for this point" rather than offering a blank.

Two smaller decisions in the client: the button is **disabled** rather than
hidden without coordinates, so it reads as something that will work once there
is a point; and any change to the coordinates **drops a pending offer**, because
an address for the old point is worse than none — accepting it after moving the
pin would file the wrong one. Coordinates set by the map fire no `input` event,
so the picker's `location-picked` and `position-found` are wired to the same
clearing.

**A stale-token trap in `base.css` was avoided by reading the file.** The offer
panel first said `background: var(--color-surface-muted, var(--color-bg))`, and
there is a comment forty lines below explaining that a previous rule did exactly
that, that `--color-surface-muted` exists nowhere, and that a var() always
falling through to its fallback is the case `tests/ui/contrast.js` warns gives
meaningless readings. It uses `--color-bg` directly.

**`stubGeocoder` in the Go tests now configures `.../search`.** It pointed at a
bare host, which after this change is a URL no reverse endpoint can be derived
from — so every reverse test would have exercised the "cannot derive one" path
while looking like it tested a lookup.

**Verified.** `make ci` green (384 keys in sync). Eleven new Go tests: seven
derivation cases including three honest refusals, the queried-coordinates
contract, the User-Agent, `ErrNoResult` from both shapes of empty answer, the
upstream 503, and at the HTTP layer 501 (no geocoder, *and* a geocoder whose URL
has no derivable reverse while forward search still works), 404, 502, 401, and
eleven rejected coordinate pairs — with an assertion that a refused coordinate
never reached the upstream at all. A final test asserts the capability flag and
the endpoint agree, since a client that trusts `/auth/me` and finds the control
fails anyway is worse off than one with no control.

Full UI suite green at **148 passed**, four new specs. **They intercept
Caravel's own `/api/geocode/reverse` rather than letting it through**: the plan
said not to widen the real-Nominatim dependency, and `with_server.sh` leaves
`CARAVEL_GEOCODER_URL` at its default. So the client is driven end to end
against a canned answer — offer, no-write-until-accepted, accept, save, read
back through the API — while the server half stays with the Go tests. That is
better than the plan's fallback of asserting only the control's presence, and it
needed no stub-geocoder plumbing. The specs also cover the stale-offer drop, the
404 and 502 messages, and the control being absent when the capability is off.

By hand at 324×756 against `make dev` (one real Nominatim call, deliberately):
the button is 44px tall and disabled until there are coordinates, a genuine
Reykjavík address arrives, the field stays empty until Accept, focus moves to
Accept and then to the address field, a long address wraps inside the panel with
no page overflow, and the German pass reads "Adresse ermitteln" / "Diese Adresse
übernehmen".

---

## 6. A pasted Google Maps link becomes coordinates

Two parts, the first being the reason this is its own milestone.

**6a. `internal/safefetch`.** Lift the address policy and the two guards out of
`internal/assist/fetch.go` — `addressPolicy`, `guardScheme`, `guardURL`,
`guardIP`, `isUniqueLocal`, `checkDialAddress`, the `dialer()` and the
`CheckRedirect` hook — into a new `internal/safefetch` package exporting a
policy type and a constructor for a guarded `*http.Client`. `assist` keeps
`pageFetcher`, the size and time caps and all the HTML extraction, and imports
the guard. Mechanical but security-critical: the existing tests in
`internal/assist` that exercise the refusals move with the code, and none of
them should need rewriting beyond the package name.

**6b. The resolver.** `internal/geocode` (beside `Reverse`, same limiter, same
proxy reasoning) gains `ResolveMapLink(ctx, rawURL)`:

- Host allowlist, checked *before* anything leaves the building:
  `maps.app.goo.gl`, `goo.gl`, `maps.google.<tld>`, `www.google.<tld>/maps`.
  Anything else is refused without a request.
- A guarded client from 6a, `CheckRedirect` capped at 5 hops, and the response
  body ignored — the coordinates are in the expanded **URL**.
- Extraction, in order: `@lat,lng,zoom` in the path, the `!3d…!4d…` pair in the
  `data=` blob, then `q=`, `ll=`, `center=`. Return the first that parses to a
  valid lat/lng, with whatever place name the URL carries as an optional label.
- No match is a distinct error, so the endpoint can say "that link does not
  carry coordinates" rather than "the service is unavailable".

`GET /api/geocode/link?url=…`, same auth and limiter. Client: the existing
address-search button in the location editor recognises a Google Maps URL in the
field and calls this endpoint instead of `/geocode?q=`, filling the coordinates
the same way a search result does and offering the label for the empty address
field under the same never-overwrite rule.

**Verify:** `make ci`. Go tests: the `assist` refusal tests still pass after
the move; the allowlist refuses a non-Google host with no outbound request
(assert against an `httptest` server that records hits); a redirect chain to a
private address is refused; each of the four URL shapes extracts correctly; a
Google URL with no coordinates produces the distinct error. Playwright: paste a
`maps.app.goo.gl`-shaped URL served by a stub and assert the coordinate fields
fill — or, if stubbing the outbound host is not practical from the suite, assert
the client-side *recognition* branch and cover the resolution in Go only, and
say so in the plan's Done paragraph.

**Done.** Both halves as planned: `internal/safefetch` holds the guard, and
`geocode.ResolveMapLink` plus `GET /api/geocode/link?url=` resolves a link. The
address-search field recognises a Maps URL and sends it to the resolver instead
of to Nominatim — no new control.

**6a.** `Policy` with `PublicOnly()`, `Allowing(...)` and
`AllowPrivateForTests()`, a `Guard` method, an exported `CheckDialAddress`, and
`Client(Options)` that wires all three checks in. The zero value is the strict
policy, so a caller who forgets to build one gets the safe behaviour. `assist`
keeps `pageFetcher`, its caps, its User-Agent and all the HTML extraction, and
its constructors kept their names — the diff there is small on purpose.

Two things worth stating about the API:

- **`Client` is the only supported way to get one.** A caller holding a `Policy`
  and its own `http.Client` would have the pre-flight check and neither of the
  other two, which is the shape of guard that looks present and stops nothing.
- **A caller's `CheckRedirect` runs in addition to the guard, never instead.**
  The resolver uses one to keep a chain on its host allowlist;
  `TestCallerCheckRedirectCannotReplaceTheGuard` asserts it cannot pre-empt the
  address check by supplying one.

`AllowPrivateForTests` is the one loosening: it was an unexported field, and
across a package boundary it has to be reachable. The name is the documentation
and no configuration value can produce it.

**6b.** The resolver tries the URL it was given **before** making any request —
a full `/maps/@...` or `?q=lat,lng` link is read directly, which is most of what
people paste and costs nothing. Only a shortener is followed. The body is never
read: the answer is in the expanded URL, and a Maps page is a megabyte of
JavaScript.

Extraction order differs from the plan, on a detail the plan had backwards. It
listed `@lat,lng` first; `@` is the **viewport**, which follows the screen when
the map is panned, while `!3d…!4d…` in the `data=` blob is the place that was
actually clicked. The marker wins, and
`TestResolveMapLinkPrefersTheMarkerOverTheViewport` pins it with a URL whose two
candidates are 54 degrees apart.

**The test seam is a struct, not a global.** The tests need both policies
relaxed — an `httptest` server is on loopback *and* is not google.com — and a
pair of mutable package variables that production code reads would be a worse
answer than a value the tests construct. `mapLinkResolver` is unexported with
`ResolveMapLink` as the only door in.

**The host check is structural rather than a TLD list**, since Maps is served
from per-country domains: the label before the public suffix must be `google`,
or the host is one of the two shorteners. The test table is mostly lookalikes —
`google.com.evil.example`, `notgoogle.com`, `www.google.com.attacker.net`,
`evil-goo.gl` — because that is where this kind of check goes wrong.

**Verified.** `make ci` green (388 keys). Eight new tests in `safefetch` and
21 in `geocode` (11 of them for the resolver), plus four for the endpoint.
`internal/assist`'s own guard tests pass **unchanged**, which is the evidence a
move of security-critical code has to produce.

Two of my own tests were wrong first and are worth recording. A stub that
redirected *every* path redirected its own destination too, so it looped until
the cap — the redirect cap earning its keep, but not what the test was about.
And the out-of-range-coordinates test went through the front door, so a URL with
no *usable* point fell through to the shortener path and made a **live request
to google.com** to prove a parsing rule; it now tests `coordinatesFrom`
directly. The package's tests run in 0.012s, which is what a suite that reaches
nothing looks like.

Full UI suite green at **152 passed**, four new specs intercepting
`/api/geocode/link` — a short link filling the fields and naming the place, a
hand-written address surviving, the 404 and 502 messages reading differently,
and an ordinary search term still going to the search endpoint with nothing
sent to the resolver. By hand at 324×756: a full `google.com` URL and a
`google.de` one both resolve to the marker rather than the viewport, the name
lands in an empty address field, the map moves, and the German pass reads
"Koordinaten aus dem Link übernommen."

**Follow-up: verified live, and it found a bug.** A real short link
(`maps.app.goo.gl/gMeQfY4RMpQg4DHeA`, the Brandenburg Gate) resolves correctly
end to end: 200 in ~1.5s, `52.5162746,13.3777041`, name "Brandenburg Gate". So
Google's shortener does behave as this code expects — a plain GET, no consent
interstitial, and the chain ends on a URL carrying `!3d`/`!4d`. The backlog note
recording that as unproven is gone.

What the live run exposed is a **coordination bug that was not in this
milestone's new code**. The fields filled and the reverse-geocoding button from
Milestone 5 stayed *disabled*. There are five ways the coordinates can change --
typing, a map click, the locate control, choosing an address-search result, and
resolving a link -- and **four of them write `form.lat.value` directly**, which
fires no `input` event. Milestone 5's button watched the input event and the two
map events, so it was already wrong for a chosen search result before this
milestone existed; the link path was simply a third way to reach it.

The fix is one notification path rather than another listener:
`coordinatesChanged()` runs the map sync, the hint and any subscriber, and every
writer calls it. `bindPlaceSearch` took two callbacks and now takes that one;
`bindAddressLookup` subscribes instead of listening to two of the five writers.
The shape is the point -- the next writer has one call to make, and forgetting
it is visible rather than silent.

**Verified.** A new spec drives both halves (a resolved link and a chosen search
result, each enabling the lookup) and was **checked against the unfixed code**:
it fails with "a resolved link must enable the lookup ... Received: disabled".
`make ci` green, full UI suite green at **153 passed**. By hand: the real short
link fills the fields and enables the button, a search result does the same, and
clearing a field disables it again.

**Second follow-up: the name is a name, not an address.** The live test also
showed the link filling the *address* field with "Brandenburg Gate", which is
the name of the site. That raised two questions, both answered by measurement
before deciding anything.

**Can more metadata be had from the link? No.** The expanded page is 219KB of
JavaScript whose `og:title` is literally "Google Maps" and whose
`og:description` is "Find local businesses, view maps and get driving
directions". The street address appears **nowhere** in the HTML — grep for
"Pariser Platz" returns 0 hits — because the content is rendered from an
undocumented `APP_INITIALIZATION_STATE` blob that would break on any Google
change. The expanded URL does carry a place id and a Knowledge Graph MID, but
turning either into an address needs Google's paid Places API and a key.

**And it is not needed, because Milestone 5 already built the answer.**
Reverse-geocoding the point the link gives produces
"Quadriga mit Victoria, 1, Pariser Platz, ..., 10117, Deutschland" — a real
address, from the geocoder this app already talks to.

So, decided with the person driving the stage: **keep the paste in the Location
card** rather than promoting it to a top-of-editor control (no new UI, no
competition with the assist panel for that slot; promoting it later is easy and
better informed), and **fill the title and the coordinates, never the address**.
The name goes to the title when the title is empty, the address is left for the
**Look up address** button one press away — which the first follow-up had just
made reachable — and the placeholder now says the field takes a link.

The status message names the title it set (`"…and “{name}” used as the
title."`), because the title lives in the card *above* this one and is off
screen at 324px: a silent change to a field you cannot see is the thing to
avoid. `setStatus` grew a params argument rather than a caller writing
`textContent` past it.

**Verified.** `make ci` green (389 keys), full UI suite green at **153 passed**,
two specs rewritten (the title is set and the address left empty; a typed title
survives, and the message then claims only the coordinates). By hand with the
real short link: title "Brandenburg Gate", coordinates set, address empty,
button enabled — then one press of Look up address and Accept fills the genuine
Pariser Platz address.

**Third follow-up: the locale is forwarded, and it helps in one of the two
places.** The name arriving as "Brandenburg Gate" rather than "Brandenburger
Tor" was the last loose end. All three outbound calls now carry the app's
language: `Search` and `Reverse` add Nominatim's `accept-language` parameter,
and the map-link resolver sends an `Accept-Language` header. It comes from the
client rather than from the browser's own header, for the reason the
assistant's `locale` field already gives — the app's language is a
`localStorage` setting, and a German UI on an English system is the ordinary
case. `normaliseLocale`, which the assistant already uses before a locale
reaches a third party, is the validation.

**Nominatim honours it. Google does not, and that was measured rather than
assumed:**

| | with `locale=de` | with `locale=en` |
| --- | --- | --- |
| reverse geocode | Quadriga **mit** Victoria, …, **Deutschland** | Quadriga **with** Victoria, …, **Germany** |
| map link | Brandenburg Gate | Brandenburg Gate |

The name a link resolves to comes from the `/maps/place/<name>/` segment of the
expanded URL, and Google bakes that in when the short link is *created*: neither
`Accept-Language: de` nor `hl=de` moves it, verified against the live service.
Only whoever made the link could have made it German. The header is sent anyway
— one header, the correct thing to ask, and a link created in a German session
already carries a German name — with the finding recorded in `expand()` so the
next reader does not re-run the experiment.

Both assist call sites pass an empty locale, which preserves their behaviour
exactly: they resolve an address to coordinates and discard the display name,
so the language it comes back in changes nothing.

**Verified.** `make ci` green, full UI suite green at **153 passed**. New tests:
the parameter reaches Nominatim for both verbs and is absent when no locale is
asked for; the header travels the whole redirect chain; and at the HTTP layer a
table of six locales including `de&format=xml`, an embedded newline and a
40-character string — each of which is dropped rather than forwarded to a third
party. Live: the reverse lookup returns Deutschland/Germany as asked.

---

## 7. Two writes that lie, and sweep-up

**The checklist tick.** `web/js/components/checklist-list.js:256-270`: wrap the
PATCH in `try/catch`; on failure put the box back to `!e.target.checked`, leave
`item.checked` and the strikethrough alone, and print a message the way the
admin page's open-signup toggle does. One new i18n key per locale.

**The dropped reorder.** `itinerary-tab.js`'s `moveEntry`: instead of the
guard swallowing a second press, coalesce — apply the swap locally, and when a
request is already in flight remember the latest order and send it once the
first answers. The existing failure path (re-read the day) stays as it is. The
guard still owns the in-flight flag; what changes is that a swallowed press is
queued rather than lost.

**Sweep-up.** `scripts/check_i18n.py` parity across every new key;
`plans/todo.md` reconciled in both directions — delete the itinerary-move,
reverse-geocoding, `capabilities`-object, failed-tick and dropped-reorder
entries, rewrite the expenses entry (the location link lands; a per-location
cost was considered and dropped, so say so rather than leaving it as
outstanding) and the Google Maps entry (the inbound half lands, the outbound
place-ID half stays blocked), and add whatever this stage deferred: any UI-suite
gap Milestones 5–6 had to record, and the duplicate-guard note that
`internal/safefetch` resolves. If a milestone changed a screen in the
documentation tour — the itinerary entry row now carries a menu, the expense
form a select — regenerate with `make screenshots` and note it.

**Verify:** `make ci`. `tests/ui/checklists.spec.js` for the failed tick, by
making the PATCH fail (route interception) and asserting the box returns to its
prior state *and* a message is shown. `itinerary-order.spec.js` for two rapid
presses producing the final order on the server, not the first one.

---

## Build order

0 → 1 → 2 → 3 → 4 → 5 → 6 → 7. The itinerary pair (1–2) and the expense link
(3) are independent of each other and of the geocoding half; 4 must land before
5 because 5 adds the fourth capability flag; 6a must land before 6b.

## Files this touches

- `internal/httpapi/`: `itinerary.go`, `expenses.go`, `geocode.go`, `auth.go`,
  `router.go`, and their tests.
- `internal/db/`: `migrations/{sqlite,postgres}/0003_expense_item.*`,
  `sqlc/queries/{itinerary_entries,expenses}.sql`, regenerated dialect packages.
- `internal/geocode/geocode.go`; new `internal/safefetch/`;
  `internal/assist/fetch.go` (guard removed, import added).
- `web/js/pages/`: `itinerary-tab.js`, `expenses-tab.js`,
  `location-editor-page.js`; `web/js/components/checklist-list.js`;
  `web/js/session.js`; `web/locales/{en,de}.json`; `web/css/base.css` for the
  expense row's location link and the new controls.
- `tests/ui/`: `itinerary-order.spec.js`, `expenses.spec.js`,
  `locations.spec.js`, `checklists.spec.js`.
- `plans/stage-22.md`, `plans/todo.md`, `CLAUDE.md` (the migration-number line).

## Verification

Every milestone: `make ci` green, plus `make test-postgres` for 1 and 3 (they
change `internal/db` queries), plus an assertion-based Playwright or Go test
proving the behaviour changed — not a screenshot. Mobile pass at 324×756
against `make dev` for Milestones 2, 3, 5 and 6, since each adds a control to a
screen that is already tight at that width.

## Workflow

One milestone at a time, in the order above. For each: implement, verify
(`make ci` plus the milestone's own proof), add a **Done.** paragraph to that
milestone's section here describing what actually landed and how it was
verified, reconcile `plans/todo.md` in both directions, commit (one commit per
milestone; a follow-up fix gets its own "... follow-up: ..." commit), make sure
`make dev` is running, then stop and hand back control. Do not start the next
milestone until told to continue; feedback at a checkpoint is fixed and
re-verified before moving on.
