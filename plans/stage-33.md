# Stage 33 — Where a place actually is

## Context

The assistant's coordinates are frequently *nearby but not right* — the
street rather than the building, the block rather than the restaurant.
The cause is visible in the code and is not the model's fault: the model
is forbidden to emit coordinates ([prompt.go:37](internal/assist/prompt.go#L37))
and the agent re-derives them itself, but it does so by taking Nominatim's
**first result for the model's postal address**
([agent.go:857-882](internal/assist/agent.go#L857), and a near-identical
copy in `locate` at [agent.go:987-1010](internal/assist/agent.go#L987)).

Three things are wrong with that:

- **Address-first is the wrong precision.** Nominatim answers a postal
  address with a house-number node, an interpolated point along the way,
  or the street centroid. For a *named* place, searching the name usually
  hits the OSM element somebody actually mapped — the building, the
  amenity — which is the point the user expects to see.
- **`results[0]` is accepted with no idea what it is.** The `jsonv2`
  payload already says whether the match is a building, an amenity, a
  road or a whole district (`category`, `type`, `addresstype`), and
  [geocode.go:318-333](internal/geocode/geocode.go#L318) throws all of it
  away. The one field that would separate "the café" from "the street the
  café is on" is read and discarded.
- **OSM is thin on commercial places.** Restaurants, cafés, bars, shops,
  hotels — precisely the things a trip is made of — are far better mapped
  by Google than by OSM, and Google's pin is the *business's own*
  position rather than an address interpolation.

So this stage does three things: it asks the better question first, it
reads the answer's quality instead of ignoring it, and it adds a second
opinion — **Serper's `/places` endpoint**, which the operator's existing
`serper` search key already pays for.

The second opinion also buys something no single source can give:
**disagreement**. When OpenStreetMap and Google put a place two kilometres
apart, that is not a pin to show with confidence — it is a question, and
the UI should ask it rather than guessing. That is the one case where
**Accept all** must not decide on the user's behalf.

Decisions taken up front:

- **Both sources are asked for every location, and the better answer
  wins.** Not an escalation ladder. A paid Serper call per location (up
  to six in a trip-suggestion run) is worth it, and asking both is what
  makes disagreement detectable at all. The two lookups run concurrently
  — they are different services — while Nominatim stays serialised
  *across* locations, for the reason [agent.go:975-981](internal/assist/agent.go#L975)
  already gives.
- **A precise OSM match beats Google.** OSM is better for landmarks,
  museums, churches and stations, and only an OSM match carries the
  `osm_type`/`osm_id` identity that Stage 29 built the feature link on
  ([geocode.go:57-77](internal/geocode/geocode.go#L57)). Google wins when
  OSM's answer is coarse, and when OSM has no answer at all.
- **The user is shown what was matched, not a pair of numbers.** Today
  the panel offers `64.146600, -21.942600`
  ([assist-panel.js:392](web/js/components/assist-panel.js#L392)), which
  nobody can judge. It will offer the matched name, the source it came
  from, and a warning when the match is only street-accurate.
- **No Serper in automated tests.** The key costs money. A stub places
  backend answers under `CARAVEL_SEARCH_PROVIDER=stub`, and the live API
  is exercised once, by hand, in Milestone 3.

---

## Milestone 1 — A stub geocoder, and a stub for places

Clears the long-standing `plans/todo.md` entry ("The UI suite reaches the
real Nominatim"), and it comes first because every later milestone needs
to be verifiable offline — including the one that must never reach a paid
API from CI.

- **`CARAVEL_GEOCODER_URL=stub`**, matching the sentinels the LLM
  (`LLMStub`, [config.go:176](internal/config/config.go#L176)) and the
  search provider ([search.go:112](internal/assist/search.go#L112))
  already use. `geocode.New` recognises it and returns a `*Client` whose
  `get` is an in-process fixture instead of an HTTP call — both `Search`
  and `Reverse` funnel through that one method
  ([geocode.go:242](internal/geocode/geocode.go#L242)), so a single
  substitution covers the whole package and the fixture answers in
  Nominatim's own JSON shape. The exported `Result` behaviour, the
  `ReverseURL` derivation and `ReverseAvailable` stay exactly as they
  are.
- The fixture answers a small table keyed on the query — enough to cover
  a precise match, a coarse (street-level) match, and a miss — so
  Milestone 2's classification and Milestone 5's ambiguity case are both
  reachable without a network.
- **`scripts/with_server.sh`** exports `CARAVEL_GEOCODER_URL=stub`
  alongside the three sentinels it already sets (line 92-94).

Files: [internal/geocode/geocode.go](internal/geocode/geocode.go), a new
`internal/geocode/stub.go`, [scripts/with_server.sh](scripts/with_server.sh),
[.env.sample](.env.sample), `plans/todo.md`.

**Verify:** `make ci`; `make test-ui` green with the network's geocoder
unreachable (assert it by pointing `CARAVEL_GEOCODER_URL` at the sentinel
and confirming `assist.spec.js` and `locations.spec.js` still pass);
`go test ./internal/geocode/...`.

**Done.** `CARAVEL_GEOCODER_URL=stub` starts an in-process fixture
geocoder on loopback (`internal/geocode/stub.go`), recognised in `New`
and otherwise invisible: `Client`, `get`, `Search`, `Reverse` and the
`/search` → `/reverse` derivation are all unchanged and all still
exercised, because the fixture fakes only who is on the other end of the
socket. That was the deviation from the plan worth naming — the plan
proposed substituting `Client.get`, which would have left the timeout,
the User-Agent, the non-200 handling and the JSON decoding untested
under the stub. A loopback server in the shape of
`internal/wikimedia/stub.go` costs three lines in `New` instead, and it
is also what keeps `ReverseAvailable()` true, so the suite does not
silently lose the reverse-geocoding control it is meant to be testing.

The fixture table holds four places covering the three cases the
resolution algorithm has to tell apart. The pair that matters:
`Kex Hostel, Reykjavik` answers with the hostel itself — a `tourism`/
`hostel` node at 64.14659,-21.92535 with an OSM identity — while its own
postal address `Skulagata 28, 101 Reykjavik, Iceland` answers with the
street, a `highway`/`residential` way 176 m away. That is this stage's
bug, now reproducible offline and without a network. `category`, `type`
and `addresstype` are in the fixture already even though `Result` does
not read them until Milestone 2. Anything not in the table returns an
empty array; reverse lookups answer one canned address, and past ±85°
latitude answer nothing, so `ErrNoResult` stays reachable.

**The stub found a spec that had never been intercepting what it thought
it was.** `map.spec.js`'s German 324px case registers a
`page.route("**/api/geocode?*")` handler and fulfils it with two canned
results. It registered it *before* `login(page)` — and `login` installs
`blockExternalRequests`, whose catch-all `**/*` route continues every
same-origin request. Playwright runs handlers in reverse registration
order, so the catch-all shadowed the geocode handler and it never fired:
for the whole of that test's life the request went to the server, and the
server proxied it to the public Nominatim, which happens to return
exactly two results for "Kirkjufell". Nothing ever looked wrong. Proved
rather than guessed: the test passes with the old wiring, fails against
the stub with the server's own "no places found" message in the page
snapshot, and passes again once the two lines are swapped. The handler
now sits after `login()` and the comment says why the order matters. It
is the only place in the suite with that ordering — checked across every
spec, not assumed.

Verified: `make ci` green. `go test ./internal/geocode/` including four
new tests in `stub_test.go` — the sentinel builds a client with a
derivable reverse endpoint, the name and the address resolve to
*different* points (the fixture would be useless otherwise), a miss is an
empty slice rather than nil, and reverse answers both an address and
`ErrNoResult`. `make test-ui` at 236 passed / 1 failed against the stub,
including `assist.spec.js`'s coordinates suggestion — the assertion that
actually needed a live Nominatim — passing unchanged.

The one failure is `assist-suggest.spec.js`'s first test, which
`plans/todo.md` already recorded as flaky under parallel load before this
stage began. It passes alone in ~8s every time and failed in both full
runs here, on the batch add rather than on the menu click. Ruled out as a
consequence of the stub rather than assumed to be: the whole flow was
driven through the API against a stub-configured server — the suggest run,
then `POST /items/batch` with the exact candidates and the exact fixture
coordinates — and answered 200 and 201, and that endpoint has no rate
limiter. The todo entry now carries both symptoms and the missing piece
(nothing captures the failed request's status or body).

Also updated: the now-false comments in `tests/ui/locations.spec.js` and
`tests/ui/map.spec.js` that told the reader the suite runs against the
real Nominatim. Both specs keep intercepting `/api/geocode*` at the
network boundary, and both now say why that is still right — they assert
client behaviour, and a canned answer says it more directly than a
fixture shared with three other specs.

---

## Milestone 2 — What was matched, and asking the better question

The milestone that fixes the reported bug. No new provider yet.

- **`geocode.Result` keeps the match's kind.** Add `Class`, `Type` and
  `AddressType` (from `jsonv2`'s `category`, `type`, `addresstype`), and
  a `Precise() bool` derived from them in one place with the reasoning in
  a comment:
  - **precise** — `amenity`, `tourism`, `shop`, `leisure`, `historic`,
    `office`, `craft`, `building`, `aeroway`, `railway`; or an
    `addresstype` of `building` / `house`.
  - **coarse** — `road`, `postcode`, `boundary`, and the `place`
    administrative levels (city, town, suburb, neighbourhood, county,
    state). This is the pin that lands 200 m off.
- **One resolver, not two.** `buildProposal` and `locate` currently carry
  the same loop twice. Collapse them into a single
  `resolvePosition(ctx, raw modelProposal, log) *Position` in a new
  `internal/assist/locate.go`, called from both — otherwise this stage
  doubles a duplication instead of removing it.
- **Place name before address.** The order becomes `place_name` →
  `address`, reversing today's. The schema already asks for `place_name`
  to be "the searchable name and city"
  ([schema.go:91-93](internal/assist/schema.go#L91)), which is exactly a
  geocoder query; the address stays as the fallback, which is what
  rescues a private apartment with no findable name.
- **A `Position` value** replaces the bare `*float64` pair inside the
  package: latitude, longitude, the matched label (`display_name`), the
  source, whether it was precise, and the OSM identity when there is one.
  `Proposal` and `Candidate` ([types.go:163-210](internal/assist/types.go#L163))
  carry `Position *Position`; the wire shape is Milestone 4's job, so
  this milestone leaves `toAssistProposalResponse` filling `lat`/`lng`
  from it and nothing else changes for the client yet.

Files: [internal/geocode/geocode.go](internal/geocode/geocode.go), new
`internal/assist/locate.go`, [internal/assist/agent.go](internal/assist/agent.go),
[internal/assist/types.go](internal/assist/types.go),
[internal/httpapi/assist.go](internal/httpapi/assist.go).

**Verify:** `make ci`. Go tests over the classifier against captured
Nominatim payloads (a café node, a street way, a city relation) asserting
`Precise()` for each. A test proving the resolver tries the name before
the address, and falls back when the name misses. Then a **live check**
against `make dev` with the real Nominatim: enrich a location whose
address resolves to a street and confirm the pin moves onto the building.

**Done.** `geocode.Result` now keeps `Class`, `Kind` and `AddressType`
(jsonv2's `category`, `type`, `addresstype`) and answers `Precise()` from
them. Two deviations from the plan, both small: the class is read under
*either* spelling — jsonv2 calls it `category` and the older `json`
format calls it `class`, and an operator pointing
`CARAVEL_GEOCODER_URL` at a compatible service answering in the other
shape should not silently lose its precision signal. And the lists grew
past the plan's: `natural` and `man_made` are in, because a named
waterfall or a lighthouse is a destination, and `house_number` joined the
address types. `Precise()` is deliberately conservative — an unknown
class is **not** precise, so the cost of being wrong is a second opinion
rather than a confident wrong pin. The three fields are additive on
`/api/geocode` too, `omitempty`, with no consumer yet.

`resolvePosition` in the new `internal/assist/locate.go` replaces the two
near-identical copies in `buildProposal` and `locate`, and asks in the
order **place name, then address**. `Position` — coordinates, the matched
label, the source, `Precise`, which query answered, and the OSM identity
— replaces the bare `*float64` pair on `Proposal` and `Candidate`. The
wire is unchanged this milestone: `toAssistProposalResponse` fills
`lat`/`lng` from the position and drops the rest, because a client that
would ignore it is not worth sending it to. That is Milestone 4.

**The pins moved, and by how much was measured.** Against a
stub-configured server, the same suggestion run that Milestone 1 recorded
returning `64.14243,-21.92722` for Hallgrímskirkja and
`64.14793,-21.92341` for Kex Hostel — the square and the street — now
returns `64.1419,-21.9265` and `64.14659,-21.92535`: the church and the
hostel. 69 m and 176 m, both onto the building. The third candidate,
which proposes neither a name nor an address, still resolves to nothing
and still costs no request.

Verified: `make ci` green. New Go tests — eight rows through `Precise()`
against real Nominatim payload shapes (hostel, church, house number,
road, city, postcode, an invented class, and a result with no class at
all), the `class`/`category` spelling, and three resolver tests driving
the fixture geocoder directly: the name wins over an address that would
*also* have resolved (which is exactly what made the old order look
correct), the address is the fallback when the name misses, and nothing
configured / nothing to ask / a miss all give nil rather than a guess.
The existing agent and suggest tests were rewritten rather than patched,
since the thing they asserted — address first — is the thing that
changed. `make test-ui` green across all 17 assist and address-search
specs.

No live Nominatim check was needed in the end: the fixture reproduces the
exact failure the stage exists to fix (a place whose name and whose
postal address both resolve, to points 176 m apart), which is a stronger
and repeatable version of the manual check the plan asked for.

---

## Milestone 3 — Serper Places as a second opinion

- **`PlaceLocator`, an optional capability**, discovered by type
  assertion exactly as `ImageSearcher` already is
  ([search.go:74-79](internal/assist/search.go#L74)) — so ddgs, Ollama
  and an unconfigured instance simply don't have it and the resolver
  falls back to Milestone 2's behaviour with no branching in the config.

  ```go
  type PlaceLocator interface {
      SearchPlaces(ctx context.Context, query string) ([]PlaceResult, error)
  }
  type PlaceResult struct{ Title, Address string; Lat, Lng float64; Category, Website string }
  ```
- **`*serperSearcher` implements it** via `POST /places`, with the
  endpoint derived from the configured URL the same way `imageURL`
  already is ([search_serper.go:40](internal/assist/search_serper.go#L40)),
  the same `X-API-KEY` header, and the same status handling (402 = out of
  credit, 403 = bad key). **The response field names must be confirmed
  against a live call before the parser is written**, as `/images` was
  ([search_serper.go:114](internal/assist/search_serper.go#L114)) — the
  documented shape and the real one have already differed once in this
  file's history.
- **`stubSearcher` implements it too**, so the two-source path is
  exercised by `go test` and by the UI suite without a key.
- **The choice**, in `locate.go`, with `metresBetween`
  ([agent.go:1077](internal/assist/agent.go#L1077)) doing the arithmetic:

  | OSM | Google | Result |
  |---|---|---|
  | precise | — | OSM (keeps the OSM identity) |
  | coarse | hit | Google |
  | miss | hit | Google |
  | hit | miss | OSM |
  | miss | miss | no position |
  | **> 2 km apart** | | **ambiguous**: the preferred one is primary, the other is kept as an alternative |

  `ambiguousMetres = 2000`, with the comment saying what it separates: two
  services pinning opposite ends of one complex, versus two different
  branches or two different towns. Two genuine franchises 1 km apart will
  not trip it, and the matched label shown in Milestone 4 is what covers
  that case.
- The two lookups run concurrently for one location (different services);
  the loop over candidates stays sequential, because Nominatim's
  one-request-per-second policy is the reason it is sequential today.

Files: [internal/assist/search.go](internal/assist/search.go),
[internal/assist/search_serper.go](internal/assist/search_serper.go),
`internal/assist/locate.go`.

**Verify:** `make ci`, with table-driven tests over the choice matrix
against a fake HTTP server and the stub. Then **one live run**, by hand,
with the real Serper key exported into `make dev`: enrich a restaurant
and a hotel that OSM does not map, and confirm the pin lands on the
business. Record the observed `/places` response shape in the milestone's
**Done.** paragraph, since nothing in CI will ever see it again.

---

## Milestone 4 — Provenance on the wire and in the panel

- **The API carries a position, not a coordinate pair.**
  `assistProposalResponse` and `assistCandidateResponse`
  ([assist.go:88-100](internal/httpapi/assist.go#L88),
  [assist.go:170-180](internal/httpapi/assist.go#L170)) replace
  `lat`/`lng` with:

  ```json
  "position": {
    "lat": 64.1466, "lng": -21.9426,
    "label": "Café Loki, Lokastígur 28, Reykjavík",
    "source": "osm",            // or "google"
    "precise": true,
    "osm_type": "node", "osm_id": "240109189",
    "alternatives": [ { … same shape, without alternatives … } ]
  }
  ```

  Both clients are served from this build and both are updated in this
  stage, so the old keys go rather than lingering; the service worker's
  cache key already moves on its own since Stage 23 Milestone 2.
- **The panel shows the place, not the numbers.** The `coordinates`
  suggestion ([assist-panel.js:388-396](web/js/components/assist-panel.js#L388))
  renders the label, a source badge, and — when `precise` is false — a
  line saying the match is street-accurate only. The coordinates stay
  visible underneath in small type; they are still the thing being
  accepted.
- **An accepted OSM position keeps its identity.** `applyCoordinates`
  gains the optional identity and the editor sets it *after*
  `coordinatesChanged()`, mirroring exactly what the address-search
  result path already does
  ([location-editor-page.js:665-671](web/js/pages/location-editor-page.js#L665)).
  Assist-placed locations currently lose the OSM feature link for no
  reason other than that nobody passed it through.
- **The suggestions page** shows the matched label on a card instead of
  the bare `suggest.located` string
  ([suggest-page.js:230](web/js/pages/suggest-page.js#L230)).

Files: [internal/httpapi/assist.go](internal/httpapi/assist.go),
[web/js/components/assist-panel.js](web/js/components/assist-panel.js),
[web/js/pages/location-editor-page.js](web/js/pages/location-editor-page.js),
[web/js/pages/suggest-page.js](web/js/pages/suggest-page.js),
`web/locales/{en,de}.json`.

New i18n keys (both locales — `scripts/check_i18n.py` enforces parity):
`assist.position.source.osm`, `assist.position.source.google`,
`assist.position.approximate`.

**Verify:** `make ci`. A Playwright assertion in `assist.spec.js` that the
coordinates suggestion's accessible text contains the stub's matched
label and its source badge, and that accepting it leaves the location
view showing an OpenStreetMap feature link — which is the assertion that
proves the identity actually survived the hand-off.

---

## Milestone 5 — Ambiguity, Accept all, and the docs

- **An ambiguous position is a question, not a suggestion.** When
  `alternatives` is non-empty the panel renders the row with a radio list
  — up to three options, **none preselected** — and the row is **excluded
  from Accept all** ([assist-panel.js:429-432](web/js/components/assist-panel.js#L429)),
  staying outstanding until the user picks one or rejects it. The
  outstanding counter (`assist.outstanding`) says one still needs a
  choice. This is the whole point: Accept all should never silently
  choose between two places 40 km apart.
- **On the suggestions page there is no picker.** An ambiguous candidate
  is added **with its address and no pin**, through the path that already
  exists at [suggest-page.js:367](web/js/pages/suggest-page.js#L367). The
  place lands on the trip and the pin gets set in the location editor,
  which already has a map picker, an address search and the map-link
  resolver. Six checkbox cards each growing a nested chooser is a lot of
  UI for a rare case, and it would make "Add selected" mean something
  different per card.
- **Docs.** [docs/configuration/assistant.md](docs/configuration/assistant.md)
  gains a paragraph on how a position is resolved and what the `serper`
  provider now additionally buys (its table row at line 42 currently
  describes web results only); the geocoder section notes the `stub`
  sentinel. Run `make docs` — `--strict` is load-bearing.

New i18n keys: `assist.position.choose`, `assist.position.needsChoice`,
`suggest.positionUnclear`.

Files: [web/js/components/assist-panel.js](web/js/components/assist-panel.js),
[web/js/pages/suggest-page.js](web/js/pages/suggest-page.js),
`web/locales/{en,de}.json`, [docs/configuration/assistant.md](docs/configuration/assistant.md).

**Verify:** `make ci` and `make docs`. A Playwright case driving the stub
into disagreement: assert the coordinates row has no checked option, that
**Accept all** leaves it outstanding with the counter at one, and that
picking an option then clears it. A second case on the suggestions page
asserting an ambiguous candidate is created with an address and null
coordinates. Mobile check at 324×756 that the radio list does not
overflow the panel.

---

## Build order

1. **The stubs first** — everything after this needs offline verification,
   and Milestone 3 must never reach a paid API from CI.
2. **Precision and the reordered resolver** next: it fixes the reported
   bug on its own, and it is worth having in the diff by itself.
3. **Serper** after the resolver exists to plug into.
4. **The wire and the panel** before ambiguity, so the ambiguous case has
   a place to render into.
5. **Ambiguity and the docs** last.

## Workflow

Per `CLAUDE.md`, for each milestone in order: implement → verify
(`make ci` green plus a real behavioural check, assertions preferred over
screenshots) → add a **Done.** paragraph to `plans/stage-33.md` recording
what actually landed and any deviation → reconcile `plans/todo.md` in
both directions → one commit describing what, why and exactly how it was
verified → make sure `make dev` is up → **stop and hand back control**,
and wait before starting the next milestone.

## Verification (whole stage)

- `make ci` green at every milestone; `make docs` at Milestone 5.
- `make test-ui` green with `CARAVEL_GEOCODER_URL=stub` from Milestone 1
  onward, and no automated test ever reaching Serper or Nominatim.
- **Two live checks, by hand, both recorded in the plan's Done
  paragraphs:** one against real Nominatim in Milestone 2 (a place whose
  address resolves to a street), one against the real Serper key in
  Milestone 3 (a restaurant and a hotel OSM does not map).
- End to end against `make dev-seed` + `make dev`: enrich a café through
  the panel, confirm the suggestion names the place rather than showing
  bare numbers, accept it, and confirm the location view shows the pin on
  the building and an OpenStreetMap feature link where the match was OSM.

## Deliberately out of scope

- **Sanity-checking a position against the trip's other places.** The
  two-source disagreement covers the real failure and needs no notion of
  "where this trip is". `plans/todo.md` gets the entry.
- **Offering the places lookup to the *model* as a tool.**
  `toolGeocode`'s description ([tools.go:146-158](internal/assist/tools.go#L146))
  stays OSM-only; the model's tool use is for verification, and the
  coordinates that reach a proposal never come from it anyway.
- **Serper's `website` and `phoneNumber`** as link proposals. Real, and a
  different feature.
- **Backfilling `osm_type`/`osm_id`** for older locations — an existing
  todo entry, and a guess made on the user's behalf.

## How complex is this, really

The risk is not in the algorithm — the choice matrix is six rows and a
distance comparison. It is in two places. The first is the Serper
response shape, which is unknowable from here and has already burned this
file once; the parser gets written *after* the live call, not before. The
second is Accept all: excluding a row from a bulk action means the row's
state and the counter's state can disagree, and `outstanding` is a plain
array that both the accept path and the reject path mutate
([assist-panel.js:285-296](web/js/components/assist-panel.js#L285)).
That interaction is where a defect will be, and it is worth reading twice
rather than trusting a green suite.
