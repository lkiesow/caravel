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

- **An unknown /api path answers 200 with the SPA shell.** (Noticed in Stage 25
  Milestone 2, while confirming a deleted route was gone.) `POST
  /api/items/{id}/nonsense` and `GET /api/does-not-exist` both return 200 and
  the index page rather than a 404, because the static handler is the fallback
  for everything the router did not match. Pre-existing and unrelated to that
  milestone -- a route that never existed behaves the same as one just removed
  -- but it makes a client typo look like a success, and it is why
  `ownership_test.go` asserts on the body rather than only the status. An
  /api-scoped NotFound handler that writes the usual JSON error would fix it.

- **Creating an itinerary day can 500 on a race.** (Stage 25 planning.)
  `EnsureItineraryDay` (`internal/db/store.go:333`) is get-then-insert, so two
  clients adding the same day at the same time lose one to the `(trip_id, date)`
  unique constraint, and the handler reports it as a 500. Pre-existing, but
  Stage 25 makes it reachable from saving a location rather than only from
  moving an itinerary entry, which is a much more common act. A 409 with
  "the itinerary changed, please try again" is the honest answer, and the
  handler already has the `errItineraryEntryVanished` -> 409 shape
  (`internal/httpapi/itinerary.go:313`) to copy.

- **The zoom hint names Ctrl on every platform.** (Stage 23 Milestone 6.) The
  gate accepts Ctrl *or* Meta, so Cmd + wheel zooms on a Mac, but the string
  `map.ctrlZoomHint` says "Ctrl" everywhere -- Google Maps shows the Mac key
  instead. Fixing it means either platform detection in the component or two
  strings chosen at render time, and neither is worth it until somebody runs
  this on a Mac and says so. Note also that macOS binds Ctrl + wheel to its own
  screen zoom, so a Mac user pressing the key the hint names may get the
  operating system rather than the map -- which is the other half of the reason
  the string should probably follow the platform.

- **A card's overflow tag badge can still wrap, in one narrow case.** (Stage 30
  follow-up.) `fitTags()` frees exactly one chip to make room for the `+N`
  badge, on the reasoning that a badge is never wider than a chip. That holds
  for every realistic tag, but a one-character tag (`x`) is narrower than `+2`,
  so a row ending in one could still push the badge onto a second line. The fix
  is to keep freeing chips until the badge stops wrapping, at the cost of a
  layout read per iteration on every card; not worth it until somebody tags a
  location with a single letter.

---

## Planned features

- **The dark map is a patched vendored style, and a refetch would drop that.**
  (Stage 30 follow-up.) `web/js/vendor/map-styles/dark.json` carries 32 colour
  substitutions, because upstream it failed WCAG AA for text -- 3.36:1 place
  names -- and drew water at dE 6.7 from land. It is the only dark style
  OpenFreeMap serves, so there was nothing to switch to. The README records
  the upstream hash, what changed and the warning, but nothing enforces it: a
  future re-vendor that forgets would silently reintroduce an accessibility
  failure. Worth turning into a small script that fetches upstream and applies
  the substitutions, the way `gen_icon_sprite.py` does for icons, if the style
  is ever refetched. The alternative is deriving a dark variant from liberty,
  which means owning 111 layers of cartography.

- **Panning across the terminator does not re-light the map.** (Stage 30
  Milestone 5.) The day/night mode takes its coordinate from a remembered fix
  first and the map's viewport centre only as a fallback, and it re-resolves
  when something announces a change -- a preference edit, a new fix, the
  scheduled sunrise or sunset -- but not on `moveend`. So panning a map from
  Europe to a night-time Pacific leaves it light until one of those happens.
  Deliberate rather than missed: re-resolving on every pan would make the
  cartography flip mid-drag, which is a worse experience than a slightly stale
  answer, and the viewport is the *fallback* coordinate rather than the
  intended one. Revisit if anyone reports it, probably by re-resolving on
  `moveend` with a debounce and only when there is no remembered fix.

- **Right-to-left map labels are unshaped.** (Stage 30 Milestone 4.) MapLibre
  renders Arabic and Hebrew correctly only with `setRTLTextPlugin`, which is a
  separate ~200KB JS+WASM download it fetches from unpkg -- a third-party
  runtime dependency in a project that vendors everything, for a case the label
  chain already mostly covers: `name:latin` means an en or de reader sees
  Cairo, not an unshaped attempt at the Arabic. What is left uncovered is a
  place that has no Latin name at all, where the local name renders with its
  letters in isolated forms. Worth doing if the app ever supports an RTL
  interface language, at which point vendoring the plugin is the smaller
  question.

- **A time on an itinerary entry.** (Stage 25.) `item_dates` carried
  `all_day`, `start_time` and `end_time` from Stage 01 and never grew a UI for
  any of them; Stage 25 dropped the table without replacing them, so a 09:40
  ferry can only say so in the entry note. If they come back they belong on
  `itinerary_entries`, not on the location: a time is a property of being
  somewhere on a *day*, which is the whole point of that stage. Needs columns on
  both dialects, a time formatter in `web/js/format.js` (which has none), new
  i18n keys, and entry-row layout work at 324px, where the row is already
  a thumbnail, a title and a menu.

- **Per-file notes before the upload, and upload progress.** (The files-tab
  staging patch after Stage 29.) That patch made a pick wait in a pending list
  until the Upload button is pressed, and the note and visibility controls are
  read for the whole batch at that moment. Two things it deliberately did not
  do. A pending row has no note of its own, so uploading three files with three
  different notes is still three uploads -- the staging mode the location create
  page uses *does* carry a per-row note, so the shape exists and it is the batch
  controls, the row menu and the copy that would need reconciling. And there is
  still no progress indication beyond `.file-drop--busy` dimming the zone: the
  upload uses `fetch`, so a 40 MB file on a slow connection shows nothing at all
  for a minute. `XMLHttpRequest` and its `upload.onprogress` is the only way to
  measure it, per file, which is a real change of shape for the request loop.

- **Multi-select tag filtering.** (Stage 26.) The tag filter that stage builds
  is single-select, so it sits inside the `menuitemradio` model every menu in
  the app shares. Combining tags -- Reykjavik AND for-kids -- is where tags
  actually start paying off, but it needs `menuitemcheckbox` rows, a trigger
  label that can say "2 tags" rather than naming one, and a way to clear the
  set. Worth doing once there is evidence people tag densely enough to want it.

- **Tag management across a trip: rename, merge, delete.** (Stage 26.) Tags are
  stored as text on `item_tags` with no tags table, so a rename is an `UPDATE`
  over one column and a merge is the same statement -- there is no schema work
  here, only UI and a place to put it. Deliberately deferred until tag drift is
  observed rather than predicted: the editor suggests the trip's existing tags,
  which is the cheap defence against it. Note the stage stores tags as typed and
  dedupes case-insensitively only *within* one location, so `Museum` and
  `museum` can coexist on two different ones.

- **Whether a suggestion run needs a longer deadline than an enrichment one.**
  (Stage 27 Milestone 3.) `RunDuration` is 90s for both, and a trip-level run
  researches up to six places rather than one -- a search and a page read each,
  plus a serialised geocoder lookup per candidate. It was left alone
  deliberately: against the stub a run takes about four seconds, which measures
  the loop and not the model, so there was nothing to decide it with. What this
  needs is a handful of real runs against a real endpoint and the wall times
  they report, not a guess. If it does need its own value, note that `Limits`
  is one struct shared by both tasks today, so a per-task deadline is a small
  change to `withDefaults` and a new environment variable.

- **A way into the itinerary from a location.** (Stage 25.) The location page
  shows the days it is on but does not link to them, so the way to see a
  location in context is to go back to the trip and pick the tab.

- **A location's OpenStreetMap identity, for places that predate it.** (Stage
  29 Milestone 3.) `osm_type`/`osm_id` are captured from the address search from
  that milestone onward, so the OpenStreetMap feature link appears only on
  locations saved since. Everything older, and anything positioned by dropping a
  pin or pasting a Google Maps link, has no identity and shows no link. A
  backfill would mean re-searching each stored address through Nominatim and
  accepting a match, which is a guess made on the user's behalf; an "is this the
  right OSM feature?" affordance in the editor would be honest but is a screen
  nobody has asked for. Worth doing only if the link turns out to be one people
  use.

- **The outbound map link could be in the reader's language.** (Stage 29
  Milestone 2.) Appending `hl=de` to the link that milestone builds returns a
  fully German Google place card -- measured, so this is a known-working
  parameter and not a guess. Note it contradicts nothing from Stage 22, which
  found Google ignoring `Accept-Language`: that was a server-side fetch of a
  page, this is a query parameter on a link a browser opens.

  It was dropped because the server cannot know the app locale. It lives in
  `localStorage` (`web/js/i18n.js`), never reaches the backend, so the
  server-built `google_maps_url` could not carry it -- and adding it on the
  client link only would mean the two twins (`googleMapsUrl` in `web/js/url.js`
  and `googleMapsURL` in `internal/httpapi/map.go`) stop producing the same URL,
  which Stage 29 Milestone 1 spent a whole milestone establishing and asserts in
  `tests/ui/map.spec.js`. Two honest ways out, both bigger than the feature:
  send the locale to the server (it has no reason to know it otherwise), or drop
  `google_maps_url` from the API and let the browser build all three links --
  which is now nearly possible, since the map payload carries the address as of
  that milestone. The second is tempting and would make the JS helper the single
  source outright.

- **Apple Maps and `geo:` links beside the Google one.** (Stage 29 planning.)
  Apple's URL form gets right what Google's does not: `q` for the name and `ll`
  for the coordinates are separate documented parameters, so name-plus-coordinate
  biasing is a one-liner rather than an undocumented path segment. A `geo:` URI
  opens whichever map app the reader actually chose and sends nothing to anyone,
  but has no handler on desktop browsers or iOS Safari. Both are additions to a
  link list rather than fixes to a broken link, which is why Stage 29 left them
  out; worth revisiting if the location view ever grows a row of map handoffs.

- **Prompt caching for the assistant.** (Stage 21 Milestone 4; the two companion
  levers -- a reasoning-effort knob and mechanical conversation compaction --
  were measured and dropped in the 2026-08-29 review.) Every turn resends the
  whole conversation, and OpenRouter and others can cache the repeated prefix.
  Never measured, so the size of the prize is unknown; it is the one lever that
  would help every request in a run rather than one of them. Context for
  expectations: 85% of a run is the model, spread over roughly 4.4 sequential
  requests, and switching the instance to `nvidia/nemotron-3.5-lightning` took a
  Tokyo Tower run from 59.1s to 16.4s -- more than any code change is likely to.

  **Two findings from Stage 27 planning that change where this starts.** First,
  the prefix is *not* stable across runs: `systemPrompt` embeds the trip's tag
  vocabulary and the user locale (`internal/assist/prompt.go:54-65`), so two
  runs on different trips share nothing. Moving those into the first user
  message would make the system block plus the tool definitions a genuinely
  cacheable prefix, and that is the first move -- before any cache directive.
  Second, a hit is currently invisible *to Caravel*, though not on the wire:
  `usage` (`internal/assist/provider.go:110-116`) decodes only
  prompt/completion/total, so neither the budget nor the run trace could tell
  you whether caching happened. A real OpenRouter response observed on
  2026-08-30 carries `usage.prompt_tokens_details.cached_tokens` and
  `cache_write_tokens` alongside a `cost` breakdown, so making a hit visible is
  a few struct fields rather than a protocol problem -- and it is the cheapest
  first step, since it turns the whole question into something measurable
  before any behaviour changes. Within a run the message list is
  already append-only and never rewritten, which is the good news -- and the
  composing turn, which resends everything, is the single biggest beneficiary.
  Note also that `chatMessage` is a flat {role, content, ...} shape:
  OpenAI-style automatic prefix caching needs no wire change, Anthropic-style
  explicit breakpoints would need content blocks in `provider.go`.

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
- **Per-field review of a suggested candidate.** (Assist tag cleanup,
  2026-09-03.) The location editor reviews an AI proposal field by field, with
  its own accept and reject per field; `/suggest` shows a candidate whole and
  takes its tags and notes wholesale on save (`web/js/pages/suggest-page.js`).
  The tag normalisation now applies to both paths, so this is about control
  rather than quality -- five candidates times five fields is a lot of UI for
  a small gain, which is why it was left.

---

## Multi-user and sharing

- **Invite links / joining by token.** Adding a member needs their exact username
  today (Stage 14 Milestone 3), which is fine on a self-hosted instance where you
  know who you are inviting. This only becomes genuinely interesting **after
  federation**, where the person you are inviting is not a user of your instance
  at all — so treat it as a federation follow-on.

---

## Consistency and cleanup

- **List view state lives only in memory.** (Stage 26.) The locations tab's
  filters and its sort, and the trips list's sort, are closure variables --
  reloading the page or following a link out and coming back resets them, and a
  filtered list cannot be shared or bookmarked. Putting them in the query string
  would fix all three, but nothing in the app puts view state in the URL today,
  so doing it for one tab would be its own inconsistency; it wants deciding for
  the trips list and the locations tab together. Stage 26 deliberately left it
  alone while rebuilding that toolbar.

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
  `/trips/:id/documents` URL outright rather than redirecting it. Stage 26
  considered folding this in -- it was already inside `renderItemsTab`,
  `<item-card>` and the `item.category.*` keys -- and declined: a mechanical
  rename through every diff of that stage would have hidden the real changes.

- **Number and date formatting follows the *browser* locale, not the app's.**
  **(soon)** -- Stage 26 made this considerably more visible: `formatDateRange`
  now renders on every location card and inside the locations filter menu, not
  only on a location page, so a reader whose app is in German and whose browser
  is in English sees English dates all over the locations tab.
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

- **`escapeAttr` promises attribute safety and delivers entity escaping.**
  (Stage 27 Milestone 4a; the count corrected in Stage 29.) **Eight** files
  define `escapeAttr` -- the entry said five and `web/js/url.js:12` says seven,
  both undercounts -- and in most of them
  it is a bare alias of `escapeHtml`, which escapes `&<>"'` and says nothing
  whatever about what the value *means* in the attribute it lands in. Quoting a
  `javascript:` URL into an `href` produces a perfectly well-formed dangerous
  link, which is exactly the bug that milestone fixed -- and the name is part
  of why it went unnoticed for so long. The fix landed a `safeHref` in
  `web/js/url.js` for the two link render sites; what is left is the name.
  Renaming it to `escapeHtmlAttr`, or collapsing the duplicates into one shared
  helper, would stop the next person reading `escapeAttr(url)` as "this is
  safe". Note the duplication is deliberate elsewhere in this codebase and
  fine for entity escaping; it is the *promise in the name* that is the
  problem here.

## Testing, CI and dev tooling

- **Map attribution is not asserted anywhere.** (Stage 30 Milestone 6.)
  Removing the attribution variable made the credit a property of loaded map
  data -- a style's source metadata, or the TileJSON it points at -- and the UI
  suite blocks every request for map data, so the attribution control has no
  loaded source to report and there is nothing to read. Three routes round it
  were tried and rejected; the reasoning is in `map.spec.js` where the test
  would otherwise sit. Verified by hand instead: a running instance credits
  "OpenFreeMap (c) OpenMapTiles Data from OpenStreetMap" with all three links
  live. It matters because a silent regression here is a licence-compliance
  problem rather than a cosmetic one, so it is worth an assertion the day the
  suite gains a way to let one source load -- a stubbed vector tile, most
  likely, which the "UI suite reaches the real Nominatim" entry below would
  also benefit from.

- **A vertical two-finger pan cannot move the trip map, by design.** (Stage 30
  Milestone 2.) MapLibre pins the camera when the world already fits the
  viewport, and at the trip map's `fitBounds` zoom the world height and the
  container height agree to within a pixel (measured: 643 and 643). That is
  correct behaviour and an improvement on Leaflet, which would drag the world
  off the top of the screen -- but it means latitude is the one axis that
  cannot demonstrate a pan there, and both gesture tests had to move to
  longitude to keep asserting anything. Worth knowing before writing any
  further gesture test against that route.

- **`scripts/check_js.sh` cannot see a `.mjs` file.** (Stage 30 Milestone 1.)
  It walks `find web/js -name '*.js'`, so the three vendored MapLibre modules
  go unparsed where `leaflet.esm.js` was covered by virtue of its extension.
  Accepted at the time rather than worked around: the files are unmodified
  upstream output and `web/js/vendor/maplibre/README.md` records a sha256 for
  each, which catches a corrupted copy that a syntax parse would not. It stops
  being a fair trade the moment any hand-written module in this project is
  named `.mjs` -- at that point widen the `find` rather than renaming the file.

- **A UI spec can still 500 on register in a full run.** (Stage 30 Milestone 4
  first saw it; still open.) `register.spec.js`'s "logs the newcomer straight
  in" answered 500 to `POST /api/auth/register` in one full `make test-ui` run
  and passed alone, and passed in the immediately preceding full run. Not a
  duplicate username, which is a 409 (`internal/httpapi/auth.go:126`), and not
  the shared login rate limit, which the test checks for by name. SQLite runs
  with WAL and a 5s busy timeout, so a plain lock storm is not the obvious
  answer either. Undiagnosed: `scripts/with_server.sh` deletes its temp
  directory with the server log on exit, so catching it means a run that keeps
  the log.

  This entry used to also cover `map.spec.js`'s two distance-filter tests and
  `itinerary-order.spec.js`'s move-an-entry test as one shared-seed problem.
  The distance-filter half turned out to be a single specific collision and is
  fixed (2026-09-03): `locations.spec.js`'s OSM-link tests created two
  locations on the shared `full` trip and never removed them, one of them under
  a kilometre from the hotel the 5km filter centres on. They have their own
  trip now. Whether the itinerary-order case has its own cause or is more of
  the same is unknown -- it has not been seen since.

- **Two map specs race the map itself under a full parallel run.** (Seen
  2026-09-03, in the run that confirmed the distance-filter fix below.)
  `map.spec.js`'s label-localisation test failed with `host._map is null`, and
  `map-theme.spec.js`'s appearance-setting test with `page.reload: <unknown
  error>`; both pass when the two files are run together on their own, and
  neither is a count on the shared seed. `mapState` in `map-theme.spec.js`
  already carries a guard for the sibling case -- `getStyle()` returning
  undefined mid-swap, which its comment says showed up only under a full
  parallel run -- so the shape is known: a map-view that exists before its
  MapLibre instance does. The fix is probably to wait on `_map` the way
  `gotoTripMap` does rather than to reach for it, wherever a spec touches the
  map directly.

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

  **Stage 27 Milestone 3 made this materially worse and is the reason to fix
  it.** A trip-level suggestion run geocodes *every* candidate it proposes --
  up to six lookups for one run, against the same volunteer-run service, and
  serialised precisely because of that service's one-request-per-second policy.
  One assist spec asking for three coordinates was arguable; a suggest spec is
  six on its own, and the serialisation means they also make the run slower to
  no purpose in a test. The stub geocoder is now the cheapest of the three
  fixes.

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
- **The documentation site has no social preview.** (Surfaced fixing the app's,
  Aug 2026.) `zensical.toml` sets `site_description` and `site_url` but nothing
  emits `og:image`, so a link to the project site previews as bare text the way
  a link to an instance used to. Easier than the app's was: `site_url` is a
  fixed absolute URL, so the tags can be written literally into
  `overrides/home.html` with no substitution. Use `og-card-cta.png` --
  `docs/assets/brand/README.md` explains why that is the right one of the two
  there and the plain card is the right one in the app.

- **Day/night guesses a latitude of zero.** (Stage 30 follow-up, Sep 2026.)
  With no remembered fix, `map-theme.js` derives the reader's longitude from
  the browser's UTC offset and uses latitude 0, so the switch happens near
  06:00 and 18:00 local in every season -- up to about 90 minutes off a real
  sunset at European latitudes, and wrong about polar summer entirely. A
  timezone-to-representative-coordinate table (`Intl` already gives the zone
  name) would fix it for a few kilobytes; the locate control already fixes it
  exactly for anyone who uses it once.

- **The trip note records who saved it, and nobody shows it.** (Stage 31
  Milestone 1, Sep 2026.) `trip_notes.updated_by` and `updated_at` are written
  on every save and `updated_at` reaches the client, but the Notes tab renders
  no byline. Written from the first save on purpose -- a column added later
  records only edits made after it exists -- so the data will be there whenever
  "last edited by Anna, Tuesday" is wanted. Needs a display-name lookup in the
  response, which the note payload deliberately does not do yet.

- **The notes tab has no preview while you type.** (Stage 31 Milestone 2, Sep
  2026.) The location form's notes field has an Edit/Preview toggle
  (`.notes-field__toggle`, backed by `POST /api/markdown/preview`); the trip
  notepad deliberately does not, because there Save *is* the switch to the
  rendered view. That is the right call for a short note and a doubtful one for
  a long one, where you would rather check a table renders before committing to
  it. If it is added, it must go through the same preview endpoint -- a second
  client-side renderer would be a second sanitizer.

- **The docs build does not catch a missing image.** (Stage 31 Milestone 3, Sep
  2026.) `zensical build --strict` fails on a dead *internal link* -- which is
  the whole reason CLAUDE.md calls the flag load-bearing -- but an
  `![alt](../assets/whatever.png)` pointing at a file that does not exist
  builds clean and exits 0. Reproduced deliberately with a made-up filename,
  and hit for real: `make docs` passed while the new Notes page referenced
  `notes.png` before the screenshot run had created it. `check_screenshots.py`
  covers only the other direction (every committed screenshot is shown by some
  page). The missing half is cheap -- walk the image references in `docs/` and
  assert each target exists -- and belongs next to that script in `make ci`.

