# Stage 10 — Backlog burn-down: low-hanging fruit

## Context

`docs/plans/todo.md` has grown to 407 lines across nine stages. Most of it is
genuinely large work — multi-user roles and sharing, device geolocation, a trip
journal, click-to-pick coordinates — but mixed in are entries whose fix is a
handful of lines, sitting there only because no stage happened to be *about*
them. Stage 09 explicitly deferred several of them by name (`sw.js` syntax
checking, build-SHA stamping, the identifier rename), and the UI-suite section
has been accumulating "worth adding as the app grows" notes since Stage 08.

This stage is deliberately about those: seven independently reviewable
milestones, each closing at least one confirmed backlog entry, ordered
cheapest-first so the visible wins land early and a stall in the expensive one
blocks nothing. Every claim below was re-verified against the current tree while
planning — file paths and line numbers are as of today, not as of the stage that
wrote the backlog entry, and two of them turned out cheaper than the entry
assumed.

Outcome: notes read at sensible density, a trip's cover photo is visible on the
trip, a long itinerary collapses to what matters, the Files tab stops hiding
files that exist, `make ci` covers the one JS file it never parsed, any running
binary can name its own commit, and the UI suite stops failing for reasons that
aren't the app's fault.

Deliberately **out of scope** (staying in `todo.md`): the menu/settings cluster —
`renderMenu`'s action-item mode, the `user-menu.js` → `menu.js` refactor, the
in-app language switcher and the manual light/dark toggle. That cluster is real
value but needs design work first and would double this stage. Also out: the
rest of the `item` → `location` / `documents` → `files` identifier sweep, the
mobile map's scroll-swallowing, migration squashing, and contrast-as-a-spec.

---

## 1. Notes render with sane spacing

`.location-view__notes` ([web/css/base.css:830-832](web/css/base.css#L830-L832))
sets `white-space: pre-wrap` on a container that holds *rendered HTML*
([location-view-page.js:138](web/js/pages/location-view-page.js#L138) assigns
`item.notes_html`). The newlines *between* block elements survive as literal
blank lines and stack on top of each element's own margins — measured 58px vs
20px between a paragraph and a following `<h2>`, with the same penalty before
every list, list item and paragraph.

- Drop `white-space: pre-wrap` from that rule. If nothing else is left in it,
  remove the rule.
- Compensate for the trade-off it was hiding: `internal/markdown/markdown.go`
  calls the package-level `goldmark.Convert` with **no options at all**
  ([markdown.go:22](internal/markdown/markdown.go#L22)), and goldmark collapses
  a single newline into a space — so a note relying on single line breaks would
  reflow once CSS stops preserving them. Replace the call with a package-level
  `goldmark.New(goldmark.WithRendererOptions(html.WithHardWraps()))` and use
  `md.Convert`. Line breaks then come from `<br>` rather than from CSS
  preserving source whitespace.
- `bluemonday.UGCPolicy()` already permits `<br>`; leave the sanitize step
  exactly as-is, and keep the comment at
  [markdown.go:16-19](internal/markdown/markdown.go#L16-L19) accurate.

**Verify.** Extend `internal/markdown/markdown_test.go`: a single newline yields
`<br>`, a blank line still yields separate `<p>`s. Then a Playwright assertion
on a seeded location: the computed gap between a `<p>` and the following `<h2>`
inside `.location-view__notes` is ≤ 24px (it is ~58px today).

Closes the first "Bugs and rough edges" entry.

**Done.** Landed as planned, both halves together. `.location-view__notes` lost
its `white-space: pre-wrap` — the rule now holds only a comment explaining why
the property must not come back, since the container holds rendered HTML and the
next person to see loose-looking prose would reach for exactly that property.
`internal/markdown` gained a package-level
`goldmark.New(goldmark.WithRendererOptions(html.WithHardWraps()))` in place of
the bare package-level `goldmark.Convert`, so single newlines survive as `<br>`
instead of relying on CSS to preserve source whitespace. The sanitize step is
untouched; `bluemonday.UGCPolicy()` already permits `<br>`.

Verified: `make ci` green (123 keys in sync), plus a new
`TestToSafeHTML_HardWraps` asserting a single newline yields one `<p>` with a
`<br>` and a blank line yields two `<p>`s with none — proven non-vacuous with
`scripts/without.sh internal/markdown/markdown.go`, which fails on
`<p>first line\nsecond line</p>` without the change. In the browser at 324×756,
on the seeded Kirkjufell location (a paragraph, an `<h2>`, a paragraph): computed
`white-space` is now `normal` and the paragraph-to-`<h2>` gap measures **20px,
down from the 58px** the backlog entry recorded — the whole note is 138px tall.
The hard-wrap half was checked against the live API: PATCHing notes to
`line one\nline two\n\nsecond para` returns
`<p>line one<br>\nline two</p>\n<p>second para</p>`, and the seeded notes were
PATCHed back afterwards (confirmed byte-identical to the original
`notes_html`).

---

## 2. Three one-line consistency fixes

Batched because each is a couple of lines and none warrants its own commit.

- **Upload button variant.**
  [document-list.js:57](web/js/components/document-list.js#L57) renders
  `btn btn-secondary btn-row` for what is that row's primary action, while the
  new-checklist button on an identical input row is `btn btn-primary
  btn-collapse` ([checklist-list.js:22](web/js/components/checklist-list.js#L22)).
  Make Upload match. This also changes the staging-mode ("Add") variant of the
  same button, which is correct — it is likewise its row's primary action.
- **Category/Type separator.**
  [location-view-page.js:57-61](web/js/pages/location-view-page.js#L57-L61)
  emits `.category-label` and `.type-label` as bare adjacent spans, so a
  landmark reads "Site landmark" — one broken phrase with mismatched
  capitalisation. Add a separator between them, reusing the `trip-summary__dot`
  idiom (`<span class="dot-sep" aria-hidden="true">·</span>`), and normalise the
  type's display capitalisation in CSS (`text-transform: capitalize` on
  `.type-label`) rather than mutating the stored value. Deriving Type from a
  per-category list stays deferred; the backlog entry shrinks to that half.
- **Stale i18n key.** `trip.overview.image` names the Overview tab removed in
  Stage 05; its value ("Cover photo" / "Titelbild") is correct. Rename the key to
  `trip.settings.image` in `web/locales/en.json:95` and `web/locales/de.json:95`
  and at both call sites: [settings-tab.js:26](web/js/pages/settings-tab.js#L26)
  and [trip-editor-page.js:41](web/js/pages/trip-editor-page.js#L41).

**Verify.** `make ci` — i18n parity catches a half-done rename. Playwright: the
Files tab's submit button carries `btn-primary`; the location view's meta row has
a separator between category and type; no page renders a raw key. Plus
`grep -r 'trip.overview.image'` returning nothing.

Closes two "Bugs and rough edges" entries and one third of the identifier-sweep
entry; rewrites the Category/Type entry down to its remaining half.

**Done.** All three landed, with **one deliberate deviation**: the Upload button
became `btn btn-primary btn-row`, *not* `btn-collapse` as this plan said. The
plan was wrong to propose that half — `.btn-row` opts out of `.btn-collapse` on
purpose ([base.css:138-142](web/css/base.css#L138-L142)), because these add-rows
stack full-width under 640px where a bare "+" spanning the whole row reads as a
stray button rather than that row's action. That comment even names "Add file"
as one of its cases. The backlog entry only ever asked for the two rows to agree
on *variant*, so only `btn-secondary` → `btn-primary` changed, and the "New
checklist" button keeps its own `btn-collapse` (a text input beside it, not a
file picker plus a note field).

The category/type separator is a `.meta-sep` span (`aria-hidden`, matching the
`trip-summary__dot` idiom) plus `text-transform: capitalize` on `.type-label` —
display only, the stored value stays exactly as the user typed it. Unlike
`.trip-summary__dot` it is *not* hidden under 640px, since two short words never
need to stack. `trip.overview.image` → `trip.settings.image` in both locale files
and both call sites.

Verified: `make ci` green (123 keys still in sync — the rename is symmetrical).
In Firefox at 324×756: the meta row reads "Site · Landmark" with the separator
between the two labels (`order: dot, category-label, meta-sep, type-label`), 8px
on each side of it, both on one line, `text-transform: capitalize` computed, and
`scrollWidth === clientWidth` so nothing overflows. The Files-tab submit button
computes `btn btn-primary btn-row`, `rgb(37, 99, 235)` on white text at 44px
tall, with its "Upload" label still laid out — the check that it did *not*
silently collapse. The renamed key was checked on both call sites and in both
locales: the settings tab's headings read Basic info / Cover photo / Delete this
trip and Basisinformationen / Titelbild / Diese Reise löschen, the new-trip
editor reads Cover photo, no `[data-i18n="trip.overview.image"]` survives
anywhere, and a raw-key regex sweep over the rendered text finds nothing — the
check that a renamed key left nothing dangling in either file.

---

## 3. Cover photo on the trip detail header

`trip.preview_image_url` is consumed in exactly two places today — the Settings
card ([settings-tab.js:44](web/js/pages/settings-tab.js#L44)) and the trips-list
card ([trips-page.js:35](web/js/pages/trips-page.js#L35)). So a trip's cover
photo is settable and previewable but invisible on the trip itself, which is what
removing the Overview tab in Stage 05 took away.

- In `render()`
  ([trip-detail-page.js:38-71](web/js/pages/trip-detail-page.js#L38-L71)), emit a
  cover image directly above `.page__header` when `trip.preview_image_url` is
  set, and nothing at all when it isn't — no placeholder box.
- Shape: full-width banner, fixed aspect ratio, `object-fit: cover`; new
  `.trip-detail__cover` rules in `base.css` beside the other trip-detail ones.
  `alt=""` — it is decorative, the `<h1>` right below already names the trip.
- Keep it inside `render()` so the Settings tab's `onTripUpdated` callback
  ([trip-detail-page.js:127-129](web/js/pages/trip-detail-page.js#L127-L129))
  refreshes it for free when the photo changes.

**Verify.** Set a cover photo on a seeded trip, then assert via Playwright that
`.trip-detail__cover` exists with a non-empty `src`, that it does not overflow
its parent at 324×756, and that a trip *without* a photo renders no such
element.

Closes the "cover photo isn't shown anywhere on its default view" entry.

**Done.** Landed with **one addition to scope**: there was no test data to see it
with. No scenario set a trip cover photo or an item image, so this milestone
would have been verifiable only by hand-uploading a file every time — and that
same gap is a separate backlog entry ("the UI sweeps only measure what the seed
actually renders", which named `.image-field__preview`,
`.itinerary-entry__thumb` and the location card's thumbnail as never measured by
anything). So the seeder grew image support first.

Two fixture JPEGs (`cmd/seed/images/`, ~343×200, ~19KB each) are cropped from a
contact sheet the repo now keeps at `docs/plans/preview-images.jpg` for
provenance, the same way `caravel-logo-drafts.png` is kept. They are embedded
with `//go:embed images/*.jpg` — the precedent being `internal/db`'s migrations —
so `go run ./cmd/seed` works from any directory. A new `addImage` helper puts
them through `imaging.DecodeAndResize`, the *same* call `handleUploadMedia`
makes, rather than copying bytes to the blob store directly: a seeded asset is
then byte-for-byte what uploading that file would have produced, content type
and dimensions included, instead of exercising a path no real upload takes. The
`full` scenario alone gets them (Godafoss as the trip cover, Moraine Lake on
Kirkjufell), deliberately — the trips list now shows one card with a thumbnail
and six without, so the no-image path stays covered.

The cover itself renders in `render()` between the back link and the header, with
`src` assigned as a property alongside the existing `textContent` assignments for
title and subtitle rather than interpolated into the template — this file has no
local escape helper and now needs none. `alt=""`, decorative. CSS gives it
`aspect-ratio: 16 / 6` with `object-fit: cover` so a portrait upload can't push
the tabs off the first screen, `max-height: 14rem` to cap it on wide viewports
(16/6 of a 60rem column would otherwise be a ~340px band), and it joins the
`grid-column: 1 / -1` list in the ≥768px sidebar grid.

Verified: `make ci` green, and `make test-ui` 9/9 — which now genuinely measures
the image elements that had only ever rendered their empty state. In Firefox at
324×756 the banner is 292×110 with `object-fit: cover`, above the `<h1>`, inside
its parent, no document overflow, tabs still at y=472 on the first screen; at
1280×800 it is 928×224 (the `max-height` cap doing its job on what 16/6 would
have made 348px) with `grid-column: 1 / -1` computed. A trip *without* a cover
renders zero `.trip-detail__cover` elements and no placeholder `<img>` at all.
The live-update path was exercised through the UI, not asserted from the code:
clicking Remove in the Settings tab's Cover photo card dropped the banner
immediately with no reload and the page still on the Settings tab, and PUTting
the asset back brought it straight back — the `onTripUpdated` callback carrying
it, as intended.

---

## 4. Collapse past and empty itinerary days

`renderDay` ([itinerary-tab.js:59-115](web/js/pages/itinerary-tab.js#L59-L115))
emits every day as a fully expanded `<div class="itinerary-day">`, so a 10-day
trip is 10 open cards and one unbroken scroll. There is **no `<details>` or
`<summary>` anywhere in the tree** today — no existing disclosure pattern and no
`[open]` styling — so this milestone introduces both.

- Make the day card a `<details class="itinerary-day">`, with the existing
  `.itinerary-day__header` content moving inside `<summary>`.
- Open-by-default rule: a day is open if it is today or later **and** has
  content, plus the nearest upcoming day even when empty. Past days and empty
  days start collapsed. A trip with no dates set has no "today" to anchor on, so
  open all of its days — the same special case `isRemovable`
  ([itinerary-tab.js:53-57](web/js/pages/itinerary-tab.js#L53-L57)) already
  makes for that trip shape.
- Give `<summary>` a useful collapsed line: the date as now, plus an entry count,
  so a closed day isn't opaque. New i18n keys in **both** locales.
- Two gotchas. The remove-day button lives in the header and must not toggle the
  disclosure (`stopPropagation` plus `preventDefault` on its click), and
  `summary` needs `min-height: var(--tap-min)` to stay inside the 44px guideline
  `routes.spec.js` asserts.

**Verify.** Playwright on a seeded multi-day trip: `details[open]` vs `details`
counts prove past/empty days start closed; clicking a `summary` flips `open`;
the remove button does not; `summary` measures ≥ 44px at 324×756. Then
`make test-ui` — the heading-outline spec walks `<h2>`s that a collapsed
`<details>` now hides, so expect to force-open or adjust that spec.

Closes the `<details>` disclosure entry.

**Done.** Day cards are `<details>` with the header as `<summary>`, opened by the
planned rule, plus three things the plan didn't anticipate.

First, **the open set is owned by the user after the first render.** The rule
only seeds it. Adding or removing a day re-renders the whole list, and without
this any day the user had expanded would have snapped shut underneath them; a
`toggle` listener keeps an `openDates` set (keyed by date, since days get
inserted in the middle) and a newly added day is added to it — the user just
asked for that day, so they are about to put something on it.

Second, **a trip entirely in the past would have rendered as nothing but closed
rows**, which reads as a page that failed to load rather than as a summary. If
the rule opens nothing, the last day opens as "where the trip got to".

Third, **the copy had to get shorter.** "Nothing planned" as the collapsed badge
was wide enough at 324px to push the date onto a second line, so every collapsed
row was two lines of wrapped date — worse than what this milestone set out to
fix. It is "Empty" / "Leer" with `white-space: nowrap`, and the collapsed rows are
single-line again (h2 22px, summary exactly 44px). Non-empty days show a
pluralized count (`itinerary.entryCount` / `_plural`, three new keys per locale).

The chevron is drawn from the sprite rather than left to the UA marker, because a
flex `<summary>` loses the native triangle, and `list-style: none` plus the
`::-webkit-details-marker` reset keeps that consistent across engines. The
remove-day button stays inside the summary with `preventDefault` +
`stopPropagation` — a nested interactive control, but moving it into the body
would mean expanding a day to delete it. The plan's predicted problem with the
heading-outline spec **did not materialize**: the `<h2>` moved into the
`<summary>`, which is always visible, so a collapsed day still contributes its
heading and the outline is unchanged.

`tests/ui/routes.spec.js` gained `summary` in its tap-target selector, since
`<summary>` is now a real control and the 44px claim would otherwise rest on one
manual measurement.

Verified: `make ci` (126 keys in sync) and `make test-ui` 9/9. The new selector
is provably not vacuous — `scripts/without.sh web/css/base.css -- make test-ui
GREP="tap target"` fails with 12 summaries at 22px, naming them by selector, so
CI would catch the min-height being lost. In Firefox at 324×756 on the `full`
trip: 4 days, 2 open (both future days with content), 2 closed, every summary
44px, no overflow, page 1399px against 1745px with everything open — **346px, a
fifth of the scroll, gone on a 4-day trip**. Clicking a closed summary opens it
with notes and entries actually rendered (not just an attribute flip) and hides
its badge. On `out-of-range-days` (a trip whose range is entirely past, with a
day dated today): the four past days start closed and today's opens. Clicking
remove on that day raised the confirm dialog **without folding the day shut** —
the specific gotcha — and Cancel left all 5 days intact. Adding 2026-08-25 kept a
manually-opened past day open and arrived open itself; removing it again needed
no confirmation and left the seed as it was. The no-date-range branch has no
seeded days to exercise it, so two past days were created on the `no-dates` trip
by hand: both opened despite being past, proving the branch, and both were
deleted afterwards (0 days remain). German copy checked in the same pass: "2
Einträge", "1 Eintrag", "Leer", no raw keys.

---

## 5. Tooling: check `sw.js`, and stamp the build SHA

Two independent holes, both small, both about knowing what you are actually
running.

- **`web/sw.js` is never syntax-checked.**
  [scripts/check_js.sh:32](scripts/check_js.sh#L32) walks `web/js` only and the
  service worker sits one level up, so a syntax error in it reaches the browser
  with `make ci` green — the same class of hole Stage 08 Milestone 1 closed, one
  directory over. It needs the *opposite* parse mode: `app.js` registers it
  without `{type: "module"}`, so it is a classic script and script-mode
  `node --check` is correct. Add a second pass — keep the module loop as-is, then
  check top-level `web/*.js` in script mode, with its own count guard and its own
  summary line so a future move of `sw.js` fails loudly instead of silently
  checking zero files. Extend the header comment to explain why there are two
  modes.
- **Nothing carries a version.** The startup log
  ([cmd/caravel/main.go:46](cmd/caravel/main.go#L46)) prints port and driver, and
  `/api/health` ([internal/httpapi/router.go:173-182](internal/httpapi/router.go#L173-L182))
  writes a literal `{"status":"ok"}` byte-by-byte. Add an `internal/buildinfo`
  package with `var Version = "dev"` (a package, not a `cmd` const, because the
  health handler needs it too), stamp it from the Makefile `build` target
  ([Makefile:33-34](Makefile#L33-L34)) via
  `-ldflags "-X .../buildinfo.Version=$(git rev-parse --short HEAD)"`, log it in
  the startup banner, and add a `version` field to the health response —
  switching that handler to `writeJSON` while there. This is the other half of
  the stale-binary problem `make dev-marker` left open: no marker string to
  invent, and any test can ask which build it is talking to.

**Verify.** Introduce a deliberate syntax error into `web/sw.js` and confirm
`scripts/check_js.sh` fails, then passes again once removed — a passing run
proves nothing on its own here. For the SHA: `make build && ./bin/caravel` shows
it in the banner and `curl -s localhost:8080/api/health` returns it; `make dev`
uses `go run` with no ldflags, so confirm it degrades to `dev` rather than
erroring.

Closes the `sw.js` and startup-banner entries.

**Done.** Both landed, and the `sw.js` half turned out to be a trap worth
recording.

**`node --check <path>` is not script mode on Node 22.** The plan (and the
backlog entry) both said to use it, on the reasoning that a path argument means
CommonJS. It does not any more: Node 22 detects module syntax and silently
re-parses as ESM, so an `import` statement added to `sw.js` **passed** that check
— while the browser, loading it via `register("/sw.js")` with no
`{type: "module"}`, would refuse it outright. Caught by testing the mode rather
than only the failure, and the pass now uses `node --input-type=commonjs --check`
over stdin, mirroring the module pass for the same reason: the mode has to be
stated, not inferred. Second counter, second zero guard, so moving `sw.js` under
`web/js` fails loudly rather than silently checking nothing.

For the version: `internal/buildinfo` holds `var Version = "dev"`, stamped at
link time. `scripts/version.sh` owns the string (short SHA plus `-dirty`,
`unknown` outside a checkout) because the Makefile and `scripts/dev_server.sh`
both start servers and would otherwise be free to disagree — `dev-restart` runs
its own `go run`, so without that the restarted dev server would have reported
`dev` while `make dev` reported a SHA. `run`, `dev` and `build` all stamp it; the
startup banner logs it; `/api/health` returns it (now via `writeJSON`, with a
`healthResponse` struct, instead of hand-written bytes).

Added beyond the plan: **`make dev-version`**, which asks the running server what
build it is and compares with the tree. That is the payoff the backlog entry was
after — `dev-marker` answers the same question but only with a marker string you
have to invent per test, whereas the stamped version is always present. Its
limitation is documented in the Makefile: two `-dirty` versions matching means
"same commit, both dirty", not "same code", so uncommitted edits still want
`dev-marker` with a real string.

Verified, and precise about what each check proves:

- **The `sw.js` pass is not vacuous, in both directions.** A plain syntax error
  (`const broken = ;`) fails it, naming the file. An `import` statement fails it
  too — the check that the *mode* is right, and the one that would have passed
  under `node --check <path>`.
- **The stamping is verified at runtime, four ways**, because that is the only
  thing that can prove `-ldflags` works: `make build && ./bin/caravel` logs
  `caravel ea02bf2-dirty listening on :8099` and `/api/health` returns
  `{"status":"ok","version":"ea02bf2-dirty"}`; a bare `go build` with no ldflags
  reports `dev` in both places, degrading rather than erroring; a
  `make dev-restart` through `dev_server.sh` reports the stamped version, not
  `dev`; and `make dev-version` prints "matches this tree", reports "the server
  is stale" against the unstamped binary (exit 1), and "no server answered"
  against a dead port (exit 1).
- **The Go test guards the response *shape*, not the stamping** —
  `TestHealthReportsStatusAndVersion` asserts 200, `application/json`,
  `status: "ok"` and a non-empty `version` equal to `buildinfo.Version`. Under
  `go test` there are no ldflags, so both sides are `"dev"`: this test would not
  notice the stamping breaking. Worth stating plainly, because
  `scripts/without.sh internal/httpapi/router.go` "passes" here only by failing
  to *compile* — exactly the false-proof its own backlog entry describes, so it
  is not evidence of anything.
- `make ci` green (31 modules + 1 script, 126 keys) and `make test-ui` 9/9.

---

## 6. UI suite: stop the false 429s, and catch content overflowing its box

Three suite gaps, all from "Testing, CI and dev tooling".

- **Shared `storageState`.** `login()`
  ([tests/ui/helpers/scenarios.js:94-107](tests/ui/helpers/scenarios.js#L94-L107))
  POSTs `/api/auth/login` once per spec — 9 per run against
  `newLoginLimiter(10, time.Minute)` per IP — so two runs inside a minute, or one
  run alongside a hand-written script, trips HTTP 429. The specs then render the
  login page and fail on unrelated assertions, with a message that actively
  misleads ("has `make dev-reset FORCE=1` been run?" when the seed is fine).
  `storageState` appears **nowhere** in the tree today. Add a Playwright `setup`
  project that logs in once and writes `tests/ui/.auth/demo.json`, make the
  `firefox` project depend on it with `use: { storageState }`, and reduce
  `login()` to its remaining jobs — `installFetchTracker`,
  `blockExternalRequests`, `goto("/")` — with no POST. Have the setup name 429
  explicitly. Gitignore `tests/ui/.auth/`.
- **Per-element content overflow.** The overflow test
  ([tests/ui/routes.spec.js:41-59](tests/ui/routes.spec.js#L41-L59)) measures
  only document-level `scrollWidth - innerWidth`, and its per-element loop runs
  *only once the document already overflows* — which is exactly why six
  overlapping tab labels passed in Stage 09 Milestone 6. Add an unconditional
  sweep in the same `page.evaluate` for elements where
  `scrollWidth > clientWidth + 1`, reported with tag and class like the existing
  widest-offender path.
- **Tap-target width.** The tap-target test asserts only `rect.height < min`
  ([routes.spec.js:74-158](tests/ui/routes.spec.js#L74-L158)); width is never
  checked, which is how a 45px trigger inside a 58px cell slipped through. Assert
  it alongside.

**Expect real failures.** Both new assertions will likely find existing
offenders. Deliberately scrollable regions (the map, any `<pre>`) are legitimate
and want an explicit commented exclusion like the three `scope()` already
carries; anything else is a bug to fix here. If the list is long, fix what is
cheap and move the rest to `todo.md` behind a documented allowlist rather than
quietly weakening the assertion.

Also check in the Stage 09 More-menu interaction spec, which exists only as an
uncommitted script today. The backlog names it as the cheapest first interaction
spec — a page load, a few clicks and computed-style reads, mutating nothing.

**Verify.** `make test-ui` green, then run it twice inside one minute — no 429.
Delete `tests/ui/.auth/` and re-run, to confirm the setup project regenerates it.

---

## 7. The Files tab shows location-attached files

The only DB-touching milestone, sequenced last. `ListTripDocuments`
([documents.sql:9-10](internal/db/sqlc/queries/documents.sql#L9-L10)) filters
`AND item_id IS NULL`, so the trip Files tab only ever shows files attached
directly to the trip — even though every document row already carries the trip's
`trip_id` regardless (set in `uploadDocument`,
[internal/httpapi/documents.go](internal/httpapi/documents.go)), so no join
through `items` is needed to *find* them. *Repro:* the `full` seed has
`trip-notes.txt` at trip level and `hotel-booking.txt` on the Foss Hotel
location; the tab shows only the former.

Display shape is already decided: one flat list, sorted by upload date, with a
small inline label on location-attached files naming their location ("Hotel
booking.pdf — Foss Hotel Reykjavik"); trip-level files unlabeled.

- **Query.** Drop the `item_id IS NULL` filter and `LEFT JOIN items i ON
  i.id = d.item_id` selecting `i.title AS item_title`. `LEFT`, not `INNER` —
  trip-level rows have a NULL `item_id` and must survive.
  `ListItineraryEntriesByTrip`
  ([itinerary_entries.sql:9-17](internal/db/sqlc/queries/itinerary_entries.sql#L9-L17))
  is the exact precedent, down to how its joined row maps to a `*Detail` domain
  type.
- **Regenerate.** `sqlc generate` by hand from `internal/db/sqlc/` — **both**
  dialects, per `CLAUDE.md`. Touches
  `internal/db/sqlc/{sqlite,postgres}/gen/documents.sql.go` and both
  `querier.go`.
- **Domain + adapters.** The joined query returns a new row struct, so add a
  `DocumentDetail` (embedding `Document`, plus `ItemTitle *string`) beside
  `ItineraryEntryDetail`, change the signature in
  [store.go:235-236](internal/db/store.go#L235-L236) — and fix the comment there,
  which currently documents the `item_id IS NULL` behaviour being removed — then
  update both hand-written adapters,
  [sqlite_store.go:614](internal/db/sqlite_store.go#L614) and
  [postgres_store.go:677](internal/db/postgres_store.go#L677). This is the real
  cost of the milestone, not the sqlc step.
- **API.** Add `item_title` to `documentResponse`
  ([documents.go:58-68](internal/httpapi/documents.go#L58-L68)) via a second
  mapper for the detail type, leaving `documentToResponse` alone for the
  item-level and upload paths.
- **Frontend.** `renderDocumentList`
  ([document-list.js:22](web/js/components/document-list.js#L22)) renders one
  homogeneous list of identical `<li>`s. Add a `.document-source` span to the
  uploaded-row template when `row.item_title` is set, escaped via the existing
  local `escapeHtml` like every other interpolated field, plus CSS beside
  `.document-note`. No new labeled-list *mode* is needed given the decided flat
  shape — cheaper than the backlog entry assumed.
- **Delete stays correct.** `DeleteDocument(id, tripID)` already scopes by trip,
  so deleting a location-attached file from the trip tab works unchanged — worth
  an explicit test rather than an assumption.

No migration: this is a query change only.

**Verify.** `make ci`, plus a Go test asserting the query returns both seeded
documents with `item_title` set on exactly one and NULL on the other. Then
Playwright against the `full` scenario: the Files tab lists 2 rows,
`hotel-booking.txt` carries a `.document-source` reading "Foss Hotel Reykjavik",
`trip-notes.txt` carries none, and the location's own Files list is unchanged.
Exercise the Postgres path too — sqlc generates two dialects and only one runs by
default.

---

## Build order

1 → 2 → 3 → 4 → 5 → 6 → 7. Cheapest and most visible first; the only DB-touching
milestone last, so a stall there blocks nothing. 6 sits before 7 deliberately, so
its new assertions guard 7's frontend change on arrival. Otherwise every
milestone is independent of the others.

## Workflow

Per `CLAUDE.md`: one milestone at a time — implement, verify with `make ci` plus
a real behavioural pass (assertions over screenshots), add a "**Done.**"
paragraph to this file and update `docs/plans/todo.md` in both directions,
commit (one commit per milestone; follow-ups get their own "... follow-up:"
commit), make sure `make dev` is running, then stop and wait for the go-ahead.

## Verification (stage level)

- `make ci` green before every commit — build, vet, JS syntax, i18n parity,
  `go test`.
- `make test-ui` green from Milestone 6 onward, and re-run after 7.
- Mobile pass at 324×756 via the Playwright MCP tools against a running
  `make dev`, for Milestones 1, 3 and 4 in particular.
- End of stage, `todo.md` should be materially shorter: eight entries fully
  closed and two rewritten smaller.
