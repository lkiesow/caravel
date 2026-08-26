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
