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

- **`checklists.spec.js` "clears the message once a tick succeeds" is flaky.**
  (Concurrent-writes fix, 2026-10-03.) Failed once in a full `make test-ui` run
  with "Clicking the checkbox did not change its state", then passed 5 of 5
  alone. The route answers the PATCH with a 503 in the browser, the app puts the
  box back at once, and `box.check()` can read the state after the revert. The
  server is never involved. `box.click()` plus an assertion on the error message
  avoids the race; "puts the box back and says so" uses `check()` the same way
  and is exposed to it too.

- **A deep link to a missing record still answers 200.** (Unknown-path 404 fix,
  2026-10-03.) The SPA fallback now sends the shell with a 404 for any path no
  client route matches (`isClientRoute`, `internal/httpapi/clientroutes.go`),
  but `/trips/<missing id>` has a valid shape and stays a 200: only the API call
  the page makes finds out the trip is gone. Fixing it means a database lookup
  (and an access check) while serving the shell. Probably not worth it; kept so
  the gap is known.

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
  alongside the position, and `PlaceResult` (`internal/websearch/websearch.go`)
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
