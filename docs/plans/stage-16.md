# Stage 16 — AI-assisted metadata for locations

## Context

Filling in a location by hand is the most tedious thing Caravel asks of you.
You know the hotel's name; getting its type, address, official website and a
sentence of notes into the form means four tabs and a lot of copy-paste. The
data exists on the open web, and the form already knows exactly which fields
are empty.

`docs/plans/todo.md` carries a designed-out entry for this ("AI-assisted
metadata for locations", tagged **(soon)**, agreed in the Stage 15 backlog
review). This stage builds it. The design decisions in that entry are taken as
settled except where this plan says otherwise; what follows is the
implementation plan against the code as it actually stands today.

**The feature.** In the location editor, a "Search via AI" control either
enriches the location you are editing or takes a free prompt ("a cheap hostel
near Hallgrímskirkja") and builds one from scratch. It proposes `category`,
`type`, `notes`, an address and links; you accept or reject **per field**,
including for fields that already have content — those show a visible
before/after. The sources the agent used are listed with the proposal and
stored nowhere. Nothing is written until you press Save, as today.

**Off by default and invisible when unconfigured.** Configured through env vars
only, never the database. This is exactly the `CARAVEL_GEOCODER_URL` precedent:
unset means the endpoint answers 501 and the client never renders the control.

### What exploring the code changed about the plan

- **The geocode logic has to move.** "Never let the model produce coordinates"
  means `internal/assist` resolves an address through Nominatim. But that logic
  is a `*Server` method — `(*Server).geocode` in
  `internal/httpapi/geocode.go:73` — and `assist` cannot import `httpapi`. It
  gets lifted into `internal/geocode` first, with `handleGeocode` becoming a
  thin caller. Milestone 3.
- **The capability rail already exists.** `userResponse.Geocoding`
  (`internal/httpapi/auth.go:27`) is read as `getCurrentUser()?.geocoding` at
  `location-editor-page.js:428`. An `assist` bool rides the same rail — flat
  field, not a nested `capabilities` object; two flags do not justify a
  client-visible reshape.
- **The propose-and-accept idiom already exists.** `bindPlaceSearch`
  (`location-editor-page.js:419-497`) is search → status line → list of results
  → click applies into the form. The AI panel is a richer sibling, and copies
  its shape (hidden unless the capability is on, never fires per keystroke,
  applies into `draft` only).
- **No migration.** Every proposed field maps onto an existing column
  (`items.category/type/title/notes`, `item_locations.address/lat/lng`,
  `item_links`). Nothing is persisted about the run itself — the backlog says
  sources are stored nowhere, so there is no new table.
- **No new SQL either.** The `type` vocabulary sent to the model is the set of
  distinct `Type` values on the trip's own items, computed in Go from the
  `ListItemsByTrip` result the handler already loads.
- **SSE over `fetch`, not `EventSource`.** `EventSource` cannot POST and cannot
  send a JSON body. The endpoint is a POST returning `text/event-stream`,
  consumed by `fetch()` plus a small line parser; cancel is an
  `AbortController`, which the server sees as `r.Context()` being done.

### One backlog decision this plan reverses

`todo.md` rejects `ddgs` as a search provider, partly on the grounds that it is
Python and "a Go binary that ships as one static file would grow a Python
runtime." **That premise is wrong.** `ddgs` ships a built-in FastAPI server
(`pip install -U ddgs[api]`, then `ddgs api -d --host 127.0.0.1 --port 4479`,
exposing `/search/text`, `/extract` and `/health`). Python therefore never
enters Caravel's process — it is a separate service on localhost, exactly the
SearXNG model, and in the coming Docker stage it is a second service in
`docker-compose.yml` rather than a second runtime in our image.

Two further corrections: it is no longer a DuckDuckGo scraper (the name is
legacy — text search aggregates bing, brave, duckduckgo, google, mojeek,
startpage, yandex, yahoo and wikipedia, with per-query backend selection and
fallback), and it also offers an MCP server mode, which we ignore because MCP
is ruled out for this stage.

What still holds against it, and belongs in the README rather than in a
rejection: it is scraping, so backends break when sites change markup
(multi-backend fallback mitigates this, which is why it is supported but never
the default); datacenter-IP rate limiting makes it much better suited to a home
server than a VPS; and scraping Google and Bing is against their terms of
service, which is negligible practical risk for a personal self-hosted tool but
should be stated plainly since we ship support for it.

---

## Search providers

Five, all behind one `Searcher` interface returning a normalized
`{title, url, snippet}` — the lowest common denominator every one of them
returns. Each real implementation is ~60–80 lines of HTTP+JSON. The provider is
chosen by `CARAVEL_SEARCH_PROVIDER`, and the three config vars from Milestone 1
cover all five with no reshaping.

| Provider | Key? | Runs where | Notes |
| --- | --- | --- | --- |
| `stub` | no | in-process | CI and Playwright only; never a real answer |
| Ollama Cloud | yes | hosted | free tier; same account and key as the LLM half |
| `ddgs` | no | your own host | `pip install ddgs[api]`; multi-backend metasearch, scraping |
| SearXNG | no | your own host | needs `json` added to `search.formats` in `settings.yml` |
| Serper | yes | hosted | real Google results, cheapest commercial SERP API |

**No documented default.** The README presents all five as equals — but as a
decision table (needs a key? self-hosted? scraping?), not five flat sections, so
a first-time operator is not left with a bare five-way choice.

`ddgs` and SearXNG overlap heavily — both self-hosted, keyless, metasearch,
JSON over HTTP. Both are supported anyway because they are cheap and serve two
existing setups ("I already run SearXNG" versus "I want the quickest thing"),
but they are not distinct niches and the docs should not pretend otherwise.

**Deliberately not implemented**, and listed in the README as ~80 lines each for
anyone who wants them: SerpAPI (same shape as Serper, pricier, multi-engine we
do not need), Brave Search API (good independent index, but occupies Serper's
slot), Tavily and Exa (agent-oriented, return cleaned page content, so adopting
one would partly subsume `fetch_page` and raise a question this stage does not
need to answer).

---

## 0. Land the plan

Commit this as `docs/plans/stage-16.md` before any code. In the same commit,
update `docs/plans/todo.md`: leave the "AI-assisted metadata for locations"
entry in place for now (Milestone 9 deletes it, when it is actually built), but
rewrite the paragraph rejecting `ddgs` — the Python objection is factually
wrong, and leaving it standing would have a future reader re-derive this same
finding.

## 1. Config, capability, and the disabled seam

The whole feature switched off, end to end, before any of it exists.

- `internal/config/config.go` gains `LLMURL`, `LLMKey`, `LLMModel`,
  `SearchProvider`, `SearchKey`, `SearchURL`, from `CARAVEL_LLM_URL` /
  `_KEY` / `_MODEL` and `CARAVEL_SEARCH_PROVIDER` / `_KEY` / `_URL`. Validate
  the combination the way `DBDriver` is validated: an `LLMURL` with no
  `LLMModel` is a startup error, not a runtime surprise, and an unknown
  `SearchProvider` names the five valid values.
- New package `internal/assist` with the narrow interface the rest of the stage
  fills in:

  ```go
  type Assistant interface {
      // Propose runs the agent and streams progress on events, returning
      // the validated proposal or an error.
      Propose(ctx context.Context, req Request, events func(Event)) (*Proposal, error)
  }
  ```

  plus `Request`, `Proposal`, `Event` and `Field` types. `assist.New(cfg)`
  returns `nil, nil` when unconfigured — a nil `Assistant` is the off switch,
  and there is exactly one.
- `httpapi.Options`/`Server` gain `Assist assist.Assistant`, wired in
  `cmd/caravel`.
- `userResponse` gains `Assist bool` (`s.Assist != nil`), next to `Geocoding`,
  with a comment pointing at the same reasoning.
- Route registered now, answering 501 when `s.Assist == nil` — the
  `handleGeocode` pattern (`geocode.go:47-55`), including its "the client
  already knows and should not be asking" comment.

**Verify.** `go test`: config validation table test; `/auth/me` reports
`assist:false` on the default test server and `true` when `Options.Assist` is
set; the endpoint answers 501 unconfigured and 401 unauthenticated.

**Done.** Landed as planned, with three additions the plan did not specify.

`internal/config` gained the six env vars plus `AssistEnabled()`, `LLMStub` and
`SearchProviders`. Validation turned out to be worth more than the plan credited
it for, so it covers five half-configurations rather than one: URL without
model, model without URL, an unknown provider (the message names the five valid
values), a search provider with no assistant to use it, and `ddgs`/`searxng`
without a `CARAVEL_SEARCH_URL` they cannot function without. All five refuse at
startup rather than at first use -- the failure mode being avoided is an
instance where the capability reads as on, so the control renders, and the
feature breaks only when somebody presses it.

`internal/assist` is the interface, `Options`, the plain-data `Request` /
`Proposal` / `Field` / `Event` / `Location` types, and `New` returning
`(nil, nil)` when `LLMURL` is empty. `Agent.Propose` returns `ErrNotImplemented`
for now, which is what lets the seam be wired and tested end to end before any
machinery exists.

Three additions beyond the plan:

1. **`AssistLimiter` landed here rather than in Milestone 6.** Adding a third
   limiter meant touching `NewServer` and `sweepLimitersPeriodically`, and doing
   that in the milestone that already touches both is cheaper than coming back.
   Set to 6/minute/IP, far tighter than geocode's 20 -- the budget being
   protected is different in kind, since one run is a multi-turn LLM
   conversation the instance owner pays for by the token.
2. **The capability check runs before the trip lookup**, with a test pinning it
   (`TestAssistDisabledAnswersBeforeAuthorizing`). The other order would let a
   disabled instance leak whether a trip id exists to anyone who asks.
3. **The startup banner reports `assist=true|false`**, so "is this instance
   configured" is answerable from the log without a session.

Note that the endpoint answers 501 in two distinguishable situations -- "not
enabled on this server" before authorization, "not implemented yet" after it --
which is what lets the authorization tests assert they got all the way through.
The second message disappears in Milestone 6.

**Verified.** `make ci` green, including a ten-case config table test and five
new httpapi tests (501 unconfigured, 501-before-404, 401 unauthenticated,
404/403/501 for stranger/viewer/owner, and `/auth/me` both ways). Then live
against a real server: unconfigured, `/auth/me` reports `assist:false` and the
endpoint answers 501 "not enabled"; with `CARAVEL_LLM_URL=stub
CARAVEL_LLM_MODEL=stub`, the banner logs `assist=true`, `/auth/me` reports
`assist:true`, an owner reaches the handler body, an anonymous caller gets 401
and a non-member gets 404. Each of the five half-configurations was started by
hand and refused with a message naming the missing variable.

## 2. The LLM client, and the stub provider

`internal/assist/provider.go` — an OpenAI-compatible chat-completions client:
messages, tool definitions, tool-call round trips, and `response_format:
{type:"json_schema", strict:true}` for the final answer. The schema is a
hand-written literal next to the Go struct; the fallback path for proxies that
only support `json_object` is prompt-plus-validate-plus-retry-once. Usage
tokens are read off each response so the budget in Milestone 4 has something to
count.

`internal/assist/stub.go` — the sentinel `CARAVEL_LLM_URL=stub` selects an
in-process fake provider that returns a scripted sequence of *tool calls*
followed by a final structured answer. The point is that only the outbound HTTP
call is canned: the real agent loop, the real validation, the real HEAD checks
and the real SSE transport all run. That is what makes Milestone 9 worth
anything.

**Verify.** `go test` against an `httptest.Server` speaking the OpenAI shape:
a plain completion, a tool-call round trip, a `json_schema` response, and the
`json_object` fallback including the retry. A test that the stub provider
drives a multi-step exchange.

**Done.** Landed as planned, in four files.

`provider.go` is the OpenAI-compatible client. `CARAVEL_LLM_URL` is the *full*
chat-completions endpoint rather than a base URL, matching
`CARAVEL_GEOCODER_URL`. `completeJSON` is where the plan's "real work" lives:
it asks for structured output, decodes, and on a wrong shape sends the answer
back with the complaint attached and tries once more -- once, not in a loop,
since a model that cannot produce the shape twice will not produce it on the
fifth attempt and every attempt is billed. Usage is summed across attempts, so
a retry is not free in the budget Milestone 4 will enforce.

`schema.go` is the contract with the model: `modelProposal` plus a hand-written
strict JSON Schema literal beside it. Deliberately *not* `Proposal` -- it has no
coordinates (resolved from the address, never returned), no current values and
no sources (recorded from the tool calls actually made, not from what the model
claims it read).

`stub.go` is the in-process fake, and `tools.go` holds the three tool names it
scripts against, which Milestone 3 implements.

Four things worth recording, three of them decisions the plan did not make:

1. **The `json_schema` downgrade is sticky.** A server that rejects the format
   sets a flag, so the fallback costs one failed request per process rather
   than one per turn.
2. **The "is this a format complaint?" match is deliberately narrow** -- a 400
   or 422 that mentions `response_format`/`json_schema` *and* a
   support-flavoured word. Being wrong is cheap in one direction and expensive
   in the other: a false positive costs one extra request that fails the same
   way, while a false negative would hide real errors behind a confusing retry.
   `TestProviderDoesNotFallBackOnUnrelatedBadRequest` pins the expensive
   direction.
3. **Code fences are tolerated, unknown fields ignored.** Models emit fenced
   JSON even when told not to, and both would otherwise burn a paid retry on a
   formatting habit. Note this is the opposite of `readJSON` in the HTTP layer,
   which rejects unknown fields -- there they mean a bug in our own client.
4. **`errors.As`, not a type assertion**, for detecting the format error. The
   original hand-rolled helper worked but would have silently stopped working
   the first time anyone wrapped the error, which is the kind of latent
   breakage that surfaces as "the fallback just doesn't happen any more".

The stub's default script is three turns -- search, fetch, answer -- rather
than one. The number of turns is the point: a single-turn script would never
exercise the loop, the dispatcher or the history-echoing rule that real servers
enforce, so the first real provider in Milestone 5 would be the first time any
of it ran.

**Verified.** `make ci` green, plus `go test -race`. 24 tests in
`internal/assist`: plain completion, keyed and keyless auth headers, a two-turn
tool round trip asserting the `tool_call_id` survives, `json_schema` with
`strict` actually set on the wire, the full `json_object` fallback including
that the downgrade sticks, an unrelated 400 *not* triggering it, the
wrong-shape retry carrying its complaint and counting both attempts' tokens,
giving up after exactly two, code fences and unknown fields costing no retry, a
non-JSON body reported as such rather than as a syntax error, context
cancellation, the stub's three-turn script, its answer decoding against the
schema and carrying no coordinates, script exhaustion erroring rather than
panicking, concurrent use under `-race`, and `New` selecting stub versus HTTP
client versus nil.

No app-level behaviour changed and none was expected: `Propose` still returns
`ErrNotImplemented`, so the endpoint still answers 501. The `httptest.Server`
suite is the behavioural evidence for this milestone -- there is nothing to see
in a browser until Milestone 4 makes the loop real.

## 3. Tools: the geocoder lift, page fetch, and the Searcher seam

Three tools the model may call, all native Go, dispatched through one small
`map[string]toolFunc` seam — narrow enough that an MCP-backed tool could be
added later without a refactor, as the backlog asks. **No real search provider
lands here**: the interface plus a stub is all anything downstream needs, and
real providers are leaf work with nothing depending on them.

- **Lift `internal/geocode`.** `(*Server).geocode` and `nominatimResult` move
  into a new `internal/geocode` package as `geocode.Client`;
  `handleGeocode` becomes a caller and keeps its own status-code behaviour.
  `geocode_test.go` should need only import and constructor edits — if it needs
  more, the lift changed behaviour and that is the bug.
- **`internal/assist/fetch.go`** — `fetch_page`, with a guard that is the
  security-relevant part of this milestone: http/https only; DNS-resolved
  address rejected if loopback, private, link-local, ULA or unspecified;
  redirects re-checked, not followed blindly; 512 KB body cap; 8 s timeout;
  HTML reduced to text before it reaches the model. The same guard backs the
  parallel HEAD checks in Milestone 4.
- **`internal/assist/search.go`** — the `Searcher` interface and the `stub`
  implementation, selected by `CARAVEL_SEARCH_PROVIDER=stub`.

**Verify.** `go test`: geocode suite still green after the lift; the SSRF guard
as a table test over `http://127.0.0.1`, `http://10.0.0.1`, `http://[::1]`,
`file://`, a public redirect into a private address, and an oversized body;
the stub searcher satisfying the interface.

**Done.** All three parts landed, and the lift went exactly as the plan
predicted: `internal/httpapi/geocode_test.go` needed only import and
constructor edits and stayed green, which is the evidence that behaviour did
not change.

**The geocoder lift** went slightly further than "move the function". Rather
than keeping `GeocoderURL string` on the Server and building a client per
request, `Server.Geocoder` is now a `*geocode.Client` and `geocode.New("")`
returns nil -- so nil is the off switch and `s.Geocoder != nil` is the
capability check, the same shape `Assist` uses. `cmd/caravel` builds one client
and shares it between the HTTP handler and the assistant, so there is one
connection pool and one place that knows the User-Agent. `geocode.Search` on a
nil client returns `ErrNotConfigured` rather than panicking, so a missed check
fails where it happens.

**`fetch_page` has three layers of guard, not one.** The plan called for a
pre-flight check and a redirect check; a third was added at dial time. The
pre-flight check resolves the name and rejects on *every* A/AAAA record rather
than the first, the `CheckRedirect` hook re-checks each hop, and
`DialContext` checks the address actually being connected to -- which is the
only one of the three that closes DNS rebinding, where a name passes the
lookup and then resolves to something private a moment later. All three share
one `guardIP` so they cannot drift apart.

**The dispatcher never returns an error**, which is the load-bearing decision
in `tools.go`. A tool failure is turned into text for the model -- "That did
not work: ..." -- because a 404 or an unresolvable address should make it try
something else, exactly as a person would. Aborting instead would turn every
dead link on the web into a failed enrichment. The genuinely fatal cases stay
the agent's job in Milestone 4.

Four smaller decisions:

1. **Only tools that can work are offered.** Describing web search to a model
   with no search backend produces a run that calls it, gets an error and
   wastes a turn discovering what the config already knew.
2. **The geocode tool returns formatted addresses with no coordinates.** The
   model has no use for lat/lng and showing them invites it to copy one into
   the answer, which is the exact failure the design forbids. A test asserts
   no coordinate appears in the tool output.
3. **`Fetch` is split into the guard and `fetchUnguarded`** for a testing
   reason: `httptest` servers listen on loopback, which the guard exists to
   refuse. Without the split, either the guard or everything past it goes
   untested because the other is in the way.
4. **The ULA check runs before `IsPrivate`.** Both refuse `fd00::1`, but
   `IsPrivate` answered first with the vaguer reason. Reordering means the
   refusal names the kind of address, and the policy does not quietly depend on
   one stdlib helper's IPv6 behaviour.

**One new direct dependency**: `golang.org/x/net`, for `html.Parse` in the text
extractor. It was already in the module graph as an indirect dependency so
nothing new is downloaded, but it is now direct. The alternative was
regex-based tag stripping, which is fragile enough that the parser is worth
the line in `go.mod`.

**Verified.** `make ci` green and `go test -race ./internal/...` clean. 68
tests across `internal/assist` and `internal/geocode`. The SSRF table covers 13
targets -- loopback by literal, by name and over IPv6; all three RFC1918
ranges; `169.254.169.254` specifically, since cloud metadata is the most
valuable thing an SSRF reaches; link-local and unique-local IPv6; the
unspecified address; `file://` and `gopher://`; and a URL with no host -- plus
a live redirect from a public-looking server into private space, and the
dial-time guard on its own. Past the guard: content-type refusal, the body cap
against a server streaming 4MB, an empty page, an error status, and script and
style content actually being stripped. The geocode suite in `internal/httpapi`
passed unchanged but for imports.

## 4. The agent loop and its guard rails

`internal/assist/agent.go`. Open-ended — model calls tools until it emits the
final structured proposal — which makes the rails a hard requirement, not a
nicety:

- a wall-clock deadline on the whole run (`context.WithTimeout`),
- a token budget summed from provider usage, checked between turns,
- an iteration ceiling and a tool-call ceiling as the backstop for a model that
  loops without spending much,
- `ctx.Err()` honoured at every turn boundary, so the client's abort really
  stops the run rather than orphaning it.

Then the validation the backlog is emphatic about:

- **Coordinates are never taken from the model.** It returns a place name and
  address string; `internal/geocode` resolves them. A plausible lat/lng 40 km
  out is the one failure mode with no visible tell.
- **`category` is validated against the three-value enum**, not trusted.
- **`type` is free text**, so the distinct values already in use on the trip go
  out as a vocabulary with an instruction to reuse one where it fits.
- **Every proposed link gets a HEAD check in parallel**; dead ones are dropped
  before the proposal is built. Hallucinated URLs are the classic failure.
- Prompt-injection posture, stated in the package comment: the agent reads web
  pages, so a page can carry instructions. The blast radius is bounded by
  design — no tool has a side effect, and the output is a validated structure
  the user confirms field by field.

**Verify.** `go test` driving the stub provider through a full run: a proposal
whose model-supplied coordinates are discarded in favour of the geocoded ones,
an invalid `category` rejected, a dead link dropped, deadline exhaustion
returning a partial-progress error rather than hanging, and cancellation
returning promptly.

**Done.** The loop, the rails and the three enforced validations all landed.
`Propose` is real; `ErrNotImplemented` is gone.

**The loop is two phases, which the plan did not specify.** A plain
tool-calling conversation with no response format, and then -- once a turn
arrives with no tool calls -- one final turn that asks for the structured
answer. Offering tools *and* a strict `json_schema` on every turn is the
obvious shortcut and a compatibility minefield: several OpenAI-compatible
servers constrain all output to the schema when one is set, which makes tool
calls impossible, and they differ on whether it applies to tool arguments too.
The separation costs one extra billed turn per run and buys behaviour that does
not depend on which server the operator pointed us at. The stub script grew a
fourth turn to match.

**Four rails, not three.** Wall-clock deadline, token budget, turn ceiling and
tool-call ceiling. The turn ceiling is not redundant with the budget: a server
that reports no usage makes the budget vacuous, and something still has to end
the run. Hitting the tool-call ceiling *answers* the outstanding calls with
"no more tool calls are available" rather than dropping them -- a tool call
with no result leaves the conversation malformed, and telling the model it is
out of budget lets it answer with what it has.

**Cancellation is passed through as `context.Canceled`, not dressed up.** The
user pressing Cancel is not an error condition, and Milestone 6 needs to tell
it apart from a timeout to decide whether to report anything at all.

Decisions inside `buildProposal`:

1. **An invalid category is dropped, not corrected.** Guessing which of three
   the model meant is how a hotel becomes a ferry terminal. One bad field does
   not discard the rest of a good run.
2. **An empty proposal is silence, not a request to clear the field.** The
   feature never offers to delete what somebody wrote.
3. **The title is left alone when enriching something already named.** The user
   chose that name; renaming is not enrichment, and proposing a near-identical
   title every run trains people to click past the review. Prompt mode still
   proposes one.
4. **Link liveness falls back to GET on 405 and 501.** A meaningful minority of
   servers refuse HEAD and serve the page fine, and treating those as dead
   would silently drop working links from a whole class of sites.

**The fetcher's address policy became explicit** rather than the ad-hoc client
swapping Milestone 3 used in tests. `newFetcherWithPolicy(true)` relaxes
*only* the address check -- the scheme check, redirect re-check and size and
time caps all still apply -- and it is an unexported constructor with no path
from an operator's environment, so there is no config value that can weaken the
guard in production. A test pins that the relaxed policy still refuses
`file://`.

Also fixed a stale comment in `internal/db/domain.go`: `Item.Category` was
documented as `"location" | "stay" | "transport"`, which migration `0002`
made wrong. It now matches the CHECK constraint, and
`TestValidCategoriesMatchTheSchema` pins this package's copy of the list
against it.

**Verified.** `make ci` green, `go test -race` clean, 91 tests in
`internal/assist`. Coverage of the rails: the turn ceiling against a model that
calls a tool forever, the tool-call ceiling against one turn requesting 40
calls, the budget against a provider reporting it all spent at once,
cancellation mid-tool-call, and an already-expired deadline. Coverage of the
validations: coordinates taken from the geocoder with the address tried before
the place name, no coordinates at all without a geocoder, four invalid
categories dropped while valid fields survive, case normalisation, dead links
dropped, GET-only links kept, duplicates of existing links suppressed, unchanged
and empty fields not proposed, and overwriting fields carrying their current
value for the before/after.

Then one run outside the test harness, against the **real** Nominatim: the stub
answer carries no coordinates, and the proposal came back with
`64.1453191, -21.9195604` resolved from "Skulagata 28, 101 Reykjavik, Iceland"
-- which is Kex Hostel. That is the guarantee the unit tests only exercise
against a fake upstream.

**One finding for Milestone 7.** That same run returned `Links: null` and
`Sources: []`, correctly: the stub's URLs point at `example.invalid`, which
does not resolve, so the liveness check drops the link and the failed fetch
records no source. Both behaviours are right, but it means **the stub cannot
currently produce a live link or a source**, because anything it could reach
locally is loopback and the guard refuses that. So the UI milestone has nothing
to develop the links list and the sources list against. Milestone 7 has to
decide between three options, none of them obviously best: accept that those
two lists are only exercised by unit tests and a manual real-provider run;
point the stub at a public URL and give up on CI having no network; or let
`CARAVEL_LLM_URL=stub` relax the fetcher's address policy the way the tests do,
which is a config value weakening a security control and wants thinking about
rather than doing quietly.

## 5. The first real provider: Ollama Cloud

One provider, ~80 lines — and the smallest milestone in the stage by line count
but not by value. Everything so far has been tuned against a script, and real
search results are messy in ways a script is not. Landing one real provider
*here*, rather than at the end, means the prompt and the guard rails meet real
data before the SSE transport and the UI are built on top of them; discovering a
needed prompt rewrite in Milestone 9 would mean editing Milestone 4's code long
after its checkpoint.

Ollama Cloud specifically, because the key is already in hand and there is no
local service to stand up first.

**Stop and ask for the credentials before starting this milestone** — an Ollama
Cloud API key, and the LLM endpoint, key and model name to point
`CARAVEL_LLM_URL` / `_KEY` / `_MODEL` at. See "Credentials" under Workflow: do
not guess, do not go hunting in the environment, and do not quietly fall back
to the stub and call the milestone done.

**Verify.** `go test` against an `httptest.Server` for the response mapping,
then — the actual point — a **live run from a Go test or a small `cmd` harness
against the real endpoint and a real model**, on one real place, confirming the
notes are plausible, the links resolve, and the coordinates came from Nominatim
rather than the model. This is the first honest answer to "is this feature any
good", and it is manual by nature.

## 6. The SSE endpoint

`POST /api/trips/{tripId}/assist/location`, `text/event-stream`.

- Trip-scoped and behind `authorizeTrip(..., db.RoleEditor, ...)`
  (`internal/httpapi/authz.go`) — a viewer cannot write the result anyway, and
  the request may carry trip context outward.
- Its own limiter alongside `LoginLimiter`/`GeocodeLimiter` via
  `newRateLimiter` (`security.go:50`), added to `sweepLimitersPeriodically`.
  Tighter than geocode's 20/min — a run costs
  real money. No per-user limit: the instance owner configured the key.
- Request body: mode (`enrich` | `prompt`), the free prompt, the location's own
  current metadata, and a `include_trip_context` bool — the checkbox the
  backlog asks for, defaulting on, that suppresses trip title and dates for
  anyone who would rather not send them. The user's locale rides along so notes
  come back in it.
- Events: `progress` (a translatable key plus params, not an English sentence —
  the server must not be the one writing UI copy), `proposal`, `error`.
  `http.Flusher` after each.

**Verify.** `go test` with the stub: the response is `text/event-stream`, the
event sequence is progress-then-proposal, a viewer gets 403 and a stranger 404,
the limiter returns 429 on the N+1th call, and aborting the request context
mid-run stops the agent.

## 7. The editor UI

`web/js/pages/location-editor-page.js` and `web/js/components/location-form.js`.

- A "Search via AI" control in the Basic info card, `hidden` unless
  `getCurrentUser()?.assist` — the `bindPlaceSearch` guard, verbatim in shape.
  It offers the free prompt; with a location already filled in, an "enrich
  this location" affordance sits beside it.
- While running: the progress line (rendering the streamed keys through `t()`),
  a Cancel button wired to `AbortController`. A run can take 30+ seconds, and
  without this it reads as a hung spinner.
- The proposal panel — the real work of this milestone. One row per proposed
  field with an Accept/Reject pair. A field that is currently **empty** shows
  the proposed value; a field with **existing content** shows a visible
  before/after, so accepting can never silently destroy something you wrote.
  Links are per-link. Accepting writes into `draft` only; Save is unchanged.
  Sources are listed under the panel and go nowhere else.
- New i18n keys in **both** `web/locales/en.json` and `de.json` — flat keys, an
  `assist.*` group. `scripts/check_i18n.py` enforces parity and is easy to
  forget.

**Verify.** Against `make dev` with the stub configured: the control is absent
unconfigured and present configured; a run streams progress; Cancel stops it;
accepting one field and rejecting another leaves the other untouched; an
overwrite shows its before/after. Assertions over screenshots — DOM counts,
accessible names, field values. Mobile pass at 324×756.

## 8. The remaining three providers

`ddgs`, SearXNG and Serper, ~60–80 lines each behind the interface Milestone 3
defined. Landing them together, and late, is deliberate: adding three
implementations with **zero changes to the agent loop** is itself the proof the
interface did not leak provider details, and by now the UI exists, so each one
is verified by running the real feature in a browser rather than against an
`httptest.Server`.

`ddgs` and SearXNG both need a local service; the README section written in
Milestone 9 gets its content from actually standing each of them up here.

**Stop and ask before starting this milestone.** Serper needs an API key. `ddgs`
and SearXNG need no key but do need a running service — ask whether to stand
them up locally (`pip install ddgs[api]`; a SearXNG container) or whether you
already have one to point at. If a key is not forthcoming for Serper, build it
against an `httptest.Server` and say plainly in the "Done." paragraph that the
live half is unverified, rather than reporting the milestone as fully done.

**Verify.** Per provider: `go test` against an `httptest.Server` for the
response mapping, plus one real enrichment run in the browser. For `ddgs`,
confirm the `/search/text` request and response schema against the running
server's own `/docs` — this plan has not verified it. For SearXNG, confirm the
`settings.yml` change the README will tell people to make.

## 9. Playwright coverage, and the docs

- `tests/ui/assist.spec.js`, driving the stub end to end. It mutates a
  location, so it follows the isolation pattern `files.spec.js` established
  (Stage 11 Milestone 5): create its own trip in `beforeEach`, delete it in
  `afterEach`.
- The stub needs the CI server started with it configured — a small addition to
  the `ui` job's env in `.github/workflows/ci.yml`. Worth noting that this makes
  the stub the first thing in the suite that depends on server configuration
  rather than on seed data.
- `README.md`: the env vars, what unset means, that the key never touches the
  database so it never ends up in a backup, and the five-provider decision table
  from the Search providers section above — no default, with the honest caveats
  on `ddgs` (scraping, datacenter rate limits, terms of service) alongside it.
- `docs/plans/todo.md`: delete the "AI-assisted metadata for locations" entry —
  it is now built. Keep "AI trip-level suggestions", and rewrite its "blocked
  on the single-location version existing first" line, which is no longer true.
  Add whatever this stage defers.

---

## Build order

1 → 9, strictly. Everything is backend until 7, so the risky half lands and is
tested before any UI depends on it. Two orderings are load-bearing rather than
incidental:

- **Milestone 3's geocoder lift is early on purpose**, because it is the only
  thing in the stage that touches existing green code, and a regression is
  cheapest to spot when little else has changed.
- **Milestone 5 sits where it does on purpose**, so the agent loop meets real
  search results one milestone after it is written, not four.

## Out of scope, deliberately

- **Auto-filling a cover image.** The workable route (a Wikipedia article title
  → the Wikimedia API's lead image, with a known licence) needs a Wikimedia
  client, an attribution column on `media_assets` and UI for the credit. A
  follow-on, as the backlog says.
- **Trip-level suggestions** ("suggest things to do in Reykjavík"). Needs a
  multi-result review UI and an add-N-in-one-transaction endpoint. Stays in the
  backlog; this stage is what unblocks it.
- **MCP.** Decided against in the backlog review and not revisited: three tools,
  one already implemented. Note that `ddgs` does ship an MCP server mode, which
  we ignore — its plain HTTP API is what we call.
- **A sidecar process for Caravel itself.** A package boundary buys the same
  clean seam without a second image, a second config and a new class of
  failure. (`ddgs` and SearXNG are separate services, but they are the
  operator's to run, not ours to ship.)
- **SerpAPI, Brave, Tavily, Exa** — see the Search providers section.

## Workflow

The repo's standard loop, per `CLAUDE.md`: implement → verify (`make ci` green,
plus a manual/Playwright pass proving behaviour actually changed) → write a
"**Done.**" paragraph into `docs/plans/stage-16.md` and update
`docs/plans/todo.md` in both directions → one commit per milestone → leave
`make dev` running → **stop and hand back control**, and do not start the next
milestone until told to.

### Credentials

This is the first stage that needs secrets from outside the repo, so the rule is
explicit: **ask for every API key, endpoint and model name at the start of the
milestone that needs it**, and wait. Specifically, Milestone 5 needs the LLM
endpoint, key and model plus an Ollama Cloud search key; Milestone 8 needs a
Serper key and a decision on standing up `ddgs` and SearXNG locally; Milestone 9
re-uses all of them for the final four-provider verification pass.

Three things not to do, each of which turns a missing key into a silent
problem:

- **Do not invent or guess** a key, endpoint or model name.
- **Do not go looking** for credentials in the environment, the shell history,
  dotfiles or anywhere else on the machine. If they are not in the plan or given
  in conversation, ask.
- **Do not quietly fall back to the stub** and report the milestone done. The
  stub proves the plumbing, never the feature; a milestone whose live half did
  not run says so in its "Done." paragraph.

Keys go in the environment for a `make dev` run, never into a file in the repo,
never into `docs/plans/`, and never into a commit message — the whole reason the
config is env-var-only is to keep the key out of the database and therefore out
of every backup, and checking it into git would defeat that just as thoroughly.

## Verification

Per milestone as listed above. At the end of the stage, end to end:

- `make ci` green.
- `make test-ui` green, including the new `assist.spec.js`.
- With no `CARAVEL_LLM_URL`: the control does not render, `/auth/me` reports
  `assist:false`, and the endpoint answers 501.
- With `CARAVEL_LLM_URL=stub`: a full run from prompt to accepted fields to a
  saved location, in the browser, at 324×756 as well as desktop.
- **One real enrichment per configured search provider**, in the browser,
  against a real model — four runs. Confirming each time that the coordinates
  came from Nominatim and not the model, and that the listed sources are real
  pages. Manual by nature, and the only check that the thing is actually useful.
