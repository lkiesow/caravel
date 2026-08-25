# Stage 21 — The assistant: trustworthy, legible, faster

## Context

Stage 16 built the AI assistant: an agent loop over an OpenAI-compatible
provider, three tools, four search backends, seven guard rails, an SSE
transport and a per-field review UI. It works. Living with it for a stage
has produced three complaints, all in `plans/notes.md`, and all about the
same underlying thing — **you cannot see what it is doing**:

1. **A counter that never empties.** Accept every suggestion, one at a time
   or with "Accept all", and the bar sticks on "1 suggestion" forever with
   "Accept all" and "Dismiss all" still on screen.
2. **It is slow, and it regularly gives up** with "The assistant took too
   long and was stopped. Try a narrower description."
3. **Nothing explains either.** `internal/assist` contains not one log
   statement; `internal/httpapi/assist.go:337` even comments that "the real
   error is in the server log for the operator", which is not true — nothing
   logs it. There is no `slog` anywhere in the repository and no log level to
   configure. So "why was that run slow" is currently unanswerable from
   outside a debugger.

This stage fixes the bug, gives the assistant an account of itself in the log
and in the browser, uses that account to make it faster, and then spends the
remaining room on the thing the backlog wants next that fits the theme:
**a location's cover image, filled in for you** — offered by the assistant
from the pages it already read, and searched for directly by anyone who would
rather pick one themselves.

Deliberately **not** in this stage, and staying in the backlog: trip-level
suggestions ("suggest things to do in Reykjavík"), which is a stage of its
own; SearXNG, still blocked on nobody having an instance.

### What exploring the code changed about the plan

- **The "1 suggestion" bug is one line, and it is in `renderSources`.**
  `assist-panel.js:355` pushes the sources box into `outstanding` — the array
  the counter counts — with `accept: () => box.remove()`, which removes the
  node but never calls `forget()`. Every real entry's `accept` (`:211-217`)
  does. So the box leaves the page and stays in the array: count floors at 1,
  `syncBar()` never hides the bar. It also means the count is inflated by one
  from the moment sources render.
- **The UI suite cannot see that bug, and that is why it shipped.** The stub's
  URLs are `example.invalid`, so no page fetch succeeds, so
  `proposal.sources` is empty and `renderSources` returns at `:331` before the
  bad push. This is the same known gap `todo.md` already records for the links
  and sources lists. The two are one problem and are fixed together in
  Milestone 1.
- **The likely cause of the timeouts is the composing turn.** The gathering
  deadline does *not* produce `ErrTimedOut` — hitting it ends the research and
  the run still answers (`agent.go:245-252`). `assist_timeout` can only come
  from the caller's context or from a provider call's deadline, and the
  composing turn resends the entire conversation including every page's 12KB
  of text. Stage 16 Milestone 8 already had to raise `AnswerTimeout` from 60s
  to 2m for exactly this. That is a hypothesis, not a finding — Milestone 2
  exists to turn it into one.
- **Tool calls in a turn run one after another.** `agent.go:278-296` is a
  plain `for` loop over `resp.ToolCalls`. A turn asking for three page reads
  pays for three sequential 8-second fetches. `toolset` is already
  mutex-guarded and `checkLinks` already uses a `WaitGroup`, so the pattern
  exists in the package.
- **Nothing tunable reaches the provider except temperature.** `wireRequest`
  (`provider.go:147`) sends `model`, `messages`, `tools`, `tool_choice`,
  `response_format` and a hard-coded `temperature: 0.2`. No `max_tokens`, no
  `reasoning_effort`, no `stream`. Adding one is a struct field plus a config
  var, but see the note in Milestone 4 about servers that 400 on an unknown
  parameter — `isUnsupportedFormat`/`schemaUnsupported` (`provider.go:200`) is
  the precedent for handling that.
- **The cover-image feature already has most of its plumbing.**
  `POST /api/trips/{tripId}/media/url` (`media.go:118`) fetches an image from a
  URL server-side and stores it, and `image-field.js` already drives it from
  the editor. What is missing is a way to *find* an image, and somewhere to
  record where it came from: `media_assets` (`0001_init.up.sql:83`) has `kind`,
  `storage_path`, `external_url`, `content_type`, `width`, `height` and no
  provenance column at all. So this needs migration **`0002`**, in both
  dialects.
- **The agent already reads the official page, and already parses it.**
  `extractText` (`fetch.go:334`) walks the document with `x/net/html` and
  harvests `<title>`. Harvesting `<meta property="og:image">` at the same time
  costs no extra request, no new backend and no key — and it is the venue's own
  photograph of itself, from the page the agent is proposing as the official
  link. That is why it is the *primary* source for a suggested cover, with
  Wikipedia as the fallback for the landmarks that have an article and no
  useful official site. Generic image search was considered for this and
  rejected: the model has no vision, so it would be picking a photograph by
  surrounding text, and a wrong-but-plausible picture of a place you have
  never been is the same failure mode with no tell that made Stage 16 refuse
  to take coordinates from the model.
- **Image search is a good feature, but not an assistant feature.** Picking
  from a grid of candidates is something a person does well and a blind agent
  does badly, so it becomes its own control in the editor — the sibling of the
  address search — in Milestone 7, and nothing about it goes through the LLM.
- **`Searcher` cannot do images, and only some backends could.**
  `search.go:36` returns `{title, url, snippet}`. Serper has an images
  endpoint and so does ddgs; **Ollama Cloud's `web_search` does not**, and
  neither does the stub. So image search is an *optional* capability a backend
  may implement, discovered by type assertion rather than a second registry —
  and the Wikipedia half must work with nothing configured at all, which is
  what keeps the feature useful on a stock instance.
- **`CARAVEL_SEARCH_PROVIDER` currently requires the assistant.**
  `config.go:201` refuses a search provider without `CARAVEL_LLM_URL`, on the
  reasoning that "web search is only used by the assistant". Milestone 7 makes
  that false, so the rule has to be relaxed and the searcher constructed in
  `main.go` for two consumers rather than inside `assist.New`.
- **There is no CSP in the app** — `docs/running/reverse-proxy.md:90` says so
  explicitly and leaves it to the proxy. So hotlinked third-party thumbnails
  in an image-search grid will render; an operator who adds an `img-src`
  policy is the one case that breaks, and it is theirs to widen.
- **`<details>` has exactly one precedent**, `itinerary-tab.js:95-115`: a
  `document.createElement("details")` with a drawn `chevron-down` in the
  `<summary>` rather than the UA marker, because `display:flex` on a summary
  kills the triangle. Milestone 3 copies that shape rather than inventing one.
- **The mobile map height is not part of this stage.** `notes.md`'s fourth
  item goes to the backlog tagged **(soon)** — with the warning that
  `leaflet-map.js:268`'s `min(50vh, 20rem)` is deliberate, the Stage 13
  Milestone 1 fix for the map swallowing the page scroll at 324×756.

---

## 0. Land the plan, and fold the notes into the backlog

Land this document as `plans/stage-21.md`. Then empty `plans/notes.md` into
`plans/todo.md`:

- items 1–3 are this stage's Milestones 1, 4 and 2–3 respectively, so they go
  in only as far as anything is deferred;
- item 4 (the mobile map is half the screen) becomes a **Bugs and rough edges**
  entry tagged **(soon)**, naming `leaflet-map.js:257-269`, the `min(50vh,
  20rem)` cap, *and* the reason it is capped — Stage 13 Milestone 1, where a
  flat 50vh left ~67px of page below the map at 324×756 and a drag starting in
  the lower half had nowhere to go but the map. Note that the legend's
  `order: -1` exists partly to give a drag-safe strip, and that
  `map.gesture.spec.js` is what would catch a regression.

Delete `plans/notes.md` once it is folded in — the backlog is the single
input to planning, and a second file that also collects notes is how the two
drift.

---

## 1. Sources and links in the suite, and the counter that never emptied

Two things, one subject: the sources code path is both where the bug is and
what the test suite cannot reach.

**The fix.** Stop treating the sources box as a suggestion. A separate
`sourcesBox` variable in `renderAssistPanel`, cleared by `clearSuggestions()`
and untouched by `syncBar()`. Two consequences worth taking deliberately:
the count now means "suggestions you have not decided", which is what the
label says; and `placeProposal`'s early return at `:315` must move so that a
run with sources but no suggestions still shows them rather than only
`assist.noSuggestions`.

**The seam.** The suite cannot exercise any of this because the fetcher
refuses loopback by design, so the stub can never produce a live link or a
recorded source. Stage 16 rejected the two obvious answers and was right to:
giving CI a network dependency, and letting `CARAVEL_LLM_URL=stub` relax the
address policy, which is a config value weakening a security control.

The third answer: **the stub owns its own fixture host.** When the stub
provider is constructed, it starts an in-process HTTP server on loopback
serving two fixture pages, and hands the fetcher an allowlist containing
exactly that one `ip:port` — an address the OS chose at run start, that no
environment variable can name and that does not exist unless the stub does.
`newFetcherWithPolicy(bool)` (`fetch.go:70-79`) becomes a policy value
carrying that allowlist instead of a boolean. Everything else about the guard
is unchanged: scheme check, redirect re-check, size and time caps, and a test
pins that the allowlisting policy still refuses `file://` and still refuses
*other* loopback addresses.

This is a smaller weakening than the rejected option — it is not reachable
from configuration and it opens one address rather than a class — but it is a
weakening, and the plan should say so rather than present it as free. If it
proves awkward in practice, land the counter fix alone and split the seam
into its own follow-up commit rather than blocking the fix behind it.

**Files.** `web/js/components/assist-panel.js`, `internal/assist/stub.go`,
`internal/assist/fetch.go`, `internal/assist/assist.go` (wiring the policy),
`tests/ui/assist.spec.js`.

**Verify.** `make ci`. `go test` for the policy in both directions. Then the
Playwright spec, which can now assert what it never could: a run produces a
links suggestion and a populated `.assist-sources` list; accepting every
suggestion one at a time ends with `.assist__bar` hidden and no count;
"Accept all" from a full proposal likewise; "Dismiss all" clears the sources
box too. Delete the coverage-gap paragraph from that spec's header and the
matching `todo.md` entry.

**Done.** Both halves landed as planned, including the seam -- it did not turn
out awkward, so the fallback of splitting it off was not needed.

**The fix is what the plan said it was.** `sourcesBox` is its own variable,
`clearSuggestions()` clears it, `syncBar()` never sees it, and `outstanding`
now means only "suggestions you have not decided" -- which is what the label
above it has always claimed. Two consequences taken deliberately:

1. **`renderSources` moved above the empty check.** It used to sit after a
   `return` that fired whenever nothing was worth suggesting, so a run that
   found nothing also said nothing about where it had looked. Now the account
   of the run is rendered first and the "no suggestions" note follows it.
2. **The sources box survives "Accept all".** It did not before, but only as a
   side effect of the bug: the pseudo-entry's `accept` removed the node. Having
   accepted seven suggestions is the moment you might most want to see what
   they came from, so it stays; "Dismiss all" still clears it, because then
   there is no proposal left for it to explain.

**The seam: `addressPolicy` replaced the boolean.** `newFetcherWithPolicy`
took `allowPrivate bool`; it now takes an `addressPolicy` with two fields --
`allowPrivate`, which the package's own tests use and nothing else may, and
`allowed`, a set of exact `host:port` strings. `newFetcherAllowing(addrs...)`
is the constructor for the second. The allowlist is matched in both places the
policy is enforced, the pre-flight `guard` and the dial-time
`checkDialAddress`, and the two agree because its entries are literal
addresses with no name in between for a resolver to change its mind about.
The scheme check runs before either exception, so neither can make `file://`
fetchable.

`internal/assist/stub_fixture.go` is the fixture host: a `sync.OnceValue`
singleton that binds `127.0.0.1:0`, serves two plain HTML pages, and hands back
its address. A singleton because `newStubProvider` is called by every test in
the package as well as by the server, and a listener per call would leak dozens
across a run with nothing to close them. It has no shutdown path for the same
reason `Assistant` has none: one idle loopback listener for the life of a
process that only exists because somebody selected the stub.

**The stub script grew to five turns**, reading two pages rather than one, so
the sources list has more than a single entry in it -- a list of one renders
the same whether it was built correctly or by accident. `fetchArgs` encodes the
tool arguments rather than formatting them, because the fixture URL carries a
port the script cannot know in advance.

**Verified.** `make ci` green. `go test ./internal/assist` covers the
allowlist in both directions: the named address is fetched and its title
extracted, a *different* loopback address one server along is refused as a
blocked address, and `file://`, `169.254.169.254` and `10.0.0.1` are all still
refused through an allowlisting fetcher. The dial-time check is tested
directly on both a listed and an unlisted address, and `LinkIsLive` through
the allowlist. `TestTheDefaultStubScriptRunsEndToEnd` now asserts what it could
not before -- a live link survives the check, two sources are recorded, and
both carry a real title rather than `(untitled)`.

Then the browser suite. The assist spec asserts seven suggestions rather than
six (the links slot is populated now), a `.assist-sources` list of two whose
first entry reads "Kex Hostel — Reykjavik", the count stepping 7 -> 6 -> 5 as
suggestions are taken, "Accept all" leaving zero suggestions with the bar
hidden and the accepted link in the form's own list, and "Dismiss all"
clearing the sources box as well.

**The regression assertion was proved rather than assumed.** The old
`outstanding.push` was put back and the spec run against it: it fails at the
count assertion ("7 suggestions" vs 8). Restored, it passes. That is the
difference between a test that covers the bug and a test written beside it.
Full suite: 135 passed.

Then by hand at 324x756 against a stub-configured server: seven suggestions
and two sources with their real titles; after "Accept all", zero suggestions,
the bar hidden, title, category and the link all applied, the sources box
still standing and reading correctly on its own ("Pages used ... Shown so you
can check the suggestions. They are not saved."), and no horizontal overflow.

**One thing surfaced and deferred**, now in `todo.md`: `with_server.sh` sets
the LLM and search providers to their stubs but leaves `CARAVEL_GEOCODER_URL`
at its default, so the coordinates suggestion this spec asserts on -- and the
address search in `locations.spec.js` -- reach the real Nominatim on every
run. The assist spec's own header says CI has no network budget, which is true
of everything except that.

---

## 2. Structured logging with levels, and the assistant's run trace in the log

The repository has no `slog`, no log level and no request logging. This
milestone introduces the seam and then uses it where it is needed most.

**The seam.** `log/slog` with a `CARAVEL_LOG_LEVEL` variable (`debug`, `info`,
`warn`, `error`; default `info`) parsed in `internal/config` alongside the
existing vars, and a handler installed as the default in
`cmd/caravel/main.go` before anything else runs. Convert the handful of
existing `log.Printf`/`log.Fatalf` call sites (`main.go:61-158`,
`db.go:145`) rather than leaving two idioms. `cmd/seed` keeps `log` — it is a
CLI writing to a person, not a service.

Format: text by default, because a self-hosted instance's log is read by a
human in `journalctl`. A `CARAVEL_LOG_FORMAT=json` escape hatch is a
three-line addition and worth having for anyone shipping to a collector;
decide it here rather than leaving it to be retrofitted.

**The trace.** `internal/assist` gets a run-scoped logger and emits, at
**debug** level and nowhere else, the account that does not exist today:

- run start: mode, model, search provider, whether trip context was included,
  the effective limits;
- per turn: turn number, wall time of the provider call, `finish_reason`,
  prompt/completion/total tokens, cumulative spend, how many tool calls came
  back;
- per tool call: name, arguments, wall time, outcome, and the size of the
  result — for `web_search` the query and result count, for `fetch_page` the
  URL, status and extracted bytes, for `geocode` the query and match count;
- why the loop ended: no tool calls, turn ceiling, tool-call ceiling, budget,
  gathering deadline, cancellation;
- the composing turn separately, with its own wall time and token count,
  since the hypothesis above says this is the expensive one;
- `buildProposal`: fields proposed and dropped, each link kept or dropped with
  the reason, whether coordinates came from the address or the place name.

At **error** level, unconditionally: the provider's actual error, which
`assist.go:338` deliberately does not send to the client and currently sends
nowhere else either. That comment becomes true.

**What must not be logged**, stated in the package comment so the next person
adding a line knows the rule: the API key, the `Authorization` header, and
full page text. Page text is user-adjacent content from a third party and can
be large; the URL and the byte count are what a person debugging actually
needs.

**Files.** `internal/config/config.go`, `cmd/caravel/main.go`,
`internal/assist/*.go`, `internal/db/db.go`, `.env.sample`,
`docs/configuration/`.

**Verify.** `make ci`. A `go test` asserting the level gate actually gates —
a run at `info` emits no per-turn records and a run at `debug` does — using
`slog.NewTextHandler` over a buffer, which is the cheap and reliable way to
test this. Then a real run at `debug` against the stub and one against the
live provider in `credentials.yaml`, reading the output to confirm it answers
"where did the time go". Document `CARAVEL_LOG_LEVEL` in `.env.sample` and
the configuration docs.

**Done.** Both halves landed, and the trace answered the stage's central
question on its first live run.

**The seam.** `CARAVEL_LOG_LEVEL` (debug/info/warn/error, default info) and
`CARAVEL_LOG_FORMAT` (text/json, default text) are parsed and *validated* in
`internal/config` -- an unrecognised value is a startup error naming the
variable, not a fall back to info, because somebody who wrote "verbose" and got
silence would conclude the flag does nothing. `DEBUG+2`, which slog itself
accepts, is refused: documenting that syntax buys a level with no name.
`setupLogging` in `main.go` installs the handler as the default, so packages
reach it with `slog.Default` rather than being handed a logger through five
constructors.

Every existing `log.` call site is converted -- `main.go` and `db.go` -- and a
`fatal(what, err)` helper replaces `log.Fatalf` so a startup failure goes
*through* the operator's logger rather than around it. One deliberate
exception: `config.Load` failing is still written straight to stderr, because a
malformed `CARAVEL_LOG_LEVEL` is one of the things it fails on and there is no
logger to install yet. `cmd/seed` keeps `log`: it is a CLI writing to a person.

**The trace.** `Options.Logger` (nil means `slog.Default`) plus a run-scoped
child carrying a `run` number from an atomic counter, so two concurrent runs
can be told apart in a log that interleaves them. Records at debug and nowhere
else: run started with the whole configuration, one per turn with its wall
time, finish reason and token usage, one per tool call, why gathering stopped,
the composing turn on its own, every field proposed or not proposed with the
reason, every link kept or dropped, which query resolved the coordinates, and a
final total.

The rule about what must never be logged is in the package comment and pinned
by a test: a run with a distinctive fake key and the fixture pages text is
asserted to contain neither, while still containing `result_bytes`.

**The stale comment is now true.** `assist.go` said "the real error is in the
server log for the operator" and nothing logged it. `streamAssistRun` now logs
the provider actual error at **error** level, unconditionally -- the browser
only ever sees a fixed sentence, so this was previously written down nowhere at
all.

**Two flaws the first live run exposed in the trace itself**, both fixed before
committing, and both the kind only real output shows:

1. **`turns` undercounted by one.** It reported the loop counter, and the
   ordinary exit -- a turn with no tool calls -- breaks *inside* an iteration,
   leaving the counter one behind the turns actually billed. A separate
   `turnsUsed`, incremented after each answered call, is the honest number. A
   trace whose job is accuracy must not miscount the thing it counts.
2. **`err=<nil>` on every successful tool call.** A column of nothing in the
   one place a reader is scanning for something. The attribute is now appended
   only on failure.

**Verified.** `make ci` green, `make docs` clean, the assist UI spec still
passing. `go test` covers the level gate as an absolute -- a successful run at
info, warn and error logs *nothing at all*, not merely less -- the presence of
each of the six record kinds at debug, and the no-keys-no-page-bodies rule.
`internal/config` gains a table over both variables including the two refusals.

Then two live runs against the model in `credentials.yaml` with Serper, at
debug. **The hypothesis in this plan Context section was right**, and this is
the data Milestone 4 starts from:

| | run 1 | run 2 |
| --- | --- | --- |
| total | 71.6s | 37.4s |
| gathering | 18.1s | 25.7s |
| **composing (one request)** | **53.5s** | **11.2s** |
| turns | 3 | 4 |
| tool calls | 3 | 4 |
| tokens | 18.3k | 26.9k |

The composing turn was **75% of the first run** and a third of the second, off
the same prompt and the same place. Two observations for Milestone 4: the
variance in that single request dwarfs everything the gathering phase does, and
the three tool calls in run 1 cost 2.3s of the 71.6 between them -- so
parallelising tool dispatch, the lever that looked most obviously right when
this stage was scoped, would have saved about a second. That is exactly the
kind of thing the measurement was for.

The measuring harness was a throwaway `zz_live_test.go`, not committed;
Milestone 4 should decide whether a skipped-by-default live harness is worth
keeping, since it will want before-and-after runs of the same shape.

---

## 3. The run trace in the editor

The same account, for the person using the app rather than the person running
it. Hidden by default, because it is an explanation and not part of the task.

**Shape.** A `<details>` below the suggestions, following
`itinerary-tab.js:95-115` exactly: `createElement("details")`, a `<summary>`
carrying a drawn `chevron-down` and a one-line summary — elapsed time, number
of steps, and tokens when the provider reported any — with the step timeline
inside. One row per step: what it was, its argument (the query, the host),
how long it took, and whether it worked.

**What the server has to send.** Progress events today are fire-at-start only
and carry no timing (`Event{Key, Params}`, `types.go`). The client could
derive durations from arrival times, but not outcomes and not tokens. So
`assist.Event` grows optional `DurationMS` and `Status` fields and a step is
emitted on *completion* as well as on start, plus one final summary event at
the end of a run carrying elapsed time, turns, tool calls and total tokens.

Two constraints inherited from Stage 16 and not to be broken here: progress
events carry **i18n keys and parameters, never sentences** — the server does
not write UI copy — and every value is placed with `textContent`, never a
template string, because a query or a host in a trace came off a web page the
agent read. Any new progress key must also be added to the `PROGRESS_KEYS`
`Set` in `assist-panel.js:41-49`, which is the only way `scripts/i18n.py` can
see runtime keys at all.

**Open decision, to make while building:** whether the trace is always
available or only when the server is at debug level. Recommendation: always.
It costs one collapsed line, it is the user's own run, and "what did the AI
do to get this" is a trust question rather than a debugging one. An operator
who dislikes it can be given a variable later if anyone asks.

**Files.** `internal/assist/types.go`, `agent.go`, `tools.go`,
`internal/httpapi/assist.go`, `web/js/components/assist-panel.js`,
`web/css/base.css`, both files under `web/locales/`.

**Verify.** `make ci` with i18n parity green — every new key in `en.json`
*and* `de.json`. `go test` on the event sequence: a completion event follows
each start event, and the summary event is last. Then Playwright: the
`<details>` is present and closed after a run, opens to a step per tool call,
and the summary line's step count matches the rows. Then by hand in a real
browser: German, dark mode, and 324×756 with no horizontal overflow — a
trace row holds a URL and a duration, which is exactly the content that
overflows a 324px screen.

**Done.** The trace ships, always available and with no configuration -- the
open decision above was settled that way: it costs one closed line, it
describes the reader's own run, and it can be hidden later if it turns out to
be in the way.

**Three event kinds, not one with a duration bolted on.** `assist.Event` gained
a `Kind`: `EventProgress` (the zero value, so every existing construction still
means "say this in the status line"), `EventStep` and `EventSummary`. Progress
fires when a step *starts* and is replaced; a step fires when it *ends* and is
accumulated. Putting a duration on a progress event would have meant either
holding the status line back until the step finished, or shipping a duration of
zero.

**The dispatcher now owns both halves of a tool step.** `doSearch`, `doFetch`
and `doGeocode` each used to emit their own progress event; the timing had to
be measured somewhere, and doing it in three places would have measured it
three ways. `describeCall` maps a tool name and its raw arguments to the two
keys and the parameter, and `dispatch` emits the start, times the call, and
emits the step. A fourth tool gets its trace for nothing.

Reading the argument in the dispatcher rather than passing it out of the tool
function has a payoff the plan did not anticipate: **a call whose arguments do
not parse is still traced**, which is exactly when a reader most wants to see
that the model spent a turn on something malformed.

**The summary is deferred.** A run that timed out or failed is the one somebody
most wants an account of, so it closes the run however the run ends rather than
only on the success path. `emit` counts steps centrally, so the total in the
heading cannot drift from the list underneath it -- a trace that contradicts
itself on its own first line would be worse than none.

**Two real bugs found by looking at the working page**, neither of which any
test would have caught:

1. **`t()` clobbers a `count` supplied in params.** The tokens line rendered as
   a literal `{count} tokens`. `Object.entries({ ...params, count })` sets
   `count` to `undefined` when the third argument is absent, overwriting the
   one in params, and the `value !== undefined` guard then skips it. Every
   caller in the app happened to pass both, so this had never fired.
   **Fixed in `i18n.js` rather than worked around at the call site**, because
   the next person to want a placeholder without a plural would hit it too.
   The spec now asserts a number rather than a placeholder.
2. **The trace heading collapsed at 324px in German.** `.assist-trace__meta` is
   `nowrap` with `margin-left: auto`, and flex items shrink before they wrap --
   so "0.1 s · 9 Schritte · 3000 Tokens" took the line and squeezed "Was die KI
   gemacht hat" into a four-line column one word wide. `flex-wrap: wrap` plus a
   `min-width` floor on the title drops the totals to their own line instead.

**And one accessibility failure found by measuring rather than assuming.** The
failed marker in `--color-danger-fg` at 13px measures **4.39:1** against the
card -- under the 4.5 this app holds normal text to, and 13px is not "large
text" at any weight, so bolding would not have helped. The fix is the idiom
`.image-field__error` established: full-contrast text, with the red carried by
a left border and the danger tint. Every row already has a transparent
left border so a failed one colours without shifting its neighbours sideways.

**Verified.** `make ci` green with i18n parity at 358 keys, `scripts/i18n.py
unused` reporting no orphaned assist keys. Go tests cover the contract the UI
is built on: a step per turn and per tool call, exactly one summary and it is
last, the summary's step count matching the number of steps actually sent, a
failed tool call still traced and marked, an unparseable one traced too, and a
summary closing a *failed* run. The transport tests assert step and summary
events arrive over SSE with i18n keys and never-null params.

Playwright: the trace is present and closed after a run, its heading counts
what the list holds, opening it shows a row per step, both fixture hosts appear
as "Read ..." rows, every row carries a duration matching `/^[\d.]+ s$/`, and
"Dismiss all" clears the suggestions and the sources while leaving the trace --
the run still happened, and "why was that useless?" is the likeliest question
at that moment.

Then by hand against a stub-configured server. German and dark at 324x756: the
heading is a 44px tap target on one line with the totals wrapped beneath, no
horizontal overflow. English and light at 1280px: title left, totals right, one
row per step. Contrast measured in both themes on both a plain and a failed
row -- 7.03 and 6.25 light, 5.81 and 4.72 dark, with the failed marker at 14.34
and 11.60. All above 4.5.

**One copy change made while looking at it**: the per-turn label was "Thought
about what to do next", which appears four times in a nine-step trace and read
as filler at 324px. It is now "Worked out what to do next" / "Nächsten Schritt
überlegt", the German being the half that actually needed shortening.

---

## 4. Speed, measured

**This milestone starts with a checkpoint, not with code.** Milestones 2 and 3
exist to answer where the time goes; the levers below are an initial
estimation made before any measurement, and the measurement may well change
what is worth doing. So: run the assistant against the live provider at
`debug` level over a handful of real places, write the timings up in
`plans/stage-21.md`, and **stop and agree what to implement** before touching
`agent.go`.

Authorised for that conversation, subject to what the numbers say:

- **Parallel tool dispatch within a turn.** `agent.go:278-296` runs a turn's
  tool calls sequentially. Concurrent dispatch with results collected into a
  slice indexed by call, then appended in call order — the order matters,
  since a `tool` message must follow its `tool_calls` and most servers reject
  a mismatch. `toolset` is already mutex-guarded. The tool-call ceiling has to
  be decided *before* the fan-out rather than inside the loop.
- **Compacting the conversation before composing.** The composing turn
  resends every page's 12KB of text. Truncating older tool results — the most
  recent N kept whole, earlier ones cut to a lead fragment — would cut that
  prompt sharply. The risk is losing a detail the model had already read, so
  whatever is dropped has to be visible in the debug trace.
- **A reasoning-effort knob.** `reasoning_effort` and `max_tokens` on
  `wireRequest`, unset meaning not sent. Note two things flagged when this
  stage was scoped: the right effort may differ **per step** — gathering turns
  and the composing turn are not the same kind of work — and it may need setting
  differently **per model**, since providers disagree about both the parameter
  name and the accepted values. Whatever shape this takes, an unknown
  parameter must degrade the way `json_schema` already does
  (`provider.go:200-205`), or a server that 400s on it takes the feature down
  entirely.

**Considered and not authorised:** a speculative first search fired alongside
turn 1. It would save a round trip but changes what the model sees on its
first turn, and there is no evidence yet that the first round trip is where
the time is.

**Verify.** `go test` for each change — call-order preservation under
parallel dispatch, the compaction keeping the most recent results intact, the
parameter downgrade in both directions. Then the honest one: the same handful
of real places re-run against the live provider, before-and-after timings in
the plan document, and a check that the proposals did not get worse. A run
that is twice as fast and half as good is a regression.

### The measurements, and what was agreed from them

Run ahead of the milestone, once Milestone 2 made it possible. Six places, two
runs each, all **prompt mode** with `locale: en` and a real Nominatim wired --
Hallgrimskirkja, Brooklyn Bridge, Tokyo Tower, Brandenburger Tor, Heger Tor,
KANZASHI Tokyo Asakusa -- against `deepseek/deepseek-v4-flash-0731` on
OpenRouter with Serper. Then six more with per-turn timings.

**Mean 36.5s, range 17.9-73.5s.** Where it goes:

| | mean | share |
| --- | --- | --- |
| Model, gathering turns | 19.5s | 53% |
| Model, composing turn | 11.4s | 31% |
| Tool calls (search, fetch, geocode) | 4.5s | 12% |
| Link checks and geocode | 1.1s | 3% |
| **Model, total** | **30.9s** | **85%** |

**The single-run reading in Milestone 2 was wrong and is corrected here.** That
run had a 53.5s composing phase and it was an outlier: across twelve runs
composing averages 11.4s and never exceeds 22.1s. The real shape is that 85% of
a run is the model, spread over ~4.4 sequential requests, not concentrated in
one. `calls` was 1 in every run, so no run ever needed its answer reshaped.

**What the per-turn data found, and what the milestone is now built around.**
The last gathering turn is the one that returns no tool calls -- the model
saying "I have enough to describe this place" and nothing else. It carries the
whole conversation as its prompt and produces one sentence, and it is **the
slowest gathering turn in five of six runs, averaging 6.5s = 22% of a run**.
With the composing turn, the final two round trips are 59% of a run and one of
them produces no information at all.

So the agreed primary change is **to make the answer a tool call**: a `propose`
tool whose parameters are the proposal schema. The model calls tools until it
calls `propose(...)`, and those arguments are the structured answer. The prose
turn disappears; the composing work happens in the turn that was wasted.

This is deliberately *not* the shortcut Stage 16 rejected. Offering tools and a
strict `json_schema` together is a compatibility minefield -- several servers
constrain all output to the schema when one is set, which makes tool calls
impossible. A tool call is ordinary tool use, which every OpenAI-compatible
server already has to support to run this feature at all. The two-phase path
stays as the fallback, and `completeJSON`'s validate-and-retry already covers
the one real risk (tool arguments enforced less strictly than `strict: true`).

**Revised from the pre-measurement list:**

- **Parallel tool dispatch: kept, but not as a speed fix.** All tool calls
  together are 12% of a run at 1.1s each, and only a turn issuing two or more
  benefits. It is worth 1-2 seconds of 36. It goes in because the sequential
  loop reads oddly beside `checkLinks`, which already fans out -- tidiness with
  a small dividend, not the point of the milestone.
- **Reasoning effort: keep the knob, expect nothing locally.** This model
  spends 70-450 completion tokens a turn; it is not thinking. The variable is
  for operators pointing at a reasoning model.
- **Prompt caching: added to the milestone, as a spike first.** Every turn
  resends the conversation and OpenRouter supports caching. Unmeasured, so it
  is measure-then-decide rather than a committed change.
- **Compaction of page text: deferred behind caching, not dropped.** See below.

**Why compaction is subordinate to caching.** Compaction here means *mechanical
truncation* -- keep the most recent tool results whole, cut older ones to a
lead fragment. It must never mean asking a model to summarise: that is an extra
LLM round trip, and since 85% of a run is already model time, it would make the
run slower in order to make it cheaper. Truncation is free in wall time.

The gain it can offer is bounded and now roughly known. Turn 1 has an 822-token
prompt and costs 2-4s; the done turn has ~7-8k tokens and costs 6.5s. That is
about 0.4-0.5s per thousand prompt tokens on this provider. Page reads are
capped at 12KB each, so two or three of them are 6-9k tokens resent on every
later request -- cutting five thousand of them saves roughly 2-2.5s per
affected request. Once the propose-tool removes the done turn, one request is
affected, so the honest estimate is **~2s, about 7%**, in exchange for possibly
dropping a detail the model had already read.

Prompt caching addresses the same cost without discarding anything: the
repeated prefix stops being re-processed at all. That is strictly better than
throwing information away, which is why the order is caching first, and
compaction only if caching turns out to be unavailable or ineffective.

**The harness stays throwaway.** `zz_bench_test.go` is not committed -- a test
that spends money and needs the network is not something to leave in the tree.
It lives in the session scratchpad and is rebuilt for the before-and-after
comparison.

### The model matters more than any change in this milestone

A second comparison, one "Tokyo Tower" run per model with Serper and Nominatim
held constant: five models on the Osnabrueck LiteLLM instance, three on
OpenRouter. **14.9s to 59.1s** -- and within LiteLLM alone, same host and same
network, 14.9s to 39.9s, which isolates the model from the wire. No lever in
this milestone is worth anything close to that spread.

All eight completed with five fields and resolved coordinates, so tool calling
and `json_schema` work across the range including the small models. That is a
better robustness result for the pipeline than any test gives.

Two conclusions worth keeping:

1. **The instance switched to `nvidia/nemotron-3.5-lightning`.** Fast (16.9s,
   then 16.4s on a re-run -- the only model that repeated closely), three
   sources, and turns of 590-620ms. Use it for further live runs; the
   measurement harnesses read the model from `credentials.yaml`, so nothing
   else needs changing.
2. **The done-turn finding is not an artefact of one model.** It is the slowest
   gathering turn in **seven of eight** models across two providers and four
   vendors, mean 21% of a run -- and it is proportionally *worse* on the fast
   models, which are the ones anyone would actually run. On nemotron the five
   working turns average 880ms and the turn that produces nothing takes 2905ms.

**Variance is larger than the tables suggest**, and this is the caveat to carry
into the before-and-after comparison. Re-running the three OpenRouter models
gave 27.3s for a model that had measured 59.1s, and a done turn of 17.2s where
the first run saw 5.5s -- same model, same prompt. A single run per
configuration is not enough to attribute a change to the change. Milestone 4's
comparison needs repeats, not one pass.

**Cost was deliberately not pursued.** It is visible on the provider's own
dashboard, so instrumenting it here would duplicate something the operator can
already see. Not a backlog entry either.

**Not chased, by decision:** Brandenburger Tor run 1 returned no coordinates
because Nominatim missed both the proposed address and the place-name fallback,
while run 2 on the same place succeeded. Intermittent, correctness rather than
speed, and deliberately left alone rather than added to the backlog.

### The second checkpoint, after the model switch

Re-opened before any code, because the ground had moved: the instance switched
to `nvidia/nemotron-3.5-lightning` and the same Tokyo Tower run went from 59.1s
to 16.4s. **The problem this milestone was written for is largely solved by a
configuration change.** What is left is worth seconds on a 16s run, not tens of
seconds on a 60s one, and the milestone is scoped accordingly.

Where the 16.4s goes on nemotron: six model requests at 1341, 958, 603, 680,
816 and **2905** ms, 5.4s of tool calls and 3.2s composing.

**Split into 4a and 4b**, one change each, so each is measured on its own --
which matters more than usual here, because run-to-run variance has been large
enough to swallow a real improvement whole.

- **4a, the propose-tool.** The done turn is 2.9s of 16.4s and confirmed across
  seven of eight models. Best-evidenced change available.
- **4b, batching and parallel dispatch.** Five tool calls arrived mostly one
  per turn, which is *why* there were six turns. Prompting for several reads at
  once and dispatching them concurrently attacks round trips, where 85% of the
  time is. Less certain than 4a: a search cannot be batched with reads of its
  own results, so the realistic saving is one or two turns.

**Dropped from scope:**

- **The reasoning-effort knob.** No measured benefit on any model tested --
  completion tokens ran 70-450 a turn, so none of them are thinking. It also
  needs the same try-then-degrade handling as `json_schema` to survive a strict
  server. Backlog, for whoever points Caravel at a reasoning model.
- **Prompt caching.** Not taken up. Backlog.
- **Compaction.** Was already subordinate to caching; with caching dropped it
  stays in the backlog on its own ~7% estimate.

---

## 4a. The answer becomes a tool call

`propose`, a tool whose parameters are the proposal schema. The model calls
tools until it calls `propose(...)`, and those arguments are the structured
answer. The turn that used to say "I have enough to describe this place" and
nothing else disappears; the composing work happens in it instead.

- **Not the shortcut Stage 16 rejected.** Offering tools *and* a strict
  `json_schema` together is a compatibility minefield -- several servers
  constrain all output to the schema when one is set, which makes tool calls
  impossible. A tool call is ordinary tool use, which every server already has
  to support to run this feature at all.
- **The two-phase path stays, as the fallback.** A turn that returns no tool
  calls still means "done gathering", and still leads to the composing request
  exactly as today. So a model that ignores `propose` loses nothing.
- **A malformed `propose` is answered, not fatal.** Arguments that do not
  decode come back to the model as a tool result saying what was wrong, which
  is the idiom `dispatch` already uses for every other failure. The turn and
  tool-call ceilings bound the retries.
- **`propose` is not dispatched like the others.** It has no result to feed
  back and it ends the run, so it is handled in the loop rather than in the
  tool map.
- The trace and the debug log must record **which path a run took**, or the
  before-and-after cannot be attributed.

**Verify.** `go test`: a scripted model that calls `propose` produces a
proposal with no composing request at all; one that never calls it still gets
the two-phase answer; a malformed `propose` is answered and the run recovers.
Then live, against nemotron with repeats -- see the note on variance.

---

### Measuring 4a honestly

The same harness for both, and **repeats, not one pass**. Re-running three
models earlier produced 27.3s for one that had measured 59.1s, and a done turn
of 17.2s where the first run saw 5.5s. A single run per configuration would
attribute noise to the change.

Baseline first, on nemotron, several places, N runs each, recorded here. Then
the same after 4a. Watch two numbers, not one: wall time, and the number of
model round trips -- the second is what the change actually targets, and it
should be far less noisy than the first.

**Done.** The propose tool ships and every live run used it. The speed premise
did not survive contact with the measurements, and this section records what
actually happened rather than what was hoped for.

**What was built.** `propose`, a tool whose parameters are `proposalSchema`,
offered on every run. When a turn contains a propose call, its arguments are
decoded as the answer and the run ends there -- no composing request. Handled
in the loop rather than in the tool map, because it produces no result to feed
back. Three deliberate behaviours:

1. **The two-phase path stays as the fallback.** A turn with no tool calls
   still means "done gathering" and still leads to the composing request. A
   model that ignores `propose`, or a server that mishandles a tool schema,
   loses nothing. `answer()` is the seam: a pass-through when a proposal
   already exists, the original flow otherwise.
2. **A malformed propose is answered, not fatal.** Arguments that do not decode
   go back as a tool result saying what was wrong -- the idiom `dispatch`
   already uses for every other failure -- and the ceilings bound the retries.
3. **A propose call ends the turn even alongside other calls.** The model has
   said it is finished; dispatching a page read whose result nobody will see is
   a request paid for and discarded.

The stub now ends with a propose call, so the browser suite exercises the path
people actually get; the fallback is covered by `agent_test.go`. The trace and
the debug log carry `answered_by`, without which none of the below could have
been attributed.

**The measurements: 15 runs before, 15 after, five places, nemotron, Serper,
real Nominatim.** All 15 after-runs took the propose path.

| | baseline | propose | permutation p |
| --- | ---: | ---: | ---: |
| total time | 8.93s | 8.08s | 0.44 |
| round trips | 5.27 | 5.00 | 0.51 |
| tokens | 14.0k | 17.9k | 0.12 |
| tool calls | 3.27 | **4.00** | **0.037** |
| links proposed | 1.33 | **1.93** | **0.013** |
| sources | 1.40 | 2.00 | 0.13 |

**The change is not measurably faster.** The wall-time point estimate is 10%
better, but the standard deviation is about 2.9s on an 8.9s mean, which
swallows an 850ms effect whole -- p = 0.44 over twenty thousand shuffles.

**What did change, significantly, is that the model does more.** An extra tool
call per run and half again as many links. The round trip freed by removing the
signalling turn was *reinvested in more gathering* rather than banked as speed.
That is a defensible outcome -- the same latency, better sourced -- but it is
not the one this milestone was written for, and calling a p = 0.44 result a 10%
improvement would have been the easy and wrong thing to write down.

**Kept anyway**, on the argument that it deletes a request which provably
produced nothing (structurally, not statistically: `compose=-1` in all 15
runs), costs nothing in latency, and improves sourcing measurably. The extra
tokens are fractions of a cent.

**And this is why 4b was dropped.** Detecting a 10% latency change against this
variance needs roughly 180 runs per arm. Batching targets one or two round
trips out of five -- the same order as 4a, and therefore also below the noise
floor of the system being measured. After the model switch took a run from 59s
to 8s, further speed work is chasing effects smaller than run-to-run variance.
Parallel tool dispatch moves to `todo.md` as the tidiness item it always was.


---

## 5. Cover image, backend: `og:image`, Wikipedia, and provenance

Two sources, in priority order, and neither of them a generic image search.

- **`og:image`, from a page the agent already read.** `extractText`
  (`fetch.go:334`) grows a harvest of `<meta property="og:image">` (with
  `og:image:secure_url` and `name=` as the near-universal variants) alongside
  the `<title>` it already collects, resolved against the page URL. It rides
  back on the same `fetched` struct and is recorded per source. No extra
  request, no key, no backend. The provenance is the page it came from, which
  is the strongest claim any of this has: the venue's own picture of itself.
- **Wikipedia, as the fallback.** `internal/wikimedia`, a small client in the
  shape of `internal/geocode`: article title in, `{imageURL, contentType,
  width, height, licence, credit, descriptionURL}` out. One endpoint, its own
  timeout, the project `User-Agent`, a size cap. It needs no configuration and
  no key, so it works on a stock instance; a construction failure degrades to
  today's behaviour rather than failing a run.
- **The proposal grows two things.** `wikipedia_title` in `schema.go`'s
  `modelProposal` and its JSON Schema, plus a prompt line asking for the
  article title of the place itself and nothing when unsure. The model's title
  is a *lookup key*, exactly as the address is for coordinates — what reaches
  the user comes from Wikimedia. `buildProposal` then picks the cover: the
  `og:image` of the page it proposed as the official link if there is one,
  otherwise the Wikipedia lead image, otherwise silence. A title that resolves
  to nothing is silence too, not an error.
- **Migration `0002`**, both dialects (`internal/db/migrations/sqlite/` and
  `.../postgres/`): provenance on `media_assets` — the source page URL, and a
  credit line and licence name that are populated for a Wikimedia image and
  empty for an `og:image`. All nullable, because every asset that exists today
  has none. Then edit `internal/db/sqlc/queries/*.sql` and run `sqlc generate`
  **by hand** from `internal/db/sqlc/`, remembering both dialects and keeping
  the comment prose plain (no backticks, no quotes, avoid apostrophes — see
  CLAUDE.md).
- The image is **not** downloaded here. The proposal carries the URL and
  whatever provenance there is; fetching happens on accept, through the
  endpoint that already does it.

**Verify.** `go test` for the `og:image` harvest over the shapes that matter —
`property=`, `name=`, `og:image:secure_url`, a relative URL, several tags,
none at all — and against an `httptest.Server` for the Wikimedia client,
including a missing article, an article with no lead image, and a response
carrying no licence information. `make test-postgres` for the migration and
the regenerated queries — a query change is exactly the case CLAUDE.md says
to run it for. Then live runs against the provider in `credentials.yaml`: one
place with a good official site (expect its `og:image`) and one landmark
without (expect the Wikipedia lead image, with the licence and credit the
article actually states).

**Done.** Both sources work, and the live verification earned its place by
finding a bug that every unit test had passed over.

**`og:image`, from a page already read.** `extractText` returns a third value
and `page` carries `Image`. `property=` and `name=` are both accepted --
Open Graph specifies the former and a large minority of real sites emit the
latter, often through a CMS that does not know the difference, and refusing
those would drop a working image over a spelling nobody outside a validator
notices. `og:image:secure_url` and `twitter:image` are accepted too. Relative
and protocol-relative values are resolved against the page URL in the fetcher,
which is the only place that knows it, and anything that does not resolve to
http(s) is dropped: a broken image URL in a suggestion is worse than no
suggestion.

**`internal/wikimedia`**, in the shape of `internal/geocode`. One exported
method, no configuration, no key. Two API calls: the page lookup for the lead
image, then the file lookup for the licence and the author. **A failure of the
second is not fatal** -- an image with no credit is still an image, and
refusing to offer it because a metadata call timed out would trade a working
feature for a missing attribution line.

**The language edition is chosen per lookup, not pinned.** The plan said
nothing about this and it turned out to matter: article titles are not
translations of each other -- the German article is "Brandenburger Tor" and the
English one is "Brandenburg Gate" -- and smaller places often have an article
in one language and none in another. Osnabrueck's Heger Tor is the case in
point. So the lookup goes to the edition for the user's own locale, the prompt
asks for the title as it appears in *that* edition, and the language is
normalised down to a subdomain (`de-AT` becomes `de`) with anything that is
not plainly a language code falling back to English -- it ends up in a
hostname.

**What the live run found.** Kex Hostel and Hallgrimskirkja both produced their
own site's `og:image`, as designed. The German landmark produced an image too
-- from `de.wikipedia.org`, through the og path, **with credit and licence
empty**. The model had proposed the Wikipedia article as a link, the agent read
it, and took its `og:image` like any other page. That is a Wikimedia
photograph stored with no record of whose it is: precisely the failure the
provenance columns exist to prevent, and no unit test would have caught it
because no unit test had a Wikipedia article in it.

`wikipediaArticle` now recognises an article URL and keeps it out of the og
path. It also earns a second keep: when the model links an article but names
no `wikipedia_title`, the title is recovered from the URL -- a model that finds
the article well enough to link it has already done the hard part. Re-run
live, the same place came back `from=wikipedia`, credited "MrsMyer in der
Wikipedia auf Deutsch", CC BY-SA 3.0, and with the API's clean upload URL
rather than the tagged og one.

**Migration `0002`**, both dialects: `source_url`, `credit` and `license` on
`media_assets`, all nullable because every asset that exists today has none and
so does every photo anybody uploads from their own camera. Queries regenerated
with `sqlc generate` for both dialects and checked for unsubstituted
`sqlc.arg`, which compiles fine and fails at runtime.

**Verified.** `make ci` green, `make test-postgres` green -- the query change
is exactly the case CLAUDE.md says to run it for. `go test` covers the og:image
harvest over eight shapes and the URL resolution over five, the Wikimedia
client over its whole surface (missing article, no lead image, an image too
small to be a photograph, a file-lookup failure, an API error in a 200 body, an
oversized response, the request parameters, and the language-to-edition
mapping including three hostile inputs), and `chooseCover` over both routes
plus the three ways it can come to nothing. Then three live runs, twice: once
to find the Wikipedia bug and once to confirm it fixed.

---

## 6. Cover image, the suggestion and the credit

- A `[data-assist-field="cover"]` slot beside the image control in the location
  editor, and a suggestion that shows the **image**, not a URL — a thumbnail
  with its source host, and the credit and licence under it when there is one.
  This is the one suggestion whose value cannot be judged as text.
- Accepting posts to the existing `POST /api/trips/{tripId}/media/url`
  (`media.go:118`), extended to carry the provenance through to the new
  columns, and sets it as the location's image exactly as choosing a photo
  does today. Still not committed until Save, like every other suggestion.
- **The credit has to be shown wherever the image is**, or storing it is
  pointless: the location view, and anywhere else a location cover appears.
  Small, quiet, linked to the source page. Assets with no credit — which is
  every asset that exists today, and every `og:image` — render exactly as they
  do now.
- New keys in **both** locale files.

**Verify.** `make ci` with i18n parity. Playwright: the suggestion renders an
`<img>` with a non-empty `alt`, accepting it sets the location's image, and
the credit appears on the saved location's view page. Then by hand: a live run
all the way through Save for both sources, and a look at the row in the
database to confirm the provenance is stored and not merely displayed. German,
dark mode, 324×756 — a thumbnail plus a credit line is exactly the content
that overflows a 324px screen.

**Done.** The cover is offered, accepted and credited, end to end.

**One correction to the plan's own wording**: it asked for an `<img>` with a
*non-empty* `alt`. It ships with an empty one, deliberately. The suggestion is
described in words directly beneath the image -- the source host, the credit,
the licence -- and naming it again in `alt` would be repetition for a screen
reader. An empty `alt` is the correct markup for an image whose caption is
already adjacent.

**Accepting goes through the image field, not around it.** `renderImageField`
now returns a handle with `setFromURL(url, provenance)`, and the URL form is
one of its two callers -- the same shape `renderItemForm` took when it gained
`setValues` in Stage 16. That means accepting a cover behaves exactly like
pasting a URL into that card, *including* the staging path on a location that
does not exist yet: the pick is held in memory and `flushUploads` posts it
after Create, provenance and all. Getting that for free is the whole argument
for writing through the component.

**The provenance is accepted from the client rather than re-derived.** The
client is passing back what the assistant found, and the assistant is the only
thing that knows -- an image URL says nothing about who took the photograph.
A caller could therefore send any credit it likes; the blast radius is a wrong
attribution on that person's own trip, which is exactly what they could achieve
by typing one. Malformed values cost the value and never the image: a source
URL that will not parse is dropped, credit strings are truncated on a rune
boundary, and the picture is still stored.

**A credit needs somewhere to point.** `resolveImage` returns one only when a
source URL exists: an image with a named author and no source page cannot be
credited usefully, while one with a source and no author still can -- "from
this page" is a real attribution, and Wikimedia returns exactly that shape.

**The credit appears on the location view only**, not on the thumbnails in the
locations list or the itinerary. A credit line under a 60px thumbnail is
unreadable, and those thumbnails link to the page that carries it.

**The stub grew a real image.** The fixture host serves a generated 64px PNG
and the `/kex` page advertises it as its `og:image`, so a stub run proposes a
cover and accepting it exercises the whole fetch-decode-store path rather than
failing on a placeholder.

**One bug found by looking at the working page.** The thumbnail had
`loading="lazy"`, and an unloaded `<img>` with no intrinsic size collapses to
zero height -- so the suggestion row grew from nothing exactly as the reader
scrolled to it. Measured at 324px: `naturalWidth` 0 and a 0x0 box until
scrolled into view. Lazy loading is wrong here on principle as well as in
practice: the picture *is* the suggestion, and it was asked for by an explicit
action.

**Verified.** `make ci` green, i18n parity at 360 keys, `make test-postgres`
green from Milestone 5 and unaffected here. Go tests cover the endpoint:
provenance stored and returned on the item, an ordinary image reporting
`"image_credit":null`, a `javascript:` source URL and an over-long credit both
dropped while the image survives, and rune-boundary truncation.

Playwright now asserts eight suggestions rather than seven, the cover rendering
an `<img>` with its source host, and -- the guarantee the milestone exists for
-- the full round trip: accept the cover on a *new* location, Save, and the
view page serves it from `/api/media/…` rather than hotlinking. A second test
attaches a credited image and asserts the credit text, the licence and the link
target.

Then by hand at 324x756 in German and dark: the suggestion reads
"KI-VORSCHLAG" with the thumbnail and its source host, accepting it fills the
image field, Save carries it through, and the view page shows "Foto: MrsMyer in
der Wikipedia auf Deutsch · CC BY-SA 3.0" linked to the article. Credit
contrast 6.91:1 at 13px, no horizontal overflow.

---

## 7. "Search for an image", without the assistant

The last milestone, and the only one with no LLM in it. Picking a photograph
from a grid is something a person does well and a blind agent does badly, so
this is a control of its own: the sibling of the address search that already
sits two cards below, and it works on an instance with no assistant at all.

**The control.** A "Search for an image" button in the image field
(`image-field.js`), modelled on `bindPlaceSearch`
(`location-editor-page.js:495+`) in shape: never fires per keystroke, hidden
unless the server reports the capability, and it writes through the field's
existing URL path rather than reaching around it. It seeds its query from the
location's title, which is usually the whole search.

**The endpoint.** `GET /api/trips/{tripId}/image-search?q=…`, trip-scoped
behind `authorizeTrip(..., db.RoleEditor, ...)`, with its own limiter
alongside `GeocodeLimiter` via `newRateLimiter` (`security.go:50`) and added
to `sweepLimitersPeriodically`. Results come back grouped by where they came
from, so the UI can label them honestly:

- **Wikipedia, always, with nothing configured.** Search for a matching
  article; if there is one, offer its images. The lead image is
  straightforward. Returning *all* of an article's images is the part with a
  catch worth stating in advance: `generator=images` lists every file on the
  page, which includes icons, flags, logos, maps and Commons chrome, so it
  needs an `imageinfo` pass and a filter (raster only, above a minimum
  dimension, capped in number). **If that turns out noisy in practice, fall
  back to the lead image alone and say so in the Done paragraph** — this is
  the "if it is easy" gate, and offering six pictures of which four are icons
  is worse than offering one good one.
- **An image search, when a capable backend is configured.** An optional
  `ImageSearcher` interface a `Searcher` may also implement, discovered by
  type assertion — no second provider registry, no new config vars. Serper
  (`/images`) and ddgs (`/search/images`) implement it; Ollama Cloud does not
  and simply contributes nothing. **Take the ddgs request and response shapes
  from a live instance rather than from this document**, exactly as Stage 16
  Milestone 8 had to.

**Two things this milestone has to change on its way past.**
`config.go:201` refuses `CARAVEL_SEARCH_PROVIDER` without
`CARAVEL_LLM_URL`, which stops being true here, so the rule is relaxed and
the searcher is constructed in `cmd/caravel/main.go` and shared between the
assistant and this endpoint. And `/auth/me` gains one more flat capability
flag beside `geocoding` and `assist` (`auth.go:27-34`) — the same rail, not a
reshape, on the same reasoning Stage 16 used for the second one.

**The grid.** Thumbnails, hotlinked for the preview only; picking one sends
the *full* image URL to `media/url`, which fetches and stores it server-side
as it does today. Every result carries its source host under it, and a
Wikipedia result carries its credit and licence. Two things to be deliberate
about: a thumbnail that fails to load must not leave an invisible empty cell
— `image-field.js` already learned that lesson for its preview (`:52-57`) and
the same handler applies; and hotlinking third-party thumbnails discloses the
viewer's IP to those hosts, which is worth a line in the docs rather than a
proxy, since the alternative is streaming every thumbnail through the
instance.

**Verify.** `go test` for each backend against an `httptest.Server`, for the
Wikipedia filter (an article whose image list is mostly icons must not return
mostly icons), for the type-assertion path with a `Searcher` that does not
implement `ImageSearcher`, for the relaxed config rule in both directions, and
for the endpoint at 501 with nothing available, 403 for a viewer and 429 on
the limiter. Then Playwright against a stub-configured server: the button is
absent when the capability is off, a search renders a grid, picking a result
sets the image, and a dead thumbnail is dropped rather than left as a hole.
Then by hand with a real provider: a landmark (Wikipedia results), a hotel
(image-search results), and one with neither. German, dark mode, 324×756 —
a results grid at 324px is the interesting case.

---

## Build order

`0 → 1 → 2 → 3 → 4a → 5 → 6 → 7`, and the order carries an argument. 4b was
dropped once 4a had been measured; the reasoning is in that milestone.

The bug goes first because it is the thing that annoys daily and its fix is
small. Observability (2, 3) comes before speed (4) because optimising without
measurement is guessing, and the levers are to be re-discussed once numbers
exist rather than committed to here. The three image milestones come last as a block, and in that order:
5 lands the schema and the two sources, 6 puts them in front of the user, and
7 — the only milestone with no LLM in it — reuses 5's Wikipedia client and 6's
provenance plumbing for a control anyone can drive by hand. Doing 7 before
either would mean building the same client twice.

## Out of scope, deliberately

- **Trip-level suggestions.** Stays in `todo.md`. It reuses everything Stage
  16 built but needs a multi-result review UI, a transactional add of N
  locations and dedup against the trip — a stage, not a milestone.
- **SearXNG.** Still blocked on having an instance to verify against, and it
  overlaps almost entirely with `ddgs`, which shipped. If Milestone 7 gives
  `Searcher` an optional image capability, note in `todo.md` that a future
  SearXNG backend would want to implement it too.
- **Letting the assistant pick from an image search.** The model has no
  vision, so it would be choosing a photograph by the text around it. Image
  search is offered to the person, in Milestone 7, and not to the agent.
- **Streaming the provider's composing turn.** It would make a long wait feel
  shorter without making it shorter. The numbers say composing is 3.2s of 16.4s
  on the model this instance runs, so there is nothing left here to hide.
- **The reasoning-effort knob, prompt caching and conversation compaction.**
  All three dropped at the second checkpoint once the model switch had taken
  the run from 59s to 16.4s. Backlog, with the reasoning behind each.
- **The mobile map height.** Backlog, tagged (soon), with the scroll-trap
  warning attached.

## Workflow

The standard loop, per CLAUDE.md, one milestone at a time:

1. Implement.
2. Verify — `make ci` green, plus a manual or Playwright pass proving the
   behaviour actually changed. Assertions over screenshots.
3. Update this document with a **Done.** paragraph saying what really landed
   and how it was verified, then update `plans/todo.md` in both directions.
4. Commit — one commit per milestone, saying what changed, why, and how it
   was verified.
5. Make sure `make dev` is running, then stop and hand back control.
6. Wait. Feedback at a checkpoint is fixed and re-verified before the next
   milestone starts.

Milestone 4 has an extra step before step 1: present the measurements and
agree the levers.

### Credentials

Milestones 1, 2, 4, 5, 6 and 7 all want a live provider. Valid credentials are in
`credentials.yaml`, which is untracked and covered by `.gitignore`. Read it
with its values masked; only the non-secret URL and model name may appear in
any file, log or commit message.

## Verification

The stage is done when, on a real instance with a real provider configured:

- accepting every suggestion, one at a time or with "Accept all", leaves no
  bar and no count, and the sources list is cleared by "Dismiss all";
- `CARAVEL_LOG_LEVEL=debug` produces an account of a run from which the
  wall-clock cost of every turn and every tool call can be read, and no key or
  page body appears anywhere in it;
- the editor shows a collapsed trace after a run, in both languages, that
  matches what the log says;
- a run against the same place is measurably faster than it was at the start
  of the stage, with the before-and-after recorded in this document, and the
  proposals are no worse;
- a location with a good official site gets that site's own photograph offered
  as a cover, a landmark without one gets the Wikipedia image with its licence
  and author, and accepting either stores where it came from;
- "Search for an image" returns something useful on an instance with no search
  provider configured at all, and more when one is;
- `make ci` and `make test-postgres` are green.
