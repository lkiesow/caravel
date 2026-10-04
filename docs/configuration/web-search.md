# Web search

One setting connects Caravel to a web-search backend, and three features use it:
the [assistant](assistant.md) researching a place, the [image
picker](images.md) finding a photo of it, and the assistant's second opinion on
where a place is. It is optional — everything works without it, with less to go
on.

There is no default provider. The right choice depends on what you are willing
to run and pay for, and the providers do not all offer the same features.

## Turning it on

| Variable | Purpose |
|---|---|
| `CARAVEL_SEARCH_PROVIDER` | `ollama`, `serper`, `ddgs` or `stub`. Empty — the default — means no web search |
| `CARAVEL_SEARCH_KEY` | The API key, for the hosted providers |
| `CARAVEL_SEARCH_URL` | The service root for `ddgs`, which you run yourself, so it has no address to default to. For the hosted providers it is an optional override of their endpoint, for pointing at a proxy |

A provider missing its key or URL is refused at startup rather than at first
use. The search provider is independent of `CARAVEL_LLM_URL`: a provider on its
own is a working configuration, and gives you web image search with no LLM
anywhere near it.

## What each provider can do

| | `ollama` | `serper` | `ddgs` |
|---|:-:|:-:|:-:|
| [Web search](#what-uses-it) for the assistant | ✅ | ✅ | ✅ |
| [Image search](#what-uses-it) in the image picker | — | ✅ | ✅ |
| [Place lookup](#what-uses-it) for the assistant's pins | — | ✅ | — |
| Needs | key | key | URL |
| Runs where | hosted | hosted | your own host |
| Costs | free tier | per query | nothing |

A feature a provider lacks is not an error. Each one falls back to what works
without it, described [below](#what-uses-it).

The startup log says what the web half of the image picker ended up with, as
`image_search_web=serper`, `image_search_web=ollama (no image search)` or
`image_search_web=none`.

## The providers

### `ollama` — Ollama Cloud

[Ollama Cloud](https://ollama.com)'s web search API. Hosted, with a free tier,
and if you already use Ollama for the model, one account and one key cover
both. It is a web search and nothing else: no images, no places.

```sh
CARAVEL_SEARCH_PROVIDER=ollama
CARAVEL_SEARCH_KEY=...
```

### `serper` — Google, through an API

[Serper](https://serper.dev) returns real Google results through an API. It is
the only option here that is neither scraping nor something you host, and the
only one that offers all three features — including Google Maps data for place
lookup.

The trade is money: every query is paid for, and place lookup adds one per
place on top of the searches, up to six for one trip-level suggestion run.

```sh
CARAVEL_SEARCH_PROVIDER=serper
CARAVEL_SEARCH_KEY=...
```

### `ddgs` — self-hosted metasearch

[DDGS](https://github.com/deedy5/ddgs) is a metasearch library with a built-in
API server, which you run yourself. No key, no account:

```sh
pip install "ddgs[api]"
ddgs api
```

```sh
CARAVEL_SEARCH_PROVIDER=ddgs
CARAVEL_SEARCH_URL=http://localhost:8000
```

Two honest caveats, since it is the keyless option and therefore tempting. It
works by **scraping** search engines — Bing, Brave, DuckDuckGo, Google and
others — so one can break when someone changes their markup. It aggregates
several and falls back between them, which softens this a lot. And scraped
engines rate-limit datacenter addresses, so it suits a home server better than a
VPS. Scraping Google and Bing is also against their terms of service.

### `stub`

A built-in fake used by the test suite. It answers from a small fixed table that
points at addresses which cannot resolve. Never a real answer.

## What uses it

**The assistant's research.** The assistant searches the web to find out about
a place before it reads any pages. Without a provider it has only what the model
already knows and the pages it can name itself — a worse assistant, but a
working one. See [The assistant](assistant.md).

**Image search.** The image picker always searches Wikipedia, which needs no
configuration. With `serper` or `ddgs` it also runs a web image search, which is
far better for hotels and restaurants but cannot tell you the licence of what it
finds. See [Finding an image](images.md).

**Place lookup.** The assistant never takes coordinates from the model; it looks
the place up instead. The address search is always asked. With `serper`,
Google Maps is asked as well, which is much better than OpenStreetMap for
restaurants, cafés, bars, shops and hotels. With any other provider, only the
address search is asked. See [Coordinates are never taken from the
model](assistant.md#coordinates-are-never-taken-from-the-model).

Address search when you type into a location is a separate thing and does not
use this setting at all — see [Address search](address-search.md).
