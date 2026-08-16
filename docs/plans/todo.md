# Caravel — TODO / Backlog

Everything below is **not yet built**. Compiled from three sources: Stage 01's
plan (`stage-01.md`) and Stage 02's plan (`stage-02.md`) — both marked
"complete" for their own scope, but each explicitly deferred things — plus
`notes.md` (hands-on notes jotted down separately). Nothing here is
prioritized or scheduled; this is raw input for planning the next stage.

Each item cites where it came from, so the reasoning behind it isn't lost.

## Confirmed bugs / gaps (verified by reading the current code)

These aren't just suspicions from `notes.md` — each was checked against the
actual source before going in this list.


## Deferred from Stage 01 (explicit "future phases," Section 7)

Stage 01's schema/architecture were deliberately designed so none of these
require a redesign later — they're additive, not blocked, but none are built:

- **Sharing/collaboration/permissions** — owner/participant/viewer roles on
  a trip. Needs a `trip_collaborators` join table and a change from
  "owner_id == current user" to "role >= X" checks.
- **Public shareable links** — unauthenticated read-only trip view via a
  token. Needs a `share_links` table (token, trip_id, scope, expires_at)
  plus an unauthenticated route variant. IDs are already non-guessable
  UUIDs, so this is low-friction whenever it's picked up.
- **Expenses / cost-splitting** — a new `expenses` table referencing
  `trip_id`/optionally `item_id`, no changes to existing tables.
- **Federation between self-hosted instances** — real sync-protocol design
  still needed; v1 only avoided the integer-PK/local-only-ID mistakes that
  would have made this harder later.
- **Trip journal with photos** — a `journal_entries` table (trip_id, date,
  body markdown) reusing the existing `media_assets` pipeline for photos.
- **S3-compatible object storage** — swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze,
  etc.); the interface already isolates callers from the backend.
- **Prometheus/OpenMetrics metrics** — a `GET /metrics` endpoint via
  `promhttp.Handler()`. The routing reservation (`/metrics` outside `/api`
  and outside session-auth middleware) is already in place; the actual
  instrumentation (HTTP request count/duration/status, DB query duration,
  upload counts/sizes, session counts) is not.
- **OpenID Connect / external auth providers** — `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no
  provider integration exists yet.

## Deferred from Stage 01 (smaller items, mentioned in the plan body)

- **No manual light/dark theme toggle.** Theming is purely
  `prefers-color-scheme`-driven; Stage 01's plan explicitly structured this
  "so a manual `data-theme` override can be added later," but no such
  control exists — no way to override the OS setting from inside the app.
- **No itinerary entry reordering.** `itinerary_entries.sort_order` exists
  in the schema and Stage 01 floated "native drag-and-drop or a minimal
  pointer-events reorder," but `itinerary-tab.js` has no reordering UI at
  all — entries render in whatever order the API returns, with no drag
  handle, no up/down buttons.
- **No "add to every day of this stay" convenience action.** Stage 01
  floated this for multi-day items (e.g. a 3-night hotel stay) driven by
  the item's date range, but adding an item to a day is always manual,
  one-day-at-a-time via each day's own "Add item" dropdown.
- **No in-app language switcher.** `i18n.js` has a working `setLocale()`
  function and a `localStorage` cache for it, but nothing in the UI calls
  it — locale is autodetected from the browser/OS only, with no way to
  override it from inside the app. Re-confirmed by Stage 07's test round:
  `setLocale` still has no caller anywhere outside `i18n.js`, and the
  German UI is only reachable by changing the browser's language (it
  renders cleanly when you do — no overflow, no untranslated strings).
  Needs a decision on where the control lives (user menu vs. a settings
  screen) and whether the choice should persist per account rather than
  per browser.
- **No frontend build step / bundler** — deliberate for v1 ("revisit only
  if it becomes a real pain point"), not a bug, just worth remembering this
  was a conscious deferral, not an oversight, if load-time ever becomes a
  complaint.

## Deferred / scope-limited from Stage 02

- **User menu dropdown only has "Log out."** Built "structured so more
  items can be added later" (per the Stage 02 plan) — admin/settings items
  were explicitly deferred, not forgotten.

## Testing / CI / dev-workflow gaps (`notes.md`)

- **No real Playwright UI test suite.** Stage 03 added GitHub Actions CI
  (`.github/workflows/ci.yml`) running a build check, `go vet`, a JS syntax
  check across `web/js/`, and an i18n-key-parity check
  (`scripts/check_i18n.py`, generalized to any number of locale files) —
  but everything UI-facing is still verified manually or via one-off
  Playwright runs during development, not a checked-in, repeatable suite
  (using Firefox specifically, per the original note). Still wanted. When
  it's built, fold in a 324×756 mobile-regression pass — Stage 04's fixes
  (see `stage-04.md`) were verified by hand each milestone; a scripted
  version checking `document.documentElement.scrollWidth <=
  window.innerWidth` and a ~44px minimum control height across every route
  would catch future regressions cheaply, and was explicitly deferred out of
  that stage rather than built ad hoc.
- **Mobile route sweeps should assert the landed-on URL, not just the
  absence of overflow.** Stage 04 discovered mid-implementation that an
  earlier version of its own manual verification script used a URL pattern
  matching no real route; the app's router silently redirects any unmatched
  path to `/trips`, so the check had been passing trivially against the
  wrong page for several milestones. Worth remembering as a general
  footgun once a scripted suite exists: always assert
  `window.location.pathname` equals the intended route before asserting
  anything about that page's layout.
- **Verifying a backend change needs proof the *running* binary has it.**
  Stage 07 Milestone 3 nearly recorded a false pass: the API returned
  200/201 for input the new validation rejects, because a stale server from
  an earlier session still held :8080 and `make dev` had failed behind it
  with "address already in use" (a background start whose failure is easy
  to miss). `pkill -f "go run ./cmd/caravel"` doesn't catch it either — the
  compiled child's command line is its `~/.cache/go-build/...` path, not
  the `go run` invocation. What worked: find the listener with
  `ss -lptn 'sport = :8080'`, then grep `/proc/<pid>/exe` for a string the
  change introduces. Frontend edits are immune (served from disk under
  `CARAVEL_WEB_DIR`), which is exactly why this is easy to forget for Go
  changes. Worth folding into the scripted suite when it exists — e.g. a
  `make dev` that fails loudly on a busy port, or a startup banner carrying
  the build's git SHA that a test can assert against.
- **Migrations should be collapsed/squashed before the first real
  release.** There are three migration files per dialect now
  (0001/0002/0003); since nobody has actually deployed this yet, squashing
  them into a single `0001_init` is safe and worth doing before that
  changes.

## New feature ideas (not previously in either plan, from `notes.md`)

- **Re-evaluate Leaflet+OSM vs. Google Maps.** Stage 01 already weighed this
  and chose Leaflet+OSM (no API key/billing, low-regret since the tile URL
  is a one-line swap later) — this note re-opens that question rather than
  reporting a gap. Worth a deliberate "still the right call, or not"
  decision rather than silently re-litigating it.
- **LLM-assisted metadata fetching for locations** (e.g. a small local model
  + web search to auto-fill an item's details from its title). Noted as
  "not that important for the MVP" — low priority by the user's own note.
- **Collapse empty/past itinerary days behind a `<details>` disclosure.**
  Stage 04's mobile pass fixed the itinerary tab's per-day add-item row
  overflowing (`itinerary-tab.js`), but flagged as out of scope for that
  stage a separate, feature-level issue the original mobile test report
  raised: a 10-day trip renders all 10 day cards open and expanded, an
  unbroken vertical scroll with no way to jump to "today" or collapse days
  that are already past or still empty. Collapsing days by default (open
  only the current/next upcoming one) would shorten that scroll
  significantly on longer trips, on any screen size.
- **Search, filter, and sort on the trips list.** Confirmed absent:
  `trips-page.js` has no search input, filter control, or sort control at
  all, and the backend (`ListTripsByOwner`,
  `internal/db/sqlc/queries/trips.sql`) has a fixed `ORDER BY created_at
  DESC` with no parameters for sort field/direction or a title search
  predicate. Would need both a frontend control and backend query
  changes — not just a client-side reorder, since the API returns every
  trip unconditionally today.
- **Trip-level Documents tab doesn't show item-level documents.** Confirmed:
  `GET /trips/{id}/documents` explicitly filters `AND item_id IS NULL`
  (`ListTripDocuments`, `internal/db/sqlc/queries/documents.sql`), so a
  trip's Documents tab only ever shows documents attached directly to the
  trip, never ones attached to its locations/items — even though every
  document row already carries the trip's `trip_id` regardless (set in
  `uploadDocument`, `internal/httpapi/documents.go`), so the fix doesn't
  need a join through `items`, just dropping that one filter (or a new
  query) plus joining in each document's item title for display.
  Decided display shape: one flat list (as today), sorted by upload date,
  with a small inline label on item-attached documents showing which
  location they belong to (e.g. "Hotel booking.pdf — Foss Hotel
  Reykjavik"); trip-level documents show no label. `document-list.js`
  would need a new labeled-list mode, since today it only ever renders
  one homogeneous list for exactly one `path` at a time.

## Deferred from Stage 05

- **Checklist editing/duplication, and a ⋮-menu to hold them.**
  `checklist-list.js` currently only supports creating and deleting a
  checklist/item — there's no way to rename a checklist's title or edit an
  item's text after creation, and no way to duplicate a checklist (useful
  for reusing a packing list across trips). Suggested UI: replace the
  bare delete icon on each checklist with a vertical-ellipsis button that
  opens a small dropdown (Edit / Duplicate / Delete), mirroring how other
  contextual actions might grow later. Needs a bit more concept work
  before building — how in-place title/item editing should look, whether
  duplication copies checked-state or resets it — not picked up this
  stage.
- **Cover photo isn't shown anywhere on the trip's default view.** Stage
  05 removed the Overview tab and, with it, the only place a trip's cover
  photo was visible without opening Settings (see `stage-05.md` Section
  2's cover-photo decision). It's still settable/previewable inside
  Settings, just no longer visible by default. Worth revisiting whether
  the photo should reappear near the title/subtitle/dates block once that
  layout has settled from more real use.
- **Broader "item" → "location" terminology sweep.** Stage 05 fixed the
  most user-visible instances (`locations.new`/`location.editor.newTitle`
  copy, the dynamic "Edit {title}" heading), but a real inconsistency
  remains underneath: `location.editor.createButton` still reads "Create
  item"/"Eintrag erstellen", and the whole `item.detail.*`/
  `item.category.*`/`item.deleteConfirm` i18n namespace is still
  item-flavored despite `location.form.*`/`location.editor.*` already
  having migrated. On the JS side, `location-form.js` exports
  `renderItemForm`, `locations-tab.js` exports `renderItemsTab` and still
  uses `data-action="new-item"` internally. None of this is user-visible
  beyond the one leftover button label, so it's cosmetic/consistency
  cleanup rather than a bug — but worth doing as one deliberate pass
  (rename every key and identifier together) rather than piecemeal, to
  avoid leaving the codebase in a half-migrated state indefinitely.

## Deferred from Stage 06

- **Refactor `user-menu.js` onto `components/menu.js`.** Stage 06
  Milestone 1 extracted the popup pattern (hidden-attribute visibility,
  `aria-expanded` sync, outside-click + Escape listeners attached on open
  and removed on close) into a generic single-select `renderMenu`, but
  wired only the locations filter to it — `user-menu.js` still carries its
  own copy of the same behavior plus `.user-menu__dropdown` CSS that
  `.menu__dropdown` now duplicates. Two popup implementations in the tree
  is exactly the half-migrated state worth avoiding. Folding user-menu
  onto the component needs `renderMenu` to grow a non-select "action item"
  mode first (Log out isn't a selection), which is also what the ⋮
  contextual menu in the checklist entry above wants.
- **Create-mode writes aren't atomic.** Stage 06 Milestone 4 lets a new
  location carry coordinates, links, dates and documents, but it commits
  them as a sequence of requests after the item POST returns an ID (every
  sub-resource endpoint requires an existing item). If one fails, the
  location is left half-populated: the failures are reported in one alert
  and the user lands on the edit page to finish by hand — the same policy
  the staged cover photo has always used, so not a new gap, but a gap. The
  proper fix is a transactional create: optional nested
  `location`/`links`/`dates` on `itemRequest`, with `handleCreateItem`
  inserting them in one transaction. Documents can't ride along either
  way, being multipart, so they'd stay a post-create upload regardless.
- **Click-to-pick coordinates on a map.** Both create and edit still take
  latitude/longitude as raw number inputs — fine for pasting from
  elsewhere, unpleasant on a phone. `leaflet-map.js` is read-only today
  (attribute-driven, no click handler), so a picker means teaching it a
  pick mode (click or drag a marker, feed the coordinates back to the
  form), ideally with an address search via a geocoder. Deliberately kept
  out of Stage 06, whose Milestone 4 was scoped to plumbing endpoints that
  already exist — but it's the obvious next step for making the Location
  card pleasant to fill in.
  *Stage 07's test round reinforces this: entering coordinates by hand is
  also where the split-save trap below bites hardest.*
- **`make dev-seed` leaves every demo item off the map.** `cmd/seed/main.go`
  never sets `ShowOnMap` when inserting its three items, so they're all
  `show_on_map: false` (the Go zero value) and the seeded trip's Map tab is
  empty until you edit each location by hand — confirmed on two
  independently seeded demo trips while verifying Stage 06 Milestone 5. A
  one-line seed fix, but worth deciding deliberately: the map is easier to
  demo with pins, and the seeded items have no coordinates either, so
  showing anything means seeding lat/lng too. See also "Coordinates alone
  don't put a location on the map" below — the same `show_on_map` gate,
  hit from the user's side rather than the seed's.

## Deferred from Stage 07 (automated UI/UX test round)

Stage 07's Playwright pass over desktop (1280×800), mobile (324×756) and
dark mode found 19 issues; 11 are being fixed in that stage
(`stage-07.md`), and these are the ones deliberately deferred. Each was
triaged with the user rather than dropped silently.

- **The "Not found" state renders unstyled.** Hitting a nonexistent trip or
  location ID (`/trips/<bad-uuid>/locations`) renders bare "Not found."
  text and a raw underlined link flush against x=0 — no page container, no
  padding, nothing else on the page. It's the state a stale bookmark or a
  deleted-trip link lands on, so it's worth looking deliberate; the fix is
  wrapping it in the same page layout every other route uses.
- **Location edit's split saves discard typed coordinates.** Latitude and
  longitude live in their own card with a "Save location" button, while the
  visually primary "Save" in the Basic info card above them submits only
  title/category/type/notes. Verified: typing coordinates and pressing
  Save leaves `location` null on the item with no warning, and there's no
  unsaved-changes guard when navigating away, so the input is simply lost.
  The new-location form has the opposite shape — one Create button commits
  everything — so the two forms teach contradictory habits. Fixing it means
  either unifying the edit form onto one submit (which wants the
  transactional-create work in the Stage 06 entry above) or warning about
  unsaved section changes.
- **Coordinates alone don't put a location on the map.** `show_on_map` is a
  checkbox in the *Basic info* card, several cards above the coordinate
  fields, so setting coordinates and expecting a pin gets an empty map:
  verified that `GET /api/trips/{id}/map` returns `[]` while the item's
  coordinates are saved, with nothing in the UI explaining why. Options:
  default `show_on_map` on once coordinates exist, or move the checkbox
  next to the coordinates it actually gates.
- **Image-URL errors surface too late, as a raw alert.** A cover photo set
  by URL on the *new trip* form is staged locally and only fetched
  server-side when Create is pressed, where a failure becomes a native
  `alert()` carrying developer text ("could not fetch image from url:
  server returned status 403"). Worse, the trip is created anyway, so the
  alert reads like a total failure that wasn't one. Should validate/fetch
  at "Set image" time and report inline. (Stage 07 Milestone 9 makes the
  *broken preview* visible, which is the other half of the same confusion.)
- **Trip settings' date inputs clip at 324px.** The start/end date fields
  sit side by side in `.trip-form__dates`; at the phone's width each is
  ~123px and the browser truncates the year under the calendar icon —
  "20 / 08 / 202". The location editor's rows already stack under the
  640px breakpoint (Stage 06 Milestone 5); this row was missed.
- **The mobile map page swallows vertical scrolling.** On the Map tab at
  324×756 the map is 424px tall starting at y=383, with only ~67px of page
  below it — so a touch drag starting anywhere in the lower half of the
  screen pans the map instead of scrolling the page. The category legend
  (Site/Stay/Transport) ends up at y=769, just below the fold, with no
  affordance suggesting it exists. Wants a deliberate decision about map
  height, gesture handling (e.g. Leaflet's one-finger-pan opt-in) and
  legend placement rather than a quick tweak.
- **Polish batch, all confirmed in the same round:**
    - Destructive confirmations and error reporting still go through native
      `window.confirm()`/`alert()` (documents, checklists, trips, locations),
      which on mobile renders as a "localhost:8080 says" system dialog —
      visually disconnected from the app.
    - Mobile trip-tab labels are 0.625rem (10px), below the ~11-12px floor a
      tab bar usually holds to; the tap targets themselves are fine (≥44px).
    - Category is a fixed `<select>` (Site/Stay/Transport) while Type is
      free text, so a location detail page reads "Site landmark" with
      mismatched capitalisation. Either derive Type from a per-category list
      or at least normalise its display.
    - Unmatched URLs silently redirect to `/trips` with no "that page
      doesn't exist" feedback. (Already recorded above as a *testing*
      footgun; this is the user-facing side of the same behaviour.)
    - The Documents tab's "Upload" is styled `btn-secondary` though it is
      that row's primary action, while "New checklist" next to an identical
      input row is `btn-primary` — the two rows should agree.
