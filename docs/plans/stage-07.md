# Stage 07 — UI/UX fixes from the automated test round

> **Status: in progress.** Built one milestone at a time per the Workflow
> section below, each with its own commit and a manual-testing checkpoint.

## Context

An automated Playwright pass over both viewports — desktop 1280×800 and
324×756 (the phone's native resolution, per `CLAUDE.md`'s mobile testing
convention), plus dark mode via `prefers-color-scheme` emulation — walked
every route of the app: login/register, trips list, all six trip tabs,
location view/edit/create, and the error states around them. It surfaced 19
issues, each triaged with the user into "fix in this stage", "record in
`todo.md`", or "drop". Nothing was dropped.

This stage fixes the 11 items marked for immediate work. They fall into four
groups:

1. **Visible breakage.** A trip with exactly one mappable location renders a
   blank grey map (zero-size bounds → zoom 19 → every OSM tile 404s), and the
   per-location map draws a broken-image box instead of a pin (Leaflet's
   default icon URL resolves against the SPA route).
2. **Silent failures.** An end date before the start date is accepted without
   complaint, producing a "20 Aug – 1 Aug 2026" header and silently emptying
   the itinerary; an itinerary day added by typo can never be removed;
   clicking Upload with no file selected does nothing at all; an image URL
   the browser can't load leaves an invisible `alt=""` preview.
3. **Accessibility.** Dark-mode primary buttons measure 2.54:1 against AA's
   4.5:1; checklist items are a 22px touch target on a phone; three form
   controls have no accessible name; error paragraphs are never announced;
   heading levels skip from h1 to h3/h4.
4. **Clarity.** Trip cards print raw ISO dates while every other view formats
   them, and both Delete buttons sit in unlabelled cards that never say what
   gets deleted.

The remaining 8 findings are recorded in `todo.md` (see Milestone 0), each
citing this test round.

Two decisions taken with the user up front:

- **Itinerary day deletion applies only to days outside the trip's date
  range.** Days inside the range are generated placeholders that would simply
  reappear; a day added outside it is the one a typo can strand.
- **The dark-mode contrast fix gives buttons their own darker background
  token** rather than dark text on the existing light blue. `--color-accent`
  is also the link colour, where `#60a5fa` is fine at 6.97:1, so the token
  cannot simply be darkened globally.

Milestones are deliberately small — one behavioural change each, so every
commit is independently reviewable and revertible.

**Milestone 0 (lands with this document, one commit):** write this file and
add the eight deferred findings to `docs/plans/todo.md`, before touching any
code.

---

## Milestone 1 — Map: clamp zoom when fitting bounds

`web/js/components/leaflet-map.js:238`. With one mappable item the bounds
passed to `fitBounds` are zero-size, so Leaflet zooms to the tile layer's
`maxZoom` (19) and every tile 404s — verified in the network log: 12 requests
to `https://{a,b,c}.tile.openstreetmap.org/19/228200/136690.png` and its
neighbours, all 404, leaving a grey rectangle with a single dot on it.

Pass `maxZoom: 14` to `fitBounds`, matching the `setView([lat, lng], 14)` the
single-location branch already uses on line 217.

**Verify:** trip with exactly one mappable location — assert
`.leaflet-tile-loaded` count > 0, the map's zoom ≤ 14, and no 404 among the
tile requests. Re-check a multi-marker trip still frames all its markers.

**Done.** `fitBounds` now takes `maxZoom: SINGLE_MARKER_ZOOM`. The literal
`14` the single-marker branch already used became that named constant at the
top of the file, so both paths state the same intent in one place rather than
repeating a bare number — the deviation from the plan is only that the
constant is new; the value is unchanged.

Verified against a purpose-made trip with exactly one mappable location
(created via the API so the demo trip's data stayed untouched): the map now
loads at zoom 14 with 12 tiles requested and **all 12 returning 200**, where
before the same shape produced 12 requests at zoom 19 all returning 404 and a
blank grey rectangle. Re-checked the two-marker demo trip: still 2 markers,
12 tiles loaded, and every marker inside the map's viewport rect — the
`maxZoom` cap doesn't interfere with genuine bounds-fitting. `make ci` green.

## Milestone 2 — Map: real marker on the single-location map

`web/js/components/leaflet-map.js:212`. The `_singleMarker` branch uses a bare
`L.marker`, so Leaflet's default icon resolves relative to the current route
— `…/locations/marker-icon.png`, which the SPA answers with HTML — and the
browser draws a broken-image box labelled "Marker" (`naturalWidth: 0`,
confirmed through the component's shadow root).

Build the marker with the same `L.divIcon` the multi-marker branch uses
(lines 224–228), factored into one local helper both branches call, coloured
by category with the existing `#71717a` fallback.

**Verify:** on a location detail page, assert the marker element is the
`divIcon` span — or that no marker `<img>` has `naturalWidth === 0`.

**Done.** Both branches now call one `markerIcon(L, category)` helper, so
neither map builds an icon of its own. Beyond the plan: the single-marker
mode had no category to colour by (its attributes were only `lat`/`lng`/
`marker-title`), so it gained a `marker-category` observed attribute, passed
from `location-view-page.js`. The dot on a location's own map therefore
matches both the trip map's colour coding and the category dot printed
directly above it on the same page — greyed via a named
`FALLBACK_MARKER_COLOR` when the attribute is absent.

Verified on a `site` location: exactly 1 marker, `tagName` `DIV` (the
divIcon), 0 broken images, dot `rgb(22, 163, 74)` = the site green, and
**no `marker-icon.png` request in the network log at all** — previously that
request 404'd into the SPA's HTML and drew a broken-image box. Trip map
re-checked: both markers still site-green, popup still opens on click with
"Kirkjufell / View on Google Maps". Fallback re-checked by mounting a
`<leaflet-map>` with no `marker-category`: grey `rgb(113, 113, 122)`. The two
console warnings seen during the click test are pre-existing Leaflet
`mozPressure`/`mozInputSource` deprecations from the synthetic event, not
from this change. `make ci` green.

## Milestone 3 — Reject an end date before the start date

`internal/httpapi/trips.go` — extend `tripRequest.validate()`, already called
by both `handleCreateTrip` and `handleUpdateTrip`, to return an error when
both dates are set and the end precedes the start. Today the API accepts it;
the trip header then reads "20 Aug – 1 Aug 2026" and `datesInRange`
(`itinerary.go:107`) returns nil for the inverted range, so the itinerary
silently shows only days that happen to have content.

**Verify:** `PATCH /api/trips/{id}` with an inverted range returns 400 and
leaves the trip unchanged; `go test ./...` covers both create and update.

**Done.** `tripRequest.validate()` now compares the two bounds once both
parse, returning "end date must not be before start date". Since that method
is the single gate both `handleCreateTrip` and `handleUpdateTrip` run their
body through, one check covers both.

New `internal/httpapi/trips_test.go` table-tests it: an inverted range (same
year and across a year boundary) is rejected; a well-formed range, a same-day
range, and a one-bound-only trip are accepted; a malformed date still reports
the format error rather than the new one; and the pre-existing blank-title
rule is asserted so the added check can't quietly displace it. The suite was
confirmed non-vacuous by stashing `trips.go` and re-running — only the two
inverted-range cases fail without the fix.

End-to-end against a **freshly restarted** server: create and update both
return 400 with that message, the demo trip's dates are unchanged after the
rejected PATCH, and a same-day range still returns 200.

Worth recording, because it nearly produced a false pass: the first
end-to-end run reported 200/201 — the API happily accepting inverted ranges.
The cause was a stale server still holding :8080 whose binary predated the
edit, while `make dev` had failed with "address already in use". `pkill`
missed it because a `go run` child's command line is its go-build cache path,
not `go run ./cmd/caravel`. The reliable check is to find the listener via
`ss -lptn 'sport = :8080'` and grep `/proc/<pid>/exe` for a string the fix
introduces — done here before re-running, confirming the serving binary
carried the change. A junk trip created during that stale run was deleted.
`make ci` green.

## Milestone 4 — Show that rejection inline in the trip form

`web/js/components/trip-form.js` — check the range before the POST/PATCH and
render the message into the existing `.trip-form__error` paragraph, so the
user sees it inline instead of as a round-trip API error. New i18n key in
`web/locales/en.json` + `de.json`.

**Verify:** submitting an inverted range in trip settings shows the inline
error and issues no request (assert via the network log).

**Done.** Two additions to the planned scope, both agreed at the Milestone 3
checkpoint or forced by what the fix exposed:

1. **`min` on the end-date input**, synced whenever the start date changes
   (the user's suggestion). The invalid day is then greyed out in the native
   picker, so the mistake mostly can't be made rather than being reported
   after the fact. Deliberately one-directional — capping the start date at
   the end date too would block the ordinary move of shifting a whole trip
   later, where the new start is picked first. An end date already set and
   now out of range is left exactly as typed; the submit check explains it
   instead of silently rewriting a date the user entered.
2. **Every form error became a callout box.** `.trip-form__error` turned out
   to have no error styling at all (only `padding`), so the message rendered
   as ordinary body text. Matching it to its three siblings' red text was the
   obvious fix — but at the checkpoint the user pointed out that red text is
   unreadable in dark mode, which measurement confirmed: `#dc2626` on the
   dark card is **3.08:1**, under AA's 4.5:1, and this affected all four
   error paragraphs, not just the new one. Lightening `--color-danger` wasn't
   available either: the same variable is the Delete button's background,
   with white text on it.

   Following the user's suggestion, errors now use GitHub's caution-block
   shape (chosen from three variants: no icon, so no sprite regeneration and
   no new i18n keys): a red left border and faint red tint, with the message
   itself in `--color-text`. Legibility no longer depends on the red passing
   a text-contrast bar anywhere — the red is decoration, held only to the
   3:1 non-text bar.

   This also introduced the token split M10 will need for accent:
   `--color-danger` stays the *background* red, `--color-danger-fg` is the
   *foreground* red (border, and `.icon-remove:hover`), lightened to
   `#f87171` in dark mode. All four error paragraphs now share one rule
   instead of three near-identical copies plus one gap.

The submit check stays necessary alongside `min`: the form is `novalidate`,
so a date typed directly into the field still submits.

Verified in the browser. `min` tracks the start date (`2026-09-05` after
changing it), clears when the start is cleared, and leaves an existing
`2026-08-23` end value untouched when it falls out of range. Submitting an
inverted range shows "The end date can't be before the start date." and
issues **0 fetch calls** (counted by wrapping `window.fetch`); a valid range
saves normally and the API confirms `2026-08-20 → 2026-08-23`. Re-checked on
the *create* form in German: same `min` behaviour, message reads "Das
Enddatum darf nicht vor dem Startdatum liegen.", and no trip is created.

Contrast measured in both themes with the translucent tint flattened over
the card behind it — message text **11.6:1 dark / 14.34:1 light** (was 3.08
dark), border **5.38:1 dark / 4.39:1 light** against the card, both well past
the 3:1 non-text bar. The Delete button is untouched at 4.83:1, confirming
the token split did its job. Spot-checked the login form's error too: same
callout, `#fafafa` text on the dark tint. i18n parity 109 keys in sync,
`make ci` green.

## Milestone 5 — Format dates on trip cards

`web/js/components/trip-card.js` interpolates its date attributes verbatim,
so the trips list reads `2026-08-20 – 2026-08-23` while the trip header two
clicks later reads `20 Aug – 23 Aug 2026`.

Extract `formatDateRange()` from `web/js/pages/trip-detail-page.js:109` into a
shared module (`web/js/format.js`) and import it in both places, rather than
duplicating the formatting logic.

**Verify:** assert a card's date text equals the same trip's header range.

**Done.** `formatDateRange()` moved verbatim to a new `web/js/format.js`
(documented as the home for presentation-only formatting) and is imported by
both `trip-detail-page.js` and `trip-card.js`. No behaviour change on the
trip header; the card previously interpolated its raw attributes.

Verified in the browser: the card for the demo trip renders "20 Aug – 23 Aug
2026" and clicking through gives a header reading the identical string
(compared with `===`, not by eye). Two edge cases the old inline
interpolation got wrong, checked with throwaway trips since none of the
existing data covers them: a **start-only** trip rendered "2026-08-20 –"
with a dangling separator and now renders "20 Aug 2026"; a **cross-year**
range now renders "28 Dec 2026 – 2 Jan 2027" with both years rather than two
bare ISO strings. A trip with no dates still renders no date line at all.
Both throwaway trips were deleted afterwards. `make ci` green.

Note for later: `Intl` is called with an undefined locale, so dates follow
the *browser's* locale rather than the app's — a German UI in an en-GB
browser still shows English month names. That was already true of the trip
header, so this milestone makes the card consistent with it rather than
introducing anything new; the mismatch is worth a look whenever the language
switcher in `todo.md` is picked up.

## Milestone 6 — Delete-day endpoint (backend)

- `internal/db/sqlc/queries/itinerary_days.sql` — add `DeleteItineraryDay
  :execrows` keyed on id + trip_id, then run `sqlc generate` by hand from
  `internal/db/sqlc/` for **both** dialects (`CLAUDE.md` gotcha) and add the
  store method alongside `GetItineraryDayByID`/`UpsertItineraryDayNotes`.
- `internal/httpapi/router.go` — `r.Delete("/", …)` on the existing
  `/itinerary/days/{dayId}` group; the handler goes in
  `internal/httpapi/itinerary.go`, reusing `loadOwnedItineraryDay` for the
  ownership check and mirroring `handleDeleteItineraryEntry`. Entries cascade
  via the FK (`itinerary_entries.itinerary_day_id … ON DELETE CASCADE`).

**Verify:** `go test ./...` with a new test covering deletion and the
cross-user ownership check (mirroring `documents_test.go`); curl the endpoint
and confirm both the day and its entries are gone.

**Done.** `DeleteItineraryDay` added to the queries file and regenerated for
both dialects (`sqlc generate`, additive diffs only: 21 lines per dialect
plus the querier interface), with the store method mirroring
`DeleteItineraryEntry`. The query is scoped by `trip_id` as well as `id`, so
the SQL enforces the same ownership the handler checks.

Deviation from the plan: the route group had to be restructured rather than
extended. It was `/itinerary/days/{dayId}/entries`, which gives no path for
an operation on the *day* itself, so it became `/itinerary/days/{dayId}`
with `DELETE /` and the entry routes nested beneath as `/entries` and
`/entries/{entryId}`. The public entry URLs are unchanged.

Verification went further than planned, because the ownership check can't be
covered by a unit test in the style of `documents_test.go`. New
`internal/httpapi/itinerary_test.go` brings up a **real Server over a real
migrated SQLite database** in a temp dir, driving requests through the full
router including the auth middleware (only the static-asset FS and blob
store are stand-ins). Six tests: deleting an out-of-range day removes it and
cascades to its entries; deleting an in-range day reverts it to a synthesized
placeholder rather than making it vanish; another user gets 404, not 403, and
the owner's day survives; an anonymous request gets 401; an unknown id gets
404; and the relocated entry routes still work. Confirmed non-vacuous by
stashing `router.go`+`itinerary.go` — all five delete tests fail without them.

The cascade assertion is deliberate rather than assumed: SQLite only honours
`ON DELETE CASCADE` when `foreign_keys` is enabled per connection, which
`db.Open` does — so the test would catch that pragma being lost.

Finally, exercised against the live dev database: the 15 Jan 2027 day
stranded on the demo trip during the original test round — the one the report
called impossible to remove — deleted with 204, and the itinerary went back
to the four days of the trip's own range. `make ci` green.

## Milestone 7 — Delete control on out-of-range itinerary days

`web/js/pages/itinerary-tab.js` — `renderDay()` gains an `.icon-remove`
button (the same shape as the entry remove button on line 116), rendered only
when the day has a persisted `id` **and** its date falls outside
`trip.start_date`–`trip.end_date`. Confirm before deleting a day that carries
notes or entries. New i18n keys in both locales.

Today "Add a day" accepts any date — 15 Jan 2027 on an August trip was
accepted, persisted, and left with no way to remove it.

**Verify:** add a day outside the range — an X appears on it and on no
in-range day; clicking it removes the day, and `GET /api/trips/{id}/itinerary`
no longer lists that date.

**Done.** `renderDay()` gained an `.icon-remove` in a new
`.itinerary-day__header` flex row, so the control sits on the date line and
reads as acting on the day rather than on the last entry in it. An
`isRemovable(day)` helper carries the rule, including one case the plan
didn't call out: a trip with **no dates set** has no range to be inside, so
every day on it was added deliberately and all are removable.

Confirmation only appears when the day has notes or entries — demanding a
dialog to undo a date the user just mistyped would be noise.

Verified in the browser end to end: the four in-range days show no control
(including the two that *do* have persisted rows, from an entry and a note);
adding 15 Jan 2027 gives exactly one X, labelled "Remove this day"; clicking
it on an empty day deletes with no dialog and the date disappears from both
the DOM and the API. With notes added, the dialog appears — **cancelling
leaves the day present in both DOM and API**, accepting removes it. On the
dateless "UI Test Trip", a manually added day is removable as intended and
the list empties cleanly. German UI: label reads "Diesen Tag entfernen" and
deletion works. At 324×756 the button measures 44×44, stays on the heading's
row, and the page doesn't overflow. i18n parity 111 keys, `make ci` green.

Found while testing, deliberately *not* folded in: `itinerary.noDates` still
tells the user to set dates "on the Overview tab", a tab Stage 05 removed —
it's Settings now. Recorded in `todo.md` rather than fixed here, since it has
nothing to do with day deletion.

**Follow-up: itinerary days come back out of order.** Raised by the user in
`notes.md` ("If you reload the Itinerary page, the dates are not ordered")
and confirmed against the API: a day added *before* the trip's start came
back last — `20, 21, 22, 23 Aug, 5 Aug`. `handleGetItinerary` builds the
response in two passes, the trip's own range first and everything outside it
appended after, so out-of-range days land at the bottom regardless of date.
It only showed up on reload because `itinerary-tab.js` re-sorts locally after
adding a day.

Fixed by sorting the assembled response by date (ISO dates are zero-padded,
so lexical order is chronological). Covered by
`TestGetItineraryIsOrderedByDate`, which seeds days on both sides of the
range in a deliberately unhelpful order; confirmed non-vacuous by stashing
the handler, where it fails with exactly the misordering the user described.
Verified live too: after a reload the tab lists 5 Aug 2026 first and 15 Jan
2027 last, with Milestone 7's remove control on exactly those two
out-of-range days. Test days deleted; the note removed from `notes.md`.

## Milestone 8 — Upload with no file reports itself

`web/js/components/document-list.js:43` marks the `hidden` file input
`required` on a form with no `novalidate`, so the browser blocks submission
and cannot focus the hidden control to show its validation bubble: the click
is swallowed, leaving only a console error (`The invalid form control with
name='file' is not focusable`).

Drop `required` and report the empty pick through the existing
`.document-form__error` paragraph — the submit handler on line 104 already
returns early when no file is picked.

**Verify:** click Upload with no file — visible error text, no console error.
Uploading a real file still works.

**Done.** `required` dropped from the hidden file input, and the submit
handler reports the empty pick through the existing `.document-form__error`
paragraph (which the Milestone 4 callout styling now renders as a box). New
`documents.noFile` key in both locales.

Verified on the trip Documents tab: clicking Upload with nothing picked
shows "Choose a file first." with **0 console errors**, where the same click
previously did nothing at all and logged "The invalid form control with
name='file' is not focusable". Picking a real file and uploading still works
and clears the message. Re-checked the same component in its staging mode on
the new-location page (its "Add file" button) and in German ("Bitte zuerst
eine Datei auswählen."). Test document deleted afterwards.

**A CI gap surfaced, and it bit first.** My initial version put the
explanation in an HTML comment inside the template literal, and that comment
contained a backtick — which closed the template early and broke the
Documents tab with "SyntaxError: unexpected token: identifier" in the
browser. `make ci` stayed **green**. Root cause, pinned by reconstructing the
broken file: `check-js` runs `node --check <file>.js`, which parses as a
CommonJS *script*, where `<!--` legally begins an HTML-like comment (Annex B)
that swallowed the stray backtick. The app loads these files as ES *modules*,
where HTML-like comments are illegal — copying the same bytes to `.mjs`
reproduces the browser's error exactly. The comment moved to a JS comment
above `render()` (it shouldn't have been shipped to the DOM anyway), and the
gap plus a verified one-line fix (`node --input-type=module --check < "$f"`)
is recorded in `todo.md`. i18n parity 112 keys, `make ci` green.

## Milestone 9 — A failed image preview becomes visible

`web/js/components/image-field.js` does render a preview (line 24), but an
image the browser can't load renders as an `alt=""` element that collapses to
nothing — which is why a hotlink-blocked Wikimedia URL looked like "no
preview at all" during testing.

Add an `onerror` handler that surfaces the failure through the existing
`.image-field__error` paragraph, so a bad URL is visible at pick time.
(Moving the server-side *fetch* earlier than "Create trip" is the separate
backlog item, not this milestone.)

**Verify:** set an unreachable image URL — visible error in the image field;
a working URL still previews.

**Done.** An `error` listener on the preview hides the empty box and shows
`image.loadFailed` through the existing `.image-field__error` paragraph. The
CSS needed a companion rule: `.image-field__preview` sets `display: block`,
which outranks the UA's `[hidden]` rule, so hiding it in JS alone would have
left the border box sitting there.

Verified on the new-trip form: an unreachable URL now shows "That image
couldn't be loaded. Try a different file or link." with the preview hidden
(`display: none`, height 0) — where the same input previously left an
`alt=""` element collapsed to nothing and no message at all, which read as
"the URL wasn't accepted" even though it had been. A working URL still
previews with no error (`naturalWidth: 32`). The same handler covers the
*file* path too: a non-image file renamed `.png` (which `accept="image/*"`
happily lets through) reports instead of silently showing nothing. Copy
reworded mid-milestone from "Check the link…" to "Try a different file or
link" once that second path was confirmed to share the message. German
checked.

Also found, recorded in `todo.md` rather than fixed: on an **existing** trip
the field is in attached mode, where "Set image" does fetch server-side at
the right moment — but reports the Go error verbatim (`dial tcp: lookup
example.invalid: no such host`), untranslated even with the German UI. So
the two modes fail at different *times* and with different *copy*; that
belongs with the existing image-timing backlog item, not here. i18n parity
113 keys, `make ci` green.

## Milestone 10 — Dark-mode button contrast

`web/css/base.css`. `.btn-primary` (line 125) is white on `--color-accent`,
which is `#60a5fa` under `prefers-color-scheme: dark` — **2.54:1**, against
AA's 4.5:1 for normal text. The token cannot simply be darkened: links use
the same variable and are fine at 6.97:1 on the dark background.

Add a separate `--color-accent-strong` (`#2563eb` by default, ≈`#1d4ed8` in
the dark block) for button backgrounds, keeping white text, and audit the
other white-on-accent surfaces (`::selection` line 46, and the accent
backgrounds at lines 260 and 548).

**Verify:** compute `.btn-primary`'s contrast ratio under emulated dark mode
and assert ≥ 4.5; confirm the link ratio is unchanged and light mode still
measures 5.17:1.

**Done.** `--color-accent-strong` added, `#2563eb` in both themes — the point
of the token is that this one *doesn't* lighten in dark mode, where
`--color-accent` must lighten to `#60a5fa` to stay readable as text. Dark
mode overrides it to `#1d4ed8` per the user's choice at planning time.

All 17 `--color-accent` usages were classified before changing any: five are
accent-as-text (links, `.back-link`, `.link-button`, the active menu trigger
and checked row) and stay on `--color-accent`; the rest are focus outlines
and active-tab borders, which are non-text and fine either way. Exactly
three are fills carrying white text — `.btn-primary`, `::selection` and
`.user-menu__avatar` — and those moved to the new token. Grepping every
`color: white` in the stylesheet confirmed those three plus `.btn-danger`
(already handled by Milestone 4's split) are the only such surfaces, so
nothing was missed.

Measured in both themes: primary-button text **6.7:1 dark** (was 2.54) and
5.17:1 light; avatar identical; links unchanged at 6.97:1 dark; the Delete
button untouched at 4.83:1. Light mode is byte-for-byte unchanged — both
fills still compute to `rgb(37, 99, 235)`. Swept the trips, locations and
itinerary pages in dark mode (7 primary buttons in total): minimum text
contrast 6.7:1 everywhere.

One trade-off worth recording: the darker fill contrasts less with the card
*behind* it — 2.22:1 in dark, against 2.90:1 had the fill stayed `#2563eb`.
Both are under the 3:1 non-text bar, though that bar applies to components
identified by their boundary alone, which a filled button with a white label
isn't. Compared side by side, both read fine; the chosen colour is also the
better of the two on the ratio that governs the label. If the button ever
feels muddy against dark cards, dropping the dark override (leaving
`#2563eb` in both themes) trades 6.7:1 down to 5.17:1 on the text — still
past AA — and buys back the fill contrast.

## Milestone 11 — Checklist items get a real touch target

`web/css/base.css`, inside the existing `max-width: 640px` block (from line
1108). A checklist item's label row measures 110×22 with a 14×14 checkbox,
while the delete button beside it is a correct 44×44 — so on a phone the
easiest thing to hit is "remove", not "done".

Give `.checklist-item label` `min-height: var(--tap-min)` and scale the
checkbox up (≈1.25rem), following the pattern the `.icon-remove` rule already
uses.

**Verify:** at 324×756, assert `.checklist-item label` height ≥ 44 and that
the row still lines up with its delete button.

**Done.** Inside the existing `max-width: 640px` block: `min-height:
var(--tap-min)` on `.checklist-item label` (the label wraps both the box and
the text, so it's what carries the tap area) and the checkbox itself grown to
`1.25rem` so it stays proportionate.

Verified at 324×756: the label measures **116×44** (was 110×22) with a 20×20
checkbox (was 14×14), beside the delete button's unchanged 44×44 — so
"done" is no longer the harder of the two to hit. Geometry alone doesn't
prove the target works, so also tested behaviourally: a click **3px above
the label's bottom edge** — dead space under the old 22px row — toggles the
item, and toggling back restores the original state. Desktop is untouched
(22px label, 14px box), confirming the rule stays inside the breakpoint. No
horizontal overflow at either width. `make ci` green.

## Milestone 12 — Accessible names for unlabelled controls

Three controls have no accessible name today:

- the per-day item `<select>`, `itinerary-tab.js:53`
- the add-a-day date input, `itinerary-tab.js:23`
- the "Dates" start-date input in `location-editor-page.js` — its sibling
  carries an "End date (optional)" placeholder, the first has neither

Use `data-i18n-aria-label`, which `translatePage` already supports (see
`document-list.js:43` for the existing usage), with keys in both locales.

**Verify:** assert each control has a non-empty accessible name.

**Done.** The add-a-day input and the location editor's start date use
`data-i18n-aria-label` with new keys. The per-day `<select>` went further
than a static label: since one exists per day and they'd otherwise be N
identical "Choose an item" comboboxes, it names its own day via `t()`'s
`{date}` interpolation — "Add an item to Thu, 20 Aug 2026". The end-date
input also gained an explicit `aria-label` (same string as its placeholder)
so its name no longer depends on placeholder-as-fallback, which is the
weakest source in the accname spec.

Rather than checking only the three controls the plan named, a sweep over
**10 routes** computed the accessible name of every input, select, textarea
and button — 157 controls — from aria-label, aria-labelledby, wrapping or
associated label, placeholder, text content and title. Result: **zero
unnamed controls**. The sweep was proven non-vacuous by stashing the two JS
files and re-running: it flags exactly `select#itemId` ×4, `input#date` and
`input#startDate`, the three cases the plan identified. German checked on
all four labels.

Noted, not fixed: the German select label reads "Einen Eintrag zum Thu, 20
Aug 2026 hinzufügen" — German copy around a browser-locale date. That's the
`Intl`-locale mismatch already recorded under Milestone 5 and in `todo.md`'s
language-switcher entry, now visible mid-sentence rather than just in a date
column. i18n parity 116 keys, `make ci` green.

## Milestone 13 — Announce form errors

`.auth-form__error` (`login-page.js:16`) and the sibling `.trip-form__error`,
`.document-form__error` and `.image-field__error` paragraphs are plain `<p>`
elements toggled via `hidden`, so a screen reader never hears "Invalid
username or password." Add `role="alert"`.

**Verify:** assert `role="alert"` on each; re-check that a failed login still
displays the message normally.

**Done.** There turned out to be **five**, not four: `.item-form__error` in
`location-form.js` (a failed save on a location's Basic info card) wasn't in
the plan's list. All five now carry `role="alert"`.

That fifth one also exposed a miss in Milestone 4, whose stated intent was
one rule for every error paragraph in the app: it had been left on its own
`padding` + `border-radius`, so a failed location save rendered as plain
body text, exactly the state `.trip-form__error` was in before. It's now in
the shared callout rule, and the milestone-4 comment corrected from "four"
to "five".

Verified through the accessibility tree rather than by reading attributes:
each error that previously appeared as `paragraph` now appears as `alert`
with its message — a failed login ("Invalid username or password."), an
inverted date range, an empty upload, and an unreachable image URL, all
triggered for real. `.item-form__error` only fires on a failed save, so it
was forced visible to confirm the role sits on the element errors are
written to, and screenshotted to confirm it now renders as a callout.
`make ci` green.

## Milestone 14 — Fix heading hierarchy

Heading levels skip h1 → h3/h4: trip cards use h3, location cards and card
titles use h4, with no h2 in between. Normalise per page — trips list,
locations tab, trip detail, and the editor cards.

**Verify:** assert no gap in heading levels on the trips, locations and
trip-detail pages.

## Milestone 15 — Label the delete cards

Both trip settings (`settings-tab.js:29`) and location edit
(`location-editor-page.js:120`) end in a bare red Delete button inside an
unlabelled grey card — no heading, no statement of what gets deleted or that
it's permanent (the scope only appears afterwards, inside a native
`confirm()`).

Give each card a heading and a line of explanatory copy, in both locales,
consistent with the existing card headings ("Basic info", "Cover photo").
Lands after Milestone 14 so the new headings slot into a hierarchy that is
already correct.

**Verify:** headings render in both locales; `scripts/check_i18n.py` parity
check passes.

---

## Build order

Milestones 1 → 15 as numbered.

The two map fixes come first: they are the most visible breakage and touch a
single file. The date pair goes server-first, so Milestone 4's inline message
layers onto a rule the API already enforces. The itinerary pair likewise goes
backend-first — Milestone 6 is independently testable via `go test` and curl,
without any UI. The pickers and styling follow. The accessibility and copy
work lands last: Milestones 14 and 15 touch the most files but carry the
least risk, and 15 depends on 14's corrected hierarchy.

## Workflow

Per `CLAUDE.md`, for each milestone: implement, verify (`make ci` green plus
a Playwright or `go test` pass proving the behaviour actually changed), add a
"**Done.**" paragraph to that milestone's section here, update
`docs/plans/todo.md` in both directions, commit (one commit per milestone,
message stating what changed, why, and how it was verified), make sure
`make dev` is running, then stop and wait for the go-ahead before starting
the next milestone.

## Verification

`make ci` before every commit — build, vet, JS syntax, i18n key parity,
`go test`. Per-milestone assertions are listed inline above; prefer
assertions (computed styles, DOM counts, accessible names, network status
codes, `go test`) over screenshots.

At the end of the stage, re-run both viewports — 1280×800 and 324×756, light
and dark — across every route touched, asserting
`document.documentElement.scrollWidth <= window.innerWidth` and that
`window.location.pathname` is the intended route *before* asserting anything
about that page (the footgun already recorded in `todo.md`: the router
silently redirects unmatched paths to `/trips`, so a typo'd URL makes layout
checks pass trivially against the wrong page).
