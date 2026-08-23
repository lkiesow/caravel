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

Then open <http://localhost:8080> and register. A fresh instance with no
accounts always allows the first registration, and that account becomes the
admin; after it exists, registration is closed to everyone else.

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
be healthy before starting. Note there is **no migration path between the two
dialects** — pick one before you put real trips in it.

## Where the data lives

Two volumes, and they belong together:

| Path in the container | Holds |
|---|---|
| `/data` | The SQLite database (`caravel.db`), when running on SQLite |
| `/uploads` | Every uploaded photo and document |

Uploads are files on disk, not blobs in the database, so a backup of one
without the other restores to a database full of references to pictures that no
longer exist. Back them up in the same operation.

## Configuration

Everything is set through environment variables, and every one has a working
default. The compose files read an optional `.env` next to them, which is where
a real deployment puts its settings:

```sh
CARAVEL_PORT=8080
```

The full list is in the [repository README](https://github.com/lkiesow/caravel#configuration)
until it moves here.

## From source

The development path, and also fine for running it:

```sh
make run
```

That builds and runs the server against a SQLite database created at
`data/caravel.db`, serving on <http://localhost:8080>, with uploads in
`uploads/`. Both directories are created for you.

For frontend work, `make dev` serves `web/` from disk so an edit needs only a
browser refresh.

## Upgrading

Pull the new image and restart. Schema migrations run automatically at startup,
so there is no separate step:

```sh
docker compose pull
docker compose up -d
```

!!! warning "Databases created before the first release"

    The migration history was squashed to a single initial migration before any
    image was published. A database created by a pre-release checkout cannot be
    migrated forward and has to be recreated. If you are installing from a
    published image, this does not apply to you.

## Checking it is running

The container has a healthcheck, and the endpoint behind it reports which build
is actually running:

```sh
curl http://localhost:8080/api/health
```

```json
{"status":"ok","version":"v1.0.0"}
```

That version string is stamped into the binary at build time, which makes
"which build is this?" answerable on a running instance rather than a guess
from the tag you think you deployed.
