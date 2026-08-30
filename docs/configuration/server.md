# Server and database

All configuration is environment variables. Every one has a working default, so
a bare `docker compose up -d` is a complete installation — this page is what to
change when the defaults do not fit.

## The variables

| Variable | Default | Purpose |
|---|---|---|
| `CARAVEL_PORT` | `8080` | HTTP listen port |
| `CARAVEL_DB_DRIVER` | `sqlite` | `sqlite` or `postgres`. Anything else is refused at startup |
| `CARAVEL_DB_DSN` | `data/caravel.db` | SQLite file path, or a Postgres connection string |
| `CARAVEL_UPLOAD_DIR` | `uploads` | Where uploaded images and documents are stored |
| `CARAVEL_WEB_DIR` | *(unset)* | Serve the frontend live from this directory instead of the copy embedded in the binary. A development setting — see `make dev` |
| `CARAVEL_GEOCODER_URL` | OpenStreetMap Nominatim | Address-search endpoint — see [Address search](address-search.md) |
| `CARAVEL_TILE_URL` | OpenStreetMap tiles | Where the browser fetches map tiles from — see [Map tiles](map-tiles.md) |
| `CARAVEL_TILE_ATTRIBUTION` | OpenStreetMap contributors | The credit shown on the map, as HTML. Whatever provider you use, meeting its attribution terms is this variable |
| `CARAVEL_TILE_MAX_ZOOM` | `19` | How far the map may zoom in, which is a property of the provider |
| `CARAVEL_TRUSTED_PROXIES` | the private ranges | Networks whose `X-Forwarded-For` is believed when attributing a request to a client address. A comma-separated list of CIDR ranges and addresses; `none` to trust nothing. Replaces the default rather than extending it — see [Behind a reverse proxy](../running/reverse-proxy.md) |
| `CARAVEL_BASE_URL` | *(unset)* | The public origin the instance is reached under, scheme and host, no path — for example `https://caravel.example`. Used only to build the absolute URLs in the social preview tags. Unset derives it from each request, which is right behind an ordinary reverse proxy; set it if something in front rewrites `Host` |
| `CARAVEL_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` — see [Logging](#logging) |
| `CARAVEL_LOG_FORMAT` | `text` | `text` or `json` |

The container image sets `CARAVEL_DB_DSN=/data/caravel.db` and
`CARAVEL_UPLOAD_DIR=/uploads`, which is why the compose files mount volumes at
those two paths.

The assistant has its own variables, all unset by default, on [The
assistant](assistant.md).

The three tile variables are worth a page of their own, because the reason to
touch them is rarely obvious from the names: the default tiles label places in
the local script, so a trip to Japan reads 東京 rather than Tokyo, and no
setting on those tiles changes it. See [Map tiles](map-tiles.md) for the
providers that do.

## Where to put them

The repository ships `.env.sample`: every variable above and every one on the
assistant page, commented out at its default, with a line on what each does.
Copy it beside the compose file and uncomment what you want to change —

```sh
cp .env.sample .env
```

— because both compose files read `.env` if it exists. It is gitignored, which
is the point: `.env.sample` never holds a value, so the file with your API key
in it is not the file tracked in git.

An RPM installation has the same thing at `/etc/caravel/caravel.conf`, read by
the systemd unit as an `EnvironmentFile` and marked `noreplace` so an upgrade
does not overwrite it. A prebuilt binary under systemd wants an
`EnvironmentFile` of its own, in the same format.

## Logging

The server logs to standard error, one record per line, at `info` by default.
`docker logs caravel` or `journalctl -u caravel` is where to read it.

`CARAVEL_LOG_FORMAT=json` swaps the format for one a collector can parse.
Text is the default because the log of a self-hosted instance is usually read
by a person, and one line beats a wall of JSON when you are looking at it
directly.

### Debug is how the assistant explains itself

`CARAVEL_LOG_LEVEL=debug` is worth knowing about specifically because of the
[assistant](assistant.md). A run takes half a minute or more and, at any other
level, says nothing at all about where that time went. At `debug` it accounts
for itself:

```
level=DEBUG msg="assist: run started" run=1 mode=enrich model=... search=serper
level=DEBUG msg="assist: turn" run=1 turn=1 ms=2104 finish=tool_calls spent_tokens=1450 tool_calls=1
level=DEBUG msg="assist: tool call" run=1 name=web_search args="{\"query\":\"...\"}" ms=880 ok=true result_bytes=1523
level=DEBUG msg="assist: tool call" run=1 name=fetch_page args="{\"url\":\"...\"}" ms=1310 ok=true result_bytes=11702
level=DEBUG msg="assist: gathering finished" run=1 reason=answered turns=4 tool_calls=5 spent_tokens=48210 ms=21400
level=DEBUG msg="assist: composed" run=1 ms=18900 messages=14 tokens=13400 ok=true
level=DEBUG msg="assist: run finished" run=1 ms=41200 fields=4 links=2 sources=3
```

Every record carries a `run` number, so two people using it at once can be
told apart in a log that interleaves them. `reason` on the gathering line says
why the research stopped — `answered`, `deadline`, `budget`, `turn_ceiling` or
`tool_call_ceiling` — which is the difference between a model that finished and
one that ran out of something.

Two things deliberately never appear in it: the API key, and the text of a
fetched page. The page URL and the number of bytes extracted from it are
what a person debugging actually needs, and the body is up to 12 KB of somebody
else content per read.

This is verbose and it is emitted per run, so it is not a level to leave on for
an instance other people use. Nothing above `debug` is logged for an ordinary
run, though, so turning it on to answer a question and off again costs nothing.

A run that *fails* is logged at `error` whatever the level is set to, with the
provider's actual complaint — the browser only ever sees a fixed sentence, so
this is the only place the real cause is written down.

## Bad values stop the server

A typo in a configuration value fails at startup with the variable named, rather
than falling back to a default nobody asked for. `CARAVEL_DB_DRIVER=postgress`
does not start; neither does a malformed number or duration. This is deliberate:
silently ignoring `CARAVEL_ASSIST_MAX_TOKENS=12O000` — with a letter O — is how
somebody spends a week wondering why their change did nothing.

## SQLite

The default, and nothing to install. The file is created and migrated on
startup. Suitable well past the size a household reaches: it is a single
process, and reads do not block each other.

## Postgres

For an existing Postgres you would rather keep everything in:

```sh
CARAVEL_DB_DRIVER=postgres
CARAVEL_DB_DSN=postgres://caravel:password@localhost:5432/caravel?sslmode=disable
```

Migrations run automatically on startup for both dialects — there is no separate
migrate step.

!!! warning "Choose once"

    Nothing moves data between the two dialects. Caravel will happily create its
    schema in either, but switching later means starting empty.

## Uploads are always local disk

Whichever database you pick, uploaded files are stored on the filesystem at
`CARAVEL_UPLOAD_DIR` — not in the database, and not in object storage. That is
why [Backup and restore](../running/backup.md) insists on backing up the
database and the upload directory *together*.

## Test-only variables

These exist for the test suite and have no effect on a running instance. They
are listed so that seeing one in a shell history is not a mystery.

| Variable | Purpose |
|---|---|
| `CARAVEL_TEST_DB_DRIVER` | Set to `postgres` to run `go test ./...` against Postgres instead of SQLite — see `make test-postgres` |
| `CARAVEL_TEST_DB_DSN` | The Postgres server those tests create a schema in, per test |
