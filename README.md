<p align="center">
  <img src="docs/assets/brand/readme-banner.png" alt="Caravel — explore the world" width="820" />
</p>

Caravel is a self-hosted trip planner: create a trip, fill it with locations
to visit, places to stay, and transportation, lay them out on a map, build a
day-by-day itinerary, and attach documents and photos along the way.

## Quick start

With Docker or Podman, and nothing else installed:

```sh
docker compose up -d
```

Then open [http://localhost:8080](http://localhost:8080) and register — a fresh
instance with no accounts always allows the first one, and it becomes the admin.
Use `docker-compose.postgres.yml` instead to run against Postgres rather than
SQLite. (If the pull fails with `403 Forbidden`, no image has been published for
this repository yet — uncomment the `build:` line and add `--build` to build it
from the checkout.)

From source, which is also the development path:

```sh
make run
```

This builds and runs the server against a local SQLite database (created
automatically at `data/caravel.db`), serving the app at
[http://localhost:8080](http://localhost:8080). Uploaded files land in
`uploads/`. Both directories are created automatically and are gitignored.

For frontend development with live-reload (edit a file under `web/`, refresh
the browser, no rebuild needed):

```sh
make dev
```

## Configuration

All configuration is via environment variables; every one has a sensible
default for local/self-hosted use.

| Variable | Default | Purpose |
|---|---|---|
| `CARAVEL_PORT` | `8080` | HTTP listen port |
| `CARAVEL_DB_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `CARAVEL_DB_DSN` | `data/caravel.db` | SQLite file path, or a Postgres connection string (e.g. `postgres://user:pass@host:5432/caravel?sslmode=disable`) |
| `CARAVEL_UPLOAD_DIR` | `uploads` | Where uploaded images/documents are stored |
| `CARAVEL_WEB_DIR` | *(unset)* | If set, serves frontend files live from this directory instead of the binary's embedded copy — the dev workflow (see `make dev`) |
| `CARAVEL_TEST_DB_DRIVER` | `sqlite` | *Tests only.* Set to `postgres` (with `CARAVEL_TEST_DB_DSN`) to run `go test ./...` against the other dialect — see `make test-postgres` and `internal/dbtest` |
| `CARAVEL_TEST_DB_DSN` | *(unset)* | *Tests only.* The Postgres server the tests create a schema in, per test |
| `CARAVEL_GEOCODER_URL` | OpenStreetMap Nominatim | Address-search endpoint. Set it empty to switch address search off entirely — the API then reports the capability as absent and the UI hides the control |

## AI assistant (optional, off by default)

The location editor can look a place up on the web and suggest a category,
type, notes, an address, links and coordinates. Every suggestion is accepted or
rejected **per field**, with anything that would replace existing text marked as
such, and nothing is written until you press Save.

It is **off unless you configure it**, and when it is off the endpoint is not
usable and the control does not render. It needs a model endpoint and usually
an API key, which is infrastructure not everyone has and which can cost money —
so it is opt-in rather than something to switch off.

Configuration is **environment variables only, never the database**. An API key
in the database is an API key in every backup and every dump you share while
debugging.

### Turning it on

| Variable | Purpose |
|---|---|
| `CARAVEL_LLM_URL` | An OpenAI-compatible endpoint. Either the base URL the provider documents (`https://openrouter.ai/api/v1`) or the full `/chat/completions` path. The value `stub` selects a built-in fake, used by the test suite |
| `CARAVEL_LLM_KEY` | Bearer token. Omit for a local Ollama or llama.cpp that needs none |
| `CARAVEL_LLM_MODEL` | Model name. Required whenever `CARAVEL_LLM_URL` is set |

Setting one of the URL/model pair without the other is refused at startup
rather than at first use, so a half-configured instance fails immediately
instead of when somebody presses the button.

### Web search

Optional but strongly recommended: without it the assistant has only
OpenStreetMap and whatever the model already knows. Pick one and set
`CARAVEL_SEARCH_PROVIDER`, plus `CARAVEL_SEARCH_KEY` or `CARAVEL_SEARCH_URL` as
the table says. There is no default — the right choice depends on what you are
willing to run and pay for.

| Provider | Needs | Runs where | Notes |
|---|---|---|---|
| `ollama` | `CARAVEL_SEARCH_KEY` | hosted | Ollama Cloud. Free tier; if you already use Ollama for the model, one account covers both |
| `serper` | `CARAVEL_SEARCH_KEY` | hosted | Real Google results via API. Cheap per query, and the only option here that is neither scraping nor something you host |
| `ddgs` | `CARAVEL_SEARCH_URL` | your own host | [DDGS](https://github.com/deedy5/ddgs): `pip install ddgs[api]` then `ddgs api`, and point the URL at it (default `http://localhost:8000`). No key, no account |
| `stub` | — | in-process | A fake for tests. Never a real answer |

Two honest caveats about `ddgs`, since it is the keyless option and therefore
tempting: it works by **scraping** search engines, so a backend can break when
someone changes their markup (it aggregates several and falls back between
them, which softens this a lot); and scraped engines rate-limit datacenter
addresses, so it suits a home server better than a VPS. Scraping Google and
Bing is also against their terms of service.

Coordinates are never taken from the model. It proposes an address, and
`CARAVEL_GEOCODER_URL` resolves it — a plausible latitude and longitude 40km
from the real hotel looks entirely correct in the form and is wrong only on the
map, which is the one error with no visible tell.

### Limits

Every guard rail is settable, because these are the numbers worth changing
quickly when a model turns out chattier or a bill turns out larger. The
defaults are tuned against a real model and a real search backend.

| Variable | Default | Bounds |
|---|---|---|
| `CARAVEL_ASSIST_MAX_TOKENS` | `120000` | Tokens one run may spend |
| `CARAVEL_ASSIST_ANSWER_RESERVE` | `20000` | Held back from the above so there is always enough left to write the answer |
| `CARAVEL_ASSIST_MAX_TURNS` | `12` | Conversation turns |
| `CARAVEL_ASSIST_MAX_TOOL_CALLS` | `20` | Searches and page reads |
| `CARAVEL_ASSIST_TIMEOUT` | `90s` | Time spent researching |
| `CARAVEL_ASSIST_ANSWER_TIMEOUT` | `2m` | Time to compose the answer, outside the above |
| `CARAVEL_ASSIST_RATE_LIMIT` | `6` | Runs per minute, per client address |
| `CARAVEL_ASSIST_MAX_CONCURRENT` | `4` | Runs in flight at once, across the instance |

Two things worth knowing. The token budget counts **billed** tokens rather than
context size: every turn resends the conversation, so a long run costs more
than the numbers suggest. And the first six bound *one run* — the last two are
what bound an instance, so the worst case is roughly them multiplied together.
Hitting a limit does not throw the run away: research stops and the assistant
answers with what it found.

The effective values are printed at startup when the assistant is enabled.

## Expenses

Each trip has an Expenses tab: what something cost, who paid, and who it was
for. There is nothing to configure.

**One currency per trip**, chosen when you create it and changeable in the trip
settings (EUR by default). Every amount is stored as a whole number of the
currency's smallest unit — cents for EUR, whole yen for JPY — so nothing is ever
a fraction of a cent out. Changing a trip's currency does **not** convert the
amounts already recorded, so a foreign-currency purchase is best entered as the
converted amount.

**Every expense on a trip is visible to everyone on it**, including viewers.
There is deliberately no private expense: hidden rows in a shared ledger make an
incorrect total look correct.

On a trip you are sharing, an expense also records **who paid** and **who it was
for**. By default it is split evenly between everybody; unticking "Split with
everyone" lets you pin it to some of them, and rows split that way say so, so
the totals can be followed back to the expenses they came from. From that,
Caravel works out where everyone stands and suggests a short set of payments
that settles up. An expense nobody is recorded as having paid is left out of
those balances and reported separately, rather than being split between people
who may not owe it.

Nothing here marks a debt as paid — the settle-up list is advice, not a record.

## Database

SQLite is the zero-config default — nothing to install, the file is created
and migrated automatically on startup. For a larger or multi-user
installation, switch to Postgres:

```sh
CARAVEL_DB_DRIVER=postgres \
CARAVEL_DB_DSN="postgres://caravel:password@localhost:5432/caravel?sslmode=disable" \
make run
```

Migrations run automatically on startup for both dialects — there's no
separate migrate step to remember.

## Backup and restore

**SQLite (default):** the entire database is one file. With the app stopped
(or accepting brief write pauses is fine — SQLite's WAL mode makes a live
copy safe in practice, but stopping the app guarantees a clean snapshot):

```sh
cp data/caravel.db data/caravel.db.bak       # backup
cp data/caravel.db.bak data/caravel.db       # restore
```

Uploaded files live outside the database — back up `uploads/` alongside it
(e.g. `tar czf backup.tar.gz data/caravel.db uploads/`). A restore needs
both: the database references files by path, and the files aren't useful
without the metadata pointing at them.

**Postgres:** use standard Postgres tooling (`pg_dump`/`pg_restore`, or your
hosting provider's snapshot mechanism) for the database, and back up
`uploads/` (or wherever `CARAVEL_UPLOAD_DIR` points) the same way as above —
Caravel's file storage is local-disk in v1 regardless of which database
you're using.

## Security notes

A few deliberate decisions worth knowing about if you're evaluating or
extending this app:

- **Sessions**: opaque random tokens in an `HttpOnly`, `SameSite=Lax` cookie;
  only a SHA-256 hash of the token is stored server-side, so a database leak
  doesn't hand over usable sessions. `Secure` is set whenever the request is
  HTTPS directly or arrives via a reverse proxy that sets
  `X-Forwarded-Proto: https` — deploy behind a TLS-terminating reverse proxy
  (Caddy, nginx, Traefik, ...) in production.
- **CSRF**: there is no separate CSRF token. `SameSite=Lax` cookies aren't
  sent on cross-site `POST`/`PUT`/`PATCH`/`DELETE` requests (only on
  cross-site top-level *navigation*, which is always `GET` in this app), so
  a malicious site can't ride a logged-in user's session to mutate data —
  the request would arrive with no session cookie at all and get a 401. This
  was a deliberate choice to avoid token-passing machinery that wouldn't add
  real protection on top of `SameSite=Lax` for a same-origin JSON API with
  no cross-origin form-style mutations. Revisit this if the app ever accepts
  credentials from a different origin (e.g. a future public API).
- **Login rate limiting**: an in-memory, per-IP fixed-window limiter (10
  attempts/minute) guards `/api/auth/login` and `/api/auth/register` against
  brute-force/spam. It's in-process and resets on restart — appropriate for
  a single-instance self-hosted app, not meant to survive a horizontally
  scaled deployment.
- **Uploads**: images are re-decoded and re-encoded server-side (never
  trusting the client's claimed content type), which also caps dimensions
  and strips anything that isn't valid image data. Documents keep their
  original bytes and claimed content type, but are always served with
  `Content-Disposition: attachment`, which stops browsers from executing an
  uploaded HTML/SVG file inline if someone opens the download link directly.
  Filenames are sanitized to a basename before being used in a storage path
  (no directory traversal), and every stored key is additionally checked in
  `internal/storagefs` before touching disk.
- **Session/rate-limiter cleanup**: a background goroutine sweeps expired
  sessions from the database hourly, and another prunes the login rate
  limiter's in-memory state every 10 minutes, so neither grows unbounded on
  a long-running instance.

## Internationalization

English and German are supported out of the box; language is auto-detected
from the browser and can be overridden per-browser (stored in
`localStorage`). Translation files live in `web/locales/`.

## Progressive Web App

Caravel is installable on mobile and desktop (manifest + service worker,
app-shell caching). In development (`CARAVEL_WEB_DIR` set), the service
worker deliberately skips caching anything served with a `no-cache` header
so live-reload keeps working.
