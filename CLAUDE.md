# Working conventions for Caravel

## Stage-based development workflow

Work happens in **stages**, each covering a related set of fixes/features,
planned up front and built one **milestone** at a time. This repo has used
this workflow since Stage 03; follow it for any new stage unless told
otherwise.

### Planning a stage

- Use plan mode to scope the stage before writing any code. Explore the
  actual current code rather than assuming prior stages' behavior still
  holds — line numbers, function names, and even file existence drift.
- Land the approved plan as `plans/stage-NN.md` (next sequential
  number) *before* implementation starts. Structure: a Context section
  (why this stage exists), one numbered section per milestone, a Build
  order, a Workflow section restating the loop below, and a Verification
  section.
- If a milestone's own scope wants revisiting mid-stage, do the smaller
  fix as a follow-up commit on the same milestone rather than restarting
  the plan.

### The milestone loop

For each milestone, in order:

1. **Implement.**
2. **Verify** — `make ci` (build, vet, JS syntax check, i18n key parity,
   go test) must be green, plus a manual/Playwright pass proving the
   actual behavior changed (not just that the code compiles). Prefer
   assertions over screenshots where practical: computed styles, DOM
   counts, accessible names, `window.location.pathname`, `go test`
   coverage — a passing test is stronger evidence than a matching
   screenshot. Regenerate screenshots only as a last resort (they're
   gitignored, not committed — see `plans/stage-04.md`'s note).
3. **Update `plans/stage-NN.md`** — add a "**Done.**" paragraph to
   that milestone's section describing what actually landed (including
   any deviation from the plan) and how it was verified. Then update
   `plans/todo.md` in both directions: remove any entry this
   milestone actually implemented (don't let it linger as if still
   outstanding), and add anything the milestone surfaced but deferred.
4. **Commit** — one commit per milestone (a milestone that needed a
   same-day follow-up fix gets its own extra commit, titled
   "... follow-up: ..."). Commit message: what changed, why, and exactly
   how it was verified — future-you should be able to tell from the
   message alone whether re-testing is needed.
5. **Make sure the dev server is running** (`make dev`) so the result is
   immediately testable, then **stop and hand back control.**
6. **Wait.** Do not start the next milestone until told to continue.
   Feedback given at a checkpoint gets fixed and re-verified *before*
   moving on, not folded silently into the next milestone.

### Why this exists

Each milestone should be independently reviewable and revertible, and the
person driving the stage needs a natural point to actually look at what
changed before more changes land on top of it. Racing ahead to the next
milestone — even when the next one looks small or obviously correct —
removes that checkpoint.

## Before every commit

Always run `make ci` locally before committing — build, vet, JS syntax
check, i18n key parity, `go test`. Don't rely on CI to catch it first.

## Planning documents (`plans/`)

- `stage-NN.md` — one per stage, the plan plus a running "Done" account of
  what actually landed per milestone (see the workflow above).
- `todo.md` — the running backlog of deferred/future work. Every entry
  cites the stage that surfaced it. Not prioritized or scheduled — raw
  input for planning the next stage. Keep it accurate in both
  directions: add anything a milestone defers, and remove any entry a
  milestone actually implements — a stale "still outstanding" item that
  was already built is worse than a missing one.

## Common gotchas

- **i18n key parity.** Any new or removed user-facing string needs a
  matching key in every file under `web/locales/` (`en.json` + `de.json`
  today) — `scripts/check_i18n.py` enforces this in `make ci`. Easy to
  forget when you're only looking at English copy.
- **Database migrations.** The schema was squashed to a single `0001_init` pair
  per dialect in Stage 18, so the next schema change is `0002_...`. New changes
  are sequential `000N_name.up/down.sql` files, written for *both* dialects
  (`internal/db/migrations/sqlite/` and `.../postgres/`). After editing
  `internal/db/sqlc/queries/*.sql`, run `sqlc generate` by hand from
  `internal/db/sqlc/` to regenerate the dialect packages — there's no
  automation for that step, and it's easy to forget one dialect.
- **A query change can now be tested on both dialects.** `make test-postgres`
  runs the whole Go suite against a Postgres container
  (`docker-compose.postgres.yml`, and `podman compose` works too);
  `internal/dbtest` is what picks the dialect up from `CARAVEL_TEST_DB_DRIVER`.
  Worth doing for anything touching `internal/db` or the queries: Stage 18
  Milestone 3 found that `sqlc.narg` without a `CAST` produces SQL Postgres
  refuses outright, which had made every location list a 500 on that dialect
  while every test stayed green. `KEEP=1` leaves the container up between runs.

- **`sqlc`'s SQLite parser has three traps, all of which report the error in
  the wrong place.** Learned the hard way in Stage 14; each cost real time
  because the reported line points at correct SQL.
  - **Keep comment prose in `internal/db/sqlc/queries/*.sql` plain**: no
    backticks, no double quotes, and avoid apostrophes (write "the trip owner",
    not "the trip's owner"). Some combination of these makes the lexer
    misparse, and it then blames a *statement*, usually the wrong one — the
    reported line points at correct SQL, which is what makes this expensive.
    Backticks alone reproduce it (Stage 14 Milestone 3); a comment full of
    apostrophes and quoted terms reproduced it again in Milestone 8, where
    rewriting the same comments in plain prose fixed it while the SQL was
    unchanged.
    Note what is *not* established: single apostrophes, single em dashes and
    apostrophe pairs each parse fine in isolation, so the exact trigger is
    unknown. Do not trust a theory about it. If `sqlc generate` reports a
    syntax error on SQL that looks correct, **bisect the comments**, not the
    SQL: append the queries one at a time with no comments to confirm the SQL
    is fine, then add the comment blocks back. That sequence found it twice.
    If you must set a term apart, use -- dashes -- or CAPITALS.
  - **Parenthesise OR-ed `LIKE` comparisons.** `LOWER(a) LIKE @p OR LOWER(b)
    LIKE @p` is rejected; wrapping each comparison in parens is accepted.
  - **`LIKE ... ESCAPE` is rejected outright**, and named args are *not*
    substituted inside `ON CONFLICT ... DO UPDATE` (use `excluded.col`).
  Read the generated file after `sqlc generate` rather than only diffing it
  for churn — an unsubstituted `sqlc.arg(...)` compiles fine and fails at
  runtime.

- **The screenshot generator dresses the set, and the dressing is the fiddly
  part.** (`scripts/gen_screenshots.sh` + `.mjs`, Stage 18 Milestone 11.) The
  seeded data is built for the UI suite, so it is titled `Demo: ...`, says
  "nothing here is real", dates its expenses outside the trip window, and uses
  deliberately-poor image fixtures. The script rewrites all of that through the
  API before capturing. Four things cost time and are easy to repeat:
  - **`readJSON` refuses unknown fields, and the trip body field is `subtitle`,
    not `description`.** Two calls were failing with 400 in silence because the
    script did not check statuses. Every API call now goes through a `must()`
    helper -- do not add one that does not.
  - **`share_user_ids` comes back as the *effective* set**, so echoing an
    expense back on PATCH converts "split with everyone" into "pinned to today's
    members". Forward it only when it is a genuine subset.
  - **Tab screenshots need scrolling.** The cover banner and title fill the
    viewport, so an unscrolled capture of the map tab is one pin and a strip of
    coastline. There is a `scrollTo` option; a card at the very bottom of a page
    needs `element` instead, because the page runs out of scroll before the card
    reaches the top.
  - **The stub provider answers with the same canned place whatever it is
    asked** (`internal/assist/stub.go`), so the prompt in the assistant
    screenshot has to match it or the picture shows a waterfall being offered a
    hostel.
  Output is quantised with `pngquant` (3.3M -> under 1M); without it installed
  the run still works and says the set will be ~3x larger.

- **The documentation site has four traps worth knowing.** All four cost time
  in Stage 18.
  - **The Zensical version is pinned in two files** —
    `.github/workflows/docs.yml` (the deploy) and the `docs` job in
    `.github/workflows/ci.yml` (the PR gate). Bump both together, or the build
    that gates a change is not the build that publishes it.
  - **`--strict` is load-bearing.** Without it a dead internal link is a line
    of output and a zero exit code; with it the build fails. Both call sites
    pass it, and `make docs` does too. Verified by adding a dead link on
    purpose: exit 1 with the flag, exit 0 without.
  - **Material sets a 125% root font size**, so every `rem` on the site is
    1.25× what the number reads as. A `minmax(15rem, ...)` grid track is 300px,
    not 240px, which overflowed the page at 324px. Wrap grid minimums in
    `min(100%, ...)` rather than trusting the arithmetic.
  - **Anything under `docs/` becomes a page.** Zensical 0.0.57 has no
    `exclude_docs`/`not_in_nav`, so a stray `README.md` in an assets folder
    builds and lands in the search index. `search: {exclude: true}` in its
    frontmatter is the available lever (it works); leaving it out of the nav
    only hides it from the sidebar.
  Also note `zensical serve` **panics** if a concurrent `zensical build
  --clean` wipes the cache underneath it — so do not run `make docs` while a
  `make docs-serve` is up.

- **Building the image.** `docker build --build-arg VERSION="$(scripts/version.sh)"
  -t caravel .` — the argument matters, because `.git` is not in the build
  context, so without it the binary calls itself `unknown`. With **podman**, add
  `--format docker` or the `HEALTHCHECK` is silently dropped (it is not an OCI
  instruction; podman warns, in the middle of a lot of build output). The image
  is distroless, so there is no shell inside: `caravel -health` is what the
  healthcheck runs, and `podman logs` rather than `exec` is how you see what a
  container is doing.

  The Dockerfile **cross-compiles** rather than emulating: the build stage is
  pinned to `$BUILDPLATFORM` and passes `TARGETARCH` to `go build`, so an arm64
  image builds at native speed. Both architectures at once, locally:
  `podman build --platform linux/amd64,linux/arm64 --manifest caravel:multi .`
  (docker uses `buildx build --platform ...`, which is what CI does).

- **Brand assets are generated, not hand-edited.** Two scripts own them, both
  run by hand with committed output (like the icon sprite below):

  ```
  python3 scripts/gen_icons.py         # web/icons/ - favicons, PWA, maskable
  python3 scripts/gen_brand_fonts.py   # web/fonts/ - the Montserrat subset
  ```

  `gen_icons.py` holds the mark's two paths and the safe-area arithmetic, so
  every raster size and `web/icons/favicon.svg` come from one source; it needs
  `cairosvg`. `gen_brand_fonts.py` subsets the distribution's OFL Montserrat
  (`julietaula-montserrat-fonts` on Fedora) and needs `fonttools[woff]`. A
  static-asset change also wants `CACHE_VERSION` in `web/sw.js` bumped, or
  clients keep the old files. The rules for using the assets (palette, clear
  space, minimum sizes, PNG-vs-SVG) live in `docs/assets/brand/README.md`.

- **Adding a new icon.** Icons come from a committed sprite
  (`web/icons/lucide-sprite.svg`), not a runtime dependency. Add the
  name to the `ICONS` list in `scripts/gen_icon_sprite.py`, then:

  ```
  npm install lucide-static --prefix /tmp/lucide-scratch
  python3 scripts/gen_icon_sprite.py /tmp/lucide-scratch/node_modules/lucide-static/icons
  ```

  Diff the result before committing — the existing symbols should come
  out byte-identical; if they don't, an upstream Lucide icon revision
  would silently restyle icons already in use.

## Dev environment

- `make dev` — runs the server with `CARAVEL_WEB_DIR=web`, so `web/js`,
  `web/css`, and `web/locales` are served live from disk; no restart
  needed after frontend edits. Backend (`internal/`, `cmd/`) changes do
  need a restart — migrations run automatically on startup.
- `make dev-seed` — seeds a demo user/trip for manual testing. It also
  **resets both seeded users' passwords** (`demo`/`demo1234`,
  `other`/`other1234`), so it is the fix when a password has drifted — the
  settings screen can change one, and the UI suite changes `other`'s on
  purpose. Sessions survive, so re-seeding won't log you out of the browser
  you're testing in.
- `make ci` — the same checks CI runs: build, vet, JS syntax, i18n parity,
  `go test`. Deliberately **does not** build the documentation site: that gate
  is the app, and it should not need a Python site generator installed to tell
  you whether the Go code compiles.
- `make screenshots` — regenerates the documentation screenshots
  (`docs/assets/screenshots/`, committed). Runs its own throwaway server on
  :8099 with its own database, so it neither needs `make dev` nor touches the
  seeded scenarios `make test-ui` depends on. Photos to dress the set come from
  `images/` (`PHOTO_DIR=…` to point elsewhere); without them the run still works
  but every image is the seeder's 343x200 test-sheet crop, and it says so.
- `make docs` / `make docs-serve` — the project website and documentation
  (`docs/`, `zensical.toml`, `overrides/`), built into `site/` (gitignored) or
  served at `localhost:8000`. Run `make docs` before committing anything under
  `docs/` — CI has its own job for it, but finding a dead link locally is
  cheaper.
- Mobile testing convention: 324×756 (the user's phone's native
  resolution), verified via the Playwright MCP tools against a running
  `make dev` server.
