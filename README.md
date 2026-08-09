# Caravel

Caravel is a self-hosted trip planner: create a trip, fill it with locations
to visit, places to stay, and transportation, lay them out on a map, build a
day-by-day itinerary, and attach documents and photos along the way.

## Quick start

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
| `CARAVEL_OPEN_SIGNUP` | `true` | Whether `/api/auth/register` accepts new accounts. Set to `false` after creating your accounts on a instance you don't want open to the public |

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
