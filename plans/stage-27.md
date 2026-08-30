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
