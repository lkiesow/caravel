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

**Done.** Landed as `scripts/i18n.py` with `set` / `rm` / `unused`
subcommands. Deviation: `--unused` is a subcommand (`i18n.py unused`)
rather than a flag, which argparse wants once `set` and `rm` are also
subcommands; `--strict` on it exits non-zero for eventual CI use, left
opt-in deliberately (see below). Formatting held up empirically — the
current files round-trip through `json.dumps(indent=2,
ensure_ascii=False) + "\n"` byte-identically, so no text splicing was
needed, and a `check_roundtrip()` guard now re-asserts that before every
write so a hand-reformatted file can't be silently rewritten wholesale.

Two bugs were found *by* verification rather than by review, both worth
recording because both were invisible until exercised:

*Partial writes.* The `--after` anchor check originally ran inside the
per-locale write loop, so an anchor present in `en` but not `de` would
write `en`, then abort on `de` — leaving exactly the parity break this
script exists to prevent. Anchors are now validated across every target
locale before anything is written; verified by deleting the anchor from
`de.json` only and confirming `en.json`'s checksum is untouched after the
refusal.

*The parity rule was too strict.* It demanded a value for every locale
whenever a key was new *anywhere*, so backfilling a half-landed key into
just the locale missing it was rejected — with a confusing message naming
a locale that already had the key. The rule is now "every locale that
lacks the key needs a value", which permits both backfilling one locale
and updating one language's copy alone.

The `unused` scanner needed three fixes, and this is the part worth
remembering: its first draft reported **16 live keys as orphans and
invented a phantom key**. Keys reach `t()` by five routes, not one —
direct `t("k")` calls; `data-i18n[-placeholder|-aria-label]` attributes;
runtime composition in a call (`` t(`item.category.${c}`) ``); runtime
composition in an *attribute* (`data-i18n="trip.tabs.${key}"` in
`trip-detail-page.js`, which made all six tab keys look unused); and bare
strings resolved elsewhere entirely — a ternary inside an attribute
(`data-i18n="${mode === "login" ? "auth.login.title" : ...}"`) or a key
passed as component data (`ariaLabel: "locations.filter.label"`, which
`menu.js` later renders into the attribute). The last route needs a real
JS parser to chase properly, so instead any quoted string exactly
matching a known key counts as a reference — erring toward "used", which
is the safe direction for a tool that suggests deletions. The phantom
`trip.tabs.` came from the literal-attribute pattern matching up to the
`$` in `trip.tabs.${key}`; requiring a closing quote fixed it. Keys
matched only by a dynamic prefix are reported in their own section marked
*not safe to delete*, never as unused. `unused` also reports the inverse
that `check_i18n.py` structurally cannot — keys referenced in `web/js`
but defined in **no** locale, where `t()` renders the raw key.

`unused` is deliberately **not** in `make ci`: the 9 dynamically-composed
keys are unprovable either way by static scan, so making it a gate would
mean either false failures or teaching it to ignore the very cases it
should flag for a human.

Verified: (a) forcing a real write of a changed-then-restored value
leaves both files byte-identical by checksum, with a one-line diff in
between; (b) a new key via `--after` lands directly after its anchor in
*both* locales (diff confirms position, not end-of-file) and
`make check-i18n` passes at 121 keys; (c) `rm` removes it from every
locale and returns the tree clean; (d) all five guard paths — unknown
locale, missing locale for a new key, bad anchor, absent key on `rm`,
non-canonical formatting — exit 1 and write nothing; (e) non-vacuity of
`unused` both ways: a freshly added unreferenced key is detected (and
`--strict` exits 1), while the same key with a `t()` reference added is
correctly not flagged; (f) `make ci` green and tree clean afterwards.

Real finding, recorded in `todo.md` rather than fixed here per the plan:
`common.edit` and `item.detail.close` are genuine orphans — no reference
by any of the five routes.

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

**Done.** Landed as `scripts/dev_server.sh` with three subcommands, wired
to `make dev` (guard), `make dev-restart` and a new `make dev-marker`.
No health endpoint had to be invented: `/api/health` already exists
(`router.go:73`) and pings the database, so it's a real readiness check
rather than a static 200. Server logs go to `.dev/server.log`
(gitignored), and the server is started with `setsid nohup` so it
outlives make.

Deviations. `grep -aq` on `/proc/<pid>/exe` replaces the plan's
`strings | grep`, dropping the binutils dependency (`strings` happens to
be installed here, but `grep -a` works anywhere and is one process
instead of two). Added beyond the plan: a `check-marker` subcommand
(`make dev-marker MARKER=...`) that asserts against the *already running*
server without restarting it — the plan's own negative test ("check it
fails against a server started before the edit") is impossible with a
restart-only tool, and asking "am I testing my change or a stale binary?"
without disturbing the server is the more useful daily form anyway.

The `pkill` trap is worse than `todo.md` records. That entry says
`pkill -f "go run ./cmd/caravel"` "doesn't catch it" — but measured here,
it *does* match a process: the `go run` **wrapper** (pid 330286), while
the listener was a **different pid** (330379) whose cmdline is the
build-cache path. So `pkill -f` doesn't fail visibly; it kills the parent,
reports success, and leaves the port held by the orphan. That's why every
lookup here goes through `ss -lptn "sport = :$PORT"`.

Two bugs found by testing, both invisible on read:

*The script died silently on a free port.* `listening_pids()` ends in a
`grep`, which exits 1 when nothing matches; under `set -euo pipefail`
that propagates out of the command substitution and aborts the script
with no output and a bare exit 1. So it broke in precisely the case it
most needs to work — a fresh machine with nothing running. Every earlier
test had passed only because the port happened to be busy. Fixed with a
trailing `|| true` and a comment explaining why it's load-bearing.

*A marker must be a string the code actually uses.* The plan says "add a
unique string constant to a Go file"; doing exactly that fails. An
unused `const devMarkerProbe = "..."` never reaches the binary — Go folds
constants at compile time and the linker drops unreferenced data — so the
check reports "not found" against a server that genuinely does have the
change. Verified both halves: the unused const failed immediately after a
clean rebuild, while the same string added to the `/api/health` response
body was found straight away. Documented at the assert function.

Verified: (a) with the stale server from the previous session holding
:8080, `make dev` now *reports* it — naming the pid and its build-cache
exe path — instead of dying quietly; (b) `make dev-restart` replaced it,
pid 330379 → 404451, health OK, ~1.4s; (c) the decisive false-pass
reproduction — with a real change to the health payload on disk but not
restarted, `make dev-marker MARKER=...` **fails** and says the process
isn't running your code, then `make dev-restart MARKER=...` **passes**
and `curl /api/health` shows the change live; (d) cold start from a fully
free port works (the bug above); (e) a build error fails in ~1.2s
printing the actual compiler message, rather than hanging out the 45s
timeout; (f) `CARAVEL_PORT` is honoured; (g) all misuse paths exit 2.

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

**Done.** `cmd/seed/main.go` rewritten around a scenario registry (all
seven from the plan), selected with `make dev-seed SCENARIO=one-pin`;
`make dev-reset` landed as `scripts/dev_reset.sh`.

Determinism went further than the plan asked. Beyond fixed dates, seeded
rows get deterministic **v5 UUIDs** derived from a fixed namespace plus
the scenario name, so every ID is byte-identical across reseeds. That
means Milestone 5's suite can hard-code `/trips/<uuid>/locations` instead
of looking a trip up by title first, and it makes the seed **idempotent**
for free: `newTrip` deletes the previous incarnation by its known ID
before recreating it (relying on the existing delete cascade), so
`make dev-seed` can be run repeatedly without piling up duplicates —
verified by running it twice and confirming the trip count stays at 7.

Deviations. Only `full` keeps `time.Now()`-relative dates, as the plan's
"upcoming" carve-out allows; the other six are anchored to a fixed
`baseDate` (2026-06-15). Added beyond the plan: a second user account
(`other` / `other1234`), so Milestone 7's cross-user ownership tests have
someone to be "another user" without building one by hand; real blob
writes for seeded documents, so the Documents tab has something that
actually downloads; and `scripts/dev_server.sh` gained a `stop`
subcommand, which `dev_reset.sh` needs — deleting a SQLite file out from
under a live server leaves it holding the deleted inode, which looks
exactly like the reset having silently done nothing. `dev_reset.sh`
therefore stops the server, wipes, seeds, and restarts it only if it was
running to begin with. It also removes the `-wal`/`-shm` siblings, not
just the main database file, since committed rows can otherwise be
resurrected from the write-ahead log. No server needs to be up to seed:
`db.Open()` already runs pending migrations before returning, so the seed
binary builds the schema itself. A third guard was added alongside the
two the plan required: `dev-reset` refuses outright when the driver is
postgres, where there is no file to delete.

Verified: (a) all three guards refuse and delete nothing — a DSN outside
the repo, `CARAVEL_DB_DRIVER=postgres`, and a non-tty run without
`FORCE=1`; (b) the reset replaced 9 stray trips (including the `Test
Trip` / `UI Test Trip` leftovers `todo.md` complained about) with the 7
scenarios; (c) IDs are byte-identical across both a plain re-seed and a
full wipe-and-reseed; (d) **the bug this fixes**: the seeded demo trip's
`/map` endpoint returns 3 pins and the Map tab renders 3 Leaflet markers,
where it previously returned `[]`; (e) `one-pin` renders exactly 1 marker
at zoom 14 with all 12 tiles loaded — no degenerate zoom — despite the
trip having a second location deliberately left off the map; (f) a
scripted 1280×800 walk of 7 trips × 6 tabs = **42 combinations**, all
landing on the intended `window.location.pathname`, none overflowing
horizontally, all rendering content; (g) scenario-specific shapes check
out — `out-of-range-days` has days on 11 Aug (before), 15 Aug (inside)
and 20 Aug (after) a 14–16 Aug trip, `year-boundary` renders "29 Dec 2026
– 3 Jan 2027" with days in both years, and seeded documents download
their real content.

Two things the scenarios now reproduce on demand, both already in
`todo.md` and neither fixed here: `no-dates` shows the `itinerary.noDates`
copy still pointing at the Overview tab that Stage 05 removed, and
`full`'s trip-level Documents tab shows only `trip-notes.txt` while
`hotel-booking.txt` (attached to a location) is filtered out by
`ListTripDocuments`. Both entries in `todo.md` gained the exact route that
reproduces them.

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

**Done.** `package.json` (devDependencies only, `type: module`),
`playwright.config.js` (one Firefox project), `tests/ui/` with three specs
plus `contrast.js`, `make test-ui`, and a separate `ui` job in CI. Nine
tests, ~30s, 17 routes swept. The app still ships zero-build: nothing in
`package.json` is a runtime or bundling dependency.

Three things went differently from the plan, each found by running it:

*"networkidle" is the wrong readiness signal, and so is the obvious
replacement.* Every test initially timed out. Standalone, `networkidle`
resolves in under a second — the real cause was the Map route pulling a
dozen tiles from `tile.openstreetmap.org`, which is slow, flaky,
third-party, and would have been the top source of random CI failures. All
non-app requests are now blocked, with map tiles fulfilled from an inline
1×1 PNG so Leaflet still lays out and still creates markers. Replacing
`networkidle` with "`#app` has children" then produced a *worse* failure:
the heading spec reported "no headings at all" on the location view page,
which in fact has a correct `h1`/`h2`/`h2` outline — routes render a shell
immediately and fill it in when their fetches resolve, so the DOM check
passed too early. Readiness is now an injected `fetch` counter: `#app`
populated, at least one fetch completed, none in flight, plus one frame.
Also raised the per-test timeout to 180s, since each test sweeps ~17
routes rather than one.

*The 44px tap-target threshold in the plan does not describe this app.*
Measured across seven routes at 324px: **nothing** reaches 44px. Buttons
bottom out at 40px, block links at 30px, the icon+text "Back"/"Home" links
at 22px, checkbox inputs at 14px (20px counting their label). Stage 04's
note that "the tap targets themselves are fine (≥44px)" was about the trip
tab bar specifically, not the app. Asserting 44px would have been red on
every route — a finding to record, not a test to run. So the check is a
regression guard at the app's measured floor (40px) scoped to controls
*styled as* buttons, and the gap is recorded in `todo.md`. The style filter
is load-bearing rather than convenient: `.itinerary-entry__link` is a
`<button>` with no background, border or padding and `font: inherit` — a
text link in disguise, sized by its text at 22px — so judging it by a
button's standard would be measuring the wrong thing. That 22px control is
the primary way to open a location from the itinerary, and it is recorded
as a real finding rather than quietly excluded.

*The contrast script's headline feature had no data to prove itself on.*
Flattening translucent backgrounds only matters where a translucent
background exists, and Caravel's only one (`--color-danger-tint`, rgba at
0.08/0.14) appears exclusively in error states — so a normal run never
exercises it, and a flattener that just returned the raw tint would look
entirely plausible in the output. Added `--self-test`, which composites
known layers and checks against hand-computed values: the tint over white
must be `rgb(252 238 238)`, and doubled must be `rgb(250 222 222)`. It
immediately earned its keep by failing — the colours were exactly right but
the layer *count* was one higher than expected, because the terminating
opaque `body` background is itself a layer. Expectation corrected, not the
code.

Verified. `make test-ui`: 9 passed in ~30s, sweeping 17 routes × 4
viewport/scheme combinations, with 294 accessible names checked. Every
check proven non-vacuous by breaking the thing it guards and confirming a
red run, then restoring and confirming green:

- **headings** — demoting the trip-card `<h2>` to `<h4>` fails with
  `h1 -> h4 skips a level ... in shadow DOM`, i.e. it catches a defect
  that is invisible to a light-DOM-only sweep, which is the entire point;
- **accessible names** — the first attempt here *passed*, correctly:
  stripping the user-menu's `aria-label` changes nothing because that
  button also has text content. Removing it from the itinerary
  add-day date input, where the label is the only name source, fails on
  every itinerary route;
- **overflow** — a forced 900px element fails both mobile checks while
  both desktop checks correctly stay green (900 < 1280), and the failure
  names the offender (`<nav class="trip-tabs"> right=916`);
- **the URL assertion** — pointing one route at `/trps` fails with
  "landed on /trips — the router redirects unmatched paths", catching
  exactly the footgun that made a Stage 04 sweep pass against the wrong
  page for several milestones;
- **contrast** — `--self-test` green, and a real run reports the primary
  button at 5.17:1 light / 6.70:1 dark, confirming Stage 07's fix for the
  2.54:1 finding is still in place, with shadow-DOM elements measured
  (`[shadow] h2` at 16.12:1).

The CI job could not be verified by pushing, so its exact sequence was
replicated locally — seed, `go build`, background start, health-poll,
`make test-ui` — against a throwaway DSN: healthy in 2s, 9 passed. The one
CI-only risk left is the Playwright browser download.

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

**Done.** Landed as `scripts/without.sh`, with all the planned guards plus
three additions that each prevent a *confident wrong answer* — the failure
mode that matters here, since a wrong verdict from this tool is worse than
an error from it.

*A `--restart` flag, and a warning when it's missing.* Reverting Go files
and then asking a **running** server about them tests the old binary: `web/`
is served live from disk, but Go code lives in the compiled build. Verified
both ways on the same command — `scripts/dev_server.sh check-marker` against
an uncommitted change to the health payload reports **VACUOUS** without
`--restart` and **OK** with it. Since that's a silent wrong answer rather
than a failure, the script now also warns when Go files are named, a server
is up, and `--restart` wasn't passed.

*No verdict for a run that didn't finish.* The first interrupt test printed
"VACUOUS — the command PASSED" for a command that had been killed mid-flight
— exactly the kind of confident-but-wrong output this tool exists to
eliminate. Now `INT`/`TERM` restore and exit 130 with "INTERRUPTED — no
verdict", and separately a command killed by a signal (exit > 128) is
reported INCONCLUSIVE rather than counted as a pass.

*Restore is verified, not assumed.* The stash entry is located by SHA rather
than assumed to be `stash@{0}` (the command just ran arbitrary code, which
may have stashed something itself), and after popping, every file's
`git hash-object` is compared against its pre-run value. Any mismatch prints
the recovery command with the stash SHA instead of failing quietly.

An honest limit found while testing: **terminal Ctrl-C cannot be faithfully
simulated from a non-interactive shell.** A backgrounded child inherits
SIGINT as ignored (POSIX), and an ignored signal can't be trapped — so
`kill -INT` on the script ran the full `sleep 30` and never fired the trap,
which is a property of the harness, not the script. Verified in isolation
with a minimal reproduction, then exercised the same code path with SIGTERM,
which isn't ignored that way: "INTERRUPTED — no verdict", exit 130, tree
byte-identical, no stash left behind.

Verified, in order: (a) every guard exits 2 and changes nothing — missing
`--`, no files, no command, an untracked file, a file with no uncommitted
changes, and a dirty index; (b) **non-vacuous** detection, with an
uncommitted source change and a test that depends on it — the command fails
without it, exit 0; (c) **vacuous** detection, naming an unrelated changed
file — the command passes, exit 1; (d) multi-file runs revert and restore
both files, hash-checked; (e) interrupt handling and `--restart` as above;
(f) then the plan's own example, on **real Stage 07 code**: the date-format
validation in `handleSetItineraryDayNotes` reconstructed as an uncommitted
change on a scratch branch (this tool only reverts uncommitted work, so a
committed change has to be staged that way first — a documented limit).

That last run produced a real finding rather than a green tick, and the
opposite of what the plan predicted: it reports **VACUOUS**. The entire
`internal/httpapi` suite passes without that validation, because *nothing
covers it* — with the check removed the API happily accepts a day dated
`13-99-2026`. Writing a test for it (`PUT
/api/trips/{id}/itinerary/days/13-99-2026` expecting 400) and re-running
reports **OK**, confirming both the finding and the tool. That test is
Milestone 7's to land — it needs the shared harness — and is noted in its
section below.

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

Added to this milestone's scope by Milestone 6: **a test for the
date-format validation in `handleSetItineraryDayNotes`**. Milestone 6
proved by measurement that nothing currently covers it — the whole
`internal/httpapi` suite passes with the check removed, and the API then
accepts a day dated `13-99-2026`. The test is one request,
`PUT /api/trips/{id}/itinerary/days/13-99-2026` expecting 400, and it
belongs here rather than in Milestone 6 because it wants the shared
harness. Worth checking the sibling date-parsing at `itinerary.go:119-120`
for the same gap while in there.

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
