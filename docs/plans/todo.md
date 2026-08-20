# Caravel — TODO / Backlog

Everything below is **not yet built**. Originally compiled from Stage 01's
plan (`stage-01.md`) and Stage 02's plan (`stage-02.md`) — both marked
"complete" for their own scope, but each explicitly deferred things — plus a
separate `notes.md` of hands-on observations, which has since been folded in
here and removed, so this file is now the single backlog. Nothing here is
prioritized or scheduled; this is raw input for planning the next stage.

Each item cites where it came from, so the reasoning behind it isn't lost.

## Confirmed bugs / gaps (verified by reading the current code)

These aren't just suspicions — each was checked against the actual source
before going in this list.


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

## Testing / CI / dev-workflow gaps

- **The UI suite covers three checks; more were listed than built.** Stage 08
  Milestone 5 landed the sweep matrix (overflow + tap targets), the
  shadow-DOM-aware heading outline and the accessible-name sweep in
  `tests/ui/`, run by `make test-ui` and a `ui` job in CI. Not covered, and
  worth adding as the app grows: anything behind an interaction (menus
  opened, forms submitted, dialogs), the login/register pages (the suite logs
  in via the API, so those routes are never rendered), and German copy (the
  suite runs in the default locale only, so `de.json` is still only
  eyeballed by hand).
  *Concrete instances from Stage 09.* Milestone 2's single Save was verified by a
  20-assertion Playwright script, and Milestone 6's follow-up (the tab bar's
  "More" menu: open, toggle, outside-click, Escape, select-and-close, active-state
  marking, in two locales) by a 33-assertion one. **Neither is checked in.** The
  first mutates data, and every existing spec is a read-only sweep, so it needs an
  isolation decision (its own trip per run? a reset between specs?) first. The
  menu one doesn't mutate anything and is the cheaper of the two to adopt — it
  only needs a page load, a few clicks and computed-style reads — so it is the
  obvious first interaction spec. Until then the stage's headline fixes have no
  automated guard.
  Related gap the tab overlap exposed: the sweeps check page-level overflow and
  control *height*, but nothing checks that a label's ink stays inside its own
  box. That is why six overlapping tab labels passed `make test-ui` — the bar fit
  the viewport and the tabs were 47px tall, while the text ran into its
  neighbours. A per-element "content fits its box" assertion
  (`scrollWidth > clientWidth`, or a child wider than its parent) would have
  caught it.
- **The UI suite still needs its in-flight-fetch counter, even now that routes
  show a loading state.** Stage 09 Milestone 5 gave every route a
  `common.loading` line (`components/loading.js`), which fixes the *user-facing*
  half of this — no more flash of empty or partial page. It does not remove the
  suite's problem: a loading line carries no `<h1>`, so `gotoRoute` must still
  wait for fetches to settle before asserting heading outlines, and that wait is
  still an injected `window.fetch` wrapper (`tests/ui/helpers/scenarios.js`)
  rather than anything the app exposes. A `data-loading` attribute on `#app`, or
  a "ready" event, would let the suite wait on the app's own state instead of on
  monkey-patched plumbing.
- **The UI suite can fail with HTTP 429 rather than a real assertion.** Found
  while verifying Stage 09 Milestone 5. `internal/httpapi/router.go` rate limits
  login to `newLoginLimiter(10, time.Minute)` per IP, and the suite logs in once
  per spec — 9 per run — so two runs inside a minute, or one run alongside a
  hand-written Playwright script, trips it. The specs then render the login page
  and fail on unrelated assertions, and the message actively misleads: it reads
  "login as demo failed — has `make dev-reset FORCE=1` been run?" when the seed
  is fine. Fixes: share one `storageState` across specs instead of logging in
  nine times, and/or have `login()` name 429 explicitly.
- **Leaflet's own controls are below the tap-target guideline, and the sweep
  excludes them.** Stage 09 Milestone 6 raised everything Caravel owns to 44px
  and `tests/ui/routes.spec.js` now asserts the guideline, but the vendored
  library's markup inside the map's shadow root is skipped by class
  (`[class*="leaflet-"]`): its zoom buttons measure 30px and the OpenStreetMap
  attribution link 14px. Restyling a dependency's internals to satisfy our own
  sweep is the tail wagging the dog, and the attribution is conventionally
  small — but if the map ever becomes a primary interaction surface on phones,
  the zoom buttons are the ones worth revisiting. (The legend, which *is* ours,
  was fixed in that milestone via `leaflet-map.js`'s own shadow styles.)
- **The UI sweeps only measure what the seed actually renders.** Two 22px tap
  targets — a location's external-link row and its "View on Google Maps" link —
  survived every sweep until Stage 09 Milestone 6, because the `full` scenario
  created no item links, so those cards only ever rendered their empty state.
  One was found only because leftover manual test data happened to be in the
  database that run. The seeder now gives that location a link and a date;
  worth a pass over the remaining empty states for the same reason — nothing in
  any scenario sets an item preview image or a trip cover photo, so
  `.image-field__preview`, `.itinerary-entry__thumb` and the location card's
  thumbnail are never measured by anything.
- **Contrast is measured but not asserted.** `tests/ui/contrast.js` reports
  ratios and has a `--min` flag, but nothing runs it in CI, so a regression
  like Stage 07's 2.54:1 primary button would not be caught automatically —
  only found by someone running it. Turning it into a spec needs a decision
  about which elements have a defensible threshold (decorative fills and
  large text differ), which is why it stayed a measurement tool.
- **`web/sw.js` is never syntax-checked.** Surfaced by Stage 08 Milestone 1
  while fixing the parse mode: both the old and new checks only walk
  `web/js`, and the service worker lives one level up at `web/sw.js`, so a
  syntax error in it reaches the browser with `make ci` green — the same
  class of hole the milestone just closed, one directory over. Note it
  needs the *opposite* mode: `app.js:76` registers it via
  `navigator.serviceWorker.register("/sw.js")` with no `{type: "module"}`,
  so it is a classic script and `node --check` (script mode) is correct for
  it — confirmed it currently parses clean that way. So this isn't a
  one-line widening of the find; it wants a second check with the other
  parse mode, which is why it wasn't folded into that milestone.
- **One orphaned i18n key: `common.edit`.** Found by `scripts/i18n.py unused`
  (Stage 08 Milestone 2), which reported two — `item.detail.close` picked up a
  real caller in Stage 09 Milestone 4 (the dialog component's dismiss button),
  leaving this one. It looks like a key some future ⋮-menu (see the checklist
  entry under Stage 05) would want to re-add rather than re-invent, so the
  decision is delete-and-re-add-later or keep-with-a-note.
- **`scripts/i18n.py unused` is not wired into `make ci`.** It has a
  `--strict` flag that exits non-zero, but 9 keys (the `trip.tabs.*` and
  `item.category.*` families) are only reachable via runtime-composed keys
  and so are unprovable either way by static scan. Gating CI on it would
  mean either false failures or teaching it to ignore exactly the cases a
  human should eyeball. Worth revisiting if an allowlist of known-dynamic
  prefixes turns out to be maintainable — that would make the check a real
  gate instead of a report.
- **`itinerary.noDates` points at a tab that no longer exists.** A trip with
  no start/end date shows "Set a start and end date on the **Overview tab**
  to build a day-by-day itinerary, or add days manually below" — but Stage
  05 removed the Overview tab; those fields live under **Settings** now.
  Spotted while verifying Stage 07 Milestone 7 on a dateless trip. A
  two-locale copy fix, deliberately not folded into that milestone since it
  isn't part of day deletion; worth grepping the rest of `locales/` for
  other references to removed UI while doing it.
  *Repro since Stage 08 Milestone 4:* `make dev-reset FORCE=1`, then the
  `no-dates` scenario's Itinerary tab shows it directly — no hand-built
  dateless trip needed.
- **`scripts/without.sh` only handles *uncommitted* changes.** By design (it
  works via `git stash push`), but it means the common case of "does this
  test actually cover the fix I landed last week?" needs the change staged as
  uncommitted first — Stage 08 Milestone 6 did that on a scratch branch to
  check a real Stage 07 change. A `--commit <sha>` mode that reverse-applies
  a commit into the working tree, runs the command, then restores, would
  remove the manual step. Not built because the uncommitted case is the one
  that comes up mid-work, which is when non-vacuity actually gets checked.
  Second limitation, found in Stage 08 Milestone 7: it asks "does this
  command depend on my uncommitted **fix**?", which is the wrong question for
  "does this test catch this **break**?" — reverting a break restores the
  guard and the tests pass, so it correctly answers VACUOUS while telling you
  nothing. Proving a new test catches a regression still means disabling the
  code under test by hand. A `--break` mode (apply an edit, expect failure,
  restore) would be the mirror image and would cover that case.
- **A startup banner carrying the build's git SHA.** The other half of the
  stale-binary problem, left over after Stage 08 Milestone 3 built
  `make dev-marker`: that check needs you to *supply* a marker string, and
  the string has to be one the code actually uses (an unused Go const is
  folded away and never reaches the binary). A SHA stamped in at build time
  via `-ldflags -X` and logged at startup — ideally also returned by
  `/api/health` — would let any test assert which build it is talking to
  without the caller inventing a marker each time. Cheap, and it would make
  the Playwright suite's "is this the right server?" check trivial.
- **Migrations should be collapsed/squashed before the first real
  release.** There are three migration files per dialect now
  (0001/0002/0003); since nobody has actually deployed this yet, squashing
  them into a single `0001_init` is safe and worth doing before that
  changes.

## New feature ideas (not previously in either plan)

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
  *Repro since Stage 08 Milestone 4:* the `full` seed scenario has one
  trip-level document (`trip-notes.txt`) and one attached to the Foss Hotel
  location (`hotel-booking.txt`); the Documents tab shows only the former,
  while the latter is reachable on the location's own page.

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
  is exactly the half-migrated state worth avoiding.
  *Closer since Stage 09 Milestone 6's follow-up:* the trip tab bar's "More"
  menu is a third caller of `renderMenu`, and getting it there gave the
  component `label` (a pinned trigger label, so the trigger no longer has to
  track the selection), `chevron: false`, `triggerClass` and `className`. A
  pinned label plus custom trigger styling is most of what user-menu needs;
  what's still missing is the non-select **action item** mode (Log out isn't a
  selection, so `role="menuitemradio"`/`aria-checked` is wrong for it), which
  is also what the ⋮ contextual menu in the checklist entry above wants.
- **The cover photo and documents are still a post-create upload.** All that
  is left of "create-mode writes aren't atomic" (Stage 06 Milestone 4) after
  Stage 09 Milestones 1–2, which made the item and its location/links/dates
  one transactional request. These two can't ride in a JSON body, so a new
  location still stages them in memory and `flushUploads()` writes them once
  the create returns an ID. If that fails the location exists without its
  photo or files; unlike before, the failure reports inline in the Basic info
  card and the page stays put so it can be retried. Closing the gap entirely
  would mean a multipart create endpoint — not obviously worth it.
- **Three item sub-resource endpoints now have no caller.** Stage 09 Milestone 2
  moved the frontend onto nested `location`/`links`/`dates` in the item request,
  which leaves `PUT /items/{id}/location`, `POST|DELETE /items/{id}/links` and
  `POST|DELETE /items/{id}/dates` reachable but unused by the app (they still
  have ownership-test coverage). Keep them as a documented API surface, or
  delete them and shrink the router — worth an explicit decision rather than
  letting them quietly rot. Deleting would also drop `itemLinkRequest`'s and
  `itemDateRequest`'s standalone handlers while keeping the structs, which the
  nested path reuses.
- **Click-to-pick coordinates on a map.** Both create and edit still take
  latitude/longitude as raw number inputs — fine for pasting from
  elsewhere, unpleasant on a phone. `leaflet-map.js` is read-only today
  (attribute-driven, no click handler), so a picker means teaching it a
  pick mode (click or drag a marker, feed the coordinates back to the
  form), ideally with an address search via a geocoder. Deliberately kept
  out of Stage 06, whose Milestone 4 was scoped to plumbing endpoints that
  already exist — but it's the obvious next step for making the Location
  card pleasant to fill in.
  *Stage 09 Milestone 2 removed the split-save trap that made hand-entry
  actively lossy, so this is now purely about convenience.*

## Deferred from Stage 07 (automated UI/UX test round)

Stage 07's Playwright pass over desktop (1280×800), mobile (324×756) and
dark mode found 19 issues; 11 are being fixed in that stage
(`stage-07.md`), and these are the ones deliberately deferred. Each was
triaged with the user rather than dropped silently.

- **A cover photo set by URL on the *new trip* form is still only validated
  server-side at Create time.** Stage 09 Milestone 4 fixed the *copy* half of
  this: both the existing-trip card and the create form now show translated
  messages (`image.fetchFailed` / `image.uploadFailed`) with the Go error going
  to `console.error` instead of into the UI, and the create form's failure is
  an in-app dialog rather than a native `alert()`. What's unchanged is the
  *timing*: the URL is staged locally, the trip is created, and only then does
  the fetch happen — so the dialog still arrives after a create that partly
  succeeded. A real fix means validating at "Set image" time, which needs
  either a trip-independent validation endpoint or accepting the browser's own
  `<img>` load as the check (Stage 07 Milestone 9's preview-error handler
  already does exactly that inline, which softens this a lot — a URL the
  browser can't load is flagged in the card before Create is ever pressed).
- **The mobile map page swallows vertical scrolling.** On the Map tab at
  324×756 the map is 424px tall starting at y=383, with only ~67px of page
  below it — so a touch drag starting anywhere in the lower half of the
  screen pans the map instead of scrolling the page. The category legend
  (Site/Stay/Transport) ends up at y=769, just below the fold, with no
  affordance suggesting it exists. Wants a deliberate decision about map
  height, gesture handling (e.g. Leaflet's one-finger-pan opt-in) and
  legend placement rather than a quick tweak.
- **Polish batch, all confirmed in the same round:**
    - Category is a fixed `<select>` (Site/Stay/Transport) while Type is
      free text, so a location detail page reads "Site landmark" with
      mismatched capitalisation. Either derive Type from a per-category list
      or at least normalise its display.
    - The Documents tab's "Upload" is styled `btn-secondary` though it is
      that row's primary action, while "New checklist" next to an identical
      input row is `btn-primary` — the two rows should agree.

## Migrated from `notes.md` (that file is now gone; add new notes here)

- **Rendered notes are spaced far too loosely — `white-space: pre-wrap` is
  the cause, not the heading margins.** `.location-view__notes`
  (`base.css`) sets `white-space: pre-wrap`, which made sense when notes
  were plain text but is wrong now they're rendered to HTML: the newlines
  *between* block elements in `notes_html` survive as literal blank lines,
  stacking on top of each element's own margins. Measured on a real note:
  the gap between a paragraph and the following `<h2>` is **58px with
  pre-wrap, 20px without** — 38px of it pure preserved newline, and the same
  penalty applies before every list, list item and paragraph. A page of
  notes that needed scrolling fits on one screen without it.
  The h2 itself is fine (24px, 19.92px margins, unaffected by Stage 07
  Milestone 14, whose `.editor-card h2` rule doesn't reach the notes block).
  Fix is to drop `pre-wrap` — but note the trade-off it was presumably
  hiding: goldmark collapses a *single* newline into a space, so a note
  relying on single line breaks would reflow. If those should stay,
  enable goldmark's hard-wrap option (`html.WithHardWraps()` in
  `internal/markdown`) at the same time, so line breaks come from `<br>`
  rather than from CSS preserving source whitespace.
- **A markdown preview for location notes would be nice.** Notes are
  authored in a plain `<textarea>` (`location-form.js`) and only rendered
  after saving, on the view page — so formatting is written blind. Wants a
  preview (side-by-side, or a toggle) using the same server-rendered
  `notes_html` the view page uses, or a client-side render if a round trip
  per keystroke is too much.
- **Per-visibility checklists: personal / trip-visible / shared.** Personal
  (only the author sees them), public (everyone on the trip can see them)
  and shared (everyone can see *and* tick them). Explicitly for after real
  multi-user support exists — it depends on the
  "Sharing/collaboration/permissions" entry near the top of this file, and
  the visibility column would want designing alongside those roles rather
  than bolted onto `checklists` first.

## Developer tooling (repeated hand-rolling during Stage 07)

Each of these was written ad hoc several times while implementing Stage 07 —
the counts are actual occurrences in that stage, not estimates. None of them
are hard; the cost is that they get rebuilt (and re-debugged) every time, and
the fiddly parts are exactly the parts that get skipped when they're
inconvenient.

- **A contrast-measurement script (`scripts/contrast.js` or similar).**
  Takes a route, one or more selectors and a colour scheme; reports the
  computed text-vs-background and fill-vs-surround ratios against the WCAG
  thresholds. *(Hand-rolled ~6 times.)* The two parts worth having written
  down once: **flattening translucent backgrounds** over whatever is behind
  them (the danger tint is `rgba(...)`, so a naive reading measures against
  transparency and reports nonsense), and **reaching into shadow roots**.
  This is what found the 2.54:1 primary buttons and the 3.08:1 error text,
  and what proved light mode was untouched afterwards.
