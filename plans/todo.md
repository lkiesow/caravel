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

## Deployment and operations

- **Drop the Zensical workarounds once upstream fixes them.** (Stage 18
  Milestone 9; re-checked against 0.0.67.) `overrides/home.html` and
  `main.html` work around `page.is_homepage` never being defined, and the skip
  link pointing at a markdown-derived anchor that an emptied content block does
  not render. Both still reproduce in 0.0.67. The pin now lives in
  `.github/requirements.txt` and Dependabot proposes bumps monthly; its header
  has what to re-test on each one.

- **S3-compatible object storage.** Swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze, and
  so on); the interface already isolates callers from the backend.

- **OpenID Connect / external auth providers.** `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no provider
  integration exists yet.

- **Per-query database timing in the metrics.** (Deferred when `/metrics`
  landed, Oct 2026.) The first cut exports the connection pool
  (`go_sql_*`) but no query durations. Wrap sqlc's `DBTX` in both stores,
  including the transaction one `WithTx` builds, and label each observation by
  the `-- name:` comment sqlc puts at the top of every query string. The name
  set is fixed, so the cardinality is too.
