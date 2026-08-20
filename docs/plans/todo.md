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

- **The mobile map page swallows vertical scrolling.** (Stage 07.) On the Map tab
  at 324×756 the map is 424px tall starting at y=383, with only ~67px of page
  below it — so a touch drag starting anywhere in the lower half of the screen
  pans the map instead of scrolling the page. The category legend
  (Site/Stay/Transport) ends up at y=769, just below the fold, with no
  affordance suggesting it exists. Wants a deliberate decision about map height,
  gesture handling (e.g. Leaflet's one-finger-pan opt-in) and legend placement
  rather than a quick tweak.
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
- **The trip-level Files tab doesn't show files attached to locations.**
  Confirmed: `GET /trips/{id}/documents` filters `AND item_id IS NULL`
  (`ListTripDocuments`, `internal/db/sqlc/queries/documents.sql`), so the tab
  only ever shows files attached directly to the trip — even though every
  document row already carries the trip's `trip_id` regardless (set in
  `uploadDocument`, `internal/httpapi/documents.go`), so the fix doesn't need a
  join through `items`, just dropping that one filter (or a new query) plus
  joining in each document's item title for display. Decided display shape: one
  flat list (as today), sorted by upload date, with a small inline label on
  location-attached files showing which location they belong to (e.g. "Hotel
  booking.pdf — Foss Hotel Reykjavik"); trip-level files show no label.
  `document-list.js` would need a new labeled-list mode, since today it only
  renders one homogeneous list for exactly one `path` at a time.
  *Repro:* the `full` seed scenario has one trip-level document
  (`trip-notes.txt`) and one attached to the Foss Hotel location
  (`hotel-booking.txt`); the tab shows only the former, while the latter is
  reachable on the location's own page.
- **Category is a fixed `<select>` while Type is free text**, so a location
  detail page reads "Site landmark" with mismatched capitalisation. (Stage 07.)
  Either derive Type from a per-category list or at least normalise its display.
- **The Files tab's "Upload" is styled `btn-secondary`** though it is that row's
  primary action, while "New checklist" next to an identical input row is
  `btn-primary`. (Stage 07.) The two rows should agree.
- **A trip's cover photo isn't shown anywhere on its default view.** (Stage 05.)
  Removing the Overview tab took away the only place the photo was visible
  without opening Settings. It's still settable and previewable there, just no
  longer visible by default. Worth revisiting whether it should reappear near
  the title/subtitle/dates block now that layout has settled.
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
- **Click-to-pick coordinates on a map.** (Stage 06.) Both create and edit take
  latitude/longitude as raw number inputs — fine for pasting from elsewhere,
  unpleasant on a phone. `leaflet-map.js` is read-only (attribute-driven, no
  click handler), so a picker means teaching it a pick mode (click or drag a
  marker, feed the coordinates back to the form), ideally with an address search
  via a geocoder. Stage 09 Milestone 2 removed the split-save trap that made
  hand-entry actively lossy, so this is now purely about convenience.
- **Search, filter and sort on the trips list.** Confirmed absent:
  `trips-page.js` has no search input, filter or sort control, and
  `ListTripsByOwner` (`internal/db/sqlc/queries/trips.sql`) has a fixed
  `ORDER BY created_at DESC` with no parameters for sort field/direction or a
  title predicate. Needs both a frontend control and backend query changes —
  not just a client-side reorder, since the API returns every trip
  unconditionally today.
- **Collapse empty/past itinerary days behind a `<details>` disclosure.**
  (Stage 04.) A 10-day trip renders all 10 day cards open and expanded, an
  unbroken vertical scroll with no way to jump to "today" or collapse days that
  are already past or still empty. Opening only the current/next upcoming one
  would shorten that scroll significantly at any screen size.
- **Checklist editing and duplication, and a ⋮-menu to hold them.** (Stage 05.)
  `checklist-list.js` supports only creating and deleting a checklist/item —
  no renaming a checklist, no editing an item's text after creation, no
  duplicating a list (useful for reusing a packing list across trips).
  Suggested UI: replace the bare delete icon with a vertical-ellipsis button
  opening a small dropdown (Edit / Duplicate / Delete). Needs concept work
  first — how in-place editing should look, whether duplication copies
  checked-state or resets it. Depends on `renderMenu` growing an action-item
  mode (see the cleanup section).
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
- **An in-app language switcher.** `i18n.js` has a working `setLocale()` and a
  `localStorage` cache for it, but nothing calls it — confirmed again during
  Stage 09 that `setLocale` has no caller outside `i18n.js`. German is reachable
  only by changing the browser's language, and it renders cleanly when you do.
  Needs a decision on where the control lives (user menu vs. a settings screen)
  and whether the choice persists per account rather than per browser.
- **A manual light/dark theme toggle.** Theming is purely
  `prefers-color-scheme`-driven. Stage 01 deliberately structured it "so a manual
  `data-theme` override can be added later"; nothing in the tree sets
  `data-theme` today, so there's still no way to override the OS setting from
  inside the app.
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
- **Expenses / cost-splitting.** (Stage 01.) A new `expenses` table referencing
  `trip_id` and optionally `item_id`, with no changes to existing tables. The
  *splitting* half only means anything once several people share a trip, which
  is why it sits here.

---

## Consistency and cleanup

No user-visible change; the point is to stop the tree drifting further out of
step with itself.

- **Identifier sweep: "item" → "location", and "documents" → "files".** Stage 05
  fixed the user-visible item/location copy and Stage 09 Milestone 7 renamed
  Documents to Files in the copy, so what's left is entirely below the surface —
  two renames wanting one deliberate pass:
    - The whole `item.detail.*`/`item.category.*`/`item.deleteConfirm` i18n
      namespace is still item-flavoured despite `location.form.*`/
      `location.editor.*` having migrated. On the JS side, `location-form.js`
      exports `renderItemForm`, `locations-tab.js` exports `renderItemsTab` and
      uses `data-action="new-item"`, and the list renders `<item-card>`.
    - The `documents.*` keys, `item.detail.documents`, `trip.tabs.documents`, the
      `data-tab="documents"` value, `document-list.js` and the
      `/trips/:id/documents` route all still say "documents". Renaming the route
      changes a user-visible URL, so it needs a redirect from the old path or a
      deliberate decision to break existing bookmarks.
    - `trip.overview.image` still names the Overview tab removed in Stage 05.
      Its value ("Cover photo" / "Titelbild") is correct; only the key is stale.
  Doing these piecemeal is what leaves the tree half-migrated indefinitely,
  which is the whole reason this is one entry rather than three.
- **Refactor `user-menu.js` onto `components/menu.js`.** Stage 06 Milestone 1
  extracted the popup pattern (hidden-attribute visibility, `aria-expanded`
  sync, outside-click and Escape listeners attached on open and removed on
  close) into `renderMenu`, but `user-menu.js` still carries its own copy of
  that behaviour plus `.user-menu__dropdown` CSS that `.menu__dropdown`
  duplicates. Two popup implementations in one tree is exactly the half-migrated
  state worth avoiding.
  *Closer since Stage 09 Milestone 6:* the tab bar's "More" menu is a third
  caller, and getting it there gave the component `label` (a pinned trigger
  label), `chevron: false`, `triggerClass`, `className` and `items[].iconName`.
  That covers most of what user-menu needs; what's missing is the non-select
  **action item** mode — Log out isn't a selection, so
  `role="menuitemradio"`/`aria-checked` is wrong for it. The ⋮ checklist menu
  wants the same mode, so one of those two should build it and the other
  should follow.
  Related: the user menu still has only "Log out" in it (Stage 02 built it
  "structured so more items can be added later"), so an in-app language
  switcher or a settings entry is the likely trigger for finishing this.

---

## Testing, CI and dev tooling

- **The UI suite covers four checks; more were listed than built.** Stage 08
  Milestone 5 landed the sweep matrix (overflow + tap targets), the
  shadow-DOM-aware heading outline and the accessible-name sweep in `tests/ui/`,
  run by `make test-ui` and a `ui` job in CI. Not covered, and worth adding as
  the app grows: anything behind an interaction (menus opened, forms submitted,
  dialogs), the login/register pages (the suite logs in via the API, so those
  routes are never rendered), and German copy (the suite runs in the default
  locale only, so `de.json` is still only eyeballed by hand).
  *Concrete instances from Stage 09.* Milestone 2's single Save was verified by a
  20-assertion Playwright script; Milestone 6's follow-up (the tab bar's "More"
  menu — open, toggle, outside-click, Escape, select-and-close, active-state
  marking, two locales) by a 34-assertion one; Milestone 7's copy pass by a
  17-assertion one. **None are checked in.** The Save one mutates data and every
  existing spec is a read-only sweep, so it needs an isolation decision (its own
  trip per run? a reset between specs?) first. The menu one mutates nothing and
  is the cheapest to adopt — a page load, a few clicks and computed-style
  reads — so it is the obvious first interaction spec. Until then the stage's
  headline fixes have no automated guard.
- **Nothing checks that content fits inside its own box.** The sweeps check
  page-level overflow and control *height*, which is why six overlapping tab
  labels passed `make test-ui` in Stage 09 Milestone 6: the bar fit the viewport
  and the tabs were 47px tall while the text ran into its neighbours. A
  per-element assertion (`scrollWidth > clientWidth`, or a child wider than its
  parent) would have caught it, and would also have caught the More trigger
  sizing to 45px inside a 58px cell later in the same milestone.
- **The UI suite can fail with HTTP 429 rather than a real assertion.**
  `internal/httpapi/router.go` rate limits login to `newLoginLimiter(10,
  time.Minute)` per IP, and the suite logs in once per spec — 9 per run — so two
  runs inside a minute, or one run alongside a hand-written Playwright script,
  trips it. The specs then render the login page and fail on unrelated
  assertions, and the message actively misleads: "login as demo failed — has
  `make dev-reset FORCE=1` been run?" when the seed is fine. Fixes: share one
  `storageState` across specs instead of logging in nine times, and/or have
  `login()` name 429 explicitly.
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
  that run. The seeder now gives that location a link and a date; the same pass
  is worth doing over the remaining empty states, since no scenario sets an item
  preview image or a trip cover photo, so `.image-field__preview`,
  `.itinerary-entry__thumb` and the location card's thumbnail are never measured
  by anything.
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
  attribution link 14px. Restyling a dependency's internals to satisfy our own
  sweep is the tail wagging the dog, and the attribution is conventionally
  small — but if the map becomes a primary interaction surface on phones, the
  zoom buttons are worth revisiting. (The legend, which *is* ours, was fixed in
  that milestone via `leaflet-map.js`'s own shadow styles.)
- **`web/sw.js` is never syntax-checked.** `scripts/check_js.sh` walks `web/js`
  only, and the service worker lives one level up, so a syntax error in it
  reaches the browser with `make ci` green — the same class of hole Stage 08
  Milestone 1 closed, one directory over. It needs the *opposite* parse mode:
  `app.js` registers it via `navigator.serviceWorker.register("/sw.js")` with no
  `{type: "module"}`, so it is a classic script and `node --check` (script mode)
  is correct for it. So this isn't a one-line widening of the `find`; it wants a
  second check with the other mode.
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
- **A startup banner carrying the build's git SHA.** The other half of the
  stale-binary problem left over after Stage 08 Milestone 3 built
  `make dev-marker`: that check needs you to *supply* a marker string, and the
  string has to be one the code actually uses (an unused Go const is folded away
  and never reaches the binary). A SHA stamped in at build time via
  `-ldflags -X` and logged at startup — ideally also returned by
  `/api/health` — would let any test assert which build it is talking to without
  inventing a marker each time.

---

## Deployment and operations

Nothing here is needed to keep developing; all of it is needed before anyone
else runs this.

- **Squash the migrations before the first real release.** There are now **five**
  files per dialect (`0001_init` through `0005_trip_subtitle`, in both
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
