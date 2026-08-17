# Stage 08 — Developer tooling

## Context

Stage 07 was a Playwright-driven UI/UX round, and the thing it produced
most of wasn't fixes — it was throwaway tooling. `docs/plans/todo.md`'s
"Developer tooling" section records the actual counts: an i18n-editing
heredoc hand-rolled **7 times**, a contrast measurement script **~6
times**, a non-vacuity check **5 times**, a server restart **4 times**
(wrong twice). Two of those re-derivations went beyond wasted effort into
recorded near-misses:

- **A false pass.** Milestone 3's validation appeared to fail because a
  stale server still held :8080 and `make dev` had died behind it.
  `pkill -f "go run ./cmd/caravel"` doesn't find the compiled child (its
  argv is a `~/.cache/go-build/...` path), so the obvious cleanup doesn't
  work.
- **A green CI on a file the browser refuses to load.** `make check-js`
  runs `node --check`, which parses each file as a *script*, where `<!--`
  legally opens a comment (Annex B). The app loads them as ES *modules*,
  where that's a syntax error. Milestone 8 shipped a broken Documents tab
  with `make ci` green.

Every UI-facing check in this repo is still manual. The heading outline,
accessible-name and light/dark sweeps that Stage 07 wrote by hand each
round are cheap to assert once and expensive to keep re-typing — and the
shadow-DOM parts (where the actually-wrong headings lived) are exactly
what gets skipped when the check is inconvenient.

**Intended outcome:** the tools Stage 07 kept rebuilding exist, are
committed, and are wired into `make`/CI — so the next stage's
verification step is running a command rather than reconstructing a
script. This stage adds **no user-facing behavior**; the one exception is
the seed data, which only affects local dev.

### Decisions taken up front (confirmed with the user)

- **Playwright lands as a real suite, local *and* CI.** A root
  `package.json` with `@playwright/test` as a **devDependency** — the
  first npm deps in the tree. This does *not* reverse Stage 01's
  "no frontend build step" call: the app still ships zero-build,
  hand-written ES modules served straight from `web/`. Nothing in
  `package.json` is a runtime or bundling dependency.
- **Firefox** is the browser, per the original note in `todo.md`.
- **`make dev-reset` is destructive but guarded** — wipes
  `data/caravel.db` and `uploads/`, but refuses unless the DSN resolves
  inside the repo, and prompts unless `FORCE=1`.
- **Scripts match existing conventions** — Python for the i18n editor
  (alongside `scripts/check_i18n.py`, stdlib `json` only), POSIX `sh` for
  the non-vacuity helper, JS for contrast (it has to run in the browser).

### Explicitly out of scope

Everything else in `todo.md`: the app-behavior fixes deferred from Stage
07 (unstyled Not-found, split-save coordinates, `show_on_map` gating,
image-URL errors, mobile map scrolling, the polish batch), the migration
squash, the `itinerary.noDates` copy fix, and every feature idea. Stage
08 builds the instruments; the next stage uses them.

---

## 1. Fix `make check-js` to parse as modules

The smallest milestone and the only one fixing an active correctness bug,
so it goes first and unblocks trusting `make ci` for the rest.

Change `Makefile`'s `check-js` target and the matching *JS syntax check*
step in `.github/workflows/ci.yml` (they duplicate the command) to pipe
each file in on stdin, which makes the parse mode explicit:

```sh
find web/js -name '*.js' -print0 | \
  xargs -0 -n1 -I{} sh -c 'node --input-type=module --check < "$1" || exit 255' _ {}
```

The `exit 255` matters: `xargs` only aborts the whole run on 255, so
without it a failing file gets reported but the target can still exit 0.

**Verify.** `make check-js` passes on the current tree including the
vendored `web/js/vendor/leaflet/leaflet.esm.js`. Then prove
non-vacuity by hand (Milestone 6 automates this): append a comment
containing `<!--` and a stray backtick to a scratch copy of a `web/js`
file, confirm the old command accepts it and the new one rejects it,
revert.

**Done.** Landed as `scripts/check_js.sh`, with `make check-js` and
ci.yml's *JS syntax check* step both reduced to calling it — a deviation
from the plan's inline `xargs` one-liner, taken because the duplication
between those two places is exactly why this bug needed fixing twice, and
because `check-i18n` already delegates to `scripts/check_i18n.py`, so a
script is the established shape. Two further deviations, both forced by
testing rather than preference: the shebang is `#!/usr/bin/env bash`, not
`sh`, because the NUL-safe `read -r -d ''` and process substitution are
bash-only and GitHub Actions' `/bin/sh` is dash — the plan's form would
have broken CI; and each failure echoes its own path, because
`--input-type=module` reads from stdin and so Node reports errors against
`[stdin]:155` with no filename, which would have made a CI failure
near-undebuggable. The `exit 255` trick the plan called for is moot in a
`while` loop: the script counts failures and exits 1 if any, so it
reports *every* bad file rather than aborting at the first. Added beyond
the plan: a guard that exits non-zero if the find turns up zero files, so
the target can't pass vacuously if the tree ever moves.

Verified: (a) passes on the current tree — 28 files including the
vendored `leaflet.esm.js`. (b) Non-vacuity, reconstructing the original
Stage 07 Milestone 8 failure in the very file it broke: appending
``<!-- a note mentioning a `template literal` -->`` to
`web/js/components/document-list.js` is **accepted** by the old
`node --check` command and **rejected** by the new one
("SyntaxError: HTML comments are not allowed in modules", naming the
file), and `make ci` as a whole goes red, confirming the check actually
gates CI. (c) The empty-tree guard exits 1 from a directory with no
`web/js` files. (d) The premise is real: `web/index.html:17` loads the
app via `<script type="module">`, so module parsing is the mode that
matters. Tree restored clean afterwards; `make ci` green.

## 2. `scripts/i18n.py` — write side of the locale files

`check_i18n.py` covers parity (the read side); this is the write side.
Same style: stdlib only, discovers `web/locales/*.json` by glob so a
third language needs no edit here.

Subcommands:

| Command | Behavior |
|---|---|
| `set <key> <locale>=<value> ...` | Create or update a key across **all** locales in one call. Fails if a locale is left unspecified and the key is new. |
| `set --after <anchor-key> ...` | Insert a new key immediately after `anchor-key` rather than at end-of-file, so related copy stays together. |
| `rm <key>` | Delete from every locale. |
| `--unused` | List keys not referenced anywhere under `web/js` (Stage 07 added 11 keys and never checked for orphans). |

Formatting is the hard requirement: preserve existing key order and the
current 2-space, `ensure_ascii=False` style, so a one-key change is a
one-line diff. Use `json.load(..., object_pairs_hook=OrderedDict)` (or
rely on dict insertion order) and re-dump; do **not** hand-splice text.

`--unused` scans for both `t("key")` and `t('key')` plus
`data-i18n="key"`-style attribute references — grep `web/js` for how
`i18n.js`'s `t()` is actually called before fixing the pattern, and have
`--unused` print (not delete) so a dynamically-composed key can't be
silently dropped.

**Verify.** Round-trip test: dump `web/locales/en.json`, run a `set` on
an existing key back to its own value, confirm `git diff` is empty.
Then add a throwaway key with `--after`, confirm it lands in the right
position in both `en.json` and `de.json` and that `make check-i18n`
passes, then `rm` it and confirm the tree is clean again. Run `--unused`
against the real tree and report what it finds (findings themselves go
to `todo.md`, not fixed here).

## 3. `make dev-restart` — restart by port, prove the binary is new

Kill whatever holds the port **by port**, not by process name:

```sh
ss -lptn "sport = :$(PORT)"   # extract pid
```

Then start `make dev` in the background, poll a health endpoint until it
answers (with a timeout that fails loudly rather than hanging), and print
the new pid. `make dev` itself should also **fail loudly on a busy port**
rather than dying quietly in the background — check the port before
`go run` and exit non-zero with a clear message.

Optional but wanted: `make dev-restart MARKER=someNewString` asserts
`strings /proc/<pid>/exe | grep -q "$MARKER"` after startup, which is the
check that would have caught Stage 07 Milestone 3's false pass. If a
`/healthz`-style endpoint doesn't exist yet, use any cheap
always-200 route rather than adding one — check `internal/httpapi/router.go`.

**Verify.** Start `make dev`, note the pid. Run `make dev-restart` and
confirm the pid changed and the app answers. Then reproduce the original
trap deliberately: leave a server running, confirm plain `make dev` now
*reports* the busy port instead of failing silently, and confirm
`make dev-restart` clears it. Finally, add a unique string constant to a
Go file, `make dev-restart MARKER=<that string>`, confirm it passes —
then check it *fails* against a server started before the edit.

## 4. Seed scenarios in `cmd/seed` + `make dev-reset`

`cmd/seed/main.go` currently seeds exactly one shape (a demo user and one
4-day Iceland trip). Stage 07 needed a specific shape every milestone and
built each through ad-hoc `fetch` calls, then hand-deleted the leftovers
imperfectly — stray test trips are in the dev database now.

Refactor `seedTrip` into named scenarios selected by
`make dev-seed SCENARIO=<name>` (default: all). From `todo.md`:

- `full` — today's demo trip, **with coordinates and `ShowOnMap: true`**
  on its items. Currently every seeded item is `show_on_map: false` (the
  Go zero value) and has no coordinates, so the seeded Map tab is empty
  until you edit each location by hand. Fixes that `todo.md` entry.
- `one-pin` — exactly one mappable location (the zero-size map-bounds case).
- `start-only` — a start date, no end date.
- `year-boundary` — a trip crossing 31 Dec.
- `no-dates` — neither date set (also the state the `itinerary.noDates`
  copy bug shows up in).
- `out-of-range-days` — itinerary days outside the trip's own range.
- `cascade` — a trip with children (items, days, entries, checklists,
  documents) for delete-cascade checks.

Scenarios must be **deterministic** so Milestone 5's UI suite can assert
against them: derive dates from a fixed base date, not `time.Now()`,
except where a scenario is explicitly about "upcoming". Trip titles get a
stable prefix so the suite can find them by name.

`make dev-reset` deletes `data/caravel.db` and `uploads/`, then reseeds.
Guards, both required: resolve `CARAVEL_DB_DSN` and refuse if it isn't
under the repo root; and prompt for confirmation unless `FORCE=1`.

**Verify.** `make dev-reset FORCE=1`, then walk each scenario in the
browser at 1280×800 and confirm it renders the shape it claims —
specifically that the `full` trip's Map tab now shows pins (the current
bug) and `one-pin` renders a single marker without a degenerate zoom.
Run `make dev-reset FORCE=1` twice in a row and confirm it's idempotent.
Then confirm the guard: with `CARAVEL_DB_DSN=/tmp/elsewhere.db`,
`make dev-reset FORCE=1` must refuse and exit non-zero.

## 5. Playwright UI suite (`tests/ui/`)

The centerpiece. New root `package.json` (devDependencies only:
`@playwright/test`), `playwright.config.js` with a **firefox** project and
`baseURL` from an env var defaulting to `http://localhost:8080`, and
`make test-ui`.

Specs, all from `todo.md`'s list of what Stage 07 hand-rolled:

1. **`routes.spec.js` — the sweep matrix.** Routes × {1280×800, 324×756}
   × {light, dark}. Routes come from `web/js/app.js`'s seven `pattern:`
   entries, with `:tripId`/`:itemId` filled from Milestone 4's
   deterministic scenarios. Dark mode via
   `page.emulateMedia({ colorScheme })` — no OS/browser theme changes.
   Per cell: assert **`window.location.pathname` equals the intended
   route first** (the app's router silently redirects unmatched paths to
   `/trips`, which is how Stage 04's own check passed trivially against
   the wrong page for several milestones), then
   `document.documentElement.scrollWidth <= window.innerWidth`, then a
   ~44px minimum height on interactive controls at mobile width.
2. **`headings.spec.js` — heading outline.** Walk the light DOM **and
   every shadow root** in document order; assert exactly one `h1`, first,
   and no skipped level. The shadow walk is the whole point — the
   trip/location card headings Stage 07 fixed live in shadow DOM and a
   plain `document.querySelectorAll` sweep misses them entirely.
3. **`a11y-names.spec.js` — accessible names.** Every `input`, `select`,
   `textarea` and `button` resolves a non-empty name from `aria-label`,
   `aria-labelledby`, a wrapping/associated `<label>`, `placeholder`,
   text content or `title`. Stage 07's hand-rolled version found 157
   controls across 10 routes.

Shared helpers in `tests/ui/helpers/`: a login fixture using the seeded
demo user, a `deepQueryAll` that pierces shadow roots (specs 2 and 3 both
need it), and the scenario-trip lookup.

Also `tests/ui/contrast.js` — a **standalone script**, not a spec, since
its output is a measurement to read rather than a pass/fail: takes a
route, selectors and a colour scheme; reports computed
text-vs-background and fill-vs-surround ratios against WCAG thresholds.
Two parts must be written down properly, because they're what made the
hand-rolled versions untrustworthy: **flattening translucent
backgrounds** over whatever is actually behind them (the danger tint is
`rgba(...)`, so a naive read measures against transparency and reports
nonsense), and **reaching into shadow roots** (reuse `deepQueryAll`).
This is what found Stage 07's 2.54:1 primary buttons and 3.08:1 error
text.

CI: a new job in `.github/workflows/ci.yml` that installs Node deps and
`npx playwright install --with-deps firefox`, builds and starts the
server against a temp SQLite DB, seeds, and runs `make test-ui`. Keep it
a **separate job** from the existing fast `ci` job so a browser-install
failure doesn't mask a Go regression.

**Verify.** `make test-ui` green against a `make dev-reset`ed server.
Non-vacuity, using Milestone 6's helper for each: temporarily demote an
`h1` to `h3` → the heading spec fails; strip one `aria-label` → the name
spec fails; force an element wider than the viewport → the mobile sweep
fails. Also confirm the URL assertion catches the original footgun: point
one route at a deliberately nonexistent path and confirm it fails rather
than silently testing `/trips`. Confirm the CI job passes on a branch push.

## 6. `scripts/without.sh` — the non-vacuity helper

```
scripts/without.sh <file>... -- <command>...
```

`git stash push` the named files, run the command, restore them
unconditionally (trap on `EXIT`/`INT` so an interrupted run can't leave
the tree stashed), and **exit non-zero if the command passed** without
the change. That inversion is the point: it turns "the test passes" into
"the test would have caught this."

Guards worth having: refuse if the named files have no changes to stash
(a no-op run would report a false "would have caught it"), and refuse on
a dirty index it doesn't own, so it can't swallow unrelated work.

Placed *after* Milestone 5 because the UI suite is its best customer, but
it applies equally to `go test` — Stage 07 hand-rolled it 5 times across
date validation, delete-day tests, day ordering, the accessible-name
sweep and the heading audit.

**Verify.** Self-referential and cheap: run
`scripts/without.sh internal/httpapi/itinerary.go -- go test ./internal/httpapi/`
against a real Stage 07 validation change and confirm it exits 0 (the
test *did* catch it). Then run it naming an unrelated file and confirm it
exits non-zero (vacuous). Confirm the trap restores the tree after
`Ctrl-C` mid-run.

## 7. Shared Go HTTP test harness

`internal/httpapi/itinerary_test.go` already brings up a real `Server`
over a real migrated SQLite database in a temp dir and drives requests
through the full router and auth middleware — `newTestServer`, `login`,
`do`, `decode` (lines 25–139). Those helpers are private to that one
file. Lift them into `internal/httpapi/testing_test.go` unchanged, then
add coverage for the handlers that have **none** today: trips, items,
checklists, documents, media — at minimum a cross-user ownership check
per resource, which is the case no unit test can express and the reason
the harness was written.

Leave `trips_test.go` and `documents_test.go` alone; they're pure unit
tests of validation and content-type sniffing and don't need the harness.

**Verify.** `make ci` green with the tests moved (a pure refactor
first, in its own commit if it helps review). For each new ownership
test, run it through `scripts/without.sh` against the handler it covers
to prove it isn't vacuous. Report the before/after picture — which
handlers went from zero coverage to covered.

---

## Build order

1 → 2 → 3 → 4 → 5 → 6 → 7.

Milestone 1 first because everything after it relies on `make ci` being
honest. Milestone 4 before 5 because the UI suite needs deterministic
data to navigate to. Milestone 6 after 5 so the UI suite is available as
its first real test subject, though 6 is independent and could move.
Milestone 7 is fully independent and sits last so it can be dropped
without disturbing anything if the stage runs long.

## Workflow

Per `CLAUDE.md`, **one milestone at a time**, in order. For each:

1. **Implement.**
2. **Verify** — `make ci` green, plus the milestone's own verification
   above. Prefer assertions over screenshots; a passing test beats a
   matching image. This stage's whole subject is verification tooling, so
   proving each tool *actually catches the thing it's for* (non-vacuity)
   is part of the milestone, not a nicety.
3. **Update `docs/plans/stage-08.md`** — add a "**Done.**" paragraph to
   that milestone's section saying what actually landed (including
   deviations) and how it was verified. Then update `docs/plans/todo.md`
   in **both** directions: remove the entries this milestone implemented
   (each of Milestones 1–7 clears at least one), and add anything it
   surfaced but deferred — e.g. whatever `--unused` reports in Milestone
   2, or issues the new UI suite finds in Milestone 5.
4. **Commit** — one commit per milestone (a follow-up fix gets its own
   "... follow-up: ..." commit). The message says what changed, why, and
   exactly how it was verified.
5. **Make sure the dev server is running** (`make dev`) so the result is
   immediately testable, then **stop and hand back control.**
6. **Wait.** Do not start the next milestone until told to continue.
   Feedback at a checkpoint gets fixed and re-verified *before* moving
   on, never folded silently into the next milestone.

## Verification (whole stage)

The stage is done when, from a clean checkout:

- `make ci` is green **and** rejects a module-invalid JS file (M1).
- `python3 scripts/i18n.py set --after <anchor> ...` round-trips with an
  empty diff and keeps `make check-i18n` green (M2).
- `make dev-restart` reliably replaces a running server by port, and
  `MARKER=` proves the new binary carries a just-added string (M3).
- `make dev-reset FORCE=1` is idempotent, seeds every named scenario,
  and refuses a DSN outside the repo (M4).
- `make test-ui` is green locally and in GitHub Actions, and each of its
  three specs demonstrably fails when the thing it checks is broken (M5).
- `scripts/without.sh` exits non-zero on a vacuous test and restores the
  tree even when interrupted (M6).
- `go test ./internal/httpapi/` covers a cross-user ownership case for
  trips, items, checklists, documents and media (M7).

Nothing user-facing changes, so there is no user-visible regression
surface beyond the seed data — but `make test-ui`'s first full run
doubles as a regression check that Stage 07's fixes are all still in
place.
