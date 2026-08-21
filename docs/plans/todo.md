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

- **Use the device's location.** (From the user's notes.) Three related
  capabilities, in rough order of usefulness:
    - show the user's own position on a map;
    - centre a map on it;
    - filter the locations list by distance from it (1 / 2 / 5 / 10 / 25 km).
  Needs `navigator.geolocation`, which means a permission prompt and a
  secure context — fine on localhost, but it will not work over plain HTTP on a
  phone, so this is the first feature that pushes toward HTTPS in deployment.
  `leaflet-map.js` is attribute-driven and read-only today, so showing a
  position means teaching it a "here" marker plus an accuracy circle. The
  distance filter is cheaper than it looks: `locations-tab.js` already loads
  every item and filters client-side, so a haversine over `item.location`
  needs no backend change — but it does need a second control next to the
  existing category menu, and a decision about what to do with locations that
  have no coordinates (hide, or always show).
- **Address search for the coordinate picker.** (Stage 06; the picking half was
  built in Stage 13 Milestones 3–4.) The location editor now has a
  `<leaflet-map pick>` under its coordinate fields — click to place, drag to
  adjust, two-way bound to the number inputs, which stay authoritative. What
  the original entry also asked for and is still missing is finding a place by
  *name*: typing an address instead of knowing roughly where to click. That is
  a geocoder, and the decision already taken is Nominatim behind our own
  endpoint rather than called from the browser.
- **Search, filter and sort on the trips list.** Confirmed absent:
  `trips-page.js` has no search input, filter or sort control, and
  `ListTripsByOwner` (`internal/db/sqlc/queries/trips.sql`) has a fixed
  `ORDER BY created_at DESC` with no parameters for sort field/direction or a
  title predicate. Needs both a frontend control and backend query changes —
  not just a client-side reorder, since the API returns every trip
  unconditionally today.
- **A way to jump to "today" in a long itinerary.** (Stage 04; the collapse half
  was built in Stage 10 Milestone 4.) Past and empty days now start collapsed, so
  the scroll is much shorter, but on a 3-week trip the open day can still be well
  below the fold — the disclosure changed how much there is to scroll, not where
  you land. Wants either scrolling the first open day into view on render
  (`scrollIntoView`, cheap) or a "Today" control in the itinerary header.
- **Checklist editing and duplication, and a ⋮-menu to hold them.** (Stage 05.)
  `checklist-list.js` supports only creating and deleting a checklist/item —
  no renaming a checklist, no editing an item's text after creation, no
  duplicating a list (useful for reusing a packing list across trips).
  Suggested UI: replace the bare delete icon with a vertical-ellipsis button
  opening a small dropdown (Edit / Duplicate / Delete). Needs concept work
  first — how in-place editing should look, whether duplication copies
  checked-state or resets it. No longer blocked: `renderMenu` grew its
  action-item mode in Stage 11 Milestone 3, and the Files row's ⋮ (Edit note /
  Delete) is the working example to copy, `promptDialog` included.
- **No progress while a large file uploads.** (Stage 11.) The drop zone accepts
  up to 50 MB per file and several files per gesture, and while a batch is in
  flight the only feedback is the zone dimming (`.file-drop--busy`) — no per-file
  progress, no "3 of 5", and on a slow connection a 40 MB upload looks like
  nothing is happening for a minute. `fetch()` cannot report request progress, so
  this means either `XMLHttpRequest` for its `upload.onprogress` or a
  `ReadableStream` request body, and a decision about what to show for a batch
  versus for one file.
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

---

## Multi-user and sharing

One cluster: the entries below depend on the first, so they want designing
together rather than bolting a column onto an existing table.

- **Sharing / collaboration / permissions.** (Stage 01.) Owner/participant/viewer
  roles on a trip. Needs a `trip_collaborators` join table and a change from
  "owner_id == current user" to "role >= X" checks — the latter touching every
  handler that currently calls `loadOwnedTrip`/`loadOwnedItem`.
- **Public shareable links.** An unauthenticated read-only trip view via a
  token. Needs a `share_links` table (token, trip_id, scope, expires_at) plus an
  unauthenticated route variant. IDs are already non-guessable UUIDs, so this is
  low-friction whenever it's picked up.
- **Per-visibility checklists: personal / trip-visible / shared.** Personal
  (only the author sees them), public (everyone on the trip can see them) and
  shared (everyone can see *and* tick them). Explicitly for after real
  multi-user support exists; the visibility column wants designing alongside the
  roles above.
- **Per-visibility files, on the same model as the checklists above.** (From the
  user's notes.) The motivating case is a private one: uploading an identity card
  or a boarding pass to a shared trip without everyone else on it seeing the
  file. So this is not merely symmetry with checklists — for documents,
  "personal" is the *default* people will expect for anything carrying their own
  details, which makes the default itself a design question rather than just a
  column value. Same dependency: design it with the roles above, not bolted on
  after. The storage side is already indifferent — a document row carries
  `trip_id` and an optional `item_id` and nothing else, so mechanically this is a
  visibility column plus a predicate in `ListTripFiles`/`ListItemFiles`.
  The interesting parts are who counts as the owner of a file and what a public
  share link (see above) is allowed to expose.
- **Per-visibility files, on the same model as the checklists above.** (From the
  user's notes.) The motivating case is a private one: uploading an identity card
  or a boarding pass to a shared trip without everyone else on it seeing the
  file. So this is not merely symmetry with checklists — for documents, "personal"
  is the *default* people will expect for anything with their own details on it,
  which makes the default itself a design question rather than a column value.
  Same dependency: it wants designing with the roles above, not bolted on
  afterwards. Note the storage side is already indifferent — every document row
  carries `trip_id` and an optional `item_id` and nothing else, so a visibility
  column plus a filter in `ListTripFiles`/`ListItemFiles` is the shape,
  with the interesting work being who counts as the author and what a shared link
  (see above) is allowed to expose.
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
    - **The login and register pages**, which no spec renders: the suite arrives
      already authenticated (`auth.setup.js`), so those two routes get no
      overflow, heading, accessible-name or tap-target coverage at all. They are
      also the only pages an unauthenticated visitor sees, which makes the gap
      worse than it looks. Cheap to fix: one spec with a fresh, unauthenticated
      context.
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
  monkey-patched plumbing.
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
  scenario, so the no-image path stays covered too. What's left is the habit
  rather than a named gap: an element that only renders when data exists is
  invisible to the sweeps until some scenario creates that data, so anything new
  wants the question "which scenario renders this?" asked of it. The known
  remaining blind spot is anything behind an interaction (menus opened, dialogs,
  forms submitted), which the first entry in this section already covers.
- **Nothing ever runs the Postgres dialect.** `sqlc generate` emits both
  dialects and `internal/db` has a hand-written adapter for each, but every test,
  the seeder and the dev server run SQLite: there is no local Postgres, no
  compose file, and no Postgres job in CI. So the Postgres half of any query
  change is verified only by compiling — which catches a type error and nothing
  else. A wrong column order, a dialect-specific NULL or timestamp difference, or
  an adapter that maps the wrong field would ship green. Noticed while changing
  `ListTripFiles` (`ListTripDocuments` at the time) in Stage 10 Milestone 7, but
  it applies to every query in the app. Cheapest fix that would actually mean something: a CI job with a
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
- **`scripts/i18n.py unused` is not wired into `make ci`.** It has a `--strict`
  flag that exits non-zero, but 9 keys (the `trip.tabs.*` and `item.category.*`
  families) are only reachable via runtime-composed keys and so are unprovable
  either way by static scan. Gating CI on it would mean either false failures or
  teaching it to ignore exactly the cases a human should eyeball. Worth
  revisiting if an allowlist of known-dynamic prefixes turns out to be
  maintainable — that would make the check a real gate instead of a report.
  (As of Stage 09 Milestone 7 it reports no unused keys at all, so wiring it up
  would start from green.)
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

- **Squash the migrations before the first real release.** There are now **six**
  files per dialect (`0001_init` through `0006_rename_documents_to_files`, in both
  `internal/db/migrations/sqlite/` and `.../postgres/`). Since nobody has
  deployed this yet, collapsing them into a single `0001_init` is safe — and
  stops being safe the moment someone has.
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
- **HTTPS in deployment** is not currently a concern, but becomes one as soon as
  the device-location feature above is picked up: `navigator.geolocation`
  requires a secure context, so it will silently do nothing when the app is
  served over plain HTTP to a phone.
