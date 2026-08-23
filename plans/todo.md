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

- **The UI suite still doesn't cover data-mutating flows, or the login pages.**
  **(soon)** Three parts:
    - **Mutating flows.** The isolation pattern exists and two specs use it:
      `files.spec.js` (Stage 11 Milestone 5) creates its own trip in
      `beforeEach`, mutates inside it and deletes it in `afterEach`;
      `settings.spec.js` (Stage 12 Milestone 5) drives the seeded `other`
      account, because changing the demo user's password would delete
      `auth.setup.js`'s shared session, and restores the password afterwards.
      Note what that exposed — a *silently* failing cleanup leaves the seed wrong
      for every later run, so a restore step has to assert its own success. Every
      other mutating flow is uncovered and should copy one of those two shapes:
      the location and trip editors, checklists, and the itinerary.
      Stage 16 Milestone 9 added a third shape worth copying: `assist.spec.js`
      creates its own trip like `files.spec.js`, but also *skips itself* when
      the server lacks a capability it needs, since that capability is
      configuration rather than seed data. CI asserts the capability is on
      before running the suite, because a spec that skips silently reads as a
      pass.
      Stage 17 Milestone 7 added `expenses.spec.js`, a fourth: its own trip, and
      it *adds a second member to that trip* rather than borrowing a seeded
      shared one, so the payer, share and balance paths are covered without
      touching the memberships other specs read. That closes the expenses flow;
      the location and trip editors, checklists and the itinerary are still
      uncovered.
    - **The register page**, which no spec renders. Stage 14 Milestone 9's
      `unauthenticated.spec.js` covers the *login* screen from a fresh
      unauthenticated context, but the register form only appears when open
      signup is on, and the seed deliberately leaves it off. Covering it means
      either flipping the setting inside the spec and restoring it (the
      `settings.spec.js` shape, with the same poisoned-run hazard) or a scenario
      that seeds an open instance.
    - **German beyond the menu.** `menu.spec.js` proves the mechanism works, but
      the sweeps themselves (overflow, headings, names, tap targets) run in one
      locale, and German copy is the longer of the two — the case most likely to
      overflow a box.
- **Manual testing in a seeded trip silently breaks the UI suite.** (Stage 16
  Milestone 9.) `map.spec.js`'s distance filter asserts an exact card count in
  the seeded Iceland trip, so a single location added by hand while trying a
  feature out makes it fail -- which is exactly what happened, twice in one
  evening, from testing the assistant in `Demo: Iceland Ring Road`. The failure
  points at the map spec and says nothing about the real cause, so the time
  goes on investigating a filter that works perfectly.

  The specs that *write* already solve this for themselves by creating their
  own trip (`files.spec.js`, `assist.spec.js`, `expenses.spec.js`), but nothing
  protects the seeded scenarios from a person. Options, none obviously best:
  have `make test-ui` reseed first, which is a big hammer and would wipe
  anything else in the dev database; have the suite assert the seed is pristine
  before running and say so plainly when it is not, which is cheap and turns a
  confusing failure into an instruction; or seed a scenario specifically for
  poking at by hand and point people at it. The middle one is probably the
  right size.

  **Two concurrent suite runs do it to each other, too** (Stage 18 Milestone 2).
  Running `make test-ui` twice at once against one dev server made four tests
  fail -- both `settings.spec.js` language cases, which change the seeded
  `other` account and then restore it, so the two runs each saw the other's
  half-finished state. Same failure shape as a person poking at a seeded trip,
  same misleading report: the named failures pass in isolation, and the run that
  loses the race looks like a regression in code that is fine. The run times
  gave it away -- 4.1 and 4.5 minutes overlapping against 2.0 minutes alone.
  Whatever gets built for the entry above should also refuse to start when
  another run is already in progress, which is a lock file rather than a seed
  check.

  **It happened again in Stage 17**, and the shape is worth recording because it
  was not extra locations this time: `file row overflow menu` failed because the
  seeded `cascade` trip had gained *members* -- a stray `pwtest` and a stray
  `other` -- and that spec asserts the trip is solo, since visibility controls
  only render on a shared trip. Nothing about files or menus had changed. Two
  lessons for whatever gets built: the check has to cover memberships and not
  just child rows, and a diff against a freshly seeded database
  (`CARAVEL_DB_DSN=/tmp/x.db go run ./cmd/seed`, then compare) is what actually
  identified it -- a cheap trick worth writing into the tooling.

- **Add a Chromium project for gesture specs.** **(soon)** (Stage 13 Milestone
  1.) `playwright.config.js` runs Firefox alone, but Playwright's `isMobile` —
  the option that flips `(pointer: coarse)` and enables real touch emulation — is
  Chromium-only, and `hasTouch: true` does not do it. So `map.spec.js` stubs
  `window.matchMedia` for that one query through `addInitScript`: honest as far
  as it goes (the stub replaces the *input*; the component's real response is
  asserted), but no spec exercises an actual touch gesture — "one finger scrolls
  the page, two fingers pan the map" is verified through Leaflet's handler state,
  not by dragging. The decision taken in the Stage 15 review: add a **Chromium
  project scoped to gesture/mobile specs**, not a second full run of the sweeps —
  those are about markup and CSS, where a second engine mostly buys duplicate
  failures and doubles the CI job.
- **Contrast is measured but not asserted.** `tests/ui/contrast.js` reports
  ratios and has a `--min` flag, but nothing runs it in CI, so a regression like
  Stage 07's 2.54:1 primary button would only be found by someone running it.
  Turning it into a spec needs a decision about which elements have a defensible
  threshold (decorative fills and large text differ), which is why it stayed a
  measurement tool. Two parts worth keeping whatever shape it takes: flattening
  translucent backgrounds over whatever is behind them (the danger tint is
  `rgba(...)`, so a naive reading measures against transparency and reports
  nonsense), and reaching into shadow roots.
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
- **`scripts/without.sh` reports success on *any* non-zero exit.** So a command
  that fails for an unrelated reason reads as proof that the test depends on your
  change. Learned twice: in Stage 09 Milestone 6 a typo in a `GREP` pattern meant
  Playwright ran no tests at all, and in Stage 10 Milestone 5 reverting the file
  under test removed a struct the *test* referenced, so `go test` failed to
  **compile** and the script reported "OK — genuinely depends on your change"
  with not one assertion having run. A compile error reads as a test failure in
  the output, which makes that case the least likely to be noticed. The fix is to
  tell "the command failed" apart from "the command never ran" — or at least echo
  enough output that the reader can tell. (Two other limitations — a
  `--commit <sha>` mode and a `--break` mode — were considered in the Stage 15
  review and dropped: both have a one-line manual equivalent, so there is no
  dance worth automating. This one stays because it makes the tool give a
  confident *wrong* answer, which is the exact failure it exists to prevent.)

---

## Deployment and operations

Nothing here is needed to keep developing; all of it is needed before anyone
else runs this.

- **`HEALTHCHECK` is invisible to a podman build.** (Stage 18 Milestone 7.)
  `podman build` defaults to the OCI image format, which has no HEALTHCHECK
  instruction, so it drops ours with a warning nobody reads in the middle of a
  build log -- `--format docker` is required. CI uses docker, so published
  images carry it; anyone building locally with podman gets an image whose
  health is silently unknown. Options: mention it in the docs only (done), or
  add a `make image` target that passes the right flag for whichever tool is
  installed, the way `scripts/test_postgres.sh` already does for compose.

- **The squash is a one-way door, and nothing enforces it.** (Stage 18
  Milestone 6.) The schema is one `0001_init` pair per dialect now, and any
  database created before that refuses to start: `no migration found for version
  12`. That is correct and nobody has such a database -- but the same reasoning
  says this must never happen again once images are published, and there is no
  mechanism preventing it. A future squash would silently brick every deployed
  instance. Options: a comment at the top of `0001_init` (weak), a check in CI
  that the migration count only ever grows (cheap, catches the mistake in
  review), or leaving it to judgement. Worth deciding before the first release
  rather than after.

- **`auth.SetPassword`'s doc comment is wrong.** (Stage 18 Milestone 10.) It
  says the function is "deliberately not reachable from any HTTP route" and that
  it exists for `cmd/seed` -- but `handleAdminResetPassword` calls it, which is
  how an admin resets somebody's password. The behaviour is right and
  deliberate (an admin reset leaves sessions alone, unlike a self-service
  change, and `admin.resetPasswordPrompt` tells the admin so); only the comment
  is stale, and it is the kind of comment somebody trusts when reasoning about
  session invalidation. One-line fix.

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
