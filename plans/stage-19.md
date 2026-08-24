# Stage 19 — A suite you can trust

## Context

Three stages running, the UI suite has reported a failure that was not a
regression.

- **Stage 16 Milestone 9** — a location added by hand while trying the
  assistant out broke `map.spec.js`'s distance filter, which asserts an exact
  card count in the seeded Iceland trip. Twice in one evening.
- **Stage 17** — the seeded `cascade` trip had gained stray members (a `pwtest`
  and an `other`), so `file row overflow menu` failed, because visibility
  controls only render on a shared trip. Nothing about files or menus had
  changed.
- **Stage 18 Milestone 2** — two concurrent `make test-ui` runs each saw the
  other's half-restored `pwtest` password, and four tests failed.

Each time the named failure passed in isolation. Each time the investigation
went into code that was fine.

The cause is one design decision. `playwright.config.js` has no `webServer`
block — its header says so deliberately, "so `make test-ui` can be pointed at
whatever is already up" — which means the suite drives whatever is listening on
8080 and reads the shared `make dev-reset` seed. Anything else touching that
database, a person or a second run, changes the fixtures under it.

`scripts/gen_screenshots.sh` already shows the alternative, and was written for
exactly this reason: its header records that a script writing to the shared
scenarios "would poison the next `make test-ui` run", so it takes its own port,
its own `mktemp` database and upload directory, its own seed, and a
`trap cleanup EXIT`. This stage gives the UI suite the same treatment — and
then spends the isolation it buys. The mutating flows that were too dangerous
to cover before, the register page that needs a server setting flipped, German
copy through the sweeps, and contrast finally asserted rather than measured.

**Not in this stage, deliberately: cutting a release.** No tag has ever been
pushed and that stays true. The release-shaped guard rails in Milestone 7 are
here because they are CI checks, not because anything is about to be published.

---

## 1. A throwaway server for the UI suite

The load-bearing milestone. Everything after it assumes the isolation.

**New `scripts/ui_test.sh`**, modelled closely on `scripts/gen_screenshots.sh`
— read that first, it is 118 lines and gets the details right.

- **Pick a free port** rather than defaulting to one. `PORT=` overrides;
  otherwise bind-probe upward from 8090 so two runs never collide. This is what
  makes concurrent runs safe, and it *replaces* the lock file `todo.md`
  suggests — with a private port, database and upload directory there is
  nothing left to contend over.
- `work="$(mktemp -d)"`, then export `CARAVEL_DB_DSN="$work/ui.db"`,
  `CARAVEL_UPLOAD_DIR="$work/uploads"`, `CARAVEL_PORT="$PORT"`,
  `CARAVEL_WEB_DIR=web`, and the stub triple the CI `ui` job already sets
  (`CARAVEL_LLM_URL=stub`, `CARAVEL_LLM_MODEL=stub`,
  `CARAVEL_SEARCH_PROVIDER=stub`) — without them `assist.spec.js` skips itself,
  and a spec that skips silently reads as a pass.
- `trap cleanup EXIT`: kill and wait the server, `rm -rf "$work"`.
- `go build -o "$work/caravel" ./cmd/caravel`, run it backgrounded into
  `$work/server.log`, then a readiness loop **with the `kill -0 "$server_pid"`
  liveness check** that `gen_screenshots.sh` has and `ci.yml` does not. A
  server that dies during startup should report its log immediately, not after
  sixty seconds of `curl`.
- Seed **all seven scenarios** (`go run ./cmd/seed`, no `-scenario` flag),
  unlike the screenshot script which seeds `full` only — `buildRoutes()` in
  `tests/ui/helpers/scenarios.js` sweeps every one of them.
- Then `CARAVEL_TEST_URL="http://127.0.0.1:$PORT" npx playwright test "$@"`.

**`playwright.config.js` does not change.** Its "point me at a running server"
contract (`CARAVEL_TEST_URL || http://localhost:8080`) is exactly what the
script uses, and it is the same handoff `contrast.js` and `gen_screenshots.mjs`
already rely on. Keeping it means the escape hatch survives:
`CARAVEL_TEST_URL=… npx playwright test` still drives a `make dev` server when
you want to watch a failure against the database you can inspect.

**`make test-ui`** becomes `$(PW_ENV) scripts/ui_test.sh $(PW_ARGS)`, and the
script honours an already-set `CARAVEL_TEST_URL` by skipping the server
entirely — the same idiom `scripts/test_postgres.sh` uses to skip its container
when `CARAVEL_TEST_DB_DSN` is set. Same file to copy it from.

**`.github/workflows/ci.yml`**: the `ui` job's "Start the server and seed the
scenarios" step collapses into `make test-ui`. The assistant-capability
assertion stays, but moves *into the script*, so it guards local runs too.

Also update the comments that state the old contract: `playwright.config.js`'s
header, the `test-ui` block in the `Makefile`, and the docs page telling a
contributor how to run the suite.

**Done when:** `make test-ui` is green from a clean clone with nothing on 8080;
two concurrent `make test-ui` runs are both green — the Stage 18 failure,
reproduced first and then gone; and `make dev`'s database and `uploads/` are
byte-identical before and after a run.

---

## 2. The location editor and the trip editor get specs

The two biggest uncovered mutating surfaces. Copy `files.spec.js`'s shape: own
trip via `page.request.post("/api/trips")` in `beforeEach`, deleted in
`afterEach`.

- **Location editor** (`web/js/pages/location-editor-page.js`,
  `web/js/components/location-form.js`): create with a name and category, set
  coordinates through the map picker, add a link and a date row, save, reload
  and assert the values round-tripped; edit one; delete behind its
  confirmation. `notes-preview.spec.js` covers the Write/Preview toggle but
  never presses Save — this is the spec that finally does.
- **Trip editor**: title, **`subtitle`** (not `description`; the screenshot
  script lost time to that field name), dates, cover photo by upload; then the
  settings tab's delete behind its confirmation.

Assert on state rather than screenshots: values after a reload,
`window.location.pathname` after a delete, DOM counts.

---

## 3. Checklists and the itinerary

Same shape, one more trip.

- **Checklists** (`web/js/components/checklist-list.js`): create a list, add
  items, tick, rename an item, **duplicate the list and assert the ticks
  reset** (Stage 15 Milestone 1, unverified by the suite today), delete. On a
  trip with a second member, the three visibilities — including the rule the
  Stage 18 documentation pass found by reading the code: a `trip` checklist is
  tickable by *its author only*, and `canModifyChecklist` refuses a viewer
  outright.
- **Itinerary** (`web/js/pages/itinerary-tab.js`): add an entry to a day, move
  it to another day, unschedule it. `itinerary-order.spec.js` already covers
  reordering within a day — extend that file rather than starting a third.

---

## 4. The register page, and German beyond the menu

Both were blocked on the shared seed; Milestone 1 unblocks them.

- **The register page.** `unauthenticated.spec.js` covers login from a fresh
  context, but the register form only renders when open signup is on, and the
  seed deliberately leaves it off. With a throwaway database the
  `settings.spec.js` restore dance is no longer needed: the spec flips the
  admin setting, registers a user, and the poisoned-run hazard dies with the
  temporary directory. Say that in the spec's header — it is the payoff of
  Milestone 1 and the reason this shape is now allowed at all.
- **German through the sweeps.** `routes.spec.js`'s overflow and tap-target
  sweep, `headings.spec.js` and `a11y-names.spec.js` all run in one locale, and
  German is the longer copy — the case most likely to overflow a box.
  **Default: parameterise by locale but run German at the 324×756 mobile
  viewport only**, rather than doubling an already 180-second matrix in every
  combination. Overflow is a width problem and 324px is where it bites. If a
  German-only desktop failure ever turns up, widen it then.

---

## 5. Real touch: a Chromium project for gesture specs

`map.spec.js` stubs `window.matchMedia` through `addInitScript` because
Playwright's `isMobile` — the option that flips `(pointer: coarse)` and enables
real touch emulation — is Chromium-only, and `hasTouch: true` does not do it.
The stub is honest as far as it goes, but no spec drags a finger: "one finger
scrolls the page, two fingers pan the map" is asserted through Leaflet's
handler state.

Add a **third project** to `playwright.config.js`, `devices["Pixel 5"]`-based,
scoped by `testMatch` to `*.gesture.spec.js` — a Chromium project for gestures,
**not** a second full run of the sweeps. That was the Stage 15 review's
decision and it is the right one: the sweeps are about markup and CSS, where a
second engine mostly buys duplicate failures and doubles the CI job. It needs
`storageState` and the `setup` dependency like the firefox project, plus
`npx playwright install chromium` in CI.

Move the two gesture assertions into `map.gesture.spec.js` as real touch input.
Keep or delete the Firefox `matchMedia` versions, whichever reads better once
both exist — and say which in the Done paragraph.

---

## 6. Contrast asserted, not merely measured

`tests/ui/contrast.js` already does the two hard parts right — it flattens
translucent backgrounds by compositing up the ancestor chain across shadow
boundaries, and it pierces shadow roots — and `--min` already exits 1. What is
missing is anything that runs it. A regression like Stage 07's 2.54:1 primary
button would still only be found by someone thinking to look.

Keep it a script (it is not a `.spec.js` by design; `testMatch` excludes it)
and give it a **threshold table** rather than one global floor, because that
decision is precisely why it stalled: normal text 4.5, large text and
non-text/UI boundaries 3.0, decorative fills excluded by name. Then a
`make check-contrast` target and a CI step inside the `ui` job — the job that
has a server. `--self-test` runs first, so a broken compositor cannot report a
false pass.

Prove it by re-creating the 2.54:1 button and watching CI go red.

---

## 7. Guard rails and sweep-up

Small, all CI- or tooling-shaped.

- **`scripts/check_migrations.py`**, in `make ci` and `ci.yml`: the migration
  count per dialect never decreases against `main`, and the two dialects agree.
  The Stage 18 squash was safe because nobody had a database; a second one
  would silently brick every instance created since, and nothing prevents it
  today.
- **`make image`** — pass `--format docker` when the tool is podman, detected
  the way `scripts/test_postgres.sh` picks its compose command. Without it the
  `HEALTHCHECK` is dropped with a warning nobody reads in the middle of a build
  log.
- **`scripts/without.sh`** — tell "the command failed" apart from "the command
  never ran". It has twice reported "OK — genuinely depends on your change"
  when the real cause was a compile error and a bad grep pattern, which is the
  exact wrong answer for a tool that exists to prevent wrong answers.
- **`auth.SetPassword`'s doc comment** claims the function is unreachable from
  any HTTP route; `handleAdminResetPassword` calls it. One line, and it is the
  kind of comment somebody trusts while reasoning about session invalidation.
- `todo.md` in both directions, per milestone.

---

## Build order

1. The throwaway server — first and alone; every later milestone assumes it.
2. Location editor and trip editor specs.
3. Checklists and itinerary specs.
4. Register page, German sweeps.
5. Chromium gesture project.
6. Contrast in CI.
7. Guard rails and sweep-up.

Coverage (2–3) comes before the rest because that is where the stage's value
is: the isolation is only worth building if something uses it.

---

## Files this touches

- `scripts/ui_test.sh` (new), `Makefile`, `.github/workflows/ci.yml`
- `playwright.config.js` — a third project only; the URL contract is unchanged
- `tests/ui/`: new `locations.spec.js`, `trip-editor.spec.js`,
  `checklists.spec.js`, `register.spec.js`, `map.gesture.spec.js`; edits to
  `itinerary-order.spec.js`, `routes.spec.js`, `headings.spec.js`,
  `a11y-names.spec.js`, `contrast.js`
- `scripts/check_migrations.py` (new), `scripts/without.sh`,
  `internal/auth/auth.go`
- `plans/stage-19.md`, `plans/todo.md`

Reused rather than rebuilt: `scripts/gen_screenshots.sh`'s server bootstrap,
`scripts/test_postgres.sh`'s tool detection and env-set escape hatch,
`tests/ui/helpers/scenarios.js` (`gotoRoute`, `blockExternalRequests`,
`openAs`, `resolveScenarioTrips`, `buildRoutes`), and `files.spec.js` /
`expenses.spec.js` as the own-trip templates.

---

## Verification

- `make ci` green at every milestone. i18n parity matters in any milestone that
  adds a user-facing string — none are planned to, so a parity failure means
  something unintended happened.
- **Milestone 1 is proved by the failure it removes**: two concurrent
  `make test-ui` runs both green, and a `make dev` database untouched by a run.
  Nothing else in this stage means much if that is asserted rather than
  demonstrated.
- Every new spec is verified by breaking the thing it covers and watching it go
  red. A mutating spec that passes against a broken app is the exact failure
  mode this stage exists to fix.
- Milestone 5: the gesture spec fails when Leaflet's `dragging` handler is
  disabled, on real touch input.
- Milestone 6: a deliberately low-contrast button fails CI.
- Milestone 7: `check_migrations.py` verified by planting both failures — a
  removed migration, and a dialect mismatch.

Mind `scripts/without.sh`'s known flaw while it is still unfixed: it reports
success on *any* non-zero exit, compile errors included. Read its output, not
its verdict — until Milestone 7.

---

## Workflow

Per `CLAUDE.md`: one milestone at a time — implement → verify (`make ci` green
plus evidence the behaviour actually changed) → add a **Done.** paragraph to
this file → update `plans/todo.md` in both directions → one commit per
milestone (follow-ups get their own) → make sure `make dev` is running → stop
and hand back control. No starting the next milestone until told to.
