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

**Done.** `scripts/ui_test.sh` starts a throwaway instance -- own port, own
SQLite database, own upload directory, own seed, own saved sessions, all under
one `mktemp -d` removed by a `trap cleanup EXIT`. `make test-ui` goes through
it; `playwright.config.js` is unchanged except its header, because the script
simply sets `CARAVEL_TEST_URL`, and setting that yourself still points the
suite at a server you already run. CI's `ui` job lost its whole
start-the-server-and-seed step and its assistant check: both moved into the
script, so they guard a developer's run too.

*Two things the plan did not anticipate.*

**Saved sessions had to move as well.** `AUTH_STATE_FILE` was the literal path
`tests/ui/.auth/demo.json`, and cookies are not scoped by port -- so two runs
sharing that directory would hand each other a token their own server never
issued, and every spec would fail as if logged out. `scenarios.js` now reads
`CARAVEL_TEST_AUTH_DIR`, which the script points into its temp dir.

**Probing for a free port and then binding it is racy, and the race is not
benign.** The first version did what `gen_screenshots.sh` does: `ss` to check
the port is free, start the server, poll `/api/health`. Two concurrent runs both
picked :8090, one server failed to bind -- and the health poll of the run that
lost was *answered by the other run's server*. So it drove an instance it did
not own, and collapsed with `NS_ERROR_CONNECTION_REFUSED` when the winner tore
that instance down: 10 failed, 98 passed, and both logs claiming "an isolated
instance is up on :8090". The hazard this milestone exists to remove,
faithfully reproduced inside the fix for it. Readiness now means *the socket on
this port is held by my own pid*, which `ss -ltnp` reports authoritatively;
liveness is checked before the port is asked anything, since a dead server has
nothing to say and the port would only give somebody else's answer. Note that
a log-based check would have been wrong too: the server prints "listening
on :8080" and *then* fails to bind.

*Verified.* `make ci` green. The Stage 18 failure reproduced first and then
gone: two concurrent `npx playwright test settings.spec.js` against one shared
dev server fail in both runs, run B blaming the seed ("has `make dev-reset
FORCE=1` been run?") exactly as recorded -- and two concurrent `make test-ui`
runs afterwards are 108/108 each on :8090 and :8091. Three simultaneous starts
take three distinct ports. A full run leaves `data/caravel.db` byte-identical
and `tests/ui/.auth/` untouched, and leaves no temp directory behind. Green
with nothing listening on :8080 at all. Both new failure paths were made to
fire rather than assumed: a seed that cannot start prints its log and stops, and
a server without the stub assistant refuses to run the suite.

*Two things this surfaced, both recorded in `todo.md` rather than fixed here.*
Playwright's `test-results/` is still shared and emptied at startup, which costs
a concurrent run its traces on failure but cannot fail a passing run. And the
10-logins-per-minute limiter is now per-run rather than per-machine, which is an
improvement -- but a `GREP` that selects a login-heavy subset finishes fast
enough to spend that budget by itself, and reports a 429 that reads like a
broken seed.

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

**Done.** Two specs, both owning their own trips: `tests/ui/locations.spec.js`
(create with links and dates, edit, delete) and `tests/ui/trip-editor.spec.js`
(create from `/trips/new` including a staged cover photo, edit from the Settings
tab, delete, and both validation paths). Six tests; the suite goes from 108 to
114.

*One scope correction.* The plan said to set coordinates through the map
picker. `map.spec.js` already does that properly -- a real DOM click inside the
map's shadow root, a Save, then a read back through the API proving the stored
point is the one the fields showed -- so repeating it here would have bought a
second copy of the same evidence. `locations.spec.js` types the coordinates
instead, which covers the half that spec does not: that lat, lng and the address
ride along with the rest of the form and come back out on the view page. Said
plainly in the spec's header so the omission does not read as an oversight.

*Two things worth knowing about the surfaces.* The trip *editor* and the trip
*settings tab* are different code around the same `trip-form.js`: `/trips/new`
puts its submit button outside the form and stages a cover photo in memory until
the trip has an id, while the settings tab renders the form's own actions row
and writes immediately. Both are driven, because a spec covering one would leave
the other's wiring unasserted. And the trip detail page canonicalises
`/trips/{id}` to `/trips/{id}/locations`, so a post-create or post-delete URL
assertion has to expect the tab, not the bare trip -- the first version of the
delete test failed on exactly that.

*Verified.* `make ci` green, full suite 114 passed. Every new spec was checked
by breaking what it covers and watching it go red, five times, each reverted
immediately: the location editor no longer sending `links`, the trip form no
longer sending `subtitle`, the trip delete no longer honouring its confirmation,
the location delete no longer honouring its confirmation, and the edit form no
longer prefilling the notes. All five failed on the assertion that names the
behaviour, not on a timeout somewhere downstream.

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

**Done.** `tests/ui/checklists.spec.js` (four tests) and a second describe in
`tests/ui/itinerary-order.spec.js` (two), both owning their trips. The suite
goes from 114 to 120.

*A scope correction, and this one is a missing feature rather than a
misjudgement.* The plan asked for "add an entry to a day, move it to another
day, unschedule it". **Neither of the last two exists.** `itinerary.go` offers
create, reorder and delete on an entry and nothing that reassigns its day; there
is no client affordance and no route, so moving an entry today means deleting it
and adding it again on the other day. And nothing is named "unschedule" --
though deleting an entry is exactly what it would mean, since the location
itself is untouched, so the spec asserts that half explicitly: after removing
the entry, `GET /trips/{id}/items` still has the location. The genuinely missing
move went to `todo.md`. What replaced the two phantom cases: adding a day
through its own form, and removing a day with entries on it behind its
confirmation.

*What was already covered, so is not covered twice.* `menu.spec.js` asserts the
visibility grouping and exactly which moves each card's menu offers, read-only
against the seed, and `sharing.spec.js` asserts what a viewer is not offered.
So the new spec drives the mutations those menus lead to -- create, add, tick,
reword, remove, duplicate, rename, delete -- plus the one move between
visibilities, which is checked from *both* sessions: after the owner makes a
list private it leaves the shared section, and it disappears from the other
editor's page entirely rather than merely going read-only.

*Two small things cost time, both worth recording.* The day-removal dialog
passes its own `confirmKey: "common.remove"`, so its confirm button says
**Remove** and not Delete -- a locator looking for "Delete" simply waits out the
180-second timeout rather than failing usefully. And a bare read straight after
`page.reload()` races the tab's own fetch and comes back empty, which reads as a
persistence failure; the rest of the file settles on a count first, and now this
does too.

*Verified.* `make ci` green, full suite 120 passed. Five breakages, each
reverted: a duplicate that carries its ticks over (the Stage 15 behaviour is now
actually pinned), a tick never sent to the server, the checklist delete
confirmation ignored, removing an entry also deleting its location, and the day
confirmation ignored. All went red. Four failed on the assertion naming the
behaviour; the fifth -- the missing day dialog -- failed by timing out on a
Cancel button that never appears, which is the right verdict arrived at slowly.

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

**Done.** `tests/ui/register.spec.js` (two tests) and a German pass added to
`routes.spec.js` and `a11y-names.spec.js`. 120 to 125, and the whole suite went
from 2.2 to 2.3 minutes -- the sweeps parallelise across workers, so the extra
dimension costs slots rather than wall clock.

*The register page.* It only exists when open signup is on, which is a global
instance setting, so the spec turns it on and off again. That is exactly what
was unaffordable before Milestone 1 and is cheap now: the database belongs to
the run. Two things are still handled with care. The window is one
open-and-close per test in a serial block rather than one per locale, because
`unauthenticated.spec.js` asserts the *absence* of the register link and skips
itself if it finds signup open -- and a silent skip reads as a pass. And the
restore is asserted, not assumed, which is the lesson `settings.spec.js` was
built around. Registration shares the login limiter (10/min/IP, one bucket for
every worker on localhost), so exactly one account is registered; the German
half asserts the rendered form, since the submit path is language-independent.
The assertions are on structure rather than copy, because in German the
heading, the submit button and the "log in" link are all the same words.

*German, and where it was not added.* `routes.spec.js` gains one combination --
mobile, light, German -- not a doubling: overflow is a width problem and neither
it nor the tap-target floor depends on colour, so the other three would cost a
full 23-route sweep each and measure nothing new. `a11y-names.spec.js` runs both
languages. **`headings.spec.js` was deliberately left alone**: heading *levels*
are structural and identical in every language, so a second pass would assert
the same tree shape twice for 23 more page loads.

*One claim checked rather than asserted, and it was wrong the first time.* The
justification for the German name sweep is that `check_i18n.py` compares keys
and never values, and `t()` resolves with `??`, so an empty German string
reaches the DOM. Emptying `auth.userMenu` proved nothing -- that button also has
visible text to take a name from. Emptying `checklists.listActions`, which is
icon-only, is the real reproduction: `make ci` green, English green, German red
on the checklist card's menu trigger. The spec's header now says the narrower
true thing, that this only catches controls whose *only* name is the translated
string.

*The other half of that.* Nothing in the sweeps proved the locale had taken
effect, so a German pass that quietly rendered English would have been green and
worthless. Both sweeps now assert `<html lang>` once per run, verified by
labelling the combination German and running it in English on purpose.

*Verified.* `make ci` green, full suite 125 passed. Three breakages, each
reverted: an empty icon-only German label (German red, English green), the
German combination running in English (the `lang` guard fires), and the register
link rendered regardless of the setting (the closed-signup assertion fires). The
German sweep currently finds no overflow and no undersized control -- it is
clean, not merely present.

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

**Done.** A third project, `chromium-gestures`, scoped by `testMatch` to
`*.gesture.spec.js` and nothing else, plus `tests/ui/map.gesture.spec.js` with
two tests. 125 to 127. The firefox project gained a matching `testIgnore`, or
the top-level `*.spec.js` match would have run the gesture file there too.

*Real input, not synthetic events.* The gestures go through CDP's
`Input.dispatchTouchEvent`. Dispatching `TouchEvent`s from `page.evaluate` would
not do: Leaflet would see them, but the browser would not, and "one finger
scrolls the page" is a claim about the browser's own scrolling — untrusted
events never scroll. Each finger needs its own `id` in the touch points, or CDP
treats two points as one touch and Leaflet never sees a second finger.

*The stubbed versions in `map.spec.js` stay.* They assert a different thing —
that the handlers are *configured* correctly, on the engine the rest of the
suite runs — and they are the only place that would catch the media query itself
breaking, since the gesture spec never consults it. Both files now say so.

*One false pass, caught and fixed.* The map sits low on the trip page: at
324×756 its box runs from about y=617 to y=937, so most of it is **below the
fold**. CDP delivers nothing outside the viewport, silently, so the first
version of the one-finger test was green with zero touch events reaching the
page — the map "did not move" because nothing had been touched. The spec now
scrolls the map into view and asserts the touch point is on screen before
dragging. Worth remembering: a gesture test that passes proves nothing unless
the target was reachable.

*Verified.* `make ci` green, full suite 127 passed. Two breakages, each
reverted, and they are the ones the milestone existed for: leaving `dragging`
enabled on a coarse pointer — the exact Stage 13 bug, the map swallowing the
page scroll — fails the one-finger test, and `touchZoom: false` fails the
two-finger test. Neither could have been caught by the handler-state assertions,
which is the whole argument for this project. CI installs Chromium alongside
Firefox; WebKit is still not downloaded.

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

**Done.** `contrast.js` gains `--strict` and a repeatable `--route`;
`make check-contrast` runs the self-test and then sweeps `/trips`, `/settings`
and `/trips/new` in both palettes; CI runs it as its own step in the `ui` job.
110 elements measured, all at or above their own threshold. No new tests -- this
is a script, not a spec, and a failure should read as "the palette moved" rather
than as a broken test.

*The threshold table already existed.* `report()` has always computed a
per-element threshold -- 4.5 for normal text, 3.0 for large text and for
non-text -- and printed it. What was missing is that `main()` ignored it and
only honoured the flat `--min`. So the work was not writing the table but
*enforcing* it, which is why `--strict` is a separate flag from `--min` rather
than a default for it.

*One exemption, and only one.* `.app-brand`, the header lockup, measures 3.59:1
in dark mode -- lightened navy on the dark ground. WCAG 1.4.3 exempts logotypes
outright ("text that is part of a logo or brand name has no minimum contrast
requirement"), and clearing 4.5 would mean the app not using its own brand
colour. Exempt elements are still measured and still printed, marked `sk` with
the reason; they just do not fail the build. The list is deliberately short and
the rule for adding to it is written down: an exemption is a claim that the
guideline does not apply, and if the claim is wrong the fix is the colour.

*A refactor the milestone needed.* After Milestone 1 the CI `ui` job starts no
server -- `make test-ui` brings up its own and tears it down -- so a contrast
step had nothing to talk to. Rather than a second copy of the bootstrap, it came
out of `ui_test.sh` into **`scripts/with_server.sh <command>`**, which both now
use; `ui_test.sh` is six lines. Milestone 1's guarantee was re-proved afterwards
rather than assumed: two concurrent runs, distinct ports (:8094 and :8095), both
green, `data/caravel.db` unchanged.

*Verified.* `make ci` green, full suite 127 passed, contrast clean. Three
breakages, each reverted. The one the plan asked for: setting
`--color-accent-strong` back to the light-mode blue in dark mode reproduces
**Stage 07's primary button at 2.54:1** -- the same number that stage found by
hand -- and it now fails the build on all three routes. Plus the two guards that
stop a false pass: a compositor that returns the raw tint instead of flattening
fails the self-test (so a broken measurement cannot report confident nonsense),
and a selector list that matches nothing is reported as "the selectors are
probably not matching" rather than as a clean sweep.

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

**Done.** Four items, no new tests -- all of this is tooling.

**`scripts/check_migrations.py`**, in `make ci` and CI. Three checks: every
version has both directions and the numbering is contiguous; the two dialects
define the same versions under the same names; and, against a base commit, no
version that existed has been removed or renamed. The third is the squash guard
and the reason the file exists. It needs git history for the base, which a
default shallow checkout does not have -- so the `ci` job now checks out with
`fetch-depth: 0`, and when there is no base the script says so and skips that
check rather than reporting a pass it did not earn. Verified by planting all
four failures: a missing `.up.sql`, a migration present for one dialect only, a
gap in the numbering, and the squash itself (renaming `0001_init` to
`0001_squashed` on both dialects, which is caught as a rename against
`origin/main`).

**`make image`**, picking the tool the way `scripts/test_postgres.sh` picks its
compose command, and passing `--format docker` for podman. Verified on this
machine, which has podman and no usable docker: a plain `podman build` prints
`HEALTHCHECK is not supported for OCI image format and will be ignored` twice in
the middle of the build log, and the resulting image has no healthcheck;
`make image` prints no such warning and `podman image inspect` shows
`{"Test":["CMD","/caravel","-health"],...}`. It also passes `VERSION`, without
which the binary calls itself `unknown` because `.git` is not in the build
context.

**`scripts/without.sh`** now tells a command that FAILED from one that NEVER
RAN. It checks four signatures before the verdict: killed by a signal, exit
127/126, `[build failed]` in the output, and "no tests found"/"no tests to run".
Output is teed so the verdict can read it, and an INCONCLUSIVE verdict prints
the last fifteen lines so the reader can see why. All four verdicts were
exercised against a real uncommitted change: the Stage 10 case (a test
referencing a symbol only the change introduces, so reverting breaks
compilation) now reports INCONCLUSIVE with the compile error shown, instead of
"OK -- genuinely depends on your change"; the Stage 09 case (a grep matching no
tests) likewise; and a genuine failure still reports OK, a genuine pass still
VACUOUS.

**`auth.SetPassword`'s doc comment** said the function was "deliberately not
reachable from any HTTP route". `handleAdminResetPassword` calls it. The comment
now names both callers and why the difference matters -- an admin reset leaves
sessions alone, so it is deliberately not a way to evict somebody. The note in
`admin.go` was already accurate and is unchanged.

*Verified.* `make ci` green (now including the migration check), full suite 127
passed, contrast clean.

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
