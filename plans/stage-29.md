# Stage 29 — Google Maps, the outbound half

## Context

Stage 22 Milestone 6 built the **inbound** half of Google Maps
interoperability: paste a Maps link into the address search and Caravel resolves
it to coordinates. The **outbound** half has been tagged **(soon)** in the
backlog since Stage 13, with an unusual instruction attached: survey what is
available *before* deciding, because the old conclusion was drawn without
looking.

The survey was done during this stage's planning, empirically, in a browser.
**It overturns the premise.** Findings are recorded in full at the bottom of
this file; the short version:

> Caravel's outbound links do not need a Google place ID. They need to stop
> sending *only coordinates*. Google's `query` parameter is documented to take
> "a place name, address, or comma-separated latitude/longitude coordinates" --
> and a bare coordinate pair is *defined* to produce a dropped pin. Sending the
> name and address instead lands on the real place card, with hours, reviews and
> photos, keylessly. Verified.

So the thing described as blocked since Stage 22 is a string change.
`internal/geocode/maplink.go:33-35` says so in the code, and is wrong:

> The other -- linking out to a place's own Google entry rather than to a
> dropped pin -- needs a place ID, which OSM cannot give us, and stays blocked.

Two further problems came out of the same look:

**The URL is spelled out three times, and the three disagree.**
`internal/httpapi/map.go:89` formats with `%f` (six decimals fixed);
`web/js/components/leaflet-map.js:692` and
`web/js/pages/location-view-page.js:105` interpolate raw JS numbers. The two
popups in the *same file* disagree about where the URL even comes from: the
trip-wide popup (`leaflet-map.js:722`) consumes the server's
`item.google_maps_url` while the single-marker popup thirty lines above builds
its own. A change made in two of the three places would not look wrong.

**Caravel discards a better link than the one it is trying to fix.** Nominatim
returns `osm_type` and `osm_id` on every result and `nominatimResult`
(`geocode.go:274-292`) keeps only `display_name`/`lat`/`lon`. Those two fields
are the difference between a pin and
`https://www.openstreetmap.org/node/240109189` -- a real feature page with
opening hours, phone, website and the full tag set. Free, keyless, no request
leaving the instance, and considerably more on-brand for this project than
improving a link to Google.

## 0. Land the plan, and reconcile the backlog

This file, plus the backlog entries the survey settles. The Google Maps entry is
**rewritten, not deleted** -- its "needs a place ID" framing is exactly what this
stage disproved, and the next person should find the correction rather than
silence. The options the survey rejected (Serper, SerpApi, Wikidata P3749) are
written down with their numbers so they are not re-litigated from memory.

## 1. One helper, three call sites

Behaviour-preserving refactor first, so Milestone 2 is a small diff in one place
rather than a wide one across three files.

- A `googleMapsURL(...)` helper in Go beside `mapItemToResponse`
  (`internal/httpapi/map.go`) and its twin in `web/js/url.js` -- already the
  module that owns URL judgement, and already home to `safeHref`.
- Rewrite all three sites to call it. Settle the `%f`-versus-raw precision
  disagreement in the helper, deliberately and once.
- Decide whether the single-marker popup keeps building its own URL or consumes
  a server-provided one like its sibling. Consistency argues for one path; the
  single-marker embed may have no server payload to read, in which case the
  shared JS helper *is* the single path and that is the answer.

**Verification.** `make ci`. A UI assertion that all three links produce the same
URL for the same coordinates -- the assertion that makes this refactor worth
having. `tests/ui/map.spec.js:523`, which pins that the popup offers exactly two
links at 44px each, must keep passing unedited.

## 2. The link names the place

The payoff. One helper signature, three callers already wired by Milestone 1.

**The form.** Name and address as a text query, biased by the stored
coordinates:

```
https://www.google.com/maps/search/{urlencode(title + ", " + address)}/@{lat},{lng},17z
```

Three things the survey established that constrain this, all measured:

- **The coordinate bias needs the path form.** Putting coordinates *inside*
  `query` does not bias anything -- `?api=1&query=Starbucks+48.8584,2.2945`
  returned a list of Starbucks in San Francisco. The coordinates were read as
  literal text. The `/@lat,lng,17z` path segment does bias, correctly.
- **The bias is what makes this trustworthy for ordinary places.** Name plus
  address alone landed on a Starbucks place card several hundred metres from the
  address given -- right chain, wrong branch. For a hotel with a unique name it
  does not matter; for a cafe called Central it does.
- **The trade-off is documented-versus-correct.** `?api=1&query=...` is a
  versioned contract with forward-compatibility promises and no bias;
  `/maps/search/.../@lat,lng,z` is Google's own internal canonical form, stable
  for a decade, promised by nobody. **Decision: the biased form**, because
  landing on the wrong branch of a chain is a worse failure than a URL shape
  changing under us -- and if it ever does change, the fallback is one line in
  one helper.

**Fallback.** No usable title, or a title that is itself a coordinate, falls back
to today's coordinate query unchanged. That link stops being the rule and
becomes the exception.

**Where the address comes from is the real work.** `db.MapItem`
(`domain.go:215`) carries ID/Category/Title/Lat/Lng and **no address**, so the
trip map's popups have a title but nothing else. Either the map query grows the
address column, or the map popups use title-plus-bias while the location view
uses title-plus-address-plus-bias. Prefer growing `MapItem` -- the two links
disagreeing about where they land would be exactly the inconsistency Milestone 1
just removed.

**While in the helper: `hl=`.** Appending the reader's locale should give a place
card in their language. Stage 22 measured that Google ignores `Accept-Language`
for the canonical place path segment (`stage-22.md:782-791`) -- but that was a
server-side fetch of a page, not a link a browser opens. Test it; if it works it
is free, and if it does not, say so in the Done paragraph and drop it.

**Deliberately not doing** what Google's docs recommend: `utm_source` and
`utm_campaign` are optional, and they tell Google which app sent the user. This
app does not do that.

**Verification.** `make ci`, plus a real click-through on a seeded location: the
link opens the place's own Google entry. Assertions on the rendered `href` for
the named case and the no-title fallback, at both widths, rather than
screenshots. No attribution requirement applies -- the survey checked, and a
plain outbound hyperlink displays no Google Maps content -- so the existing
`map.viewOnGoogleMaps` string stays as it is, and no logo ships.

## 3. The OpenStreetMap feature page

The link this project should arguably have had first.

- **Persist what Nominatim already sends.** `nominatimResult`/`toResult`
  (`geocode.go:274-292`) gain `osm_type` and `osm_id`; `geocode.Result`
  (`geocode.go:58`) carries them. Note `Result` is shared with the map-link
  resolver, which will never populate them -- worth a comment saying so, the way
  the codebase already comments this kind of asymmetry.
- **Schema**: a `0007` migration on **both dialects** adding two nullable
  columns to `item_locations`, then `sqlc generate` by hand from
  `internal/db/sqlc/` for both -- plus `db.ItemLocation` (`domain.go:410`),
  `itemLocationResponse` (`items.go:81`) and `itemLocationRequest`
  (`items.go:470`).
- **The editor** (`location-editor-page.js`) carries them through from a picked
  search result to the save, the way `resolveMapLink` already carries lat/lng.
- **A second link** beside the Google one, when the fields are present:
  `https://www.openstreetmap.org/{type}/{id}`. New i18n key in **both**
  `web/locales/en.json` and `de.json`.
- **Validation.** These are user-reachable values that end up in an `href`:
  `osm_type` is one of node, way or relation and nothing else, `osm_id` is
  digits. `link-safety.spec.js` is the precedent for how this codebase treats a
  user-supplied string that becomes a link.

**Note the coverage limit honestly**: only locations saved *through the address
search* get these, and only from this milestone onward. Every existing location,
and anything placed by dropping a pin or pasting a Maps link, has no OSM identity
and shows no OSM link. That is acceptable -- the link appears when it can -- but
it should be written down rather than discovered later.

**Verification.** `make ci` **and `make test-postgres`** -- a migration plus a
query change is exactly what that target exists for. Go tests for the new
Nominatim fields and for the validation rejecting a bad `osm_type`. A UI
assertion that the link appears with the fields and is absent without them.

## Build order

0 → 1 → 2 → 3. Milestone 1 is a pure refactor and lands first so Milestone 2 is
small; Milestone 3 is independent of both and could be dropped without harming
them.

## Files this touches

- `internal/httpapi/map.go`, `items.go`
- `internal/geocode/geocode.go` (+ the stale comment at `maplink.go:33-35`)
- `internal/db/migrations/{sqlite,postgres}/0007_*.up/down.sql`,
  `internal/db/sqlc/queries/*.sql` + both regenerated dialect packages,
  `internal/db/domain.go`
- `web/js/url.js`, `web/js/components/leaflet-map.js`,
  `web/js/pages/location-view-page.js`, `web/js/pages/location-editor-page.js`
- `web/locales/en.json` + `de.json` (Milestone 3's new key)
- `tests/ui/map.spec.js`, `tests/ui/locations.spec.js`

## Out of scope, deliberately

- **Any keyed place-ID source.** Serper (2,500 free credits, then ~$1/1,000) and
  SerpApi (~25x that) both return `placeId` and `cid`, and both are rejected on
  the same grounds: they make a third-party API key a prerequisite for a core UI
  affordance and route users' saved place names through a scraper, in an app
  whose premise is that it runs at home without one. The survey's full reasoning
  is in the Findings below so this does not get re-litigated from memory.
- **Wikidata P3749 to `?cid=`.** Genuinely free and keyless, via an OSM
  `wikidata` tag. Measured against live Wikidata during planning: **66,382 items
  carry it worldwide.** It fires for the Brandenburg Gate and never for the
  guesthouse you booked. Not worth building for this alone.
- **A paid Places API.** Contradicts the project's premise.
- **Vector tiles / MapLibre** -- the other **(soon)** map item, and its own
  stage.
- **Apple Maps and `geo:` links.** Apple's form gets right what Google does not
  (`q` for the name and `ll` for coordinates are separate documented
  parameters), and `geo:` opens whatever map app the user chose while sending
  nothing to anyone -- but it is useless on desktop and on iOS Safari. Both are
  additions to a link list, not fixes to a broken link.
- **The Mac key in `map.ctrlZoomHint`.** Adjacent, unrelated.

## Verification

`make ci` green at every milestone; `make test-postgres` additionally for
Milestone 3. Behaviour proved by assertion rather than screenshot throughout --
the rendered `href` in each case is the assertion that matters -- with one real
click-through in Milestone 2 to confirm the place card actually appears, since
that is the whole point of the stage and no local assertion can prove it.
Existing map and locations specs keep passing unedited except where a milestone
adds to them.

## Workflow

One milestone at a time. For each: implement, verify (`make ci` plus the
milestone's own proof), add a **Done.** paragraph to that milestone's section
here describing what actually landed -- including any deviation from this plan --
and how it was verified, reconcile `plans/todo.md` in both directions, commit
(one commit per milestone; a same-day follow-up fix gets its own
"... follow-up: ..." commit), make sure `make dev` is running, then stop and hand
back control.

## Findings: the survey the backlog asked for

Empirical, in a headless browser, 2026-08-31. Recorded here so the decisions
above are auditable and so nobody redoes this from memory.

**Keyless and no contract.** Google's Maps URLs are explicitly documented as
needing no API key and no acceptance of the Maps Platform terms. Deep-linking
this way is the documented purpose. No attribution requirement attaches to an
outbound hyperlink -- those obligations attach to *displaying* Maps content.
Naming Google Maps in link text is permitted referential use; a logo would not
be.

**What each form actually did:**

| URL | Result |
|---|---|
| `search/?api=1&query=52.5161791,13.3805048` | **Dropped pin.** Title is the DMS coordinate; panel offers a Plus Code and "Add a missing place". Today's behaviour. |
| `search/?api=1&query=Hotel+Adlon+Kempinski,+Unter+den+Linden+77,+10117+Berlin` | **Full place card** -- 4.6 stars, 2,286 reviews, photos, hours. **No place ID supplied.** |
| `search/?api=1&query=Starbucks+48.8584,2.2945` | **Wrong city** -- a list of Starbucks in San Francisco. Coordinates in `query` are literal text, not a bias. |
| `search/Starbucks/@48.8584,2.2945,17z` | **Correctly biased** -- Starbucks around the Eiffel Tower. |
| `search/Hotel+Adlon+...,+10117+Berlin/@52.5162,13.3805,17z` | **Place card, coordinate-disambiguated.** The chosen form. |
| `search/?api=1&query=Starbucks,+26+Avenue+de+la+Bourdonnais,+75007+Paris` | Place card, but several hundred metres off -- right chain, wrong branch. Why the bias matters. |
| `maps/place/?q=place_id:ChIJ...` | Works, but requires obtaining an ID. |
| `maps?cid=1865862350127202009` | Works. That decimal is the second half of the `!1s0x...:0x...` pair in the URL above -- hex to decimal. |

**On the data blob.** `maplink.go` already parses it correctly and already
prefers `!3d!4d` (the clicked marker) over the `/@` viewport -- the tests above
confirm that ordering is right: in the biased case the viewport stayed at the
requested rounded coordinates while `!3d!4d` carried the true ones. The blob
*does* carry a `!1s0x...:0x...` feature id whose second component is the CID, so
harvesting an identity from a pasted link is possible -- it is simply unnecessary
now that a text query suffices, and it would only ever cover pasted locations.

**Identifier sources, if one were ever needed.** OSM has no Google place-ID tag
and will not get one: Maps data is copyrighted, and OSM's external-id culture
goes through `wikidata` instead. Wikidata P3749 exists with the right formatter
URL and 66,382 items. P2671 (Knowledge Graph ID) is far better populated but does
not map to a Maps place. No public dataset maps arbitrary OSM features to Google
place IDs; anything claiming to is a paid scraper's database.

**Untested, flagged as such.** Whether the biased path form is handed off
correctly to the Google Maps app on Android or iOS was not verified on a device.
