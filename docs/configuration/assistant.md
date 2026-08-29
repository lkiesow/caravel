# The assistant

The location editor can look a place up on the web and suggest a category,
tags, notes, an address, links and coordinates. Every suggestion is accepted or
rejected **per field**, anything that would replace existing text is marked as
such, and nothing is written until you press Save.

It is **off unless you configure it**. When it is off the endpoint is not usable
and the control does not render. It needs a model endpoint and usually an API
key — infrastructure not everyone has, and which can cost money — so it is
opt-in rather than something to switch off.

!!! info "Environment variables only, never the database"

    An API key in the database is an API key in every backup and every dump you
    share while debugging. That is also why there is no admin screen for these:
    the instance owner sets them where secrets already live.

## Turning it on

| Variable | Purpose |
|---|---|
| `CARAVEL_LLM_URL` | An OpenAI-compatible endpoint. Either the base URL the provider documents (`https://openrouter.ai/api/v1`) or the full `/chat/completions` path. The value `stub` selects a built-in fake, used by the test suite |
| `CARAVEL_LLM_KEY` | Bearer token. Omit for a local Ollama or llama.cpp that needs none |
| `CARAVEL_LLM_MODEL` | Model name. Required whenever `CARAVEL_LLM_URL` is set |

Setting one of the URL/model pair without the other is refused at startup rather
than at first use, so a half-configured instance fails immediately instead of
when somebody presses the button.

## Web search

Optional but strongly recommended: without it the assistant has only
OpenStreetMap and whatever the model already knows. Pick one and set
`CARAVEL_SEARCH_PROVIDER`, plus `CARAVEL_SEARCH_KEY` or `CARAVEL_SEARCH_URL` as
the table says. There is no default — the right choice depends on what you are
willing to run and pay for.

| Provider | Needs | Runs where | Notes |
|---|---|---|---|
| `ollama` | `CARAVEL_SEARCH_KEY` | hosted | Ollama Cloud. Free tier; if you already use Ollama for the model, one account covers both |
| `serper` | `CARAVEL_SEARCH_KEY` | hosted | Real Google results via API. Cheap per query, and the only option here that is neither scraping nor something you host |
| `ddgs` | `CARAVEL_SEARCH_URL` | your own host | [DDGS](https://github.com/deedy5/ddgs): `pip install ddgs[api]`, then `ddgs api`, and point the URL at it. No key, no account |
| `stub` | — | in-process | A fake for tests. Never a real answer |

A search provider set without `CARAVEL_LLM_URL` used to be refused at startup,
back when nothing else in the app searched the web. The image picker now uses the
same backend, so the two are independent — see
[finding an image](images.md).

Two honest caveats about `ddgs`, since it is the keyless option and therefore
tempting. It works by **scraping** search engines, so a backend can break when
someone changes their markup — it aggregates several and falls back between
them, which softens this a lot. And scraped engines rate-limit datacenter
addresses, so it suits a home server better than a VPS. Scraping Google and Bing
is also against their terms of service.

## Coordinates are never taken from the model

The model proposes an address, and `CARAVEL_GEOCODER_URL` resolves it. A
plausible latitude and longitude 40km from the real hotel looks entirely correct
in the form and is wrong only on the map — the one error with no visible tell.

## Limits

Every guard rail is settable, because these are the numbers worth changing
quickly when a model turns out chattier or a bill turns out larger. The defaults
are tuned against a real model and a real search backend.

| Variable | Default | Bounds |
|---|---|---|
| `CARAVEL_ASSIST_MAX_TOKENS` | `120000` | Tokens one run may spend |
| `CARAVEL_ASSIST_ANSWER_RESERVE` | `20000` | Held back from the above, so there is always enough left to write the answer |
| `CARAVEL_ASSIST_MAX_TURNS` | `12` | Conversation turns |
| `CARAVEL_ASSIST_MAX_TOOL_CALLS` | `20` | Searches and page reads |
| `CARAVEL_ASSIST_TIMEOUT` | `90s` | Time spent researching |
| `CARAVEL_ASSIST_ANSWER_TIMEOUT` | `2m` | Time to compose the answer, outside the above |
| `CARAVEL_ASSIST_RATE_LIMIT` | `6` | Runs per minute, per client address |
| `CARAVEL_ASSIST_MAX_CONCURRENT` | `4` | Runs in flight at once, across the instance |

Three things worth knowing.

The token budget counts **billed** tokens rather than context size: every turn
resends the whole conversation, so a long run costs more than the numbers
suggest.

The first six bound *one run*. The last two are what bound an instance, so the
worst case is roughly them multiplied together. Of those two,
`CARAVEL_ASSIST_MAX_CONCURRENT` is the one that actually caps a bill:
`CARAVEL_ASSIST_RATE_LIMIT` is per client address, so ten people on ten
addresses get ten allowances, not one shared between them.

Hitting a limit does not throw the run away: research stops and the assistant
answers with what it found.

The effective values are printed at startup when the assistant is enabled, so
the log is the place to confirm a change took.
