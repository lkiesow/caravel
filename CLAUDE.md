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
- Land the approved plan as `docs/plans/stage-NN.md` (next sequential
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
   gitignored, not committed — see `docs/plans/stage-04.md`'s note).
3. **Update `docs/plans/stage-NN.md`** — add a "**Done.**" paragraph to
   that milestone's section describing what actually landed (including
   any deviation from the plan) and how it was verified. Then update
   `docs/plans/todo.md` in both directions: remove any entry this
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

## Planning documents (`docs/plans/`)

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
  `go test`.
- Mobile testing convention: 324×756 (the user's phone's native
  resolution), verified via the Playwright MCP tools against a running
  `make dev` server.
