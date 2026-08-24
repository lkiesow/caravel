<p align="center">
  <img src="docs/assets/brand/readme-banner.png" alt="Caravel — explore the world" width="820" />
</p>

Caravel is a self-hosted trip planner: create a trip, fill it with locations to
visit, places to stay, and transportation, lay them out on a map, build a
day-by-day itinerary, split the costs, and attach documents and photos along the
way.

**[Documentation and project website →](https://lkiesow.github.io/caravel/)**

## Quick start

With Docker or Podman, and nothing else installed:

```sh
docker compose up -d
```

Then open [http://localhost:8080](http://localhost:8080) and register — a fresh
instance with no accounts always allows the first one, and it becomes the admin.
Use `docker-compose.postgres.yml` instead to run against Postgres rather than
SQLite. Images are published to GHCR for `linux/amd64` and `linux/arm64`; to
build from the checkout instead, uncomment the `build:` line in the compose file
and add `--build`.

The full install guide, every configuration variable, the optional AI assistant,
backups and reverse-proxy setup are all on the
[documentation site](https://lkiesow.github.io/caravel/).

## Development

Needs Go 1.26. No npm dependencies ship with the app — the frontend is vanilla
JS, embedded into the binary.

```sh
make run     # build and run against a local SQLite database
make dev     # serve web/ from disk, so a frontend edit needs only a refresh
make ci      # what CI runs: build, vet, JS syntax, i18n parity, go test
```

Other targets worth knowing:

| Target | Does |
|---|---|
| `make dev-seed` | Seeds a demo user and trip for manual testing |
| `make test-ui` | The Playwright suite, against a throwaway server it starts and seeds itself |
| `make test-postgres` | The Go suite against a Postgres container, rather than SQLite |
| `make docs` | Builds the documentation site into `site/` |

Conventions for working in this repository — the stage-based workflow, the
gotchas worth knowing before touching migrations or `sqlc` — are in
[CLAUDE.md](CLAUDE.md), and the plans and backlog are under [plans/](plans/).

## Architecture

A single Go binary with the frontend embedded, and no runtime dependencies
beyond a database:

- **Backend** — Go 1.26, `net/http` with chi for routing, `sqlc`-generated
  queries against SQLite (default) or Postgres, migrations run at startup.
- **Frontend** — vanilla JS modules, no framework and no build step. Served
  from the binary in production, from disk in development.
- **Storage** — uploaded images and documents on local disk, not in the
  database.

## Licence

Caravel is free software under the **GNU Affero General Public License v3.0** —
see [LICENSE](LICENSE). The AGPL's network clause matters for a self-hosted web
app: if you run a modified version and let other people use it over a network,
they are entitled to its source.

The bundled Montserrat subset is used under the SIL Open Font License — see
`web/fonts/OFL.txt`.
