# Caravel — TODO / Backlog

Everything below is **not yet built**. This is the single backlog: raw input for
planning the next stage, not a prioritized or scheduled list. Entries cite where
they came from, so the reasoning behind each one survives even when the person
who wrote it doesn't remember it.

Sections group by *kind of work*, not by which stage deferred something — the
per-stage buckets this file used to have stopped meaning anything once items
started being partly resolved several stages later.

**Conventions.** Add new notes straight into the section they belong to. When a
milestone implements an entry, delete it; when a milestone changes what's left
of one, rewrite it. A stale "still outstanding" item that was quietly built is
worse than a missing one, so both directions matter. Anything captured in
scratch form elsewhere (`notes.md` and the like) gets folded in here and the
scratch file removed, so there's only ever one list.

---

## Bugs and rough edges

Things that are wrong today, each confirmed against the current source.

- **A cover photo set by URL on the *new trip* form is only validated
  server-side at Create time.** (Stage 07; half-fixed in Stage 09 Milestone 4.)
  The copy half is done: both the existing-trip card and the create form show
  translated messages (`image.fetchFailed` / `image.uploadFailed`) with the Go
  error going to `console.error` instead of into the UI, and the create form's
  failure is an in-app dialog rather than a native `alert()`. What's unchanged is
  the *timing*: the URL is staged locally, the trip is created, and only then
  does the fetch happen — so the dialog still arrives after a create that partly
  succeeded. A real fix means validating at "Set image" time, which needs either
  a trip-independent validation endpoint or accepting the browser's own `<img>`
  load as the check (Stage 07 Milestone 9's preview-error handler already does
  exactly that inline, which softens this a lot — a URL the browser can't load
  is flagged in the card before Create is ever pressed).
- **Category is a fixed `<select>` while Type is free text.** (Stage 07.) Stage
  10 Milestone 2 fixed the *display* half — the location view now reads
  "Site · Landmark", separated and capitalized in CSS — but Type is still an
  unconstrained text input, so nothing stops two locations in the same category
  carrying "hotel", "Hotel" and "hostel". What's left is the real question:
  derive Type from a per-category list (a `<select>` that changes options with
  the category, or a `<datalist>` of suggestions that still allows anything).
- **The cover photo and files are still a post-create upload.** All that is left
  of "create-mode writes aren't atomic" (Stage 06 Milestone 4) after Stage 09
  Milestones 1–2 made the item and its location/links/dates one transactional
  request. These two can't ride in a JSON body, so a new location stages them in
  memory and `flushUploads()` writes them once the create returns an ID. If that
  fails the location exists without its photo or files; unlike before, the
  failure reports inline in the Basic info card and the page stays put so it can
  be retried. Closing the gap entirely would mean a multipart create endpoint —
  not obviously worth it.

---

## Planned features

Direction already agreed or obviously wanted; none of it built.

- **Search, filter and sort on the trips list.** Confirmed absent:
  `trips-page.js` has no search input, filter or sort control, and
  `ListTripsForUser` (`internal/db/sqlc/queries/trips.sql:16` — Stage 14
  replaced `ListTripsByOwner` with it) has a fixed `ORDER BY t.created_at DESC`
  with no parameters for sort field/direction or a title predicate. Needs a
  frontend control, and a decision about whether the filtering happens in the
  browser (the API returns every trip unconditionally today, which is what
  `locations-tab.js` relies on) or in the query.
- **A markdown preview for location notes.** Notes are authored in a plain
  `<textarea>` (`location-form.js`) and only rendered after saving, on the view
  page — so formatting is written blind. Wants a preview (side-by-side, or a
  toggle) using the same server-rendered `notes_html` the view page uses, or a
  client-side render if a round trip per keystroke is too much.
- **Itinerary entry reordering.** `itinerary_entries.sort_order` exists in the
  schema and Stage 01 floated "native drag-and-drop or a minimal pointer-events
  reorder", but `itinerary-tab.js` has no reordering UI at all — entries render
  in whatever order the API returns, with no drag handle and no up/down buttons.
- **"Add to every day of this stay."** (Stage 01.) For multi-day items such as a
  3-night hotel, driven by the item's date range. Today adding an item to a day
  is manual, one day at a time, via each day's own dropdown.
- **An account settings screen, reached from the user menu.** (From the user's
  notes; this decides the "where does it live?" question the entry below used to
  carry.) Settings sits in the same menu as "Log out". The screen, the appearance
  and language controls and password changing all landed in Stage 12
  (Milestones 2-5). What it still wants:
    - a **profile picture**. This one has a schema wrinkle: `media_assets.trip_id`
      is `NOT NULL` and cascades from `trips`, so a user-scoped image has no
      valid home today and deleting a trip would take an avatar with it. Needs a
      migration — nullable `trip_id`, or a `user_id` column, or a separate table —
      before the existing upload pipeline can be reused.
  Also still open, now that two preferences exist to have the argument about:
  appearance and language are both stored **per browser**
  (`localStorage["caravel.theme"]` / `["caravel.locale"]`), which was the
  deliberate Stage 12 decision. Making either follow the account means a column
  on `users` and an endpoint to write it — worth answering once for both, not
  twice.
- **A trip journal with photos.** (Stage 01.) A `journal_entries` table
  (trip_id, date, body markdown) reusing the existing `media_assets` pipeline
  for photos.
- **Better Google Maps interoperability.** (From the user's notes.) Two halves.
  *Inbound:* pasting a shortened Google Maps link such as
  `https://maps.app.goo.gl/xfB9TzpFos2N4oAW8` into a location should resolve to
  coordinates — which means following the redirect server-side (the short form
  carries nothing parseable) and pulling lat/lng out of the expanded URL, so it
  wants the same proxy-and-limiter treatment `/api/geocode` got in Stage 13
  Milestone 5. *Outbound:* the popup's and the location view's "View on Google
  Maps" links are a `?q=lat,lng` **search**, not the place itself, so they land
  on a dropped pin rather than on the hotel's own Google entry with its hours
  and reviews. Linking the actual place needs a place ID, which Caravel does
  not have and cannot get from OSM — so this half is blocked on either storing
  a user-pasted Google URL per location or accepting the search link as good
  enough.

---

## Ideas and open questions

Not yet decided; each needs a call before it is work.

- **Re-evaluate Leaflet+OSM vs. Google Maps.** Stage 01 weighed this and chose
  Leaflet+OSM (no API key or billing, and the tile URL is a one-line swap
  later). This is a standing invitation to make that decision again
  deliberately, rather than drifting into re-litigating it.
- **LLM-assisted metadata fetching for locations** — e.g. a small local model
  plus web search to auto-fill an item's details from its title. Noted as "not
  that important for the MVP" by the user, and untouched since.
- **Federation between self-hosted instances.** (Stage 01.) Real sync-protocol
  design still needed; v1 only avoided the integer-PK and local-only-ID mistakes
  that would have made it harder later.
- **Duplicating a checklist always resets the ticks.** (Stage 15 Milestone 1.)
  The other reading — copy the checked state too, for splitting one list in two
  — was considered and deliberately not built: it wanted a second menu item on
  every card, and reuse across trips is the case the backlog actually described.
  Only worth revisiting if somebody actually tries to split a list and finds
  retyping the ticks annoying. The handler is one flag away from either
  behaviour, so the cost of changing the answer later is a parameter, not a
  rewrite.
- **Is trip-visible the right default for an upload?** (Stage 14 Milestones 7
  and 10.) A new file defaults to `visibility = trip` and a new checklist to
  `shared`, with the choice offered at creation time and the control hidden
  entirely on a solo trip. The argument for it: an invisible privacy default
  produces "where did my upload go?" rather than safety, and the personal case
  is announced by its own section in the list rather than a badge you have to
  notice. The argument against is unchanged and real — the failure mode is
  asymmetric, since over-sharing a boarding pass cannot be undone by later
  fixing the default. Deliberately left as-is pending actual use rather than
  re-argued from first principles; what would settle it is someone sharing a
  trip and reporting which mistake they actually made.
- **Per-visibility media assets** (location and trip cover images). Files and
  checklists carry `owner_user_id` + `visibility` after Stage 14; media assets
  do not. Probably correct rather than an omission: a trip cover photo is
  inherently trip-wide, so "personal" has no meaning for it. Worth a second
  look only if location *galleries* ever hold more than one image per location,
  where a personal photo would start to make sense.

---

## Multi-user and sharing

Stage 14 built this cluster: roles, membership, per-visibility files and
checklists, and account administration. The entries left are the ones it
deliberately left out, and they no longer depend on each other.

- **Trip ownership transfer.** (Stage 14 Milestone 10.) `trips.owner_id` is the
  only record of who owns a trip and `trip_members` cannot represent an owner by
  construction, so a transfer is a single UPDATE plus a members row for the old
  owner — the schema is ready. What is not decided: what happens to the previous
  owner (demoted to editor, or removed outright), and what happens to their
  personal files and checklists on a trip that is no longer theirs. Same question
  the member-removal dialog answers today, so copy that decision rather than
  invent a second one.
- **Public shareable links.** An unauthenticated read-only trip view via a
  token. Needs a `share_links` table (token, trip_id, scope, expires_at) plus an
  unauthenticated route variant. Cheaper now than when this was written: Stage 14
  gave every trip-scoped handler a minimum role through one seam
  (`internal/httpapi/authz.go`), so a link-holder is a synthetic `viewer`
  resolved from a token instead of a session, and read-only mode already exists
  in the frontend. The genuinely new part is the token lifecycle, not the
  authorization. Note that `scope` has to answer the visibility question too: a
  public link must never expose a `personal` file or checklist, whoever created
  the link.
- **Invite links / joining by token.** Adding a member needs their exact
  username today (Stage 14 Milestone 3), which is fine on a self-hosted instance
  where you know who you are inviting and worse than it needs to be otherwise.
  Overlaps the share-link token model above, so the two want doing together.
- **Expenses / cost-splitting.** (Stage 01.) A new `expenses` table referencing
  `trip_id` and optionally `item_id`, with no changes to existing tables. The
  *splitting* half only means anything once several people share a trip, which
  is why it sits here.

---

## Consistency and cleanup

No user-visible change; the point is to stop the tree drifting further out of
step with itself.

- **Identifier sweep: "item" → "location".** Stage 05 fixed the user-visible
  item/location copy, so what's left is entirely below the surface: the whole
  `item.detail.*`/`item.category.*`/`item.deleteConfirm` i18n namespace is still
  item-flavoured despite `location.form.*`/`location.editor.*` having migrated,
  and on the JS side `location-form.js` exports `renderItemForm`,
  `locations-tab.js` exports `renderItemsTab` and uses
  `data-action="new-item"`, and the list renders `<item-card>`. Note the API and
  schema say `items` too (`/api/items/{id}`, the `items` table), so this one has
  the same choose-your-depth question the files rename had.
  (Two former parts of this entry are done: the stale `trip.overview.image` key
  in Stage 10 Milestone 2, and the whole "documents" → "files" rename in Stage 11
  Milestone 1 — which went all the way down to a `0006` table rename, dropped
  the `/trips/:id/documents` URL outright rather than redirecting it, and is the
  precedent to copy here.)
---

## Testing, CI and dev tooling

- **The UI suite still doesn't cover data-mutating flows, or the login pages.**
  Stage 10 Milestone 6 took the interaction half of this entry: `menu.spec.js`
  drives the tab bar's More menu (open, toggle, outside click, Escape,
  select-and-navigate, checked row) in **both locales**, so "anything behind an
  interaction" and "German copy is only eyeballed" are no longer true as blanket
  statements. Stage 11 Milestone 3 added the Files row's ⋮ menu to the same spec,
  and Milestone 5 answered the isolation question below. What remains from the
  original list:
    - **Mutating flows: the pattern exists, and two flows use it.**
      `tests/ui/files.spec.js` (Stage 11 Milestone 5) creates its own trip in
      `beforeEach`, uploads/edits/deletes inside it, and deletes it in
      `afterEach` — so a write can't leak into the shared seed, which was the
      blocker. Stage 12 Milestone 5 added `settings.spec.js`'s password change,
      which had a harder isolation problem: it cannot use the demo user at all,
      since changing a password deletes `auth.setup.js`'s shared session, so it
      drives the seeded `other` account and restores its password afterwards.
      Note what that exposed — a *silently* failing cleanup leaves the seed
      wrong for every later run, so a restore step has to assert its own
      success. Every other mutating flow is still uncovered and should copy one
      of those two shapes: Stage 09 Milestone 2's single-Save check (still not
      checked in as a spec), the location and trip editors, checklists, and the
      itinerary.
    - **The register page**, which still no spec renders. Stage 14 Milestone 9's
      `unauthenticated.spec.js` covers the *login* screen — overflow, heading,
      accessible names, tap targets, the refusal path — from a fresh
      unauthenticated context, but the register form only appears when open
      signup is on, and the seed deliberately leaves it off. Covering it means
      either flipping the setting inside the spec and restoring it (the
      `settings.spec.js` shape, with the same "a silently failing restore
      poisons the run" hazard) or a scenario that seeds an open instance.
    - **German beyond the menu.** `menu.spec.js` proves the mechanism works, but
      the sweeps themselves (overflow, headings, names, tap targets) still run in
      one locale, and German copy is the longer of the two — the case most likely
      to overflow a box.
- **Firefox-only Playwright cannot emulate a coarse pointer.** (Stage 13
  Milestone 1.) `playwright.config.js` runs Firefox alone, deliberately, but
  Playwright's `isMobile` — the option that flips `(pointer: coarse)` — is
  Chromium-only, and `hasTouch: true` does not do it. So `tests/ui/map.spec.js`
  emulates the touch device by stubbing `window.matchMedia` for that one query
  through `addInitScript`. That is honest as far as it goes (the stub replaces
  the *input*; the component's real response is what gets asserted), but it
  means no spec exercises an actual touch gesture — the "one finger scrolls the
  page, two fingers pan the map" claim is verified through Leaflet's handler
  state, not by dragging. Any code that reads `(pointer: coarse)` from here on
  inherits the same limit. A Chromium project added just for gesture specs
  would close it, at the cost of the "one browser keeps CI cheap" decision.

- **One unexplained UI-suite failure, seen once.** (Stage 13 Milestone 3.)
  `headings.spec.js` reported "view location: no headings at all" on a single
  full run and has not recurred across a dozen since. The route renders fine by
  hand and in isolation, so the likely shape is a render/fetch race that
  `gotoRoute`'s wait does not cover — which would make it another symptom of
  the in-flight-fetch entry below rather than its own bug. Recorded because a
  once-seen failure that nobody writes down is a failure nobody looks for the
  second time.

- **The suite still needs its in-flight-fetch counter, even now that routes show
  a loading state.** Stage 09 Milestone 5 gave every route a `common.loading`
  line, which fixes the user-facing half of the old "empty shell" problem but
  not the suite's: a loading line carries no `<h1>`, so `gotoRoute` must still
  wait for fetches to settle before asserting heading outlines, and that wait is
  an injected `window.fetch` wrapper (`tests/ui/helpers/scenarios.js`) rather
  than anything the app exposes. A `data-loading` attribute on `#app`, or a
  "ready" event, would let the suite wait on the app's own state instead of on
  monkey-patched plumbing. One component now does exactly that on its own
  account: Stage 13 Milestone 3 gave `leaflet-map.js` a `data-ready` attribute,
  set once the map has laid out and cleared while it rebuilds, and `gotoRoute`
  waits for every `<leaflet-map>` to carry it — Leaflet is lazily imported
  *after* a route's fetches settle, so "fetches quiet plus two frames" did not
  mean the map was up. That is the shape the app-wide version wants; it is one
  component rather than the shell.
- **The UI sweeps only measure what the seed actually renders.** Two 22px tap
  targets — a location's external-link row and its "View on Google Maps" link —
  survived every sweep until Stage 09 Milestone 6, because the `full` scenario
  created no item links, so those cards only rendered their empty state. One was
  found only because leftover manual test data happened to be in the database
  that run. The seeder now gives that location a link and a date, and Stage 10
  Milestone 3 closed the image half: the `full` scenario carries a trip cover
  photo and an image on Kirkjufell (embedded fixtures in `cmd/seed/images/`,
  added through the same `imaging.DecodeAndResize` path a real upload takes), so
  `.image-field__preview`, `.itinerary-entry__thumb`, the trip card's thumbnail
  and the cover banner are all swept now — and deliberately only on that
  scenario, so the no-image path stays covered too. Stage 14 Milestone 10 asked
  the same question of the screens that stage added and found the gap again: the
  `full` scenario seeded only trip-visible files and only `shared` checklists, so
  the private-files section, its lock badge and the two non-shared checklist
  states had never been drawn by any sweep. Fixed by seeding one of each — and by
  making `visibility` a *required* parameter of the seeder's `addFile` and
  `addChecklist`, so the next visibility-like column cannot be silently
  defaulted past the sweeps by a new caller. What's left is the habit
  rather than a named gap: an element that only renders when data exists is
  invisible to the sweeps until some scenario creates that data, so anything new
  wants the question "which scenario renders this?" asked of it. The known
  remaining blind spot is anything behind an interaction (menus opened, dialogs,
  forms submitted), which the first entry in this section already covers.
- **Nothing ever runs the Postgres dialect.** *(Stage 14 Milestone 3's follow-up
  produced the first measured example of this costing something, rather than
  being a theoretical risk: `SearchUsers` lowercases its pattern to match a
  `LOWER()` on the column, which is the only thing making the search
  case-insensitive **on Postgres** — sqlite's `LIKE` is already
  case-insensitive for ASCII, so removing the normalisation entirely leaves
  `TestSearchUsers` green. The assertion is still worth keeping, but it is
  documentation, not coverage.)* `sqlc generate` emits both
  dialects and `internal/db` has a hand-written adapter for each, but every test,
  the seeder and the dev server run SQLite: there is no local Postgres, no
  compose file, and no Postgres job in CI. So the Postgres half of any query
  change is verified only by compiling — which catches a type error and nothing
  else. A wrong column order, a dialect-specific NULL or timestamp difference, or
  an adapter that maps the wrong field would ship green. Noticed while changing
  `ListTripFiles` (`ListTripDocuments` at the time) in Stage 10 Milestone 7, but
  it applies to every query in the app. **Stage 14 is the strongest argument yet
  and Milestone 10 is restating it deliberately:** the stage added four migration
  pairs (`0007`-`0010`) — the largest schema change since `0001` — a new
  `trip_members` table, `owner_user_id` and `visibility` columns on two existing
  tables, and new list predicates on every one of them, and *the only evidence
  the Postgres half of any of it is correct is that it compiles*. Twice in this
  stage a hand-written adapter silently dropped a field on read in **both**
  dialects (Milestone 7, a wrong receiver name), which is exactly the class of
  bug a compile cannot see and only the SQLite tests caught. Cheapest fix that would actually mean something: a CI job with a
  `postgres` service container running `go test ./...` against it, which needs
  the test harness (`newTestServerWithStore`) to take the driver from an env var
  instead of hard-coding `"sqlite"`.
- **Contrast is measured but not asserted.** `tests/ui/contrast.js` reports
  ratios and has a `--min` flag, but nothing runs it in CI, so a regression like
  Stage 07's 2.54:1 primary button would not be caught automatically — only
  found by someone running it. Turning it into a spec needs a decision about
  which elements have a defensible threshold (decorative fills and large text
  differ), which is why it stayed a measurement tool. The two parts worth
  keeping whatever shape it takes: flattening translucent backgrounds over
  whatever is behind them (the danger tint is `rgba(...)`, so a naive reading
  measures against transparency and reports nonsense), and reaching into shadow
  roots.
- **Leaflet's own controls are below the tap-target guideline, and the sweep
  excludes them.** Stage 09 Milestone 6 raised everything Caravel owns to 44px
  and `tests/ui/routes.spec.js` asserts the guideline, but the vendored library's
  markup inside the map's shadow root is skipped by class
  (`[class*="leaflet-"]`): its zoom buttons measure 30px and the OpenStreetMap
  attribution link 14px. As of Stage 13 Milestone 3 the clipped-content sweep
  additionally skips any element *containing* a `.leaflet-container` — that is
  `.map-wrap`, whose `overflow: hidden` exists precisely to clip panes Leaflet
  parks at enormous offsets on purpose, so its content width measured the
  library rather than us. Note the older exclusion is an **ancestor** test
  (`el.closest('[class*="leaflet-"]')`), so as of Stage 13 Milestone 2 it also
  hides markup *we* own: the popup's two links sit inside
  `.leaflet-popup-content` and are therefore invisible to the sweep. They are
  measured directly in `tests/ui/map.spec.js` instead — which works, but only
  because someone remembered. Anything added to a Leaflet popup from here on
  inherits the same blind spot. Restyling a dependency's internals to satisfy our own
  sweep is the tail wagging the dog, and the attribution is conventionally
  small — but if the map becomes a primary interaction surface on phones, the
  zoom buttons are worth revisiting. (The legend, which *is* ours, was fixed in
  that milestone via `leaflet-map.js`'s own shadow styles.)
- **`scripts/i18n.py`'s dynamic-prefix rule only fires inside a `t()` call.**
  (Stage 13 Milestone 8.) `DYNAMIC_CALL_RE` matches a template literal as the
  argument of `t(`, so a key composed anywhere else is invisible: Milestone 6's
  `locateErrorKey()` returned `` `map.locate.${reason}` `` from a helper, and
  all five real reason keys were reported as unused. They were not deleted —
  the report's own warning did its job — but the next person might, and the
  fix that milestone applied (spell the keys out in a lookup object, which the
  bare-string pass *does* see) is a workaround at the call site rather than in
  the tool. Teaching the scan to follow a function that returns a composed key
  is hard; recognising a template literal assigned to a `const` whose name
  ends in `Key`, or an explicit allowlist comment, would cover the realistic
  cases.

- **`scripts/i18n.py unused` is not wired into `make ci`.** It has a `--strict`
  flag that exits non-zero, but 9 keys (the `trip.tabs.*` and `item.category.*`
  families) are only reachable via runtime-composed keys and so are unprovable
  either way by static scan. Gating CI on it would mean either false failures or
  teaching it to ignore exactly the cases a human should eyeball. Worth
  revisiting if an allowlist of known-dynamic prefixes turns out to be
  maintainable — that would make the check a real gate instead of a report.
  (As of Stage 09 Milestone 7 it reports no unused keys at all, so wiring it up
  would start from green.)
- **A test that asks the code under test what to expect proves nothing.**
  (Stage 14 Milestone 1.) `TestRoleMatrix` originally derived each expected
  status from `db.TripRole.AtLeast` — the function it was testing — so forcing
  `AtLeast` to `return true` flipped the production check and the expectation
  together and the test passed while every viewer write was being permitted.
  Fixed there by restating the role ordering independently in the test file.
  The general lesson has no home in the tree yet: nothing stops the next table
  test from computing its expectations with a production helper, and the failure
  is invisible precisely because the suite stays green. Worth a note in
  `CLAUDE.md`'s verification guidance, or a habit of break-checking any test
  whose expectation is computed rather than written down.

- **An admin password reset shows the new password in a plain text field.**
  (Stage 14 Milestone 6.) The reset uses `promptDialog`, whose input is
  `type="text"`, so the temporary password the admin types is visible on screen.
  Arguably correct — they have to read it back to the person they are resetting
  it for — but it is a decision that was made by reusing the nearest component
  rather than on purpose. Worth either an explicit `type` option on
  `promptDialog` or a note in the copy saying the visibility is deliberate.

- **Three near-identical input rules in `base.css`, with drift.**
  (Stage 14 Milestone 3 follow-up.) `.auth-form`, `.trip-form`/`.password-form`
  and `.item-form` each declare their own label-and-input styling, and they do
  not quite agree: `.auth-form` uses `border-radius: var(--radius)` and
  `font-size: 1rem` where the other two use `0.375rem` and `font: inherit`. The
  Members form was joined onto the `.trip-form` group rather than becoming a
  fourth copy, which is the right move for one form and not a fix for the
  pattern — the next form added has three plausible rules to pick from and no
  reason to prefer one. Consolidating means picking the canonical values (which
  changes how the auth pages look, slightly) and is its own change. Precedent
  for how: the same file's error-callout rule already collapsed five copies into
  one.

- **`confirmDialog` hardcodes a trash icon for every danger confirmation.**
  (Stage 14 Milestone 3.) `components/dialog.js` picks the icon from `danger`
  alone (`trash-2` when set, `check` when not), which is right for the delete
  confirmations it was written for and wrong for "Leave trip" — an action that
  removes no data and shows a bin anyway. The *label* half of the same problem
  was fixed in that milestone (both member confirmations now pass their own
  `confirmKey` instead of defaulting to `common.delete`), so what is left is one
  optional `iconName` on `confirmDialog`, plus a decision about whether
  `danger` should keep implying an icon at all.

- **Trip ownership cannot be transferred.** (Stage 14 Milestone 3.) `trips.owner_id`
  is authoritative and the owner has no `trip_members` row, which makes "exactly
  one owner" true by construction — but also means the only ways out of owning a
  trip are deleting it or leaving it in someone else's hands informally. The API
  says so explicitly: removing the owner answers 409 `owner_cannot_leave`,
  pointing at a transfer that does not exist yet. A transfer needs a decision
  about what happens to the old owner (editor? removed?) and, once Milestone 7
  lands, to their personal files on that trip.

- **`scripts/without.sh` only handles *uncommitted* changes.** By design (it
  works via `git stash push`), but "does this test actually cover the fix I
  landed last week?" needs the change staged as uncommitted first. A
  `--commit <sha>` mode that reverse-applies a commit into the working tree, runs
  the command, then restores, would remove the manual step.
  Second limitation: it asks "does this command depend on my uncommitted
  **fix**?", which is the wrong question for "does this test catch this
  **break**?" — reverting a break restores the guard and the tests pass, so it
  answers VACUOUS while telling you nothing. A `--break` mode (apply an edit,
  expect failure, restore) would be the mirror image.
  Third, learned the hard way in Stage 09 Milestone 6: it reports success on
  *any* non-zero exit, so a command that fails for an unrelated reason (a typo
  in a `GREP` pattern, meaning Playwright ran no tests at all) reads as proof.
  Worth having it distinguish "the command failed" from "the command didn't
  run", or at least echoing enough output that the reader can tell.
  Fourth, and the most convincing disguise of that third one, from Stage 10
  Milestone 5: reverting the file under test removed a struct the *test*
  referenced, so `go test` failed to **compile** and `without.sh` reported
  "OK — genuinely depends on your change" with not one assertion having run. A
  compile error reads as a test failure in the output, so this is the case least
  likely to be noticed. Both want the same fix: tell "failed" apart from "never
  ran".

---

## Deployment and operations

Nothing here is needed to keep developing; all of it is needed before anyone
else runs this.

- **Squash the migrations before the first real release.** There are now **ten**
  files per dialect (`0001_init` through `0010_add_checklist_visibility`, in both
  `internal/db/migrations/sqlite/` and `.../postgres/`). Since nobody has
  deployed this yet, collapsing them into a single `0001_init` is safe — and
  stops being safe the moment someone has. Ten migration pairs of which nine
  exist purely as history is a lot of files for a schema nobody has ever migrated
  *from*. Not done at the end of Stage 14 as previously planned, on purpose: it
  would rewrite the schema history of four migrations written in the same stage,
  and it pairs naturally with the Postgres CI job above — squashing while
  nothing ever runs the Postgres dialect means hand-writing one large untested
  `0001_init` for it.
- **Reverse geocoding.** (Stage 13 Milestone 5.) `/api/geocode` turns a name
  into coordinates; the opposite — clicking the map and getting a suggested
  address for the `address` field — is not built. Nominatim has a `/reverse`
  endpoint, so it is a second handler on the same proxy and the same limiter,
  but it needs a decision about whether it fills the field automatically
  (surprising, and it would overwrite a hand-written address) or offers the
  result for the user to accept.

- **S3-compatible object storage.** Swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze, and
  so on); the interface already isolates callers from the backend.
- **Prometheus/OpenMetrics metrics.** A `GET /metrics` endpoint via
  `promhttp.Handler()`, outside `/api` and outside the session-auth middleware.
  Note: Stage 01's plan described that routing reservation as already in place,
  but there is no `metrics` reference anywhere in `internal/` or `cmd/` today —
  the route needs adding along with the instrumentation (HTTP request
  count/duration/status, DB query duration, upload counts/sizes, session
  counts).
- **OpenID Connect / external auth providers.** `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no provider
  integration exists yet.
- **HTTPS in deployment is now a real concern, not a future one.** Stage 13
  Milestone 6 shipped the locate control, and `navigator.geolocation` requires
  a secure context. `localhost` counts, so development and the UI suite are
  fine, but served over plain HTTP to a phone the feature cannot work at all.
  It fails honestly rather than mysteriously — `locateUnavailableReason()`
  disables the button and says the connection is the reason — but "honestly
  unavailable" is still unavailable, and this is the first feature that a
  deployment without TLS simply does not have. Worth remembering alongside it:
  `PositionOptions.timeout` is **not** honoured while a permission prompt is
  outstanding (measured in Firefox: still pending at 6s with a 3s timeout), so
  anything else that calls the geolocation API must bring its own timer —
  `web/js/geolocation.js` already does.
