# Caravel — TODO / Backlog

Everything below is **not yet built**. This is the single backlog: raw input for
planning the next stage.

Sections group by *kind of work*, not by which stage deferred something.

**Conventions.** Add new notes straight into the section they belong to. When a
milestone implements an entry, delete it; when a milestone changes what's left
of one, rewrite it. A stale "still outstanding" item that was quietly built is
worse than a missing one, so both directions matter.

Entries tagged **(soon)** are the ones marked as wanted in one of the next few
stages, and are listed first in each section. Everything else is unordered —
worth keeping, not worth scheduling.

**Reviewed 2026-10-03** (the third full review, after Stage 45; the earlier ones
were in Stage 15 and on 2026-08-29). Every entry, and every idea in
`plans/notes.md`, was read out and kept, tagged, folded or dropped deliberately.
About two thirds of the file went: watch-notes with no action, decisions already
settled, test-suite tidiness with no failure behind it, and features nobody
intends to build. Anything deleted in that review was deleted on purpose — do
not reconstruct it from an older stage plan or an earlier version of this file
without asking.

---

## Bugs and rough edges

- **Concurrent writes to one SQLite database return 500.** **(soon)** (Found in
  Stage 33 Milestone 5, chasing what looked like a flaky spec.) **10 of 12**
  concurrent `POST /items/batch` requests into one trip fail with HTTP 500.
  Reproduced against a plain server, with the error surfaced temporarily:

  ```
  database is locked (517)          <- SQLITE_BUSY_SNAPSHOT
  database is locked (5)            <- SQLITE_BUSY
  ```

  517 is the diagnosis. `WithTx` opens a **deferred** transaction
  (`conn.BeginTx(ctx, nil)`, `internal/db/sqlite_store.go:1252`), and the
  transaction reads before it writes. In WAL mode a deferred transaction that
  reads first takes a read snapshot; if any other writer commits before it
  tries to write, the lock upgrade can *never* succeed, so SQLite fails
  immediately with SQLITE_BUSY_SNAPSHOT. **The `busy_timeout(5000)` in the DSN
  does not apply to that case** -- there is nothing to wait for -- which is why
  the pragma looks like it should have covered this and does not.

  The usual fix is `BEGIN IMMEDIATE` for any transaction that will write, so the
  write lock is taken up front, where `busy_timeout` *does* apply. In Go that
  means a SQLite-specific `WithTx` (a connection-level option or a raw `BEGIN
  IMMEDIATE`), and it needs the retry question answered too: an immediate
  transaction that times out still needs a caller that tries again or an honest
  error. Postgres has no equivalent problem; run `make test-postgres` alongside.

  Scope: `internal/httpapi` calls `WithTx` in ten places (`writeItemNested` in
  `items.go` among them), so this is not the batch endpoint's bug. It is
  user-visible on any shared trip -- two people adding locations at the same
  time -- and presents as "could not be saved, try again", which usually works
  on the retry and so reads as a glitch. Worth its own milestone. Symptoms seen
  so far, all expected to go with the fix:

  - **Registration.** `register.spec.js` "registering an account logs the
    newcomer straight in" has answered 500 from `/api/auth/register` in full
    `make test-ui` runs (Stages 30, 41, 44) and passed alone. `Auth.Register`
    (`internal/auth/auth.go:75`) counts users inside `WithTx` before it inserts
    -- the read-before-write pattern above. Not a duplicate username (that is a
    409), not the login rate limit. The server log was never caught, because
    `scripts/with_server.sh` deletes its temp directory, log included, on exit;
    and `--repeat-each` is no way to chase it, since the repeats race each
    other on the instance-wide open-signup setting and fail with 403.
  - **The suggest batch add.** `assist-suggest.spec.js`'s first test has failed
    under parallel load with "The locations could not be added"; the response
    capture added in Stage 33 Milestone 5 showed SQLITE_BUSY_SNAPSHOT.
  - **Creating an itinerary day.** `EnsureItineraryDay`
    (`internal/db/sqlite_store.go:706`, `postgres_store.go:908`) is
    get-then-insert, so two clients adding the same day at once lose one to the
    `(trip_id, date)` unique constraint, reported as a 500 -- on *both*
    dialects, so this one is not only the SQLite locking. Reachable from saving
    a location since Stage 25. A 409 with "the itinerary changed, please try
    again" is the honest answer (or `ON CONFLICT DO NOTHING` then select); the
    handler already has the `errItineraryEntryVanished` -> 409 shape
    (`internal/httpapi/itinerary.go:308`) to copy.

- **Map pins are hard to hit.** **(soon)** (notes.md, reviewed 2026-10-03.) A
  trip-map pin is a 1rem dot with a 2px ring -- about 20x20 px in all
  (`markerElement` in `web/js/components/map-view.js`) -- below WCAG 2.5.8's
  24px minimum and well below a comfortable finger target. Wrap the visible dot
  in a transparent hit area of about 44px so the look does not change. Mind
  pins that sit close together: bigger invisible targets overlap sooner, and
  the one on top should be the one that gets the tap.

- **The zoom hint names Ctrl on every platform.** **(soon)** (Stage 23 Milestone
  6.) The gate accepts Ctrl *or* Meta, so Cmd + wheel zooms on a Mac, but
  `map.ctrlZoomHint` says "Ctrl" everywhere -- Google Maps shows the Mac key
  instead. And macOS binds Ctrl + wheel to its own screen zoom, so a Mac user
  pressing the key the hint names may get the operating system rather than the
  map. Needs platform detection in the component, or two strings chosen at
  render time.

- **A link to a deleted location renders as a live link that 404s.** **(soon)**
  (Stage 39.) The `@` picker inserts a plain markdown link --
  `[Kex Hostel](/trips/T/locations/I)` -- so nothing checks the target still
  exists. The client already has the trip's item list when it renders a note,
  and `markInternalLinks` (`web/js/rendered-markdown.js`) already walks every
  anchor in a rendered note, so that walk is where the check goes. The open
  question is what a dead reference should *look* like (struck through, muted,
  not a link at all), not how to find one. Stale link *text* after a rename was
  considered in the same review and dropped.

---

## Planned features

- **Mark places as visited.** **(soon)** (notes.md, reviewed 2026-10-03.) A
  per-location "visited" flag, shown on the card and the pin, with a filter in
  the locations toolbar ("what have we not seen yet"). Needs a column on both
  dialects, a toggle on the card or the view page, and the filter. Fits beside
  the itinerary: a day's entries are the places you mean to visit.

- **A brief summary in the map popup.** **(soon)** (notes.md, reviewed
  2026-10-03.) A pin's popup shows the title, a photo if there is one, "Open
  location" and "View on Google Maps" -- nothing about what the place is. Add a
  short summary: the category, the days it is on, perhaps the first line of the
  notes. Most of it is already in the map payload (`mapItemResponse` in
  `internal/httpapi/map.go`). The popup is capped at 200px wide
  (`popup()` in `map-view.js`), so it has to stay brief.

- **Offline mode for the PWA, read-only first.** **(soon)** (notes.md, reviewed
  2026-10-03; absorbs the Stage 23 / Stage 45 "offline map needs one online
  visit" entry.) The worker precaches the shell, the bundle, its stylesheet and
  the fonts, so the app *boots* offline -- but `/api/*` is never cached
  (`web/sw.js`), so an offline app shows no trips, and map tiles come from
  OpenFreeMap uncached. Two steps:

  1. **Read-only offline.** Cache API responses network-first, so the last-seen
     trips, locations, itinerary and notes show without signal. Precache
     MapLibre, the map style and the locale too (their URLs are in the
     worker's built-URL table since Stage 45; about 1 MB per client per
     MapLibre upgrade), so the map works offline straight after a deploy.
     Optionally download a trip's tiles and photos ahead of time.
  2. **Offline edits.** A write queue with sync and conflict handling. Much
     larger; a separate decision.

- **Remove "My location" from the location editor's map.** **(soon)** (Review
  2026-10-03.) Never used there; the trip map keeps it. The editor's picker is
  the `pick` branch of the locate control in `map-view.js` (non-continuous,
  15s settle deadline, place-grade 50m target), which also carries the "a rough
  fix is only reachable through a fifteen-second wait" complaint -- removing it
  removes that too. After the removal, check what in `web/js/geolocation.js`
  and the picker code has become dead: the locations tab's distance filter
  still uses `getCurrentPosition` (500m target), the trip map uses the
  continuous watch.

- **Mark street-only results in the address search.** **(soon)** (Stage 33
  Milestone 2.) `/api/geocode` reports `class`, `kind` and `address_type` for
  every result, and `geocode.Result.Precise()` reduces them to the one question
  that matters -- is this the building or the road outside it. The assistant
  reads it; the editor's own address search does not, and it is where a person
  picks a result by hand from a list in which a street and a building look
  identical. A badge on the coarse ones, or precise matches sorted first, would
  use what is already on the wire.

- **Does the assistant's `geocode` tool earn its keep?** **(soon)** (Review
  2026-10-03; replaces "the maps lookup is not offered to the model as a
  tool".) The model may call `geocode` (OpenStreetMap, `internal/assist/tools.go`)
  while researching, to check that a place exists and is unambiguous. Its
  result never reaches the proposal directly: the coordinates come from
  `resolvePosition` (`locate.go`), which looks up the model's final
  `place_name` and `address` in OSM and Google itself. So the tool can only
  help indirectly -- a dropped non-existent place, a better-chosen name or
  address -- while every call costs a full model round trip, which is most of
  a run's time. Measure how often real runs call it and whether results differ
  with it removed; remove it if it does not help.

- **Per-trip feature toggles.** (notes.md, reviewed 2026-10-03.) Let a trip
  switch off what it does not use -- expenses, the itinerary, notes, files, the
  assistant -- in its settings tab, so a weekend trip is not eight tabs. Hidden,
  not deleted: switching a feature back on shows its data again. Needs a
  per-trip settings column or table and the trip page honouring it.

- **Locations with a shape: lines and areas.** (notes.md, reviewed 2026-10-03.)
  A road is a line through several points, perhaps with a start and an end; an
  area is a polygon. Today a location is exactly one lat/lng, and the `area`
  category (Stage 37) is still a pin. A whole stage: a geometry column (GeoJSON
  text, both dialects), line and fill layers in `map-view.js`, a decision on
  what the distance filter, the Google Maps link and the itinerary do with a
  shape (probably a representative point), and -- the expensive part -- a
  drawing UI. The cheap way in is *showing* a shape obtained elsewhere before
  drawing one by hand: Nominatim can return a park's or a district's outline
  with the search result.

- **Serper reports a website and a phone number for every place it finds.**
  (Stage 33 Milestone 3.) `/places` carries `website` and `phoneNumber`
  alongside the position, and `PlaceResult` (`internal/assist/search.go`)
  deliberately drops both. An official site found this way needs no liveness
  check and no model to have proposed it, which makes it a better link than
  most of what a run currently offers -- but it is a different feature from
  positioning a place, and folding it in would mean a link nobody asked for
  arriving from a source the sources list does not mention.

- **An assistant-proposed place found only by Google gets no city tag.** (Stage
  33 follow-up, the city tag; reworded 2026-10-03.) The automatic city tag
  comes from `Position.City` (`internal/assist/locate.go`), and only the OSM
  answer fills it, because Nominatim returns a structured address with a city
  field. Serper's `/places` returns a single formatted string ("Laugavegur 1,
  101 Reykjavík, Iceland") -- shown as the pin's label, and the saved location
  still gets the model's address -- with no city field to take. So the tag is
  missing when OSM did not find the place at all (common for restaurants and
  shops), and also, deliberately, when the two sources disagree. Parsing the
  city out of Google's string is guesswork (the order varies by country); one
  Nominatim *reverse* lookup on the chosen coordinates is accurate, works for
  every source, and costs one request per place.

- **Outbound map links: build them in the browser, then widen them.** (Stage 29;
  two entries folded 2026-10-03.) The Google Maps link exists twice --
  server-built `google_maps_url` (`googleMapsURL`, `internal/httpapi/map.go`)
  and client-built `googleMapsUrl` (`web/js/url.js`) -- held equal by
  `tests/ui/map.spec.js`. Dropping the server copy and letting the browser
  build every link (the map payload has carried the address since Stage 29
  Milestone 2) would make the JS helper the single source, and unlock two
  things:
  - **The reader's language.** Appending `hl=de` returns a fully German Google
    place card (measured). The server cannot do it: the app locale lives in
    `localStorage` and never reaches the backend.
  - **Apple Maps and `geo:` links.** Apple's form takes the name (`q`) and the
    coordinates (`ll`) as separate documented parameters. A `geo:` URI opens
    whichever map app the reader chose and sends nothing to anyone, but has no
    handler on desktop browsers or iOS Safari.

- **Federation between self-hosted instances, with invite links.** (Stage 01;
  invite links Stage 14 Milestone 3.) Real sync-protocol design still needed;
  v1 only avoided the integer-PK and local-only-ID mistakes that would have made
  it harder later. Joining a trip by token rather than by exact username
  belongs here too: on one instance you know who you are inviting, and invite
  links only become genuinely interesting when the invitee is not a user of
  your instance at all.

---

## Consistency and cleanup

- **The category palette lives in seven places.** **(soon)** (Surfaced adding
  the `area` category.) `CATEGORY_COLORS` is copy-pasted into `map-view.js`,
  `location-card.js`, `itinerary-tab.js` and `location-view-page.js`, and the
  list of category *names* into `location-form.js`, `locations-tab.js` and
  `suggest-page.js` (and `map-view.js` again) -- with nothing checking that
  they agree. One module exporting both the names and the colours would end
  it; map-view needs the hexes to build its custom properties while the others
  want a plain lookup, which is not much of a wrinkle. Pairs with the
  `escapeAttr` entry below: both collapse duplicated helpers into one module.

- **`escapeAttr` promises attribute safety and delivers entity escaping.**
  **(soon)** (Stage 27 Milestone 4a.) **Eight** files define `escapeAttr` --
  menu, location-card, image-field, itinerary-tab, trip-card,
  location-view-page, location-editor-page and map-view; the comment at
  `web/js/url.js:12` says seven -- and in most it is a bare alias of
  `escapeHtml`, which escapes `&<>"'` and says nothing about what the value
  *means* in the attribute it lands in. Quoting a `javascript:` URL into an
  `href` produces a well-formed dangerous link, which is the bug that milestone
  fixed with `safeHref` in `url.js`; what is left is the name. Renaming it to
  `escapeHtmlAttr`, or collapsing the copies into one shared helper, would stop
  the next person reading `escapeAttr(url)` as "this is safe".

- **Web search should leave `internal/assist`.** **(soon)** (Stage 21 Milestone
  7.) `Searcher` and its backends live in that package because the assistant
  was their only consumer. It no longer is: the image picker uses the same
  backend, `cmd/caravel` builds it and `internal/httpapi` type-asserts
  `assist.ImageSearcher` off it (`router.go`), so a package named for the
  assistant is imported for something with no LLM in it. An `internal/websearch`
  in the shape of `internal/geocode` would be the honest arrangement.
  Mechanical but wide -- every test in `internal/assist` names one of these
  types.

- **Identifier sweep: "item" → "location", all the way down.** (Stage 05; depth
  decided 2026-10-03.) The user-visible copy says "location"; below it, the
  `item.*` i18n namespace (27 keys in `en.json`) is still item-flavoured while
  `location.form.*`/`location.editor.*` migrated, `location-form.js` exports
  `renderItemForm`, `locations-tab.js` exports `renderItemsTab` and uses
  `data-action="new-item"`, the list renders `<item-card>`, and the API and
  schema say `items` (`/api/items/{id}`, the `items` table and its satellites).
  Decided: go all the way, API routes and a table-rename migration included --
  precedent is Stage 11 Milestone 1's "documents" → "files" rename, which
  renamed the table in `0006` and dropped the old URL outright. Do it as its
  own milestone (or stage): a mechanical rename inside any other diff hides the
  real changes, which is why Stage 26 declined to fold it in.

---

## Testing, CI and dev tooling

- **No seeded location for `area`, `food`, `event` or `shop`.** **(soon)**
  (Stage 37.) The demo trip in `cmd/seed/main.go` has only a site and a stay,
  so most of the seven categories never appear in a screenshot, in the map
  legend with a pin behind it, or in any UI assertion. Adding them is a
  line each, but specs count cards on the seeded trips (e.g.
  `assist-suggest.spec.js` around line 121), so it wants doing together with
  those specs.

- **`scripts/check_js.sh` cannot see a `.mjs` file.** **(soon)** (Stage 30
  Milestone 1.) It walks `find web/js -name '*.js'` (line 42), so the vendored
  MapLibre `.mjs` modules go unparsed -- acceptable for them, since
  `web/js/vendor/maplibre/README.md` records a sha256 each, but a trap the day
  a hand-written module is named `.mjs`. Widen the `find` now rather than
  document the trap.

- **The font generator should download its sources.** (Stage 41 Milestone 1;
  reworded 2026-10-03.) `scripts/gen_brand_fonts.py` reads Montserrat and Inter
  from the Fedora packages' `/usr/share/fonts/...` paths, so a machine without
  `sudo dnf install julietaula-montserrat-fonts rsms-inter-fonts` cannot
  regenerate the faces; twice now the workaround was an RPM unpacked with
  `rpm2cpio`. The script is run by hand and is not part of the build, so the
  network is no objection: fetch a pinned upstream release (`rsms/inter`,
  `JulietaUla/Montserrat`) and check its sha256, the way MapLibre is vendored.
  The packaged version may not match an upstream release byte for byte, so the
  first run after the switch wants its output diffed once.

---

## Deployment and operations

- **A warm load of a trip still waits on its API calls, one after another.**
  **(soon)** (Stage 45 Milestone 2.) With every static file served immutable, a
  reload of the Map tab reaches the server for the shell, `sw.js` and four API
  calls: `/api/auth/me`, then the trip, then the map config and the map items.
  Behind a 250 ms-per-request proxy the map view attached at ~1.4 s warm, and
  nearly all of that is those requests waiting on each other. Candidates:
  starting `/auth/me` from the shell rather than after the bundle runs,
  fetching the trip and the map data in parallel, or folding the map config
  into a response the page already makes.

- **Two of the three UI font files are not preloaded.** **(soon)** (Stage 41
  Milestone 2.) `web/index.html` preloads `montserrat-700`, but not
  `inter-400` or `inter-600`, the body faces -- they are requested only once
  `base.css` has been fetched and parsed, so on a cold load body text sits in
  the fallback about one round trip longer than it needs to. Two
  `<link rel="preload" as="font" crossorigin>` tags; since Stage 45 Milestone 2
  the shell rewrites any quoted path that has a versioned URL, so they can name
  the plain `/fonts/…` paths.

- **Prometheus/OpenMetrics metrics.** **(soon)** A `GET /metrics` endpoint via
  `promhttp.Handler()`, outside `/api` and outside the session-auth middleware.
  Stage 01's plan described that routing reservation as already in place, but
  there is no `metrics` reference anywhere in `internal/` or `cmd/` -- the route
  needs adding along with the instrumentation (HTTP request count, duration and
  status; DB query duration; upload counts and sizes; session counts). Decide
  whether it needs its own listen address or a token, since it sits outside
  auth.

- **The documentation site has no social preview.** **(soon)** (Surfaced fixing
  the app's, Aug 2026.) `zensical.toml` sets `site_description` and `site_url`
  but nothing emits `og:image`, so a link to the project site previews as bare
  text. `site_url` is a fixed absolute URL, so the tags can be written
  literally into `overrides/home.html`. Use `og-card-cta.png` --
  `docs/assets/brand/README.md` explains why that one. Wanted before the first
  release is announced.

- **The Zensical pin needs periodic review, in two files.** (Stage 18 Milestone
  9.) `zensical==0.0.57` is pinned in `.github/workflows/docs.yml` and in
  `ci.yml`'s `docs` job, deliberately, because a 0.0.x generator can change its
  output between patch releases. So the site gets no fixes until somebody bumps
  it -- including for the two 0.0.57 bugs `overrides/home.html` works around
  (the `page.is_homepage` flag being falsy for `docs/index.md`, and the skip
  link pointing at a markdown-derived anchor that an emptied content block does
  not render). When bumping, drop the workarounds and re-check the landing page
  title and skip link.

- **S3-compatible object storage.** Swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze, and
  so on); the interface already isolates callers from the backend.

- **OpenID Connect / external auth providers.** `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no provider
  integration exists yet.
