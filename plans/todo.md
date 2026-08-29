# Caravel — TODO / Backlog

Everything below is **not yet built**. This is the single backlog: raw input for
planning the next stage.

Sections group by *kind of work*, not by which stage deferred something.

**Conventions.** Add new notes straight into the section they belong to. When a
milestone implements an entry, delete it; when a milestone changes what's left
of one, rewrite it. A stale "still outstanding" item that was quietly built is
worse than a missing one, so both directions matter.

Entries tagged **(soon)** are the ones marked as wanted in one of the next few
stages. Everything else is untagged and unordered — worth keeping, not worth
scheduling.

**Reviewed 2026-08-29** (the second full review; the first was in Stage 15).
Every entry was read out and kept, tagged, or dropped deliberately. Roughly half
the file went: fixes already made in all the ways that mattered, decisions that
were settled and only being re-litigated by being written down, and notes about
things nobody intends to change. Anything deleted in that review was deleted on
purpose — do not reconstruct it from an older stage plan without asking.

---

## Bugs and rough edges

- **The assistant's configured limits never reach the server.** (Found while
  planning Stage 24.) `CARAVEL_ASSIST_RATE_LIMIT` and
  `CARAVEL_ASSIST_MAX_CONCURRENT` are parsed (`internal/config/config.go:193`),
  range-checked, documented (`docs/configuration/assistant.md:78`), sampled
  (`.env.sample:150`) and logged at startup with their effective values
  (`cmd/caravel/main.go:138-152`) -- but the `httpapi.Options` literal at
  `cmd/caravel/main.go:155-171` never sets either field, so `NewServer` always
  falls back to `DefaultAssistRateLimit` (6) and `DefaultAssistMaxConcurrent`
  (4). Only tests set them. So the two variables do nothing, and the startup log
  reports a number the running server is not using -- which is worse than no log
  at all, because it is the line somebody would check. Two lines in main.go,
  plus a test that a configured limit actually bites: the existing
  `newTestServerWithOptions` path proves the plumbing works from `Options`
  inward, so what is missing is a check that `config` reaches `Options`.

- **The zoom hint names Ctrl on every platform.** (Stage 23 Milestone 6.) The
  gate accepts Ctrl *or* Meta, so Cmd + wheel zooms on a Mac, but the string
  `map.ctrlZoomHint` says "Ctrl" everywhere -- Google Maps shows the Mac key
  instead. Fixing it means either platform detection in the component or two
  strings chosen at render time, and neither is worth it until somebody runs
  this on a Mac and says so. Note also that macOS binds Ctrl + wheel to its own
  screen zoom, so a Mac user pressing the key the hint names may get the
  operating system rather than the map -- which is the other half of the reason
  the string should probably follow the platform.

---

## Planned features

- **AI trip-level suggestions.** **(soon)** (Stage 15 backlog review.) "Suggest
  things to do in Reykjavik" returning several candidate locations to add at
  once, rather than enriching one location at a time. **No longer blocked**:
  Stage 16 built the single-location version, so the provider, the search
  backends, the tools, the agent loop, the guard rails, the SSE transport and
  the stub are all in place and this reuses every one of them. What is genuinely
  new is a multi-result review UI, a way to add N locations in one transaction,
  and dedup against what the trip already has.

- **Google Maps interoperability: the outbound half.** **(soon)** (Stage 13; the
  inbound half built in Stage 22 Milestone 6.) Pasting a Maps link into the
  address search now resolves it to coordinates. What is left is the other
  direction: the popup's and the location view's "View on Google Maps" links are
  a `?q=lat,lng` **search**, so they land on a dropped pin rather than on the
  hotel's own Google entry with its hours and reviews.

  **Investigate before deciding.** Linking the actual place needs a place ID,
  which Caravel cannot get from OSM -- but the old conclusion that this leaves
  only "store a user-pasted Google URL per location or accept the search link"
  was drawn without looking at what is available. Serper has a maps search
  endpoint that returns place IDs, and it is not the only such service; there
  may also be a URL form that resolves a name plus coordinates to the right
  entry without an ID at all. Survey the options (cost, key requirement, terms,
  whether a self-hosted instance can use them at all), *then* pick between
  automatic resolution, a pasted URL per location, and the status quo.

- **Assistant round trips: batching and parallel tool dispatch.** **(soon)**
  (Stage 21 Milestone 4b, dropped after 4a was measured.) `agent.go` dispatches
  a turn's tool calls in a plain sequential loop, which reads oddly beside
  `checkLinks` in the same file -- that already fans out with a `WaitGroup`. Two
  halves: prompt the model to request several page reads in one turn rather than
  one at a time, and run a turn's calls concurrently. Results must be appended
  in call order, because a `tool` message has to follow its `tool_calls` and most
  servers reject a mismatch, so they go into a slice indexed by call; and the
  tool-call ceiling has to be decided before the fan-out rather than inside it.

  **Take this on as tidiness, not as a speed fix.** All tool calls together are
  ~12% of a run at ~1.1s each, and only a turn issuing two or more benefits at
  all. 4a measured: with a standard deviation of ~2.9s on an ~8.9s mean,
  detecting a 10% change needs roughly 180 runs per arm, and this targets the
  same order of effect. Expect a possible second of dividend and a loop that
  reads like the rest of the file; do not expect a measurable run to get faster.

- **Prompt caching for the assistant.** (Stage 21 Milestone 4; the two companion
  levers -- a reasoning-effort knob and mechanical conversation compaction --
  were measured and dropped in the 2026-08-29 review.) Every turn resends the
  whole conversation, and OpenRouter and others can cache the repeated prefix.
  Never measured, so the size of the prize is unknown; it is the one lever that
  would help every request in a run rather than one of them. Context for
  expectations: 85% of a run is the model, spread over roughly 4.4 sequential
  requests, and switching the instance to `nvidia/nemotron-3.5-lightning` took a
  Tokyo Tower run from 59.1s to 16.4s -- more than any code change is likely to.

- **SearXNG as a search backend.** (Stage 16 Milestone 8.) Planned for that
  milestone and dropped: nobody had an instance to test against, and a backend
  verified only against a fake is a backend nobody should trust. Everything
  needed already exists -- the `Searcher` interface in `internal/assist` takes
  a ~60-line implementation, `CARAVEL_SEARCH_URL` already carries a
  self-hosted address (ddgs uses it), and `config.SearchProviders` is one
  string longer. What it needs is somebody with a running SearXNG. Three things
  to know when picking it up: the JSON output format is disabled by default and
  has to be added to `search.formats` in `settings.yml`; it overlaps heavily
  with ddgs, which shipped -- both are self-hosted keyless metasearch, so this
  is for people who already run one rather than a gap in coverage; and since
  Stage 21 Milestone 7 image search is an optional capability a `Searcher` may
  also implement, so a SearXNG backend should do its images category too rather
  than only the text half.

- **Web search may want to leave `internal/assist`.** (Stage 21 Milestone 7.)
  `Searcher` and its four backends live in that package because the assistant
  was their only consumer. It no longer is: the image picker uses the same
  backend, `cmd/caravel` builds it and `internal/httpapi` type-asserts
  `assist.ImageSearcher` off it, so a package named for the assistant is now
  imported for something with no LLM in it. An `internal/websearch` in the shape
  of `internal/geocode` would be the honest arrangement. Mechanical but wide --
  every test in `internal/assist` names one of these types.

- **Account settings: a profile picture.** (Stage 12; the screen, appearance and
  language controls and password changing all landed there in Milestones 2-5.)
  It has a schema wrinkle: `media_assets.trip_id` is `NOT NULL` and cascades
  from `trips`, so a user-scoped image has no valid home today and deleting a
  trip would take an avatar with it. Needs a migration — nullable `trip_id`, a
  `user_id` column, or a separate table — before the existing upload pipeline
  can be reused.

- **A trip journal with photos.** (Stage 01.) A `journal_entries` table
  (trip_id, date, body markdown) reusing the existing `media_assets` pipeline
  for photos.
- **Federation between self-hosted instances.** (Stage 01.) Real sync-protocol
  design still needed; v1 only avoided the integer-PK and local-only-ID mistakes
  that would have made it harder later.

---

## Multi-user and sharing

- **Invite links / joining by token.** Adding a member needs their exact username
  today (Stage 14 Milestone 3), which is fine on a self-hosted instance where you
  know who you are inviting. This only becomes genuinely interesting **after
  federation**, where the person you are inviting is not a user of your instance
  at all — so treat it as a federation follow-on.

---

## Consistency and cleanup

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
  where the browser locale is a defensible choice rather than a bug.)
  `format.js` calls `Intl` with an undefined locale throughout --
  `formatDateRange`, the itinerary day headings, and now `formatMoney` -- which
  is a deliberate, pre-existing decision, documented in that file. Money makes
  the consequence louder than dates did: with the app switched to German, a
  total still renders as EUR 97.55 rather than 97,55 EUR, and a day heading
  still reads Thu 20 Aug. Nothing is *wrong* -- the numbers and dates are right
  and unambiguous -- but the app claims to be in German while formatting as
  though it were not. The fix is to pass `getLocale()` (or the resolved locale
  behind "auto") to every `Intl` constructor, which is a handful of call sites
  in one file. What it needs first is a decision: the browser locale is arguably
  the *better* source for number formats, because it is what the rest of that
  person's computer does, and someone reading a German UI may still want their
  own separators.

---

## Testing, CI and dev tooling

- **The UI suite reaches the real Nominatim.** (Stage 21 Milestone 1.)
  `scripts/with_server.sh` sets `CARAVEL_LLM_URL=stub` and
  `CARAVEL_SEARCH_PROVIDER=stub` but leaves `CARAVEL_GEOCODER_URL` at its
  default, which is `nominatim.openstreetmap.org`. So the coordinates
  suggestion in `assist.spec.js` -- and the address search in
  `locations.spec.js` -- depend on a live call to a third party, on a service
  with its own rate limits and its own opinion about automated traffic. The
  assist spec's own header says CI has no network budget, which is true of
  everything except this. Nobody has been bitten yet, and it is a real
  dependency all the same: the fix is a stub geocoder behind the same sentinel
  the LLM and search providers already use, so the suite stops asking a
  volunteer-run service for the same three coordinates on every run.

  Stage 22 Milestone 5 worked *around* this rather than fixing it: the reverse
  geocoding specs intercept Caravel's own `/api/geocode/reverse` and answer it
  from the spec, so the new tests add nothing to the outbound traffic. That is
  the right move for a client-side assertion and no move at all for the two
  older specs, which still call out.

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

- **Vector tiles, for map labels in the user's own language.** **(soon)**
  (Surfaced by the tile-source change of 2026-08-25.) `CARAVEL_TILE_URL` now
  lets an operator pick a provider whose labels are latin script, or one
  language chosen for the whole instance -- but not one that follows each user's
  own preference, because raster tiles are drawn before anyone asks. The answer
  is vector tiles: MapLibre GL against OpenFreeMap (no key, no request limits,
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

- **Going offline during the first load after a deploy loses the code cache.**
  (Stage 23 Milestone 2.) The new worker's `activate` purges the previous
  cache, and the modules that load *during* that first post-deploy navigation
  went into the outgoing one -- so a client that goes offline in that window has
  only the six precached shell URLs and cannot boot. The next online load
  repopulates all 54 entries. This is ordinary purge-on-activate behaviour and
  it converges after one load; fixing it would mean precaching the module graph
  on install, which is the brittle enumeration `web/sw.js` was written to avoid.

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
