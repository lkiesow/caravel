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

**Reviewed 2026-10-07** (the fourth full review; the earlier ones were in Stage
15, on 2026-08-29 and on 2026-10-03, which cut the file by two thirds). Every
entry was read out and kept, tagged, folded or dropped deliberately. Anything
deleted in a review was deleted on purpose — do not reconstruct it from an
older stage plan or an earlier version of this file without asking.

---

## Bugs and rough edges

- **Abort a page's stale GETs when it is left.** (Stage 48.) The router's
  per-render signal (`router.js`) cancels redirects, listeners and assist
  streams, but not ordinary fetches: `api.request` and `postForm` take no
  signal, so a page that has been left still finishes its loads into a
  detached `<main>`. Harmless today, wasted bandwidth only. Worth doing with
  offline mode, where slow requests become common. Never for mutating
  requests: aborting one does not undo it on the server.

- **Small leftovers that outlive a page.** (Stage 48.) `popup.js` keeps its
  document click and keydown listeners if a popup is open during a
  navigation (they go on the next click), and `<map-view>`'s gesture-hint
  `setTimeout` is not cleared on disconnect (it only hides a node). Neither
  does harm; fold them into the page signal if either is touched.

---

## Planned features

- **Mark places as visited.** **(soon)** (notes.md, reviewed 2026-10-03.) A
  per-location "visited" flag, shown on the card and the pin, with a filter in
  the locations toolbar ("what have we not seen yet"). Needs a column on both
  dialects, a toggle on the card or the view page, and the filter. Fits beside
  the itinerary: a day's entries are the places you mean to visit.

- **A short summary for each location.** **(soon)** (notes.md, reviewed
  2026-10-03; clarified 2026-10-07.) A new optional field answering "what is
  this place, in at most about five words" -- Tokyo Skytree: "Japan's tallest
  observation radio tower". Filled in by hand, and proposed by the assistant.
  No backfill of existing locations. The parts:

  - **Data.** A `summary` column (both dialects), sqlc, the location API.
    Enforce a length cap on the server; words are fuzzy, so a character limit
    (around 60) is the robust rule, with "five words" in the placeholder and
    the prompt.
  - **Editor.** One short text input in the location form, plus i18n.
  - **Display.** In the map popup, which was the original wish -- it shows the
    title, a photo and two links today, nothing about what the place is, and
    `summary` has to join the map payload (`mapItemResponse` in
    `internal/httpapi/map.go`, which carries only the category). Naturally
    also under the title on the location page and the location card. The
    popup is capped at 200px wide (`popup()` in `map-view.js`).
  - **Assistant.** One more field in the proposal schema
    (`internal/assist/schema.go`, beside tags and notes), through the editor's
    per-field accept/reject review, and saved by `/suggest` -- where it also
    helps pick between candidates.

  Effort about 4 of 10: every piece is small, but it touches schema, API,
  three display surfaces, the editor and the assistant pipeline. Wants the
  item -> location rename to land first (see Consistency and cleanup).

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

- **Identifier sweep: "item" → "location", all the way down.** **(soon)** (Stage
  05; depth decided 2026-10-03; scheduled 2026-10-07.) Land it *before* the
  location summary and the visited flag, so their columns, API fields and code
  are written with the new names rather than renamed afterwards. The
  user-visible copy says "location"; below it, the `item.*` i18n namespace (27
  keys in `en.json`) is still item-flavoured while
  `location.form.*`/`location.editor.*` migrated, `location-form.js` exports
  `renderItemForm`, `locations-tab.js` exports `renderItemsTab` and uses
  `data-action="new-item"`, the list renders `<item-card>`, and the API and
  schema say `items` (`/api/items/{id}`, the `items` table and its satellites).
  Decided: go all the way, API routes and a table-rename migration included --
  precedent is Stage 11 Milestone 1's "documents" → "files" rename, which
  renamed the table in `0006` and dropped the old URL outright. Do it as its own
  milestone (or stage): a mechanical rename inside any other diff hides the real
  changes, which is why Stage 26 declined to fold it in.

---

## Deployment and operations

- **S3-compatible object storage.** Swap the `internal/storagefs` `Blob`
  implementation from local filesystem to S3-compatible (MinIO, Backblaze, and
  so on); the interface already isolates callers from the backend.

- **OpenID Connect / external auth providers.** `auth_identities` already
  supports a `provider` column beyond `'local'` for exactly this; no provider
  integration exists yet.
