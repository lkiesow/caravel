# Stage 27 — The assistant suggests a trip, not just a pin

## Context

The assistant today answers exactly one question: *tell me more about this one
place.* `POST /api/trips/{tripId}/assist/location` takes a location — existing,
or described in a sentence — and streams back a single proposal that the
editor's panel offers field by field (`internal/httpapi/assist.go:143`,
`web/js/components/assist-panel.js:76`). Everything that makes that work is
general: the provider client, four search backends, the tool loop with its
deadlines and token budget, the SSE transport, the guard rails that never let a
coordinate come from the model, and a stub provider that makes the whole thing
testable offline.

What it cannot answer is the question people actually start a trip with: *what
should I do in Reykjavik?* That needs several candidates at once, a way to read
them side by side and throw two away, and a way to add the rest without six
round trips through the editor. `plans/todo.md` has carried this as **AI
trip-level suggestions**, tagged **(soon)**, since the Stage 15 review, and it
notes correctly that the entry is no longer blocked — Stage 16 built every piece
it needs. What is genuinely new is a multi-result review UI, a way to add N
locations in one transaction, and dedup against what the trip already has.

Two smaller assistant items ride along, both from the same backlog:

- **The configured limits never reach the server.** `CARAVEL_ASSIST_RATE_LIMIT`
  and `CARAVEL_ASSIST_MAX_CONCURRENT` are parsed, range-checked, documented,
  sampled in `.env.sample`, and logged at startup with their effective values —
  and then dropped: the `httpapi.Options` literal at `cmd/caravel/main.go:156`
  sets neither field, so `NewServer` always falls back to
  `DefaultAssistRateLimit` (6) and `DefaultAssistMaxConcurrent` (4). The startup
  log reports numbers the running server is not using, which is worse than no
  log at all, because it is the line somebody would check.
- **A turn's tool calls are dispatched one at a time**
  (`internal/assist/agent.go:424-442`), which reads oddly beside `checkLinks` in
  the same file — already fanning out with a `WaitGroup` at
  `internal/assist/agent.go:849-857`. The backlog is explicit that this is
  tidiness, not a speed fix: tool calls are ~12% of a run, and only a turn
  issuing two or more benefits at all.

### Decisions taken before planning

- **The entry point is a menu on the New button**, not a fifth toolbar control.
  Stage 26 filled the locations toolbar to search + filter + sort + New, and it
  is a deliberately non-wrapping row at 324px
  (`web/js/pages/locations-tab.js:41-56`). "New location" becomes a two-row
  menu — *Blank location* and *Suggest locations* — using the app's existing
  `renderMenu`. The primary action gains a tap; a wrapping toolbar would cost
  more.
- **Select, then add all at once.** Each candidate is a card with a checkbox,
  selected by default; one *Add N locations* button commits them through a new
  batch endpoint in a single transaction. This is the backlog's own wording, and
  it makes reviewing — reading six candidates and rejecting two — the shape of
  the screen.
- **Prompt caching stays in the backlog.** Exploring found the system prefix is
  *not* stable across runs: it embeds the trip's tag vocabulary and the user's
  locale (`internal/assist/prompt.go:54-65`), and the wire client has no
  cached-token fields at all (`internal/assist/provider.go:110-116`), so a cache
  hit would be invisible even if it happened. Real work, unmeasured prize,
  separate stage.

---

## 0. Land the plan, and reconcile the backlog

Commit this file as `plans/stage-27.md`. Update `plans/todo.md`: the three
entries this stage takes on — **AI trip-level suggestions**, **The assistant's
configured limits never reach the server**, **Assistant round trips: batching
and parallel tool dispatch** — stay in the file and are deleted by the milestone
that actually lands each one, per that file's own rules. Rewrite the **Prompt
caching** entry with what exploring established, so the next person planning it
starts from the measurement rather than the assumption: the prefix is unstable
per trip and per locale, and `usage` (`internal/assist/provider.go:110-116`)
carries no cached-token fields, so the first move is making the prefix stable
and making a hit visible, not sending cache directives.

## 1. The limits that never arrived

Two fields on the `httpapi.Options` literal in `cmd/caravel/main.go:156-177`:
`AssistRateLimit: cfg.AssistRateLimit` and `AssistMaxConcurrent:
cfg.AssistMaxConcurrent`.

The test is the point, and it needs a seam. Extract the literal into a
`serverOptions(...) httpapi.Options` function in `package main` and assert in a
new `cmd/caravel/main_test.go` that a config carrying non-default values
produces an `Options` carrying them. The behaviour half — `Options` to an
actually-enforced limit — is already proved by `TestAssistIsRateLimited`
(`internal/httpapi/assist_stream_test.go:426`) and
`TestAssistRefusesWhenAllSlotsAreBusy`
(`internal/httpapi/assist_stream_test.go:304`); what was missing is that config
reaches `Options` at all.

Verify by hand too: start with `CARAVEL_ASSIST_MAX_CONCURRENT=1` and confirm a
second concurrent run is refused with `assist_busy`, which today it is not.

**Done.** `cmd/caravel/main.go` gained `serverOptions(cfg, opts)`, which fills
in everything the server takes from configuration -- `NoCache`,
`TrustedProxies`, `BaseURL`, `Tiles`, and the two assist limits -- on top of
the collaborators `main` has already constructed. The literal in `main` now
holds only those collaborators and is wrapped in a `serverOptions(...)` call.
The raw configured value is passed through rather than pre-defaulted, because
`NewServer` is what turns zero into `DefaultAssistRateLimit` /
`DefaultAssistMaxConcurrent`; defaulting in both places would agree today and
diverge the day one changes.

Verified three ways. `cmd/caravel/main_test.go` (new, five tests) asserts
config reaches `Options` for every field, using values that are neither zero
nor the defaults so a dropped field cannot pass -- and it was checked against a
reverted fix, where it fails with `AssistRateLimit = 0, want 11`. End to end:
`CARAVEL_ASSIST_RATE_LIMIT=1 scripts/with_server.sh ...` logging in as `demo`
and posting twice to `/assist/location` gives 200 then **429** `too many
assistant requests`; the same script with the variable unset gives 200 twice,
which is the control that shows the 429 came from the configured value rather
than from the default of 6. `make ci` green.

The plan proposed proving this with `CARAVEL_ASSIST_MAX_CONCURRENT=1` and
`assist_busy` instead. The rate limiter was used because it is deterministic:
against the stub provider a run finishes far too fast for two sequential
requests to overlap, so a concurrency proof would have needed real parallelism
and a slow provider to be anything but flaky. Both settings travel the same
path from `config` to `Options`, and the unit test covers both.

## 2. One agent loop, two tasks

**No new behaviour in this diff.** `Agent.Propose`
(`internal/assist/agent.go:201-492`) is 290 lines of turn loop, deadline
handling, token budget, ceilings, event emission and stop accounting, and
Milestone 3 needs every line of it for a differently-shaped answer. Forking it
would leave two copies of the guard rails.

Parameterise the loop by a `task` value carrying what actually differs:

- the three prompt strings (system, user, final);
- the answer tool's name and JSON Schema — today `propose` and `proposalSchema`
  (`internal/assist/schema.go`), hard-wired into both `definitions()`
  (`internal/assist/tools.go:126-133`) and the short-circuit at
  `internal/assist/agent.go:390-417`;
- the `responseFormat` for the composing turn;
- a decode hook for the model's raw answer bytes.

`Propose` becomes a thin wrapper constructing the location task. The existing
~40 tests in `agent_test.go`, plus `tools_test.go`, `stub_test.go` and
`TestValidCategoriesMatchTheSchema`, are the proof that nothing moved; add none.
`make ci` green with the suite unchanged is the milestone.

**Done.** The loop is now `runTask[A, R](ctx, a, task, events, build)` -- a
package-level generic function rather than a method, because a method cannot
take type parameters. `A` is the shape the model answers in, `R` is what the
caller receives. `Propose` is a wrapper: it validates the mode, builds
`locationTask(req)`, and passes a `build` closure that calls `buildProposal`.
`answer` became `composeAnswer[A]` and takes the task's `final` prompt and
`format` instead of reaching for `finalPrompt(req)` and `proposalFormat()`.

Three decisions worth recording:

- **The answer tool keeps the name `propose` for every task.** Only its
  description and schema come from the task (the new `answerTool` type in
  `tools.go`). Its role in the loop -- report the finished result and stop --
  is genuinely the same act, so `findCall(..., toolPropose)`, the exclusion
  from `lookup`, and `describeCall`'s mapping to the composing trace keys all
  stay correct with no per-task branching.
- **`build` runs inside the run, not after it.** It emits events of its own
  (`checkLinks` does), and those have to be counted in the summary the run
  defers; a builder called after `runTask` returned would emit them after the
  summary the client already received. It also returns extra log fields, which
  are appended to the "run finished" record -- what a proposal contains and
  what a list of candidates contains are different facts.
- **The loop never sees a `Request`.** The prompts arrive rendered and the
  handful of request-shaped values the "run started" line wants arrive as
  `task.logFields`, so Milestone 3 can build a task from a differently-shaped
  request without touching the loop.

Deviation from the plan: the plan said add no tests and change none.
`definitions()` gained an `answerTool` parameter, so its ten call sites in
`tools_test.go` are now `definitions(locationTask(Request{}).answer)` -- purely
mechanical, and `agent_test.go`, the ~40 tests that actually describe the
loop's behaviour, is untouched.

Verified: `make ci` green with `internal/assist`'s suite otherwise unchanged,
and `scripts/with_server.sh npx playwright test tests/ui/assist.spec.js` green
-- 6 passed, including the test that asserts exactly eight suggestions across
title, category, tags, notes, address, coordinates, links and cover plus two
sources, which is a full end-to-end reading of the loop's output.

Noted in passing, not fixed: `internal/assist/types.go` is not `gofmt`-clean at
`HEAD` and was not before this stage either. `make ci` does not gate formatting.

## 3. Suggest, server side

**Types** (`internal/assist/types.go`). `ModeSuggest Mode = "suggest"`, and
`Mode.Valid()` extended. A `SuggestRequest` reusing `TagVocabulary`, `Trip`,
`Locale` and `Prompt`, plus `Existing []Location` — the trip's current
locations, so the model can be told not to offer them again. A `Suggestions`
result: a slice of `Candidate`, each carrying a proposed `Location`, its
`Links`, `Lat`/`Lng`, `Cover` and `Sources` — the fields half of today's
`Proposal` without the before/after pairing, because a candidate has no current
value to diff against.

**Schema** (`internal/assist/schema.go`). `modelSuggestions` is
`{suggestions: [modelProposal...]}`; the element type is exactly today's
`modelProposal`, so the property block is written once. `maxItems` in the schema
*and* a hard truncation in Go, because the `json_object` fallback enforces
nothing — the same reason `category` is validated rather than trusted. Six
candidates is the cap to start with, as a named constant.

**Prompts** (`internal/assist/prompt.go`). A suggest system prompt saying:
several *distinct* places, one JSON object per place, do not repeat anything
already on the trip, and the same standing rules the location prompt carries
about never inventing a URL or a coordinate. The existing titles go in the user
message.

**Finishing a candidate.** `buildProposal` (`internal/assist/agent.go:618-704`)
runs per candidate: category validated against `validCategories`, links
liveness-checked, cover chosen, coordinates resolved from the address and then
the place name. Two concurrency notes that matter:

- Link checking already fans out per candidate via `checkLinks`, and six
  candidates' link checks can safely overlap — bound the outer fan-out at 3.
- **Geocoding stays serialised.** `internal/geocode` has no rate limiting of any
  kind, and the default geocoder is `nominatim.openstreetmap.org`, whose usage
  policy is one request per second. Six parallel lookups is exactly the traffic
  a volunteer-run service asks people not to send.

**Dedup** against the trip, in Go, after the model answers: case-folded title
match, plus a coordinate proximity check — a candidate within ~150m of an
existing location with a similar name is the same place under another name.
Dropped candidates are counted and reported rather than silently vanishing.

**HTTP.** `POST /api/trips/{tripId}/assist/locations` (plural), registered
beside its sibling at `internal/httpapi/router.go:340` with the same
`rateLimitAssist` middleware and the same `acquireAssistSlot()` refusal. Same
SSE frame shapes — `progress`, `step`, `summary`, `error` with the existing
`assist_timeout` / `assist_budget` / `assist_failed` codes — plus one
`suggestions` event carrying the array. `RoleEditor`, and the
501-before-trip-lookup ordering, are copied verbatim from
`handleAssistLocation`.

Decide and record: a suggest run visits more places than an enrich run, so
whether `RunDuration` (90s) needs a separate, longer value for this mode is a
question to answer with a measurement, not a guess.

**Stub.** `stub.go`'s script is a fixed 4-turn sequence ending in one Kex Hostel
`propose` (`internal/assist/stub.go:50-84`). It gains a second script, selected
by mode, returning three Reykjavik candidates against the same loopback fixture
host — otherwise the UI suite in Milestone 5 has nothing deterministic to drive.
Go tests: the cap bites, dedup drops a duplicate, an invalid category is dropped
from one candidate without failing the run, coordinates come only from the
geocoder.

**Done.** Landed as planned, with three deviations recorded below.

`internal/assist` gained `ModeSuggest`, `SuggestRequest`, `ExistingPlace`,
`Suggestions` and `Candidate` (`types.go`); `modelSuggestions` plus a
`suggestionsSchema` built by wrapping `proposalSchema` in an array with
`maxItems` from the `maxSuggestions` constant, so the property block has one
definition (`schema.go`); `suggestSystemPrompt` / `suggestUserPrompt` /
`suggestFinalPrompt` (`prompt.go`); and `suggestTask`, `Suggest`,
`buildCandidates`, `locate` and the `placeIndex` dedup (`agent.go`). The HTTP
layer gained `POST /api/trips/{tripId}/assist/locations`, its request
validation, `tripExistingPlaces`, and the `suggestions` event.

- **The prompts were refactored rather than copied.** `systemPrompt` had one
  block of standing rules and one describing the shape of a place, and both are
  the same for a trip-level run -- a run that guessed a URL would be wrong for
  identical reasons. They are now `researchRules(extra)` and
  `placeFields(vocabulary, locale)`. `researchRules` takes its caller's extra
  bullets and places them *before* the last rule rather than after, because the
  last rule is the one about not following instructions found on web pages and
  nothing a task adds should sit between it and the end of the list. Proved
  byte-identical: a throwaway test dumped `systemPrompt` + `userPrompt` +
  `finalPrompt` for a fully-populated request before and after, and `diff`
  reported no change.
- **`checkLinks` and `chooseCover` stopped taking a whole `Request`.** They used
  one field each -- `req.Current.Links` and `req.Locale` -- and a candidate has
  neither a Request nor an existing link list. They now take exactly what they
  read.
- **`Suggestions.Sources` is per run, not per candidate.** Deviation from the
  plan, which put `Sources` on `Candidate`. The agent reads a city guide once
  and it informs several candidates, so attributing pages to individual places
  would have been a provenance trail that is not true.
- **The stub's script is chosen by mode.** `reset()` became `begin(mode)`: the
  default stub now holds two scripts and the agent selects between them at the
  start of a run. A provider built by `newScriptedProvider` has no per-mode
  scripts and keeps its one, so every existing test is unaffected. The suggest
  script deliberately includes one thin candidate -- no address, no link,
  nothing to geocode -- because a script where every candidate is complete
  would never show the review screen in Milestone 5 what a sparse one looks
  like, and sparse is the common case.
- **`RunDuration` was left at 90s.** The plan asked for this to be decided by
  measurement. A stub run takes ~4s, which measures the loop and not the model,
  so there is nothing yet to decide it with; the honest move is to leave it and
  revisit after a real run. Noted rather than guessed.

Verified: twelve new tests in `internal/assist/suggest_test.go` -- several
candidates, an empty prompt refused, the cap truncating from the end, a
duplicate of an existing place dropped, a duplicate within one answer dropped,
a same-position duplicate dropped after geocoding, per-candidate geocoding with
the address-then-place-name fallback and no request at all for a candidate
naming neither, one bad category dropped without spoiling the list, a nameless
candidate skipped without being counted as a duplicate, the propose-call path,
the stub script rewinding between runs *and* handing back to the location
script, and the wrapped schema being genuinely the proposal schema. Five more
in `internal/httpapi/assist_suggest_test.go` cover the route, 501-before-lookup,
the empty prompt, the viewer refusal, and the trip's own locations reaching the
run. `make ci` green.

End to end against a live server (`scripts/with_server.sh`, logging in as
`demo`): the stream carried ten steps -- thinking, searching, two page reads,
composing, two link checks -- a summary of 4 turns / 3 tool calls / 2400
tokens, two sources, and three candidates. Two resolved to real coordinates
through Nominatim, one carried a cover with its credit, and the thin one came
back thin. Nothing in that path is faked except the model.

Not done here, deliberately: the documentation. The assistant page describes a
flow the user cannot reach until Milestone 5 builds the screen, so it is
written there, with `make docs`.

## 4. Adding N locations in one request

`POST /api/trips/{tripId}/items/batch`, body `{"items": [itemRequest, ...]}`,
capped at the same constant, one transaction.

The blocker is that `db.Store.WithTx` **does not nest** (noted at
`internal/httpapi/item_dates.go:163`) and `createItemTx` opens its own
(`internal/httpapi/items_create.go:297`). Split it:
`createItemInStore(ctx, store, trip, id, req, image, files)` holds today's body,
`createItemTx` becomes the one-item wrapper that opens a transaction around it,
and the batch handler loops it inside a single `WithTx`. The single-item path's
behaviour must not change — `items_create_test.go` is the proof.

Every element is validated with the existing `req.validate()` **before** the
transaction opens, so a bad element is a 400 and nothing is written. The
response is 201 with the created item details in request order, built with the
existing `buildItemDetail`. The closest existing template for the whole shape is
the checklist duplicate handler (`internal/httpapi/checklists.go:218-223`).

Run `make test-postgres`: this changes how `internal/db` is used, and a loop
inside one transaction is exactly where the two dialects diverge.

**Done.** `createItemTx` split as planned into a wrapper that opens the
transaction and `createItemInStore(ctx, store, ...)` holding the body -- a
function rather than a method, since it reaches for nothing on the server the
store and its arguments do not carry. `POST /api/trips/{tripId}/items/batch`
lives in a new `internal/httpapi/items_batch.go`: validate every element, then
one `WithTx` creating each through `createItemInStore`, then the detail
response for each in request order.

Three things came out of building it that the plan did not anticipate:

- **`created_at` is not lexically sortable within a second.**
  `ListItemsByTrip` orders by `sort_order, created_at`, every location the
  single-item path creates has `sort_order` 0, so within a trip the real order
  is `created_at` -- stored with `RFC3339Nano`, which drops trailing zeros.
  `.1Z` therefore sorts *after* `.12Z` as text, which a throwaway program
  confirmed. Several rows written by one transaction in the same millisecond is
  precisely the case that would expose it, so the batch assigns `sort_order`
  explicitly from the trip's current maximum and appends. An explicit
  `sort_order` in the body still wins. The wider issue is pre-existing and goes
  to `todo.md` rather than being fixed here.
- **The cap is its own constant, not `assist.maxSuggestions`.** Deviation from
  the plan. That constant is a property of how many places are worth
  researching in one run; this one is a property of how much work one
  transaction should do. Tying them would let a change to the assistant's
  answer size silently move a general endpoint's limits.
  `maxItemsPerBatch = 20` sits comfortably above any suggest run.
- **A `javascript:` URL is accepted on a link and rendered as one.** Found
  writing a test that assumed otherwise; pre-existing, reachable from the
  single create and PATCH as much as from here, and stored XSS on a shared
  trip. Not reachable through the assistant, whose links are fetched before
  they are offered. Written up in `todo.md` under Bugs rather than folded into
  this diff, and the test now asserts the validation that does exist.

Cover photos and attachments are deliberately out: those are multipart, and a
batch of multipart bodies is a different endpoint with a different size limit
and a blob-cleanup problem on rollback. A candidate's proposed cover is a URL,
which Milestone 5's client applies through the endpoint that already fetches
one.

Verified: seven tests in `internal/httpapi/items_batch_test.go` -- every
location created with its nested location, links and tags and its generated
id; request order preserved in the list read back afterwards; a batch appending
after what was already there; nothing written when any one element is invalid
(three cases); empty and oversized lists refused; an unknown field refused, so
`readJSON` strictness survives being a level deeper; and a viewer getting 403
where a stranger gets 404. `items_create_test.go` is untouched and green, which
is the proof the single-item path did not move. `make ci` green, and
**`make test-postgres` green** -- `internal/httpapi` at 224s, exit 0.

Noted in passing: `internal/httpapi/items_create.go` was not `gofmt`-clean
before this milestone. The stray indentation was inside the literal this
milestone re-indented, so it is fixed as a side effect rather than deliberately.

## 4a. Follow-up: a link cannot carry a `javascript:` URL

Not a planned milestone. Found while writing a Milestone 4 test that assumed
the API already refused this, and fixed before Milestone 5 rather than left in
the backlog, because that milestone adds bulk writing to the same tables.

`itemRequest.validate` required only that a link URL be non-empty, and the
client rendered it into an `href` through `escapeAttr` -- which is an alias of
`escapeHtml`, so it escapes five characters and says nothing about schemes.
A `javascript:` URL was therefore storable and clickable. On a shared trip that
is stored XSS rather than a way to attack yourself: any editor can plant it,
and any member who opens that location and clicks it runs script with their own
session.

Fixed in three places, and it needs all three:

- **`validateLinkURL` in `internal/httpapi/items.go`** -- http and https only,
  scheme lowercased before comparison, a host required. Applied by
  `itemRequest.validate` (which serves create, PATCH *and* the batch endpoint)
  and separately by `handleCreateItemLink`, which writes the same column from
  its own handler and is exactly how a check applied in one place gets
  bypassed. Deliberately not `mailto` or `tel`: the field is presented as a web
  link, the assistant only proposes addresses it has fetched, and every scheme
  added here is a scheme every current and future render site must be safe for.
- **`safeHref` in a new `web/js/url.js`**, used at both render sites. The
  server check protects rows written from now on; the rows this protects are
  the ones already in somebody's database. A module rather than a per-file
  helper, breaking the convention the seven copies of `escapeHtml` set: a
  divergent copy of an entity escaper is a rendering bug, and a divergent copy
  of this is a hole.
- **Rendered inertly, not dropped.** A rejected URL becomes
  `<span class="link-list__unsafe">` -- visible, struck through, not clickable
  -- so somebody can see what is stored and go and remove it.

Two things checked rather than assumed. Markdown notes were never affected:
`internal/markdown` sanitises with bluemonday, and `[click](javascript:...)`
renders as bare text with no anchor at all. And the assistant could never have
proposed one: a link is fetched by `LinkIsLive` before it is offered, and
`internal/safefetch` refuses anything that is not public http or https.

Verified: five Go tests in `internal/httpapi/item_links_test.go` covering the
nested path, PATCH, the batch endpoint and the standalone endpoint against
eight bad URLs each -- `javascript:`, mixed case, leading whitespace, `data:`,
`vbscript:`, a scheme with no host, a relative path and empty -- plus the
http/https cases that must still work. Three Playwright tests in
`tests/ui/link-safety.spec.js`: the API refusing to store one, and both render
sites showing a *planted* one as text. The plant is an intercepted item
response rather than a real row, which is the only way to build a fixture the
server now refuses -- and is exactly the shape a pre-fix row arrives in. The
render test was checked against a reverted guard, where it fails. `make ci`
green; `locations.spec.js`, `assist.spec.js` and the new spec green together,
34 passed.

## 5. Reviewing several places at once

**Entry.** In `locations-tab.js`, the New button becomes a `renderMenu` trigger
with two rows: *Blank location* (today's navigation) and *Suggest locations*.
Rendered only when `canEdit(trip)`, as the button is today, and the suggest row
additionally only when `hasCapability("assist")` — matching how
`assist-panel.js` hides itself entirely
(`web/js/components/assist-panel.js:77-80`).

**Its own route**, `/trips/:tripId/suggest`, not an overlay on the tab. Two
reasons: six candidate cards want the full width at 324px, and `renderItemsTab`
is re-run fresh on every tab render with all of its state in closures, so an
in-tab panel would be destroyed by any tab switch.

**The page.** A prompt input, a Run button, the trip-context checkbox, and the
same status / cancel / error furniture the assist panel has. Streaming reuses
`api.postStream` (`web/js/api.js:56`) with the same event switch. The run trace
(`renderTrace`, `traceMeta`, `web/js/components/assist-panel.js:218-287`) and
the `PROGRESS_KEYS` / `STEP_KEYS` / `ERROR_KEYS` sets are lifted out of
`assist-panel.js` into a shared module rather than copied — that is the honest
reuse here, and it is a small extraction.

Each candidate renders as a card: checkbox (checked), title, category, tags,
notes excerpt, cover thumbnail with its credit line, links, and its sources.
Built with DOM calls and never `innerHTML`, for the reason
`web/js/components/assist-panel.js:359-361` already gives: these values came off
scraped pages. A footer bar carries the live count and *Add N locations*, which
posts the batch and navigates back to the locations tab.

New i18n keys in **both** `web/locales/en.json` and `de.json` —
`scripts/check_i18n.py` gates it in `make ci`.

Verification: a new `tests/ui/assist-suggest.spec.js`, skipped unless
`/api/auth/me` reports `capabilities.assist` exactly as
`tests/ui/assist.spec.js:44-45` does, driving the Milestone 3 stub script: run,
assert three cards, untick one, add, and assert the two remaining locations
exist on the tab afterwards. Plus a manual pass at 324x756.

**Done.** Landed as planned. `web/js/pages/suggest-page.js` is the new route at
`/trips/:tripId/suggest`, the New button became a two-row `renderMenu`, and the
shared parts of an assistant run moved into
`web/js/components/assist-run.js`.

- **The shared module is `assist-run.js`, not `assist-trace.js`.** It ended up
  holding the event-key sets, the trace, *and* `renderSources` -- the second
  screen needed the sources list too, and a second copy of it would have been
  the same mistake one milestone later. The panel keeps its own slot lookup,
  because where the box goes is that panel's business; only the box is shared.
- **Covers are attached after the batch, best effort.** A cover is fetched from
  a third-party server and stored as a blob, which does not belong inside a
  JSON transaction (see Milestone 4). So the page creates the locations in one
  write and then attaches each cover through `POST /trips/{id}/media/url` plus
  `PUT /items/{id}/image`, the pair the image field already uses. A cover that
  will not fetch must not undo a location the user has been told about: the
  failure costs a picture they can add by hand.

Two bugs the tests could not have caught, both found in the 324x756 pass and
both fixed before committing:

- **`.suggest` was already taken.** The member-username autocomplete owns
  `.suggest`, `.suggest__list`, `.suggest__option` and `.suggest__hint`. This
  page reused two of them, so its card list inherited `position: absolute; top:
  100%` from the autocomplete dropdown and rendered *above* the sources block,
  out of document order. Every Playwright assertion still passed -- they count
  elements and read text, neither of which layout affects. The block is now
  `suggest-page__*`.
- **`display: flex` beats `[hidden]`.** The status line and the add bar never
  hid, so "Checking the links... / Cancel" stayed on screen after the run
  finished. The tree documents this exact trap twice already
  (`.suggest__list[hidden]` and `.assist__status[hidden]` each carry a note),
  and it was reproduced anyway; the companion rules are now there, with a
  comment pointing at the other two.

Two regressions, both caught only by running the *whole* suite, and both worth
recording because neither is visible from the milestone's own tests.

**The UI suite exceeded the assistant's own rate limit.** `AssistLimiter` is
six runs a minute per client address, and every Playwright worker is
127.0.0.1 -- so the suite shares one budget. That was fine while
`assist.spec.js` was the only spec making runs; this milestone added a second,
and whichever spec happened to land later started getting 429s. It surfaced as
a flake: the first full run failed one test, and forcing the overlap
(`--repeat-each=2 --workers=4` over both assist specs) failed six. The server
log was what settled it -- the 429s are plainly there, and no amount of reading
the specs would have shown them. `scripts/with_server.sh` now sets
`CARAVEL_ASSIST_RATE_LIMIT=200` and `CARAVEL_ASSIST_MAX_CONCURRENT=8`, raised
rather than serialised: the runs are against the in-process stub, they cost
nothing, and the limiter is not what these specs are testing --
`assist_stream_test.go` tests it properly with its own `Options`. The same
stress that produced six failures now passes 20. Note this is the first thing
that could have worked at all only because of Milestone 1: before it, those two
variables did nothing.

**`sharing.spec.js` asserted
on `[data-action="new-item"]`** to prove a viewer has no way to add a location
and a promoted editor does. That selector is gone, so both assertions now name
`.locations-new-slot`. Worth noting that the same spec's *other* test drives the
member autocomplete, which is the `.suggest` namespace's real owner -- it
passing is the check that the rename did not break it.

Verified: `tests/ui/assist-suggest.spec.js` (5 tests) -- reaching the page from
the New menu, three cards from the stub script including the deliberately thin
one rendering without a cover or links, the count on the button following the
tick boxes, adding two and finding exactly those two on the tab *and* in the
API with their tags, coordinates, links and notes; nothing written to the trip
while the candidates are on screen; the dedup note when a place is already
there; the empty prompt refused; and the toolbar still not wrapping at 324px.
`make ci` green, `make docs` green, and the whole UI suite green at 201 passed.

Manually, against a dev server with the stub: the flow end to end in English
and in German. Three candidates added in one write came back with `sort_order`
0/1/2 in request order, tags split into lists, coordinates on the two that had
an address, and the cover attached to the one that proposed one. Re-running the
same prompt on the same trip then dropped all three as duplicates and said so
-- "3 Vorschläge wurden übersprungen, weil die Reise sie schon enthält" --
which is the dedup and the German plural proving themselves on real data.


**Follow-up.** The New button became a menu unconditionally, so an instance
with no assistant -- which is the *default*, since the assistant needs a model
endpoint somebody pays for -- got a menu with a single row where a plain button
used to be: a tap in front of the thing you asked for. The menu now appears
only when there is genuinely a second way in; otherwise the page renders
exactly the button it rendered before this stage.

Two notes on the fix. The gate is `hasCapability("assist")`, which is
`s.Assist != nil` and therefore `CARAVEL_LLM_URL` alone -- a configured search
backend is *not* part of it, deliberately: `newToolset` omits `web_search`
entirely when there is none, so the agent still runs, worse but working. And
`sharing.spec.js` had to stop caring which shape the control is: it asserts
that a viewer has no way to add a location and a promoted editor does, which
is true of both shapes, so it now names the trigger in either
(`NEW_LOCATION_CONTROL`) rather than counting buttons in the slot -- an open
menu contributes three.

Verified: a new test in `assist-suggest.spec.js` fakes the capability off and
asserts there is no menu trigger, that the plain button is there, and that
pressing it lands on the editor with nothing in between. `sharing.spec.js`
green in both languages, the suggest spec green, `make ci` green, and a look at
the real dev server -- which has no assistant -- showing the plain button and
an unwrapped toolbar at 324px.
**Second follow-up: the page did not look like the rest of the app.** Reported
from a screenshot on a wide window, and correct: the suggest page was the only
view in Caravel flush against the left edge, its content touching the viewport,
with none of the card grouping every other screen uses. The cause was that it
invented its own frame -- a bare `.suggest-page { max-width: 48rem }` with no
`margin: 0 auto` and no padding -- instead of the `.page` +
`.page__header` + `.editor-card` arrangement that *every* other page in
`web/js/pages` wraps itself in. Nothing was broken; it simply did not belong.

It now uses that arrangement: the ask, its status line and the run trace in one
`editor-card`, the candidates as their own cards below it, and the sources in a
card of their own that appears only when there are any. The visible field label
went with it -- the card heading names the field, and a second copy of the same
words above the input was the duplication the grouping exposed -- so the input
keeps the label as its accessible name instead.

**A test now encodes the convention, not the symptom.** None of the existing
assertions could have caught this: the page had no overflow, its tap targets
were fine, and its own spec passed, because none of them was about where the
page *sat*. `routes.spec.js` gained "every route sits in the shared page
frame", which sweeps every route and requires the content to be inside a
`.page` whose left and right margins match. Checked against the reverted
markup, where it fails with `suggest locations (...): no .page wrapper`. The
suggest route was also missing from `buildRoutes` entirely, so the whole
overflow-and-tap-target sweep had never visited it; it is in the list now.

## 6. A turn's tool calls run in parallel

`internal/assist/agent.go:424-442`, following the `checkLinks` fan-out at
`internal/assist/agent.go:849-857`. Three constraints, all from the backlog
entry and all real:

- Results go into a slice **indexed by call**, then are appended in call order.
  A `tool` message must follow its `tool_calls`, and most servers reject a
  mismatch.
- The `MaxToolCalls` ceiling is decided **before** the fan-out, not inside it.
  Today it is applied in the loop at `internal/assist/agent.go:425-435`, where
  over-ceiling calls are *answered* rather than dropped so the conversation
  stays well-formed. That property must survive.
- The prompt gains a nudge to request several page reads in one turn, since a
  turn issuing one call gains nothing from any of this.

**Take this as tidiness, and say so in the commit message.** Stage 21 Milestone
4a measured a standard deviation of ~2.9s on an ~8.9s mean, so detecting a 10%
change would need roughly 180 runs per arm. Expect a loop that reads like the
rest of the file, and possibly a second of dividend; do not report a speedup.

**Done.** The dispatch loop fans out with a `WaitGroup`, results land in a
slice indexed by call, and the `tool` messages are appended in call order
afterwards. The ceiling is computed once, before anything runs, as `allowed`.
The prompt gained one line asking for everything needed in a single turn, in
`researchRules` so both tasks get it.

One thing the plan did not anticipate, and it is the part that could have gone
wrong silently: **the event stream had to be made safe first.** `dispatch`
announces each call twice -- a progress event when it starts and a step event
when it ends -- so the moment those calls run together, two goroutines are
inside the caller's `events` handler at once. That handler is the SSE writer:
concurrent writes would interleave two events into one unparseable frame, and
`steps++` in `emit` was a plain data race besides. `emit` now holds a mutex for
the duration of the caller's handler. `toolset` needed nothing: `record` and
`Sources` were already guarded.

The visible consequence, which is correct rather than merely acceptable: with
two reads in flight the progress line names whichever started last, and the
trace lists steps in completion order. The trace is a chronological account of
what happened, so completion order is what it should say.

Verified: three new tests in `internal/assist/dispatch_test.go`, plus
`go test -race` over `internal/assist` *and* `internal/httpapi` -- the second
because that is where the SSE writer the mutex protects actually lives. Both
clean. `make ci` green, and the two assistant specs green together at 11
passed.

- **Concurrency is asserted by peak requests in flight**, not by elapsed time.
  The fixture counts how many requests are inside the handler at once and
  remembers the high-water mark; sequential dispatch can only ever reach one.
  Checked against the old code: it fails there with `peak requests in flight =
  1, want 3`. Worth recording that the *first* version of this test asserted
  only that all three requests eventually arrived, and **passed against the
  sequential implementation** -- ten seconds slower, and green. Arrival was
  never the property; simultaneity is.
- **Ordering** is asserted with three pages whose sleeps make them finish in
  reverse, so an implementation appending results as they arrive would produce
  exactly the wrong answer. Each `tool` message is matched to its call id and
  its page's marker.
- **The ceiling** with `MaxToolCalls = 1` and a turn of three calls: all three
  are answered, the first with a real result and the other two with the
  out-of-budget message, and the fixture server is asked exactly once -- so the
  ceiling stopped the fan-out rather than being noticed inside it.

No speed claim is made and none was measured. Against the stub a run is
dominated by everything except the model, so the number this could move is not
one this repository can see.

---

## Build order

0 -> 1 -> 2 -> 3 -> 4 -> 5 -> 6. Milestone 1 is independent and lands first
because it is a bug the startup log actively lies about. Milestone 2 is the
enabling refactor with no behaviour in it, deliberately its own reviewable
commit. 3 and 4 are both server-side and both testable without a browser; 5 is
the only milestone that needs both of them finished. 6 is independent of
everything after 2, and sits last because it is the one piece nobody is waiting
for.

## Files this touches

- `internal/assist/` — `types.go`, `schema.go`, `prompt.go`, `agent.go`,
  `tools.go`, `stub.go`, and their tests
- `internal/httpapi/` — `assist.go`, `router.go`, `items_create.go`, `items.go`,
  plus new tests beside `assist_stream_test.go` and `items_create_test.go`
- `cmd/caravel/main.go` and a new `cmd/caravel/main_test.go`
- `web/js/` — `pages/locations-tab.js`, a new suggest page, a shared assist
  trace module extracted from `components/assist-panel.js`, and the route table
  in `app.js`
- `web/locales/en.json` and `de.json`, `web/css/base.css`
- `tests/ui/assist-suggest.spec.js`
- `docs/` — the assistant page gains the trip-level flow; `make docs` before
  committing anything under it

## Out of scope, deliberately

- **Prompt caching.** See the decision above; stays in `todo.md`, with what
  exploring established written into the entry.
- **Google Maps interoperability, the outbound half.** Tagged **(soon)** and
  adjacent — Serper's maps endpoint is one of the search backends this stage
  touches — but it is a survey-then-decide item about place IDs and third-party
  terms, not assistant work.
- **Multi-select tag filtering**, **tag management**, **locale formatting**, and
  **the `item` -> `location` identifier sweep.** All live, all unrelated to this
  stage's subsystem.
- **Suggestions written straight to the trip.** Nothing the assistant produces
  is ever written without a person accepting it; that is the guarantee
  `internal/assist`'s package doc makes, and this stage does not weaken it.

## Verification

Every milestone ends with `make ci` green. Milestone 4 additionally runs
`make test-postgres`. Milestone 5 ends with a Playwright pass and a manual look
at 324x756.

**End-to-end proof of the whole stage.** On a trip with three locations already
on it, and `CARAVEL_ASSIST_MAX_CONCURRENT=1` set: open the locations tab, open
the New menu, choose *Suggest locations*, ask for things to do in Reykjavik, and
watch the run stream its steps. Get several candidates, none of which is a place
already on the trip. Untick one, add the rest, and land back on the locations
tab with exactly the ticked ones added — each with its coordinates on the map
tab, its cover image, and no dead link. Start a second run in another tab while
the first is going, and be refused with the busy message rather than served,
which is the Milestone 1 fix visible from the outside.

## Workflow

One milestone at a time, in the order above. For each: implement, verify
(`make ci` plus the milestone's own proof), add a **Done.** paragraph to that
milestone's section in `plans/stage-27.md` describing what actually landed —
including any deviation from this plan — and how it was verified, reconcile
`plans/todo.md` in both directions, commit (one commit per milestone; a same-day
follow-up fix gets its own "... follow-up: ..." commit), make sure `make dev` is
running, then stop and hand back control. Do not start the next milestone until
told to continue; feedback given at a checkpoint is fixed and re-verified before
moving on.
