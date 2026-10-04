# Stage 46: Brave Search as a search provider

## Context

`CARAVEL_SEARCH_PROVIDER` feeds three features: the assistant's web research,
the web half of the image picker, and the assistant's second opinion on where
a place is (`websearch.PlaceLocator`). Only Serper offers all three. On a
personal home-lab instance, Serper costs real money: 2,500 free credits
when you sign up, then $50 packs whose credits expire after six months. Brave
Search API gives $5 of credit every month (about 1,000 requests), which a
personal instance is unlikely to use up. It is also a proper API, not a
scraper, and covers all three features.

A live probe on 2026-10-04/05 (about 45 Brave requests) established the facts
this plan builds on:

- **Web** `GET https://api.search.brave.com/res/v1/web/search`, header
  `X-Subscription-Token`. Results are `web.results[]` with `title` / `url` /
  `description`. **The `description` and sometimes the `title` contain HTML**
  (`<strong>`, `&amp;`), so tags must be stripped and entities decoded.
- **Images** `GET .../res/v1/images/search`. Each result has `title`, `url`
  (the page), `properties.url` (full image), `properties.width`/`height`, and
  `thumbnail.src` on `imgs.search.brave.com` (500 px wide). All 12
  thumbnails and full images loaded when requested with a foreign `Referer`.
- **Places** `GET .../res/v1/local/place_search`. Each result has `title`,
  `postal_address.displayAddress`, `coordinates` as `[lat, lng]`, and
  `description` ("Hostel", "Coffee Shop"), which is the same kind of value as
  Serper's `category`. `country` accepts only 37 codes (no Iceland), so it is
  always sent as `ALL`.
- **Place accuracy:** 11 of 13 test places came back identical to Serper's
  answer (0 m apart, same titles). A query is only reliable when it includes
  the town: the model's `place_name` ("Kex Hostel, Reykjavik") works as it is,
  but a name with no town went abroad (Pension Sonnenhof landed in South
  Tyrol, the `plans/todo.md` item). Passing the model's **raw postal address
  as `location`** fixed that case and changed none of the others, so no
  address parsing is needed.

## 0. Land the plan

Commit this plan as `plans/stage-46.md`. Nothing in `plans/todo.md` asks for
Brave, so the todo file stays unchanged here.

## 1. Brave backend: web and image search

New `internal/websearch/brave.go`, modelled on `serper.go`:

- `braveSearcher{url, imageURL, placesURL, key, client}`, built by
  `newBraveSearcher(key, overrideURL)`. Default
  `https://api.search.brave.com/res/v1/web/search`. The image and places URLs
  are derived by trimming `/web/search` from the override, the same way
  `newSerperSearcher` derives its sibling endpoints, so a proxy override
  covers all three.
- `Search`: `q`, `count=MaxResults`. Map `title`/`url`/`description` →
  `Result`, skip rows with no URL, pass text through a `stripHTML` helper
  (regexp tag strip + `html.UnescapeString`) followed by the existing
  `collapseWhitespace`/`truncate`.
- `SearchImages`: `q`, `count=imageSearchMaxResults`. Map to `ImageResult`
  (`URL`=`properties.url`, `ThumbURL`=`thumbnail.src`, `SourceURL`=`url`).
  Skip rows with no full image URL. Dimensions are optional.
- Error mapping like Serper's: 401/403 → key rejected, 402 → out of credit,
  429 → rate limited; anything else → status code.
- Wire it up: a `"brave"` case in `websearch.New` (requires a key),
  `"brave"` in `config.SearchProviders`, and the package doc comment's
  backend count goes from four to five.
- Fix the `ImageSearcher` and `PlaceLocator` comments that list which
  backends have what.

Tests (`backends_test.go`, `images_test.go`, with httptest servers fed the
recorded response shapes): field mapping, HTML stripping, header and query
params, endpoint derivation, credit vs auth errors, rows with no URL skipped,
`New` knowing `brave`, Brave being an `ImageSearcher`, a config test for
`brave` without a key.

Docs: a `brave` column and a provider section on
`docs/configuration/web-search.md` (setup, the monthly credit, the card on
file with no spending cap by default, the advice to set a limit), and Brave
added to the image-search sentence in `docs/configuration/images.md`. Places
stay "Serper only" until Milestone 2.

**Done.** `internal/websearch/brave.go` landed as planned, with one shared
`get` helper for the HTTP request, the auth header and the status mapping,
where Serper repeats them per endpoint. `placesURL` is left for Milestone 2,
which first needs it. The `New` case, `config.SearchProviders` entry and
backend comments are updated as listed. One deviation: the docs do not tell
operators to set a spending limit in the Brave dashboard, because I could not
confirm the dashboard has one; they say only that requests past the monthly
credit are billed to the card. Tests: recorded web and image shapes
(mapping, HTML stripping in titles and snippets, a GET with
`X-Subscription-Token` and `count`), `TestStripHTML`, 401/403/402/429/500 each named, sibling
endpoint derivation including a proxy override, rows with no URL skipped,
`New` with and without a key, Brave as an `ImageSearcher`, and `brave`
accepted by `config.Load`. `make ci` and `make docs` are green. Live, on a
separate instance (:8097, throwaway seeded DB) with the real key: the startup
log showed `search=brave` and `image_search_web=brave`. The image picker for
"Kex Hostel Reykjavik" showed a "From the web (brave)" group of 12, and all 12
`imgs.search.brave.com` thumbnails had `naturalWidth` 500. Picking one stored
the 900 px original as media, with the hostelgeeks page as `source_url`. An
assistant enrich run made 6 `web_search` calls through Brave, all
`ok=true` at 0.6–0.8 s each, and finished in 21 s with 4 fields, 2 links and
a pin. A temporary live test (deleted afterwards) confirmed that none of the 6
real results kept tags or `&amp;`.

## 2. Brave place lookup, with the address as search area

**Interface change** in `internal/websearch/websearch.go`:

```go
type PlaceLocator interface {
	// near is free text naming the area to search in (the model's postal
	// address); "" when there is none. A backend may ignore it.
	SearchPlaces(ctx context.Context, query, near string) ([]PlaceResult, error)
	// PlaceSource is the badge value a pin from this backend carries.
	PlaceSource() string
}
```

- `serperSearcher.SearchPlaces` ignores `near` (the comment says why:
  over-specifying makes Serper miss, which `locateViaPlaces` already
  documents), and `PlaceSource()` returns `"google"`.
- `braveSearcher.SearchPlaces`: `q=query`, `location=near` when it is
  non-empty and different from `query`, `country=ALL`,
  `count=placeSearchMaxResults`. Skip rows without a title or with
  `len(coordinates) != 2`. `Category` = `description`. `PlaceSource()`
  returns `"brave"`.
- `Stub` gains `PlaceSource() "google"` and the new parameter, with
  behaviour unchanged.

**`internal/assist/locate.go`**:

- `locateViaPlaces` passes `address` as `near` on both steps (name, then
  address), and sets `Source: locator.PlaceSource()` instead of the
  `SourceGoogle` constant.
- Add `SourceBrave = "brave"` beside `SourceGoogle`, with the comment saying
  the badge names Brave rather than guessing at its upstream data.
  `choosePosition`'s debug logs report `google.Source` instead of the
  constant. Rename the local variable to `places` if that reads better.
- `locate_test.go`'s `recordingLocator` records `near` too. New test: the
  address reaches the locator as `near`, and the position carries the
  locator's source.

**UI:** `assist.position.source.brave` ("Brave Search") in `en.json` and
`de.json`, plus the `SOURCE_LABELS` entry in
`web/js/components/assist-panel.js`.

**Live probe:** `TestLiveSourceAgreement` reads `CARAVEL_SEARCH_PROVIDER`
(default `serper`) instead of hard-coding Serper, and adds Pension Sonnenhof
/ "4820 Bad Ischl, Austria" to the place list. Update the probe's row in
`docs/configuration/server.md`.

**Docs:** place lookup ✅ for `brave` on the web-search page. In
`assistant.md`, the coordinates section says "Serper's or Brave's places
endpoint" and notes that Brave is given the address as its search area.
`plans/todo.md`: the "place name with no town" entry gets a note that the
Brave side now passes the address, while the OSM side and Serper still have
the problem.

## Build order

0 → 1 → 2. Milestone 1 is usable on its own (web and image search with no
place lookup, like ddgs). Milestone 2 depends on the `braveSearcher` from
Milestone 1.

## Workflow

Per CLAUDE.md, for each milestone: implement; `make ci` green plus a live
check; add a **Done.** paragraph to this file and update `plans/todo.md` in
both directions; one commit (no Co-Authored-By trailer); make sure `make dev`
is running; stop and wait.

## Verification

- `make ci` after each milestone. `make docs` before committing docs changes.
- **Milestone 1, live:** run `make dev` with `CARAVEL_SEARCH_PROVIDER=brave`
  and the key from `.env`. Check the startup log shows
  `image_search_web=brave`. In the location editor's image picker, search
  "Kex Hostel Reykjavik": assert the web group renders, the thumbnail `img`s
  have `naturalWidth > 0` and come from `imgs.search.brave.com`, and picking
  one stores the image. Run an assistant enrich for one location and check
  the debug log shows `search=brave` and searches returning results whose
  snippets contain no `<strong>`.
- **Milestone 2, live:**
  `CARAVEL_LIVE_PROBE=1 CARAVEL_SEARCH_PROVIDER=brave CARAVEL_SEARCH_KEY=… go test ./internal/assist/ -run TestLiveSourceAgreement -v`
  should show Sonnenhof resolved in Bad Ischl and the other places agreeing
  with OSM about as often as the Serper run does. In the UI, an assistant
  run on a Brave-sourced place shows the "Brave Search" badge.
- Brave requests stay in the low hundreds for the whole stage (≈ $1 of the
  monthly credit).
