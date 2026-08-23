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
