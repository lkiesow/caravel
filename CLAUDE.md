# Working conventions for Caravel

## Stage-based development workflow

Work happens in **stages**, each covering a related set of fixes/features,
planned up front and built one **milestone** at a time. Follow this for any
new stage unless told otherwise.

### Planning a stage

- Use plan mode to scope the stage before writing any code. Explore the
  actual current code rather than assuming prior stages' behavior still
  holds — line numbers, function names, and even file existence drift.
- Land the approved plan as `plans/stage-NN.md` (next sequential number)
  *before* implementation starts: a Context section (why this stage
  exists), one numbered section per milestone, a Build order, a Workflow
  section restating the loop below, and a Verification section.
- If a milestone's scope wants revisiting mid-stage, do the smaller fix as
  a follow-up commit on the same milestone rather than restarting the plan.

### The milestone loop

For each milestone, in order:

1. **Implement.**
2. **Verify** — `make ci` (build, vet, JS syntax check, i18n key parity,
   go test) must be green, plus a manual/Playwright pass proving the
   actual behavior changed (not just that the code compiles). Prefer
   assertions over screenshots: computed styles, DOM counts, accessible
   names, `window.location.pathname`, `go test` coverage.
3. **Update `plans/stage-NN.md`** — add a "**Done.**" paragraph to that
   milestone's section: what actually landed (including deviations from
   the plan) and how it was verified. Then update `plans/todo.md`, the
   unprioritized backlog where every entry cites the stage that surfaced
   it, in both directions: remove entries this milestone implemented (a
   stale "outstanding" item is worse than a missing one), add anything it
   deferred.
4. **Commit** — one commit per milestone (a same-day follow-up fix gets its
   own commit, titled "... follow-up: ..."), always after a green
   `make ci` — also for commits outside a stage; don't rely on CI to catch
   it. Message: what changed, why, and exactly how it was verified, so
   it's clear from the message alone whether re-testing is needed.
5. **Make sure the dev server is running** (`make dev`), then **stop and
   hand back control.**
6. **Wait.** Do not start the next milestone until told to continue.
   Feedback given at a checkpoint gets fixed and re-verified *before*
   moving on, not folded silently into the next milestone.

Each milestone should be independently reviewable and revertible; racing
ahead removes the checkpoint where it gets looked at.

## Common gotchas

- **i18n key parity.** New or removed UI strings need a key in every
  `web/locales/*.json`; `make ci` checks parity.

- **Database and `sqlc`.**
  - Migrations are sequential `000N_name.up/down.sql` files, written for
    *both* dialects (`internal/db/migrations/sqlite/` and `.../postgres/`).
  - After editing `internal/db/sqlc/queries/*.sql`, run `sqlc generate` by
    hand from `internal/db/sqlc/` — it regenerates both dialect packages.
  - `sqlc generate` never deletes: removing a queries file leaves its
    `sqlc/{sqlite,postgres}/gen/*.sql.go` behind, so delete them by hand.
    An unused domain struct in `internal/db/domain.go` still compiles, so
    grep for the name after a removal.
  - Query or `internal/db` changes: also run `make test-postgres`
    (`KEEP=1` keeps the container); `make ci` only covers SQLite. E.g.
    `sqlc.narg` needs a `CAST` on Postgres.

- **`sqlc`'s SQLite parser traps** — all report the error on correct SQL:
  - **Keep comment prose in `queries/*.sql` plain**: no backticks, no double
    quotes, avoid apostrophes. The exact trigger is unknown; if sqlc blames
    correct SQL, bisect the *comments*, not the SQL.
  - **Parenthesise OR-ed `LIKE` comparisons.**
  - **`LIKE ... ESCAPE` is rejected**, and named args are not substituted
    inside `ON CONFLICT ... DO UPDATE` (use `excluded.col`).
  - **Read the generated file** — an unsubstituted `sqlc.arg(...)` compiles
    and fails at runtime.

- **Screenshot generator** (`scripts/gen_screenshots.{sh,mjs}`): its traps
  are listed in the header of `gen_screenshots.mjs` — read them before
  editing it.

- **Documentation site** (`docs/`, Zensical):
  - The Zensical version is pinned in `.github/workflows/docs.yml` *and* the
    `docs` job in `ci.yml` — bump both.
  - Keep `--strict` on every build call; without it a dead link exits 0.
  - Material sets a 125% root font size (`15rem` = 300px): wrap grid
    minimums in `min(100%, ...)`.
  - Anything under `docs/` becomes a page; use `search: {exclude: true}`
    frontmatter to keep a stray one out of search.
  - Don't run `make docs` while `make docs-serve` is up — serve panics.

- **Building the image.** `docker build --build-arg
  VERSION="$(scripts/version.sh)" -t caravel .` — without the arg the
  binary calls itself `unknown`. With **podman**, add `--format docker` or
  the `HEALTHCHECK` is silently dropped. Distroless, no shell: use `logs`,
  not `exec`. Multi-arch locally: `podman build --platform
  linux/amd64,linux/arm64 --manifest caravel:multi .`

- **Production bundles the frontend; `make dev` does not.** Since Stage 45 the
  server bundles and minifies `web/js/app.js` and `web/css/base.css` with
  esbuild's Go API at startup (`internal/webbundle`, ~70 ms), serves the output
  under `/assets/<name>-<hash>`, and serves the other static files on the load
  path under `/v/<dir-hash>/<path>`, all `immutable`. `CARAVEL_WEB_DIR` (so
  `make dev`) skips all of it and serves the source live. `make test-ui`,
  `make check-contrast` and `make screenshots` run the **bundled** build, built
  from the working tree; `make run` shows it by hand. Four rules follow:
  - A static file the JS names by absolute path (a `fetch`, an `import()`, an
    `href` in built markup) goes through `assetURL()` from
    `web/js/asset-url.js`, or it revalidates on every load.
    `TestRealAssetURLCallSitesAreVersioned` checks every call site has a
    versioned URL; a `url()` in `base.css` needs nothing.
  - `index.html` must keep loading the entries as exactly `src="/js/app.js"` and
    `href="/css/base.css"`. The shell substitution matches the quoted strings,
    and a test holds the file to them.
  - **UI specs must not `import("/js/…")` a stateful module** (theme, map theme,
    i18n): against the bundle that is a second copy with its own event bus, so
    the app never hears the change. Use `window.caravel.setTheme` and the other
    setters `app.js` exposes. Pure helpers (`format.js`, `sun.js`) are fine to
    import.
  - `web/js` cannot use bare package specifiers; the bundler refuses them.
    `TestRealTreeBundles` must stay warning-free.

- **Brand assets are generated, not hand-edited.** `web/icons/` comes from
  `scripts/gen_icons.py`, `web/fonts/` from `scripts/gen_brand_fonts.py` —
  run by hand, output committed; each script's docstring lists its
  dependencies. Usage rules: `docs/assets/brand/README.md`.

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

- `make dev` — serves `web/` live from disk (`CARAVEL_WEB_DIR=web`);
  frontend edits need no restart, backend changes do. Migrations run on
  startup.
- `make dev-seed` — seeds demo data and **resets both seeded passwords**
  (`demo`/`demo1234`, `other`/`other1234`); the fix when one has drifted.
  Sessions survive.
- `make ci` — what CI runs; deliberately excludes the docs build.
- `make screenshots` — regenerates `docs/assets/screenshots/` on its own
  throwaway server (:8099); real photos from `images/` (`PHOTO_DIR=…`).
- `make docs` / `make docs-serve` — run `make docs` before committing
  anything under `docs/`.
- Mobile testing: 324×756 (the user's phone), via the Playwright MCP tools
  against `make dev`.
