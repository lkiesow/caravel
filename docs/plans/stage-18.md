# Stage 18 — Release readiness: brand, containers, docs site

*Milestone 9 moves this file: `docs/plans/` becomes `plans/`, so that `docs/`
can be the documentation site's source. Until then it lives here.*

## Context

Seventeen stages have built an app that only ever runs one way: `go run`, on
SQLite, from a git checkout, on the author's laptop — and that presents itself
with the word "Caravel" in a `<strong>` tag. This stage is about everything
between "it works for me" and "somebody else can find it, run it and recognise
it".

Three strands, all of them tagged **(soon)** in `todo.md` or newly asked for:

**Brand.** A full asset set exists in `logo/assets/` (direction 2d, the folded
sail) and nothing in the app uses it: `web/icons/` still holds the placeholder
favicons, the header is a bold text string, and the login screen is a bare card.
Two mockups define the target — a landing hero ("Every trip, one place.", the
sail as a large low-contrast watermark, a filled blue primary and an outlined
secondary, in both themes) and the lockup/app-header treatment (mark + tracked
`CARAVEL` wordmark, avatar right). The palette in the assets' own README is
navy `#23304F`, lightened navy `#5470A8` on dark grounds, cream `#FAF7F2`, and
app blue `#2563EB` — which is already exactly `--color-accent`, so the app and
the brand do not have to be reconciled, only connected.

**Containers.** There is no Dockerfile, no compose file and no
image-publishing workflow (`.github/workflows/` holds only `ci.yml`), so nobody
else can run Caravel. And **nothing ever runs the Postgres dialect**: every
test, the seeder and the dev server use SQLite, so the Postgres half of every
query change since Stage 01 is verified *only by compiling*. `todo.md` has the
measured example — `SearchUsers` lowercases its pattern purely for Postgres, and
deleting that leaves `TestSearchUsers` green. The compose file does double duty:
it is also the Postgres container that gap needs. There are also **twelve**
migration pairs (`todo.md` still says ten); squashing them to one `0001_init` is
safe while nobody has deployed and stops being safe the moment an image is
published, so it has to land before the publishing workflow — and after the
Postgres job, or the Postgres `0001_init` is one large hand-written file nothing
ever executes.

**Documentation and a project website.** `README.md` is the only documentation
and it assumes a Go toolchain and a git checkout. The site is built with
[Zensical](https://zensical.org/) — the successor to mkdocs-material — published
to GitHub Pages, with the hero mockup as its landing page.

Decisions taken with the user up front:

- Registry: **GHCR** (`GITHUB_TOKEN` suffices, no secrets to add).
- Compose: **two files** — a SQLite default, and a Postgres one that doubles as
  the dev/CI database.
- Fonts: **self-hosted subset**. `fontTools` 4.62 and `brotli` are installed and
  the OFL Montserrat OTFs are in `/usr/share/fonts/julietaula-montserrat-fonts/`,
  so a woff2 subset is generated offline — no Google Fonts request from a
  self-hosted instance.
- Redesign scope: **brand + header + login hero.** The rest of the app keeps its
  current palette; a full navy/cream repaint is explicitly not this stage.
- Docs live in **`./docs`** (the conventional Zensical layout) and the stage
  plans move to **`./plans`**, which means updating `CLAUDE.md` and the two other
  files that name `docs/plans`.
- Site shape: **one site** — hero landing page at the root, documentation
  underneath.

**Zensical is already evaluated, not assumed.** Scaffolded in a scratch
directory with the installed 0.0.57: `zensical build` works, `docs_dir` and
`theme.custom_dir` are configurable, and a page with `template: home.html`
frontmatter plus a Jinja override extending `base.html` renders that template —
which is the mechanism the hero landing page needs. It also ships a
GitHub Pages workflow template of its own. Caveat to carry: **0.0.57 is very
early**. If it fails on something structural, the fallback is
mkdocs-material with the same content and the same `docs/` layout; say so in the
milestone's Done paragraph rather than fighting it.

---

## 1. Brand assets, tokens and a self-hosted wordmark face

The foundation the next milestone paints with. No layout changes yet.

- **Move the assets into the tree properly.** `logo/` is explicitly temporary.
  Proposed homes:
  - `web/icons/` — replace `favicon-16/32.png`, `apple-touch-icon.png`,
    `icon-192.png`, `icon-512.png`, `icon-maskable-512.png` from
    `caravel-icon-16/32.png`, `caravel-icon-180.png`, `caravel-icon-192.png`,
    `caravel-app-icon.png`, and add `favicon.svg` (`caravel-favicon.svg`, which
    an SVG-capable browser prefers). Keep the existing *filenames* so
    `index.html`, `manifest.webmanifest` and `sw.js` do not all need touching
    for a swap — except the additions, which do.
    Check the maskable icon has real safe-area padding; the app icon's mark sits
    at `scale(5)` inside 512 with a 112 corner radius, which is probably too
    close to the edge for a maskable crop. If so, generate a padded variant
    rather than shipping a clipped sail.
  - `web/brand/` (new) — `caravel-mark-currentcolor.svg` for inline app use, and
    the two horizontal lockups.
  - `docs/assets/brand/` — the lockups, banner and OG cards the site and the
    README consume as images (the PNGs, per the assets README: SVG lockups keep
    live `<text>` and lose Montserrat when loaded via `<img>`).
  - Delete `logo/`, and carry its `README.md` (palette, clear space, minimum
    sizes, the SVG-vs-PNG rule) forward as `docs/assets/brand/README.md` —
    those rules are the part that stops the next person misusing the set.
- **Brand tokens in `web/css/base.css`.** Add alongside the existing palette,
  not instead of it: `--brand-navy: #23304F`, `--brand-navy-light: #5470A8`,
  `--brand-cream: #FAF7F2`, and a `--font-brand` stack. Note in a comment that
  `--color-accent`'s `#2563eb` *is* the brand's app blue, so the accent is not
  duplicated under a second name.
- **Self-hosted Montserrat.** A `scripts/gen_brand_fonts.py` in the shape of the
  existing `scripts/gen_icon_sprite.py` (committed output, documented input,
  reproducible by hand): subset `Montserrat-Bold.otf` and `-Medium.otf` to
  woff2, latin + latin-ext, into `web/fonts/`. Two `@font-face` rules with
  `font-display: swap` and a system fallback, so a blocked or missing font
  degrades to the current look rather than to invisible text. Record the OFL
  licence next to the files — a shipped font needs its licence shipped with it.
  Add the regeneration recipe to `CLAUDE.md`'s "Common gotchas", next to the
  icon-sprite one.
- **README banner and social card.** Put `caravel-readme-banner.png` at the top
  of `README.md`, and the OG card in `index.html`'s `<head>` as `og:image` /
  `twitter:image` (with `og:title`/`og:description`) — the app is about to have
  a public URL, and a link to it currently previews as nothing.
- **Theme colour.** `#2563eb` in `index.html` and the manifest is the accent, not
  the brand's chrome; decide whether the standalone-app title bar should be navy
  instead, and change both files together if so.

**Verify:** `make ci`; DevTools shows the woff2 loading from `/fonts/` and no
request to any third-party host; the favicon and installed-app icon are the
sail (check the maskable one in Chromium's app-install preview, where a bad safe
area actually shows); a Playwright assertion that
`getComputedStyle(header).fontFamily` resolves to the brand stack.

**Done.** The asset set is in the tree and `logo/` is gone. Two deviations from
the plan, both deliberate:

- **The icons are generated, not copied.** `scripts/gen_icons.py` already
  existed (it drew the placeholder mountain-and-sun set), so instead of dropping
  in the pre-rendered PNGs it now owns the mark's two paths and derives every
  size plus `web/icons/favicon.svg` from them. That answered the maskable
  question the plan flagged: the brand's own app icon puts the mark's enclosing
  circle at ~0.39 of the width, which a circular Android mask *would* clip, so
  the script takes an `ink_radius_ratio` per output and the maskable icon uses
  0.29 against a 0.40 safe radius. Verified by circle-cropping the result.
- **The editable SVG originals were kept** under `docs/assets/brand/src/` rather
  than discarded with `logo/`. Their wordmarks are live `<text>`, so they are
  the only way to change the words later; the PNGs beside them are renders.
  `app-icon-blue` is kept as the drawn alternative it is.

Fonts: `scripts/gen_brand_fonts.py` subsets the distribution's OFL Montserrat
(500 and 700) to latin + latin-ext plus the punctuation the copy uses, 17 KiB
each, with `web/fonts/OFL.txt` beside them. No network needed to rebuild them.
Tokens landed as planned, plus one the plan did not name: `--brand-ink`, which
resolves to navy on light and the lightened navy on dark, so callers never pick
between the two (the same shape as `--color-danger` / `--color-danger-fg`).

Two things found on the way that were not in the plan. **`.woff2` has no entry
in Go's MIME table**, so `http.FileServer` would serve the fonts as
`application/octet-stream`; `router.go` now registers it beside the
`.webmanifest` line. Note honestly what is *not* proven: a developer machine has
`/etc/mime.types`, from which Go picks the type up anyway, so the new assertion
passes with or without that line and only the container will show the
difference — Milestone 7 has to check it there. And the app's **description had
drifted into three copies**; all three now carry the brand's own sub-line, with
a test that they agree.

App chrome was the milestone's one open preference, and was chosen from a
rendered comparison rather than described: navy `#23304F` for `theme-color` and
`theme_color`, cream `#FAF7F2` for the splash, so an installed app frames like
the tile it launched from. `#2563eb` stays the in-app accent.

Verified by `make ci` (green), and by a new `tests/ui/brand.spec.js`: every
icon, card and font served with the right content type; the 700 face loading
from this instance and measuring differently from the fallback for
`CARAVEL Größe – „quotes“` (which proves the *subset's* coverage in a browser,
not just in fontTools); `--brand-ink` changing with the theme; the two chrome
colours agreeing across `index.html` and the manifest; and zero off-origin
requests on the login screen. Also confirmed by hand that `/fonts/`, `/brand/`
and `/icons/` are served with correct types by the running server, and that the
service worker's `CACHE_VERSION` bump ships the changed assets to existing
clients.

## 2. The app header and a branded login screen

The mockups, made real. Both themes, and 324×756 as well as desktop.

- **Header** (`web/js/app.js`'s `renderAuthenticated`, `.app-header` in
  `base.css`): inline mark (`currentColor`, so it follows the theme) plus the
  wordmark as tracked uppercase Montserrat — the second row of the lockup mock.
  Keep `t("app.name")` as the text so the accessible name and i18n survive; the
  mark gets `aria-hidden` since the wordmark already names the app.
- **Login screen** (`web/js/pages/login-page.js`, `.auth-screen`/`.auth-form`):
  the hero from the first mockup. Heading, sub-line, the sail as a large
  low-contrast watermark bleeding off the right edge, and the form beneath or
  beside it. In light mode the hero ground is the cream-to-white wash from the
  mock; in dark mode the near-black one.
  - The mock's two buttons are a *landing page's* ("Deploy Caravel" / "Read the
    docs"); the login screen's equivalents are the submit button and the
    register switch. Match the *treatment* — filled blue primary, outlined
    secondary — not the labels. That treatment is worth extracting as a
    `.btn-outline` alongside the existing `.btn-primary`, because the docs
    landing page in Milestone 10 needs the same pair.
  - **New copy needs i18n keys in both `en.json` and `de.json`** — the heading
    and sub-line are new strings, and `scripts/check_i18n.py` will say so.
    German is the longer language and this is a headline: check it does not
    overflow at 324px.
- Watch the two things a hero breaks: contrast (`tests/ui/contrast.js` measures
  it, and the watermark sits under text — flatten the translucency rather than
  guessing), and the `todo.md` note that `.auth-form` already disagrees with the
  other two form rules on radius and font size. Do not fix that pattern here;
  do not make it worse either.

**Verify:** `make ci`; Playwright assertions on both themes (heading text,
computed background, the mark present and `aria-hidden`, the accessible name of
the submit button) plus a 324×756 overflow check in German. `unauthenticated.spec.js`
already renders the login screen from a fresh context and is the natural home.
A screenshot pass against the mockups for the judgement call, but assertions for
the record.

**Done.** The header is the lockup from the second mockup — the mark as a CSS
mask (so `background-color: currentColor` themes it for free and the boom keeps
its 72% opacity, without a second copy of the geometry in JS), plus the tracked
uppercase wordmark, and it is now a link home through the router's `data-link`.

The hero took four rounds, and the plan's guess about its shape was wrong in a
way worth recording. Planned: copy left, form right. Built that first, and the
form card sat *on top of* the sail and chopped it into unrecognisable shards. So
the second attempt made the hero a pure banner with the form as a separate card
below — faithful to the mockup, but the review call was that the detached card
floated under empty space, which it did. Third: the form moved inside the hero
as one group, and four arrangements of the sail were rendered and compared
(behind the form, in the gap between copy and form, a centred stack, and
mirrored on the left). The gap won, with the form made **frameless** — no card
inside a card, the inputs' own borders carry it. That last change moved the sail
again: with no card to tuck behind, its boom crossed the inputs and read as a
glitch, so it now stops short of the fields, offset by
`calc(var(--auth-form-column) + 1.5rem)` — one variable owns the form column's
width so the grid and the sail cannot drift apart. Last round: more vertical
room, as a `min-height` floor rather than a height, since the form grows by a
field in register mode and by a line when a login fails.

Deviations worth naming. **`.btn-secondary` already existed** and is exactly the
mockup's outlined treatment, so the plan's `.btn-outline` was not added; the
register switch uses it instead of the bare link it was. **A second token,
`--brand-wordmark`**, splits the wordmark and headings from the mark: the
committed dark lockup pairs a `#5470A8` mark with a near-cream wordmark, and
following that took the dark heading from 3.26:1 — passing for large text, but
only just — to 15:1. The mark alone keeps `--brand-ink`.

Two real bugs, both found by the existing suite rather than by looking, and both
in the new header link: a `margin-right: -0.17em` meant to absorb the wordmark's
trailing tracking made the anchor's content 3px wider than its box (23 overflow
failures), and a 24px-tall link missed the 44px tap floor at phone width (23
more). The first is fixed by dropping the compensation, the second by adding
`.app-brand` to the narrow-viewport tap-target rule.

Verified: `make ci` green; the full UI suite green (105 passed, 3 skipped — the
skips are `assist.spec.js`, which needs the assistant configured). One process
note, since it produced a false report before it was understood: two of those
suite runs were launched concurrently against the same dev server, and four
tests failed as a result — both `settings.spec.js` language cases, which mutate
and restore the seeded `other` account. They pass in isolation and the clean
serial run is green, but the first report of "green" was made from a run that
had a second run interfering with it, which is not evidence. Recorded in
`todo.md`. New
`tests/ui/brand.spec.js` cases covering both themes (one `h1` and it is the
hero's, the form's title an `h2`, both marks `aria-hidden`, Montserrat actually
computed on the headline and wordmark, the watermark's mask present and its
opacity inside sane bounds, the header link's accessible name being "Caravel"
rather than "Caravel Caravel", and that clicking it routes without a document
reload). Contrast measured rather than assumed, worst-case per theme and
viewport by sampling the real pixels behind every text box with the copy hidden:
headline 9.5–15.7:1, sub-line 5.6–6.5:1, form heading 15.0–17.4:1, field labels
6.4–7.6:1, no failures. That needed ad-hoc tooling, because `contrast.js` logs
in before it measures and so cannot reach the login screen — noted in
`todo.md`. German at 324px checked for overflow (0px) in both themes.

## 3. Run the Go test suite against Postgres

Two test files open a database, both hard-coding SQLite:
`internal/httpapi/testing_test.go:65` and `internal/auth/auth_test.go:80`.

- Add **`internal/dbtest`** (a normal package, so both can import it) exposing
  `dbtest.Open(t) (driver string, conn *sql.DB)`:
  - Default: a fresh SQLite file in `t.TempDir()`, exactly as today.
  - `CARAVEL_TEST_DB_DRIVER=postgres` + `CARAVEL_TEST_DB_DSN`: a **schema per
    test** for isolation. An admin connection runs
    `CREATE SCHEMA caravel_test_<n>` (`atomic.AddUint64` — tests run in
    parallel), then `db.Open("postgres", dsn+"&search_path=<schema>")`;
    `postgres.WithInstance` derives its schema from `CURRENT_SCHEMA()`, so
    migrations and `schema_migrations` land inside it. `t.Cleanup` drops it
    `CASCADE`.
  - **No silent fallback.** If the driver is requested and unreachable, fail —
    a fallback to SQLite is how a Postgres job reports a green run it never did.
  - **Verify the `search_path` route before building on it.** If the migrator or
    a query escapes the schema, switch to a database per test
    (`CREATE DATABASE caravel_test_<n>`): slower, unambiguous, same helper
    signature. Take the fallback rather than fighting it — isolation mechanics
    are not what this stage is for.
- Rewrite both call sites through the helper. The point is that the *existing*
  assertions now also run on Postgres; no new test bodies.
- `make test-postgres`: uses the compose db service (Milestone 4) or an already
  running one via the env vars.
- **Expect real failures, and fix them here.** This is the adapter's first
  execution. `todo.md` names the likely shapes: wrong column order in a
  hand-written adapter, dialect-specific NULL or timestamp handling, a `LIKE`
  that is only case-insensitive on SQLite. Each fix gets a line in the Done
  paragraph — that list is the milestone's real output.

**Done.** `internal/dbtest` decides the dialect from `CARAVEL_TEST_DB_DRIVER`,
with a schema per test on Postgres and the old file-per-test on SQLite, and both
former call sites (`internal/httpapi/testing_test.go`,
`internal/auth/auth_test.go`) go through it. The `search_path` route the plan
was unsure about works exactly as hoped — verified before building on it: pgx
applies it, golang-migrate targets that schema, all 19 tables land inside it and
nothing leaks into `public`. The database-per-test fallback was not needed.
`make test-postgres` (via `scripts/test_postgres.sh`) brings the container up,
sweeps leftovers, runs the suite and stops the container again; `KEEP=1` leaves
it up.

**The bugs it found, which is the point.**

1. **`ListItemsByTrip` never worked on Postgres at all.** `sqlc.narg` generated
   `AND ($2 IS NULL OR category = $2)` with an untyped parameter, which Postgres
   refuses at prepare time (`could not determine data type of parameter $2`,
   SQLSTATE 42P08) — so *every* location list on a Postgres instance was a 500.
   Fixed with `CAST(... AS text)` in the query source (portable; the `::` form
   would not parse as SQLite), and regenerated. A side benefit: sqlc can now
   type the parameter, so `Category interface{}` became `sql.NullString` in both
   dialects — the adapters were already passing `nullString(...)`, which the
   `interface{}` had been silently accepting.

   Four tests failed on this one bug, and **one of them failed silently**:
   `tripTypeVocabulary` deliberately swallows list errors, so on Postgres the
   assistant simply received an empty type vocabulary and invented duplicates.
   No error, no log, wrong behaviour — precisely the failure mode this milestone
   existed to expose.

2. **Every Caravel server holds a Postgres connection forever.** golang-migrate's
   driver checks a dedicated connection out of the pool it is handed (for its
   advisory lock) and returns it only when the migrator is closed — and
   `db.Open` never closed one. In production that is one idle connection per
   server, easy to miss. In tests it is one per migrated database, and the run
   died at the 100th with "sorry, too many clients already" in whichever test
   happened to be running. `migratePostgres` now takes a DSN instead of a
   `*sql.DB` and runs on a pool it owns and closes, with
   `TestMigrationsDoNotHoldAConnection` asserting the property.

Two bugs of the harness's own are worth recording because both produced
misleading failures. A **shared counter raced across processes**: `go test ./...`
runs each package as its own binary, so `internal/auth` and `internal/httpapi`
both started at `caravel_test_1`, and a "drop any stale schema first" line then
deleted the other package's database mid-migration. Schema names now carry the
pid and random bytes, and the sweep for leftovers moved to the script, where
nothing is concurrent. And **a pool per test exhausted the server** before any
dialect difference could be seen; one shared admin pool plus small per-test
pools fixed that.

Verified: `make ci` green (SQLite unaffected), and `make test-postgres` green.
The plan's requested proof, done with a revert that **compiles** — the
`without.sh` trap caught me twice on the way, both times reporting a build
failure that could have been read as a test failure: with the lowercasing
removed from `likeContains`, `TestSearchUsers` **fails on Postgres and passes on
SQLite**, same code both times. That is the demonstration that this job means
something. The leak test was likewise proven against a compiling revert (one
connection held, versus zero).

Two now-false comments were corrected rather than left: `queries/users.sql` and
`members_test.go` both said nothing in the project ever runs the Postgres
dialect. The first version of the leak test also had to be rewritten — it
counted *every* connection to the database, so it passed alone and failed in a
full run where other packages' pools moved the number; it now counts by
`application_name`.

Deviation from the plan's order: Milestone 4's `docker-compose.postgres.yml` was
written here, since this milestone needs a database. Only the `db` service
exists so far; the app service arrives with the image in Milestone 7. Also worth
knowing for later: this machine has **podman, not docker** — see the
environment note above.

> **Environment note, found in Milestone 3:** this machine has **podman**
> (5.8.4, rootless) and **no docker**, and no `psql`/`pg_dump` client either.
> `podman compose` delegates to `podman-compose` and takes the same arguments,
> so the compose files below work unchanged; `scripts/test_postgres.sh` picks
> whichever of the two is installed. Two consequences for later milestones:
> the Milestone 6 schema dumps have to run `pg_dump` *inside* the container,
> and the Milestone 7 image is built with `podman build` locally while CI
> builds it with docker — so the Dockerfile must not rely on anything
> docker-specific (no BuildKit-only syntax without checking podman supports
> it).

## 4. A compose file per dialect

Two files at the repo root, both usable by a stranger with only Docker:

- **`docker-compose.yml`** — one `caravel` service on SQLite, named volumes for
  `/data` and `/uploads`, port 8080, `restart: unless-stopped`. References the
  GHCR image, with `build: .` commented in for local work.
- **`docker-compose.postgres.yml`** — `caravel` plus `postgres:17-alpine` with a
  healthcheck and `depends_on: condition: service_healthy`. Must be startable as
  **just the db service** (`docker compose -f … up -d db`), because Milestone 3
  and the CI job want a database, not an app image.
- Neither bakes in an assistant configuration: a commented block pointing at the
  docs, plus an `env_file: .env` hook (`.env` is already gitignored).

Ordering note: the app service may reference an image that does not exist yet;
Milestone 7 makes it real.

**Done.** `docker-compose.yml` is the SQLite case (one service, two named
volumes, `restart: unless-stopped`, `build: .` commented in) and
`docker-compose.postgres.yml` — whose `db` service arrived early, in Milestone 3
— gained its app service, pointed at `db` and gated on
`depends_on: condition: service_healthy`. Both reference
`ghcr.io/lkiesow/caravel:latest`, which does not exist yet; pulling it today
fails with a bare `403 Forbidden` rather than anything about "not found", so
until Milestone 8 publishes, `build: .` is the usable path and the docs should
say so. The optional `.env` uses the `path:`/`required: false` form, verified to
work on this box's podman-compose 1.6.0 rather than assumed.

**The find, and it is the reason this milestone was worth more than typing two
YAML files.** The first draft set `CARAVEL_OPEN_SIGNUP` in both files, with a
comment describing the first-run sequence from the README. **That variable does
not exist.** Stage 14 Milestone 5 deleted it, deliberately, because registration
became a runtime setting and two sources for one answer means the admin screen
can contradict the server. The README had been documenting it for four stages,
including an instruction ("set it to true, register, set it back") that could not
work. What actually happens is better: `registrationAllowed` lets the first
account register on an instance with no users at all, and that account becomes
the admin. So the README row is gone, both compose files describe the real
behaviour, and the two test-only variables from Milestone 3 are documented.

Rather than fix that by hand and hope, `scripts/check_env_vars.py` now runs in
`make ci` (`make check-env`): every `CARAVEL_*` name the compose files or the
README mention must be one the Go source actually reads, and every one it reads
must be documented. Proven by putting the mistake back — it fails with
`docker-compose.yml sets CARAVEL_OPEN_SIGNUP, which the app never reads`. It
deliberately strips comments first, since both compose files now discuss the
removed variable on purpose.

Verified: both files parse (`compose config`); the `db` service still starts
alone, reports healthy and creates no app container, which is what
`make test-postgres` depends on; that whole run is still green with the app
service present; `make ci` green. What is **not** verified, and cannot be until
Milestone 7: that either app service actually starts, since there is no image.
Both are checked end to end there.

## 5. A Postgres job in CI

- A `postgres` job in `.github/workflows/ci.yml` with a `services: postgres:`
  container, running `go test ./...` under `CARAVEL_TEST_DB_DRIVER=postgres`.
- Its own job, not an extra step, for the reason the file already states for
  `ui`: a slower job with more ways to fail must not mask a Go regression in the
  fast one.
- Prove it is really on Postgres. Milestone 3's no-fallback rule does most of
  this; add whatever cheap confirmation the run can print, in the spirit of the
  `ui` job's assistant-capability check — a silent skip that reads as a pass is
  the exact failure mode this stage is trying to remove.

**Done.** A `postgres` job in `.github/workflows/ci.yml` with a
`postgres:17-alpine` service (health-gated on `pg_isready`, since Postgres
restarts itself once while initialising and "the container exists" is not "the
server is accepting connections"), the two `CARAVEL_TEST_DB_*` variables at job
level, and `go test -count=1 ./...`.

The confirmation the plan asked for covers both directions, and both were
actually exercised rather than reasoned about:

- **The dangerous one** — a DSN that does not resolve — is handled by
  Milestone 3's no-fallback rule: the run fails with `cannot reach postgres at
  ...` (password redacted), rather than quietly passing on SQLite.
- **The embarrassing one** — the `env` block dropped in a later edit, leaving a
  job that tests SQLite twice and reports Postgres — is what the new step
  catches. `TestMigrationsDoNotHoldAConnection` skips itself unless the driver
  is Postgres, so "did it skip?" answers "was this really Postgres?". Same shape
  as the `ui` job's assistant-capability check, and for the same reason: a
  silent skip reads as a pass.

Verified without Actions, which this machine cannot run: the workflow parses and
`yamllint` is clean; a structural check confirms the service ports, the health
options, the localhost DSN and that no job references a secret this repo does
not have; and the job's own steps were run locally against a container started
with **the same image, env and health command the workflow declares** (on port
5433, to keep it clear of the compose service). The confirmation step passes
there, and — the part that matters — it *fails* when the driver variable is
unset, which is the mistake it exists to catch. `go test ./...` under those
variables is green.

What remains unverifiable here: that GitHub's runner wires the service the way
the workflow assumes. That is settled by the first push, and is small — the
service block is the documented form.

Note the duplication this creates: `postgres:17-alpine` is now pinned in both
`docker-compose.postgres.yml` and the workflow, with a comment in each pointing
at the other. They are separate because Actions wants a service container and a
developer wants `make test-postgres`; a shared file would mean the CI job
depending on compose being installed on the runner.

## 6. Squash the migrations to one `0001_init` per dialect

- Replace `internal/db/migrations/{sqlite,postgres}/000{1..12}_*.{up,down}.sql`
  with a single `0001_init.up.sql`/`.down.sql` per dialect describing the
  *current* schema — Stage 14's `trip_members`, the file and checklist
  visibility columns, Stage 17's expenses and expense shares.
- Generate rather than transcribe: migrate a fresh database through the old
  chain, dump its schema, and require a fresh-from-`0001_init` database to be
  **identical** to it. On both dialects (`pg_dump --schema-only` for the second)
  — which is what Milestone 3 bought and why this comes after it.
- Edit the dump back into house style. The existing `0001_init.up.sql` is
  commented and ordered deliberately, and a dump is neither; keep the comments
  explaining *why* a constraint exists (`amount_minor > 0`, the visibility
  columns, the cascade choices). Those are what a schema dump destroys.
- Gates: `go test ./...` on both dialects, `make dev-reset FORCE=1` then a full
  `make test-ui`, and a Done paragraph stating that any pre-Stage-18 database
  must be recreated — nobody has one, which is the whole licence for doing this
  now.
- Fix `todo.md`'s stale "ten files" count as part of removing the entry.

## 7. A multi-stage Dockerfile

- Builder on `golang:1.26`, `CGO_ENABLED=0` (`modernc.org/sqlite` is pure Go),
  `-ldflags` stamping `caravel/internal/buildinfo.Version` from a
  `--build-arg VERSION`. Without that arg `scripts/version.sh` prints `unknown`
  in a `.git`-less context — honest, useless on a published image — so the build
  arg is how Milestone 8 injects the tag or SHA.
- Runtime: minimal base (`gcr.io/distroless/static` preferred, `alpine` if the
  healthcheck wants a shell), non-root user, `EXPOSE 8080`, documented `/data`
  and `/uploads` volumes, `ENV CARAVEL_DB_DSN=/data/caravel.db` and
  `CARAVEL_UPLOAD_DIR=/uploads`. The frontend is embedded (`embed.go`), so no
  `web/` copy and deliberately no `CARAVEL_WEB_DIR`.
- A `HEALTHCHECK` on `/api/health` — it already reports the stamped version,
  which is what makes "which build is this?" answerable in production the way
  `make dev-version` answers it locally. On distroless the check moves to the
  compose files.
- `.dockerignore`: `node_modules`, `data`, `uploads`, `bin`, `test-results`,
  `playwright-report`, `.git`, `plans`, `credentials*`, `.env*` at minimum.
  Getting this wrong is how a context reaches hundreds of megabytes and how a
  credentials file reaches a layer.
- Then make the compose app services real and verify the SQLite one end to end.

## 8. Publish images to GHCR

- `.github/workflows/publish-docker-image.yaml`, modelled on
  `lkiesow/audiobook-notifier`'s workflow of the same name.
- Triggers: push to `main`, `v*` tags, `workflow_dispatch`.
  `permissions: {contents: read, packages: write}`, `docker/login-action` to
  `ghcr.io` with `GITHUB_TOKEN`.
- `docker/metadata-action` for tags (`latest` on main, short SHA always, semver
  on a tag), `docker/build-push-action` with `VERSION` wired to the resolved
  tag, `linux/amd64,linux/arm64` via `setup-qemu-action` + `setup-buildx-action`
  (arm64 matters — a self-hosted trip planner's natural home is a small ARM
  box), and Actions layer caching.
- **Honest limitation for the Done paragraph:** a publishing workflow cannot be
  fully verified without pushing. Locally verifiable: the multi-arch
  `docker buildx build` and the YAML's validity. The first push to `main`
  confirms the rest, and that is the accepted risk.

## 9. The site: Zensical, GitHub Pages, and the hero landing page

Structure first, content in Milestone 10.

- **Move `docs/plans/` → `plans/`** (17 stage plans, `todo.md`,
  `mobile-test-report.md`, `caravel-logo-drafts.png`) so `docs/` can be the
  site's `docs_dir` with nothing to exclude. `git mv`, then fix the three
  non-plan files that name the old path: `CLAUDE.md` (several references —
  the planning-documents section, the workflow, the screenshot note),
  `.gitignore` (the `mobile-fresh-*.png` rule), and
  `web/js/components/assist-panel.js` (a comment). The plans' own
  cross-references are historical prose and can stay as they are; say so in the
  Done paragraph so it does not read as an oversight.
- **`zensical.toml`** at the root: `site_name`, `site_url` (the Pages URL),
  `repo_url`, the sail favicon and logo from `docs/assets/brand/`, the palette
  toggle the scaffold ships, `theme.custom_dir = "overrides"`, and
  `theme.font` disabled in favour of the self-hosted faces from Milestone 1 via
  `extra_css` — the site should not phone out to Google either.
- **`docs/index.md` with `template: home.html`** plus `overrides/home.html`
  extending `base.html` — the mechanism verified during planning. This is the
  hero mockup, sharing the tokens and the button pair with Milestone 2's login
  screen: "Every trip, one place.", the sub-line, the sail watermark, a filled
  primary ("Deploy Caravel" → the install page) and an outline secondary
  ("Read the docs"), correct in both palettes, `hide: [navigation, toc]`.
- **`.github/workflows/docs.yml`**, from Zensical's own template: build on push
  to `main`, `upload-pages-artifact`, `deploy-pages`, with the
  `contents: read` / `pages: write` / `id-token: write` permissions. Pin
  `zensical` to a version rather than floating on a 0.0.x — a generator this
  young can change output between patch releases. Also run a plain
  `zensical build --clean` in the `ci` job's neighbourhood so a broken nav or a
  dead link fails a PR, not the deploy.
- Enabling Pages in the repository settings is a click only the user can make;
  the milestone ends by asking for it if it is not already on.

**Verify:** `zensical build --clean` clean; `zensical serve` inspected at
desktop and 324px, in both palettes; the hero compared against the mockup; no
third-party font or asset request in DevTools; and after the first push, the
live Pages URL. Note explicitly if 0.0.57 forced any workaround — that is the
evaluation the user asked for.

## 10. The reference documentation

The pages somebody needs to *run* Caravel, in `docs/`, taking over from what
`README.md` carries today. Written from the code, not from convention — every
default and every variable name checked against `internal/config` rather than
copied from the README, which may already have drifted.

- **Getting started / install** — `docker compose up -d` as the headline, the
  Postgres variant beside it and a sentence on when to pick which (one household
  on SQLite; Postgres when you already run one). From-source as the second path.
- **First run** — `CARAVEL_OPEN_SIGNUP=true`, register, set it `false`: the
  sequence a stranger currently has to infer.
- **Configuration** — the env-var tables, moved out of the README (which links
  to them instead), including the assistant and search-provider sections that
  are already written well and just need a home with anchors. The assist guard
  rails (`CARAVEL_ASSIST_*`) are documented only in `internal/config`'s comments
  today and belong here — they are the knobs an operator turns when a bill
  surprises them.
- **Operating it** — what to back up (`/data` *and* `/uploads` together; a
  database whose blobs were not backed up with it is worse than neither),
  upgrading (pull, restart, migrations run at startup via `db.Open`) with the
  note that pre-Stage-18 databases are not upgradable after Milestone 6, and
  reverse-proxy/TLS guidance. Check `internal/httpapi/security.go` before
  claiming anything about `X-Forwarded-*` — write what the code does, not what
  is conventional.
- **README.md** slims to the banner, a paragraph and links to the site;
  `make dev`/`make ci` and the contributor-facing bits stay.

**Verify:** follow the install page literally, from a clean clone in a scratch
directory, on both compose files — a page nobody walked through is a guess. The
build stays clean and the nav has no orphans.

## 11. The feature tour, and finish the backlog

The half that makes "Read the docs" worth pressing: what the app actually does,
with pictures.

- A page per area, in the order somebody meets them: trips and the trips list,
  locations, the map, the itinerary, files, checklists, members and sharing,
  expenses and balances, the assistant, account settings.
- **Screenshots from a seeded instance, scripted rather than hand-taken.** The
  UI suite already drives a seeded server; a small Playwright script under
  `tests/ui/` or `scripts/` that walks the seeded Iceland trip and writes PNGs
  into `docs/assets/screenshots/` makes them reproducible when the UI changes,
  which hand-taken ones never are. Take them in one theme and one locale, at a
  fixed viewport, and commit them — unlike the mobile-test screenshots, these are
  published artefacts, so the `.gitignore` rule that excludes those must not
  swallow these.
  Note the seeded-trip hazard `todo.md` records twice: a script that *writes* to
  a seeded scenario poisons the UI suite. This one only reads.
- Keep the prose short. The screenshots carry it, and a feature tour that
  restates every field is the first documentation to go stale.
- Then the bookkeeping: delete the four (soon) deployment entries and the
  Postgres-dialect entry from `todo.md`, and add what this stage surfaced and
  deferred (already visible: S3 blob storage, Prometheus metrics, a
  compose-based way to run the UI suite, a screenshot-refresh step for whenever
  the UI moves, and — if it survives — the pinned Zensical version needing
  periodic review).

---

## Build order

1. Brand assets, tokens, self-hosted Montserrat
2. Header + login hero
3. Postgres-capable test harness (`internal/dbtest`) + what it exposes
4. Compose files (db service usable standalone)
5. Postgres CI job
6. Migration squash, validated on both dialects
7. Dockerfile + `.dockerignore`, compose app service made real
8. GHCR publishing workflow
9. `docs/` + `plans/` split, Zensical, hero landing page, Pages workflow
10. Reference documentation (install, first run, configuration, operating it),
    README slim-down
11. Feature tour with scripted screenshots, backlog bookkeeping

Design leads because the site and the README consume its output, and because
the login hero and the landing page must share one set of tokens rather than
converge later. Milestones 3–4 are order-flexible (3 needs 4's db service);
if that inverts, say so in the Done paragraphs rather than reshuffling.

## Workflow

Per `CLAUDE.md`: one milestone at a time — implement → verify (`make ci` green
plus evidence the behaviour actually changed) → add a **Done.** paragraph to
this plan and update `todo.md` in both directions → commit (one per milestone,
follow-ups get their own) → make sure `make dev` is running → stop and hand back
control. No starting the next milestone until told to.

**Ask rather than decide.** This stage carries more judgement calls than a
feature stage does: how closely the login hero follows the mockup where the
mockup is a *landing page*, which brand colour a surface takes, what goes on the
landing page beside the two buttons, how the docs nav is organised, what the
feature tour covers and in what order, which screenshots to take, how much the
README keeps. Where a choice is a *preference* rather than a correctness
question, stop and ask before building it — a paragraph of taste rendered in code
is more expensive to undo than to check. Reasonable defaults still apply to the
mechanical parts (file names, token names, where a partial lives); the rule is
about anything the user would recognise on screen or read as the project's
voice.

Two natural checkpoints deserve a look before the commit, not after: the login
hero (Milestone 2) and the landing page (Milestone 9), both of which will be
screenshotted and shown rather than described.

**Imagery is asked for, never improvised.** Everything the brand set covers is
already in `logo/assets/`, and the feature tour's screenshots come out of the app
itself — so nothing in this plan *needs* a photograph. But a landing page or a
feature page may want one, and a licence is not something to be casual about on a
page that is about to be public. So: no image enters the repo unless its licence
is known and recorded. Where something genuinely helps, ask for it — the user can
supply photos — rather than reaching for whatever a search returns, or inventing
a placeholder that quietly becomes permanent. Record the source and licence for
every non-brand image beside it (a `docs/assets/CREDITS.md`), the way
`docs/assets/brand/README.md` records the brand rules. Note the precedent for
caring about this: `todo.md` rejects generic web image search for location cover
photos as "a licensing landmine" and prefers Wikimedia for exactly this reason.

## Verification

- `make ci` green at every milestone. Milestones 1–2 add user-facing strings, so
  i18n parity matters there; anywhere else a parity failure means something
  unintended happened. (The docs milestones add prose, not app strings — the
  site is English-only, which is a decision worth stating rather than
  discovering.)
- Brand: no third-party requests from either the app or the site; favicon,
  installed-app icon and maskable crop checked in a real browser; computed
  font-family and both-theme assertions in the UI suite; German headline at
  324px does not overflow.
- Postgres: `make test-postgres` green, and visibly failing when
  `SearchUsers`'s `LOWER()` normalisation is removed — the ready-made proof that
  the new job means something. (Mind `scripts/without.sh`'s known flaw: it
  reports success on *any* non-zero exit, compile errors included. Read its
  output, not its verdict.)
- Squash: schema dumps from an old-chain database and a fresh `0001_init`
  identical on both dialects; `make dev-reset FORCE=1` then a full `make test-ui`
  green.
- Container: `docker compose up -d` from a clean clone, `/api/health` returns
  the stamped version, register/login/create a trip/upload a photo by hand,
  `down && up` proves the volumes persisted; repeat for the Postgres file.
  `docker history` shows no credentials and no `node_modules` layer; the process
  runs as non-root.
- Publishing: local multi-arch `buildx` build succeeds; the workflow itself is
  confirmed by its first push to `main`, stated as such rather than claimed.
- Site: `zensical build --clean` clean and wired into PR CI; the live Pages URL
  after the first deploy; hero compared against the mockup in both palettes at
  desktop and 324px.
- Documentation: the install page walked through literally from a clean clone on
  both compose files, and the screenshot script re-run from scratch to prove it
  regenerates the committed images rather than only having produced them once.
