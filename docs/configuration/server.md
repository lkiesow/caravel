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

The container image sets `CARAVEL_DB_DSN=/data/caravel.db` and
`CARAVEL_UPLOAD_DIR=/uploads`, which is why the compose files mount volumes at
those two paths.

The assistant has its own variables, all unset by default, on [The
assistant](assistant.md).

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
