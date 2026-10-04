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

Optional but strongly recommended: without it the assistant has only what the
model already knows and the pages it can name itself. Set
`CARAVEL_SEARCH_PROVIDER` and its key or URL — [Web search](web-search.md)
compares the providers and how to set each one up. The same setting also powers
the [image picker](images.md)'s web search, so it does not need the assistant to
be on.

## Coordinates are never taken from the model

The model proposes a **name** and an **address**, and the position is looked up
separately. A plausible latitude and longitude 40km from the real hotel looks
entirely correct in the form and is wrong only on the map — the one error with
no visible tell.

Two things are asked, when both are available:

- **`CARAVEL_GEOCODER_URL`**, the address search, is asked for the place name
  first and the postal address only if that finds nothing. The order matters: a
  postal address is a question about a delivery point, and Nominatim answers it
  with a house-number node, an interpolated point along the street, or the
  street itself — a pin outside the door rather than on it. Searching the name
  finds the element somebody actually mapped. The address still earns its place
  as the fallback: it is what positions a rented flat with no findable name.
- **A places search**, when the [search provider](web-search.md) is `serper`
  or `brave`. Serper's is Google Maps data, and it is far better than
  OpenStreetMap on the restaurants, cafés, bars, shops and hotels a trip is
  mostly made of — for those, its pin is the business's own position rather
  than an address interpolation. Brave's agrees with it to the metre for most
  places, and is given the postal address as the area to search in, which
  keeps a name with no town in it near the address rather than at the
  best-known place of that name. Either costs one paid request per lookup,
  and up to six for one trip-level suggestion run. With `ollama`, `ddgs` or
  none, only the geocoder is asked and everything still works.

Where the two agree, a precise OpenStreetMap match wins, because it is the only
one of the two that carries an OSM element identity — which is what makes the
"view on OpenStreetMap" link on a location possible. Where OpenStreetMap only
found a street, the places search wins.

Where they disagree by more than 150 metres, **neither is trusted**. Both are
offered and nothing is preselected, the row is skipped by *Accept all*, and on
the trip-level suggestions screen the place is added with its address and no pin
for you to set on the map. Picking between two places kilometres apart is not a
decision to make on somebody's behalf.

The proposed position always shows the **name of the place that was matched**
rather than only its coordinates, plus which service found it, and says so when
the match is street-level. Six decimal places is not something anybody can
check; a name is.

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
