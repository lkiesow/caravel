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
- **Database migrations.** New schema changes are sequential
  `000N_name.up/down.sql` files, written for *both* dialects
  (`internal/db/migrations/sqlite/` and `.../postgres/`). After editing
  `internal/db/sqlc/queries/*.sql`, run `sqlc generate` by hand from
  `internal/db/sqlc/` to regenerate the dialect packages — there's no
  automation for that step, and it's easy to forget one dialect.

## Dev environment

- `make dev` — runs the server with `CARAVEL_WEB_DIR=web`, so `web/js`,
  `web/css`, and `web/locales` are served live from disk; no restart
  needed after frontend edits. Backend (`internal/`, `cmd/`) changes do
  need a restart — migrations run automatically on startup.
- `make dev-seed` — seeds a demo user/trip for manual testing.
- `make ci` — the same checks CI runs: build, vet, JS syntax, i18n parity,
  `go test`.
- Mobile testing convention: 324×756 (the user's phone's native
  resolution), verified via the Playwright MCP tools against a running
  `make dev` server.
