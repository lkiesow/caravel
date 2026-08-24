# Caravel — TODO / Backlog

Everything below is **not yet built**. This is the single backlog: raw input for
planning the next stage.

Sections group by *kind of work*, not by which stage deferred something.

**Conventions.** Add new notes straight into the section they belong to. When a
milestone implements an entry, delete it; when a milestone changes what's left
of one, rewrite it. A stale "still outstanding" item that was quietly built is
worse than a missing one, so both directions matter.

Entries tagged **(soon)** are the ones marked as wanted in one of the next few
stages, in the Stage 15 backlog review. Everything else is untagged and
unordered — worth keeping, not worth scheduling. The review also deleted about a
third of the file: entries whose remaining sliver wasn't worth the words, and
decisions that were already made and only being re-litigated by being written
down.

---

## Bugs and rough edges

- **A cover photo set by URL on the *new trip* form is only validated at Create
  time.** (Stage 07; half-fixed in Stage 09 Milestone 4.) The URL is staged
  locally, the trip is created, and only then does the server fetch it — so a
  failure dialog arrives after a create that partly succeeded. A real fix means
  validating at "Set image" time, which needs a trip-independent validation
  endpoint. Softened a lot by Stage 07 Milestone 9's preview-error handler: a URL
  the browser itself can't load is already flagged in the card before Create is
  pressed.
- **A failed checklist tick leaves the box disagreeing with the server.**
  (Noticed in Stage 20 Milestone 5.) `checklist-list.js`'s item checkbox has no
  `try/catch`: if the PATCH fails, the box keeps the state the click gave it
  while the server holds the other one, and nothing says so. The admin page's
  open-signup toggle is the model -- it puts the box back and prints a message.
  Stage 20 guarded the box against a *second* request; it did not give it an
  error path.
- **A new location's cover photo and files are still a post-create upload.**
  (The remainder of "create-mode writes aren't atomic", Stage 06 Milestone 4.)
  Stage 09 Milestones 1–2 made the location and its links/dates one transactional
  request, but a photo and files can't ride in a JSON body, so they're staged in
  memory and `flushUploads()` writes them once the create returns an ID. If that
  fails the location exists without them; the failure reports inline in the Basic
  info card and the page stays put, so it can be retried. Closing the gap
  entirely means a multipart create endpoint — not obviously worth it.

---

## Planned features

- **Expenses: what Stage 17 left out.** (Stage 17.) The feature shipped --
  expenses, per-trip currency, who paid, per-expense share sets, per-person
  totals and balances with suggested transfers. What it deliberately does not
  do:
    - **Linking an expense to a location.** The original Stage 01 sketch had an
      optional `item_id`, which would give a per-location cost on the location
      view. One nullable column and a select.

- **SearXNG as a search backend.** (Stage 16 Milestone 8.) Planned for that
  milestone and dropped: nobody had an instance to test against, and a backend
  verified only against a fake is a backend nobody should trust. Everything
  needed already exists -- the `Searcher` interface in `internal/assist` takes
  a ~60-line implementation, `CARAVEL_SEARCH_URL` already carries a
  self-hosted address (ddgs uses it), and `config.SearchProviders` is one
  string longer. What it needs is somebody with a running SearXNG. Two things
  to know when picking it up: the JSON output format is disabled by default and
  has to be added to `search.formats` in `settings.yml`, and it overlaps
  heavily with ddgs, which shipped -- both are self-hosted keyless metasearch,
  so this is for people who already run one rather than a gap in coverage.
- **The assistant's links and sources are not covered by the UI suite.**
  (Stage 16 Milestone 9.) `tests/ui/assist.spec.js` asserts on the suggested
  fields but not on the two list sections, because the stub provider cannot
  produce either: its URLs point at `example.invalid`, so the proposed link is
  correctly dropped as dead and the failed page fetch records no source. Both
  behaviours are right, which is what makes this awkward. The alternatives were
  rejected in that milestone -- giving CI a network dependency, or letting
  `CARAVEL_LLM_URL=stub` relax the fetcher's SSRF guard, which is a config
  value weakening a security control. Both lists have Go tests and were
  verified by hand against real providers. Closing it properly probably means a
  second fake that serves pages from an in-process `httptest` server the
  fetcher is allowed to reach, which is a bigger change than it sounds because
  the guard refuses loopback by design.
- **Auto-filling a location's cover image.** (Stage 16, deliberately out of
  scope.) The workable route is the model returning a Wikipedia article title
  and Caravel pulling the lead image through the Wikimedia API -- a real photo
  with a known licence and attribution, where generic web image search is a
  licensing landmine. Needs a Wikimedia client, an attribution field on
  `media_assets` and UI to show the credit.
- **AI trip-level suggestions.** (Stage 15 backlog review.) "Suggest things to
  do in Reykjavik" returning several candidate locations to add at once, rather
  than enriching one location at a time. **No longer blocked**: Stage 16 built
  the single-location version, so the provider, the search backends, the tools,
  the agent loop, the guard rails, the SSE transport and the stub are all in
  place and this reuses every one of them. What is genuinely new is a
  multi-result review UI, a way to add N locations in one transaction, and
  dedup against what the trip already has.
- **Account settings: what Stage 12 left out.** The screen, appearance and
  language controls and password changing all landed in Stage 12
  (Milestones 2-5). Two things remain:
    - a **profile picture**, which has a schema wrinkle: `media_assets.trip_id`
      is `NOT NULL` and cascades from `trips`, so a user-scoped image has no
      valid home today and deleting a trip would take an avatar with it. Needs a
      migration — nullable `trip_id`, a `user_id` column, or a separate table —
      before the existing upload pipeline can be reused.
    - **per-account preferences.** Appearance and language are both stored per
      browser (`localStorage["caravel.theme"]` / `["caravel.locale"]`), the
      deliberate Stage 12 decision. Making either follow the account means a
      column on `users` and an endpoint to write it — worth answering once for
      both, not twice.
- **Better Google Maps interoperability.** Two halves. *Inbound:* pasting a
  shortened link such as `https://maps.app.goo.gl/xfB9TzpFos2N4oAW8` into a
  location should resolve to coordinates, which means following the redirect
  server-side (the short form carries nothing parseable) and pulling lat/lng out
  of the expanded URL — so it wants the same proxy-and-limiter treatment
  `/api/geocode` got in Stage 13 Milestone 5. *Outbound:* the popup's and the
  location view's "View on Google Maps" links are a `?q=lat,lng` **search**, so
  they land on a dropped pin rather than on the hotel's own Google entry with its
  hours and reviews. Linking the actual place needs a place ID, which Caravel
  cannot get from OSM — so this half is blocked on either storing a user-pasted
  Google URL per location or accepting the search link as good enough.
- **A trip journal with photos.** (Stage 01.) A `journal_entries` table
  (trip_id, date, body markdown) reusing the existing `media_assets` pipeline
  for photos.
- **Reverse geocoding.** (Stage 13 Milestone 5.) `/api/geocode` turns a name into
  coordinates; the opposite — clicking the map and getting a suggested address
  for the `address` field — is not built. Nominatim has a `/reverse` endpoint, so
  it is a second handler on the same proxy and the same limiter, but it needs a
  decision about whether it fills the field automatically (surprising, and it
  would overwrite a hand-written address) or offers the result to accept.
- **Federation between self-hosted instances.** (Stage 01.) Real sync-protocol
  design still needed; v1 only avoided the integer-PK and local-only-ID mistakes
  that would have made it harder later.

---

## Multi-user and sharing

- **Trip ownership transfer.** (Stage 14 Milestones 3 and 10.) `trips.owner_id`
  is the only record of who owns a trip and the owner has no `trip_members` row,
  which makes "exactly one owner" true by construction — and also means the only
  ways out of owning a trip are deleting it or leaving it in someone else's hands
  informally. The API already points at the gap: removing the owner answers 409
  `owner_cannot_leave`. A transfer is one UPDATE plus a members row for the old
  owner, so the schema is ready; what is undecided is what happens to the
  previous owner (demoted to editor, or removed outright) and to their personal
  files and checklists on a trip that is no longer theirs. The member-removal
  dialog answers that same question today — copy its decision rather than invent
  a second one.
- **Public shareable links.** An unauthenticated read-only trip view via a token.
  Needs a `share_links` table (token, trip_id, scope, expires_at) plus an
  unauthenticated route variant. Cheaper than when this was written: Stage 14
  gave every trip-scoped handler a minimum role through one seam
  (`internal/httpapi/authz.go`), so a link-holder is a synthetic `viewer`
  resolved from a token instead of a session, and read-only mode already exists
  in the frontend. The genuinely new part is the token lifecycle. Note that
  `scope` has to answer the visibility question too: a public link must never
  expose a `personal` file or checklist, whoever created the link.
- **Invite links / joining by token.** Adding a member needs their exact username
  today (Stage 14 Milestone 3), which is fine on a self-hosted instance where you
  know who you are inviting. This only becomes genuinely interesting **after
  federation**, where the person you are inviting is not a user of your instance
  at all — so treat it as a federation follow-on rather than a share-link
  follow-on.

---

## Consistency and cleanup

- **Three near-identical input rules in `base.css`, with drift.** **(soon)**
  (Stage 14 Milestone 3 follow-up.) `.auth-form`, `.trip-form`/`.password-form`
  and `.item-form` each declare their own label-and-input styling, and they do
  not quite agree: `.auth-form` uses `border-radius: var(--radius)` and
  `font-size: 1rem` where the other two use `0.375rem` and `font: inherit`. The
  Members form was joined onto the `.trip-form` group rather than becoming a
  fourth copy, which is right for one form and not a fix for the pattern — the
  next form added has three plausible rules to pick from and no reason to prefer
  one. Consolidating means picking the canonical values, which changes how the
  auth pages look, slightly. Precedent for how: the same file's error-callout
  rule already collapsed five copies into one.
- **Two busy states that are not the shared guard.** (Stage 20.) Everything
  mutating now goes through `web/js/busy.js`, with two deliberate exceptions.
  `assist-panel.js` keeps its own `running` flag, because it also owns stream
  cancellation and a Cancel button, and `file-list.js`'s drop zone keeps its own
  `aria-busy` plus `.file-drop--busy { pointer-events: none }` around a
  sequential batch upload. Both work; neither is the shared idiom, so a reader
  finds three ways of saying "in flight". Folding either in means teaching the
  guard about a set of controls rather than one, and about cancellation.
- **A dropped reorder is dropped, not queued.** (Stage 20 Milestone 5.) The
  itinerary's move up/down is optimistic: it redraws before the PUT answers, so
  the pressed button no longer exists to disable and the guard's flag is the
  only thing stopping a second press. That press therefore does nothing at all -
  correct (two overlapping reorders can be answered in either order, leaving the
  stale one stored) but not kind. Queueing the second move, or sending the final
  order once the first answers, would be.
- **`confirmDialog` hardcodes a trash icon for every danger confirmation.**
  **(soon)** (Stage 14 Milestone 3.) `components/dialog.js` picks the icon from
  `danger` alone (`trash-2` when set, `check` when not), which is right for the
  delete confirmations it was written for and wrong for "Leave trip" — an action
  that removes no data and shows a bin anyway. The *label* half was fixed in that
  milestone (both member confirmations pass their own `confirmKey`), so what is
  left is one optional `iconName` on `confirmDialog`, plus a decision about
  whether `danger` should keep implying an icon at all.
- **Identifier sweep: "item" → "location".** Stage 05 fixed the user-visible
  copy, so what's left is entirely below the surface: the whole
  `item.detail.*`/`item.category.*`/`item.deleteConfirm` i18n namespace is still
  item-flavoured despite `location.form.*`/`location.editor.*` having migrated,
  and on the JS side `location-form.js` exports `renderItemForm`,
  `locations-tab.js` exports `renderItemsTab` and uses `data-action="new-item"`,
  and the list renders `<item-card>`. The API and schema say `items` too
  (`/api/items/{id}`, the `items` table), so this has a choose-your-depth
  question. Precedent for going all the way down: Stage 11 Milestone 1's
  "documents" → "files" rename included a `0006` table rename and dropped the
  `/trips/:id/documents` URL outright rather than redirecting it.
- **Number and date formatting follows the *browser* locale, not the app's.**
  (Stage 17 Milestone 3; narrowed by the Milestone 6 follow-up, which fixed the
  one case that was not merely cosmetic -- `Intl.ListFormat` now takes
  `getLocale()`, because it rendered "Nur für Other User *and* dich", an English
  conjunction inside German copy. What is left is the money and date formatters,
  where the browser locale is a defensible choice rather than a bug.) `format.js` calls `Intl` with an undefined locale
  throughout -- `formatDateRange`, the itinerary day headings, and now
  `formatMoney` -- which is a deliberate, pre-existing decision, documented in
  that file. Money makes the consequence louder than dates did: with the app
  switched to German, a total still renders as EUR 97.55 rather than 97,55 EUR,
  and a day heading still reads Thu 20 Aug. Nothing is *wrong* -- the numbers
  and dates are right and unambiguous -- but the app claims to be in German
  while formatting as though it were not. The fix is to pass `getLocale()` (or
  the resolved locale behind "auto") to every `Intl` constructor, which is a
  handful of call sites in one file. What it needs first is a decision: the
  browser locale is arguably the *better* source for number formats, because it
  is what the rest of that person's computer does, and someone reading a German
  UI may still want their own separators.

- **The social card is committed twice.** (Stage 18 Milestone 1.) The same
  1200x630 PNG lives at `web/brand/og-card.png`, because the app's `og:image`
  has to be served by the app, and at `docs/assets/brand/og-card.png`, because
  the documentation site and the README consume it as an image. ~100 KiB of
  duplicate bytes is not the problem; drift is -- regenerate one from
  `docs/assets/brand/src/og-card.svg` and the other silently stays old. Options:
  serve the docs copy from the site and let the app point at it (no, an instance
  must not depend on an external host), have the build copy one to the other (no
  build step for `web/` today), or accept it and document the pairing where the
  regeneration recipe lives.

- **A rendered note has no shared styling between the two places it appears.**
  (Stage 15 Milestone 3.) The editor's preview and the view page produce
  identical HTML — both from `renderNotesHTML` — but sit in different containers:
  `.notes-field__preview` has a dashed border, padding and a tinted background so
  it reads as a rendering rather than a field, while `.location-view__notes`
  carries no CSS rules at all. So the *content* cannot drift and the
  *presentation* already does. Resolving it means one rule owning "a rendered
  note" with both callers pointing at it — cheap, but it changes how the view
  page looks, which is why it was not done inside a preview milestone.

---

## Testing, CI and dev tooling

- **The sweeps run in German at one combination only.** (Stage 19 Milestone 4.)
  `routes.spec.js` sweeps overflow and tap targets in German at mobile/light,
  and `a11y-names.spec.js` runs both languages; `headings.spec.js` deliberately
  does not, because heading levels are structural and identical in every
  language. Two limits worth knowing before trusting the German pass further:
  it covers one viewport and one colour scheme, on the argument that neither
  assertion depends on colour and 324px is where width bites; and the name
  sweep only catches controls whose *only* name is the translated string, so an
  empty label on anything with visible text is still invisible to it. A third
  language would multiply this decision rather than inherit it.
- **An itinerary entry cannot be moved to another day.** (Stage 19 Milestone 3.)
  `internal/httpapi/itinerary.go` has create, reorder and delete for an entry and
  nothing that reassigns its `itinerary_day_id`; there is no client affordance
  either. So rescheduling something means removing it from one day and adding it
  again on the other, losing any note on the entry in the process. The reorder
  endpoint already renumbers a whole day inside a transaction, so a move is the
  same shape across two days -- the awkward part is the API, since entries are
  addressed as `/itinerary/days/{dayId}/entries/{entryId}` and a move changes the
  first half of that path. Found while writing the spec: the milestone had
  planned to *test* this, on the assumption it existed.
- **Two concurrent UI runs still share Playwright's `test-results/`.** (Stage 19
  Milestone 1.) Everything else about a run is now private to it -- port,
  database, uploads, saved sessions -- but the output directory is still the
  repository's, and Playwright empties it at startup. So a second run starting
  while the first is going deletes the first run's traces and screenshots. It
  cannot make a passing run fail, because nothing is written there unless a test
  fails, which is why it was left alone: fixing it means either a per-run output
  directory (and then CI's artifact upload has to find it) or accepting that
  failure artifacts from concurrent runs are best-effort.
- **A fast `GREP` can spend the login budget by itself.** (Stage 19 Milestone
  1.) Login is limited to 10/min/IP, and the limiter now lives in the run's own
  server rather than being shared with everything else on the machine -- an
  improvement. But `auth.setup.js` spends two, and a `GREP` that selects a
  login-heavy subset (the settings specs, say) finishes in twenty seconds and
  spends the rest, so the run fails on a 429 whose message reads like a broken
  seed -- it names the seed, not the limiter. The full suite is spread out
  enough not to hit it. Options: raise the limit when the assistant stubs are on
  (configuration weakening a security control, rejected once already in Stage
  16), have the specs share one login the way `auth.setup.js` does, or teach
  the message to name the 429 as its own cause.
- **Only two gestures are driven with real touch.** (Stage 19 Milestone 5.)
  `map.gesture.spec.js` covers one-finger scroll and two-finger pan in the
  `chromium-gestures` project; every other touch interaction in the app -- the
  marker drag in the coordinate picker, the itinerary reorder buttons, swiping
  anywhere -- is still only exercised with a mouse, or through Firefox with the
  `(pointer: coarse)` stub. Adding to the project is cheap now that it exists
  (a `*.gesture.spec.js` file is all it takes), so this is a note about reach
  rather than a gap that needs a plan. Mind the trap that produced a false pass
  the first time: CDP silently delivers nothing outside the viewport, so a
  target below the fold must be scrolled into view or the test passes having
  touched nothing.
- **The contrast gate covers three routes and one exemption.** (Stage 19
  Milestone 6.) `make check-contrast` sweeps `/trips`, `/settings` and
  `/trips/new` in both palettes, holding each element to its own WCAG threshold,
  with `.app-brand` exempt as a logotype. What it does not reach: every trip
  tab, the location editor, the map legend and the dialogs -- all of which the
  script *can* measure, since it pierces shadow roots, but none of which is in
  the route list. Widening it is a line in the Makefile plus whatever the
  numbers then say; the reason it starts narrow is that each route costs two
  browser page loads and nobody has read the numbers for the rest yet.
- **The suite waits on injected plumbing, not on the app's own state.** Stage 09
  Milestone 5 gave every route a `common.loading` line, which fixes the
  user-facing half of the old "empty shell" problem but not the suite's: a
  loading line carries no `<h1>`, so `gotoRoute` must wait for fetches to settle
  before asserting heading outlines, and that wait is a `window.fetch` wrapper
  injected by `tests/ui/helpers/scenarios.js`. A `data-loading` attribute on
  `#app`, or a "ready" event, would let the suite wait on a contract the app
  publishes. One component already does exactly that: `leaflet-map.js` sets
  `data-ready` once the map has laid out (Stage 13 Milestone 3), and `gotoRoute`
  waits for it — Leaflet is lazily imported *after* a route's fetches settle, so
  "fetches quiet plus two frames" did not mean the map was up. That is the shape
  the app-wide version wants.
- **`scripts/i18n.py`'s dynamic-prefix rule only fires inside a `t()` call.**
  (Stage 13 Milestone 8.) `DYNAMIC_CALL_RE` matches a template literal as the
  argument of `t(`, so a key composed anywhere else is invisible: Milestone 6's
  `locateErrorKey()` returned `` `map.locate.${reason}` `` from a helper, and all
  five real reason keys were reported as unused. They were not deleted — the
  report's own warning did its job — but the next person might, and the fix that
  milestone applied (spell the keys out in a lookup object, which the bare-string
  pass *does* see) is a workaround at the call site rather than in the tool.
  Teaching the scan to follow a function that returns a composed key is hard;
  recognising a template literal assigned to a `const` whose name ends in `Key`,
  or an explicit allowlist comment, would cover the realistic cases.

---

## Deployment and operations

Nothing here is needed to keep developing; all of it is needed before anyone
else runs this.

- **The RPM has no repository, and no real-host verification of its unit.**
  (Stage 18, RPM follow-up.) Packages are attached to each GitHub release, so
  installing is `dnf install ./caravel-....rpm` and upgrading means fetching the
  next one -- there is no `dnf update` path without a yum repository (COPR, or
  a repo published to GitHub Pages beside the docs, which is where it would
  naturally live). Separately, the unit's hardening directives
  (`ProtectSystem=strict` and friends) have only been exercised in a
  *privileged* container: rootless refuses at step USER for want of
  CAP_SYS_ADMIN, which is the container's limitation rather than the unit's, but
  a real host is the only place that settles it.

- **Generated release notes will be nearly empty until changes arrive as pull
  requests.** **(soon)** (Stage 18, releases-on-tag follow-up.) GitHub builds the
  notes `.github/workflows/release.yml` asks for from **merged pull requests**
  since the previous tag. This repository has no merge commits at all -- every
  change is pushed straight to main -- so a release will show the compare link
  and not much else. Three ways out, and it is worth picking one before the first
  real release rather than after: start routing changes through labelled PRs (the
  grouping config in `.github/release.yml` is already there for it), generate the
  notes from `git log` between tags instead and pass them as the release body, or
  accept thin notes and treat the compare link as the changelog. The stage plans
  are arguably the real changelog either way.

- **The documentation screenshots go stale silently.** (Stage 18 Milestone 11.)
  `make screenshots` regenerates them and `scripts/check_screenshots.py` keeps
  the set and the pages in agreement, but nothing notices when the UI moves and
  the committed captures start showing an older layout. There is no cheap
  automatic answer -- comparing images would fail on every unrelated pixel -- so
  the realistic options are a reminder in the stage workflow (regenerate
  whenever a milestone changes a screen that appears in the tour) or a periodic
  refresh. Worth deciding rather than drifting.

- **The screenshots depend on photographs that are not in the repository.**
  (Stage 18 Milestone 11.) The set is dressed from `images/`, which is
  deliberately untracked -- the author's own photos, used temporarily and not
  published as files. So anybody else regenerating gets the seeder's 343x200
  test-sheet fixtures instead, and the output will not match what is committed.
  The script says so loudly rather than pretending otherwise. If the project ever
  wants reproducible-by-anyone screenshots, it needs a small set of
  known-licence photographs committed for the purpose.

- **The Zensical pin needs periodic review, in two files.** (Stage 18 Milestone
  9.) `zensical==0.0.57` is pinned in `.github/workflows/docs.yml` and in
  `ci.yml`'s `docs` job, deliberately, because a 0.0.x generator can change its
  output between patch releases. That means the site silently stops receiving
  fixes until somebody bumps it -- including the two 0.0.57 bugs `home.html`
  currently works around (the `page.is_homepage` flag being falsy for
  `docs/index.md`, and the skip link pointing at a markdown-derived anchor that
  an emptied content block does not render). When bumping, drop the workarounds
  and re-check the landing page title and skip link.

- **The site ships a font face it never loads.** (Stage 18 Milestone 9.)
  `scripts/gen_brand_fonts.py` writes both weights to `docs/assets/fonts/`, but
  nothing on the site uses Montserrat 500 -- `document.fonts` confirms it stays
  unloaded, so it costs a committed 17 KiB and no request. Harmless, and the
  cost of the alternative is a generator whose two destinations differ. Revisit
  if a docs page ever wants the 500, or trim it if none ever does.

- **Nothing tests the documentation site's rendering.** (Stage 18 Milestone 9.)
  `zensical build --strict` catches dead links and unresolved references, which
  is what CI gates on, but the landing page's layout, contrast and both-palette
  behaviour were verified by hand with the Playwright MCP tools -- there is no
  committed assertion, unlike `tests/ui/brand.spec.js` for the app. A spec
  would need the built site served, which the Playwright config does not do
  today. Worth it if the landing page grows; overkill for one page.

- **Vector tiles, for map labels in the user's own language.** (Surfaced by
  the tile-source change of 2026-08-25.) `CARAVEL_TILE_URL` now lets an
  operator pick a provider whose labels are latin script, or one language
  chosen for the whole instance -- but not one that follows each user's own
  preference, because raster tiles are drawn before anyone asks. The answer is
  vector tiles: MapLibre GL against OpenFreeMap (no key, no request limits,
  unmodified OpenMapTiles schema, which carries both `name:en` and `name:de` --
  exactly the two locales `web/js/i18n.js` supports), with the label
  `text-field` set to coalesce `name:<locale>`, `name:latin`, `name` and
  re-applied on the `locale-changed` event the i18n module already dispatches.
  The cost is what makes it a stage of its own rather than a follow-up:
  vendoring MapLibre GL beside the Leaflet copy, rewriting all 717 lines of
  `web/js/components/leaflet-map.js` (markers as symbol layers or DOM overlays,
  popups, `fitBounds`, pick mode, the locate control), and reworking
  `tests/ui/map.spec.js` and `map.gesture.spec.js`, including Stage 13's
  two-finger gesture handling, which is expressed entirely in Leaflet handler
  terms.

- **The committed screenshots still show the default tile provider.**
  (Surfaced by the tile-source change of 2026-08-25.) `make screenshots` runs
  its own throwaway server, so `docs/assets/screenshots/map.png` shows whatever
  that server is configured with -- OpenStreetMap, and correct for a stock
  install. If the documented default ever changes, the set needs regenerating
  or the map page will illustrate a provider nobody uses.

- **S3-compatible object storage.** Swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze, and
  so on); the interface already isolates callers from the backend.
- **Prometheus/OpenMetrics metrics.** A `GET /metrics` endpoint via
  `promhttp.Handler()`, outside `/api` and outside the session-auth middleware.
  Note: Stage 01's plan described that routing reservation as already in place,
  but there is no `metrics` reference anywhere in `internal/` or `cmd/` today —
  the route needs adding along with the instrumentation (HTTP request
  count/duration/status, DB query duration, upload counts/sizes, session counts).
- **OpenID Connect / external auth providers.** `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no provider
  integration exists yet.
