# Install

Caravel ships as a container image and a compose file, so the whole install is
one command and a browser. Everything below assumes Docker or Podman and
nothing else — no Go toolchain, no database to set up by hand.

## With Docker Compose

```sh
git clone https://github.com/lkiesow/caravel.git
cd caravel
docker compose up -d
```

Then open <http://localhost:8080>. What to do there is [First
run](first-run.md).

Podman works the same way (`podman compose up -d`).

## SQLite or Postgres

`docker-compose.yml` runs Caravel on SQLite, which is the right default: one
household planning trips together is nowhere near what SQLite can take, and it
means one fewer container to run and back up.

Use the other file if you already run Postgres and would rather have Caravel in
it:

```sh
docker compose -f docker-compose.postgres.yml up -d
```

That file brings up a `postgres:17-alpine` alongside the app and waits for it to
be healthy before starting.

!!! warning "There is no migration path between the two dialects"

    Caravel can create its schema in either, but nothing moves data from one to
    the other. Pick one before you put real trips in it.

## Where the data lives

Two volumes, and they belong together:

| Path in the container | Holds |
|---|---|
| `/data` | The SQLite database (`caravel.db`), when running on SQLite |
| `/uploads` | Every uploaded photo and document |

Uploads are files on disk, not blobs in the database, so a backup of one without
the other restores to a database full of references to pictures that no longer
exist. See [Backup and restore](../running/backup.md).

## Configuring it

Everything is set through environment variables, and every one has a working
default — a bare `docker compose up -d` is a complete installation. The compose
files read an optional `.env` beside them, which is where a real deployment puts
its settings:

```sh
# .env
CARAVEL_PORT=8080
```

See [Server and database](../configuration/server.md) for the full list. Two
things are worth turning on once it is running: an [address
search](../configuration/address-search.md) endpoint of your own, and
optionally [the assistant](../configuration/assistant.md).

## From source

The development path, and also fine for running it. Needs Go 1.26:

```sh
make run
```

That builds and runs the server against a SQLite database created at
`data/caravel.db`, serving on <http://localhost:8080>, with uploads in
`uploads/`. Both directories are created for you.

For frontend work, `make dev` serves `web/` from disk instead of the copy
embedded in the binary, so an edit to anything under `web/` needs only a browser
refresh. Backend changes still need a restart.

## Checking it is running

The container has a healthcheck, and the endpoint behind it reports which build
is actually running:

```sh
curl http://localhost:8080/api/health
```

```json
{"status":"ok","version":"v1.0.0"}
```

That version string is stamped into the binary at build time, which makes "which
build is this?" answerable on a running instance rather than a guess from the
tag you think you deployed.
