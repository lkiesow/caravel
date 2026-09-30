# Stage 40: the UI job has been red for five weeks

## Context

`ci.yml`'s `ui` job has failed on every push since run 19 (2026-08-24). The
other three jobs — `ci`, `postgres`, `docs` — are green in every one of those
runs, so the repository has spent five weeks in a state where a red tick means
nothing and nobody looks at it.

The current failure, from run 71's log, is one cause wearing 121 hats. Every
single failing test carries the identical error:

```
TimeoutError: page.waitForFunction: Timeout 15000ms exceeded.
  at gotoRoute (tests/ui/helpers/scenarios.js:291)
  () => [...document.querySelectorAll("map-view")].every((el) => el.hasAttribute("data-ready"))
```

Every route that mounts a `<map-view>` waits 15s for `data-ready` and never
gets it. 121 failed, 170 passed, 22.2 minutes.

### What is actually wrong

**Playwright's Firefox on a GitHub runner has no WebGL2, and MapLibre v6
requires one.** The decisive evidence is in the same log: the three
`[chromium-gestures]` specs render the same map through the same `gotoRoute`
and pass. Chromium ships SwiftShader — a software WebGL2 — and Firefox has no
equivalent on a GPU-less machine.

Reproduced locally by launching Firefox with `webgl.disabled`:

```
webgl availability: { webgl2: false, webgl: false }
map-view elements: 1 -> data-ready: false
warning: Failed to create WebGL context: WebGL is currently disabled.
pageerror: can't access property "disableRotation", map.touchZoomRotate is undefined
```

That last line is the mechanism, and it is a real bug rather than a test
artefact. `map-view.js`'s comment at the `new maplibre.Map(...)` call reads:

> MapLibre v6 requires WebGL2 and throws when it cannot get a context. Failing
> here must still finish the render: data-ready is what every route sweep in
> the UI suite blocks on, so leaving it off would turn a missing GPU into a 15s
> timeout on every page with a map rather than into a message.

The intent is right and the premise is wrong. MapLibre does **not** throw: it
emits an `error` event and returns a half-constructed `Map`. The `catch` never
runs, so the next statement — `map.touchZoomRotate.disableRotation()` — throws
an uncaught `TypeError`, `render()` dies there, and `map.on("load", ...)` is
never reached. Neither the success path nor the fallback can set `data-ready`.
The comment describes precisely the failure its own code then produces.

### What was ruled out

- **Not the tile network.** `blockExternalRequests` in
  `tests/ui/helpers/scenarios.js` aborts every non-app origin locally too, and
  the map still reaches `data-ready` — verified with the openfreemap sprite,
  glyph and TileJSON fetches all aborted.
- **Not the style document.** It is served from the app itself
  (`/js/vendor/map-styles/liberty.json`), not fetched from a provider.
- **Not a regression in the specs.** The whole suite with `CI=1` is
  291 passed, 0 failed locally (17.5 min).

### The older failures are a separate question

Runs 19–47 also failed, and MapLibre only arrived in Stage 30 (`a8a8ba7`,
around run 48) — before that the map was Leaflet, which needs no WebGL. So
something else was failing the `ui` job for six weeks before this. Those logs
are gone from the retention window. This stage fixes what is failing now; the
next green-or-not run answers whether the older cause is still there.

## Milestone 1: a map-view that survives a browser with no WebGL2

Fix the guard so it guards. Replace the assumption that construction throws
with a check of what construction returned: a `Map` whose handlers are missing
never got a context, so `showMapUnavailable()` + `data-ready` + return, before
anything touches it. Keep the existing `try`/`catch` — a genuine throw is still
possible — and make the two paths converge on one piece of handling.

This is user-facing, not merely test-facing: today anyone whose browser has
WebGL disabled, blocklisted, or unavailable gets a blank rectangle and an
uncaught `TypeError` in the console, while `t("map.unavailable")` — "This map
could not be displayed in this browser." — sits unused in every locale file.

Verification: the probe from the Context section, run both ways against a
seeded server. With WebGL on, `data-ready` still lands and the map renders;
with `webgl.disabled`, `data-ready` lands too and the unavailable line is in
the shadow root, with no `pageerror`. Plus a spec that asserts it, so the
behaviour cannot rot: launch a context with WebGL off, load a map route, expect
the message and no console error.

**Done.** The `try`/`catch` stays for a genuine throw, and a check of what
construction *returned* now sits after it: `!map.touchZoomRotate ||
!map.keyboard` means no context was had, so `showMapUnavailable()` +
`data-ready` + return, before anything reads a property off the half-built
object. An `error` subscription could not have been used instead — MapLibre
emits it *during* construction, before there is an object to subscribe to.

One deviation, found while verifying rather than planned: `map.remove()` does
not reliably tear down a map that never finished being built, and the leftover
was a full-size context-less canvas sitting under the message. `remove()` is
still asked first and its failure ignored; `mapEl.replaceChildren()` after it is
what actually guarantees the result.

Verified three ways. The probe, three runs each way: with WebGL off,
`data-ready` lands, the message reads "This map could not be displayed in this
browser.", `#map` holds no canvas, zero `pageerror`s — against the same probe on
the unmodified file, which gives `data-ready: false` and `pageerror: can't
access property "disableRotation", map.touchZoomRotate is undefined`. With WebGL
on, three runs, `data-ready` still lands, no message, no errors — the working
path is untouched.

(The probe itself needed a correction first: it aborted external images where
`blockExternalRequests` fulfils them with a transparent PNG, which made the
map's `load` event flaky on *both* the fixed and unmodified files. Worth
recording because it briefly looked like a regression in this milestone and was
not — the raster `ne2_shaded` tiles are the reason the suite's helper fulfils
rather than aborts.)

Second, `tests/ui/map-no-webgl.spec.js` asserts it: its own browser via
`test.use({ launchOptions: { firefoxUserPrefs } })`, since prefs are a launch
option. It checks that `gotoRoute` returns at all (which is the data-ready
assertion, and is what timed out before), that WebGL2 really is gone so nothing
passes for the wrong reason, that the message is visible, that no canvas is left
in `#map`, and that no uncaught error was raised. It passes with the fix and
fails without it — checked by stashing the change and re-running.

Third, `make ci` green, and the full UI suite green.

## Milestone 2: the suite states its own precondition

A browser that cannot render a map should fail the suite in two seconds with a
sentence, not in twenty-two minutes with 121 timeouts that look like 121
unrelated bugs. After Milestone 1 the symptom changes shape — the routes stop
hanging and the map specs fail on their assertions instead — which is faster
but still not self-explaining.

Add a precondition check: the browser can create a WebGL2 context, or the run
stops and says that is why. This is the same idea as two guards the repository
already has and for the same reason — `ci.yml`'s "confirm the tests are really
running against Postgres", and `with_server.sh`'s refusal to start when the
assistant did not come up enabled. Both exist because a silent environmental
miss reads as a code failure.

Verification: with WebGL forced off the run stops early naming WebGL2; with it
on, nothing changes and the suite is still 291 passed.

**Done.** `tests/ui/capabilities.setup.js`, a second file in the existing
`setup` project. Both real projects already depend on that project, so a failure
there stops them instead of letting them produce the confusing output — no
config change was needed, only a file matching `/.*\.setup\.js/`.

A setup project rather than a fixture or a `beforeEach`, for three reasons
worth stating because the last one is not obvious: it runs once per run rather
than 293 times; the dependency edge already exists; and it gets the project's
default launch options, whereas a per-test guard would fire inside
`map-no-webgl.spec.js` — which turns WebGL off deliberately — and fail the very
spec that proves Milestone 1 works.

The check also logs the renderer on the way past, on the happy path too
(`capabilities: firefox WebGL2 renderer: Radeon R9 200 Series, or similar`
here; a software rasteriser on CI). "Which GL stack did this run use" is the
first question any map failure raises, and it should not take a rerun to
answer. The string is logged, never asserted on — software is a correct answer
and is the one CI is meant to give.

Verified in both directions, the failing one by temporarily adding
`firefoxUserPrefs: { "webgl.disabled": true }` to the config's shared
`launchOptions` and restoring it afterwards:

- **WebGL off, whole suite:** `1 failed, 290 did not run, 2 passed (11.3s)`,
  the failure being this check, its message naming WebGL2, MapLibre, the CI
  configuration and the local diagnosis. Against the shape this stage started
  from — 121 failed, 170 passed, 22.2 minutes, nothing mentioning WebGL.
- **WebGL on:** the check passes in 3.1s, `map-no-webgl.spec.js` still passes
  alongside it (confirming the deliberate WebGL-off spec is unaffected), `make
  ci` green, and the full suite green.

## Milestone 3: CI's Firefox gets a software WebGL2

The fix that turns the job green. Give the `ui` job Mesa's software rasteriser
and tell Firefox to use it rather than refusing: `libgl1-mesa-dri` installed
alongside Playwright's own dependencies, `LIBGL_ALWAYS_SOFTWARE=1` in the job
environment, and `webgl.force-enabled` / `gfx.webrender.software` through
`launchOptions.firefoxUserPrefs` in `playwright.config.js` so the blocklist
does not veto a software context.

Locally, MapLibre reaches `data-ready` under forced `llvmpipe`, so the software
path itself works. What cannot be proven on this machine is the runner's own
state — a GPU-less Ubuntu image is not something a workstation with a working
GL stack can honestly simulate. So this milestone is verified **on CI**, on a
branch, before it goes anywhere near `main`: push, watch the `ui` job, and read
the result. Milestone 2's guard is what makes that read unambiguous.

**Done.** Three pieces, because no two of them work without the third.

`tests/ui/helpers/software-gl.js` holds the prefs, keyed off `CI`.
`webgl.force-enabled` is the load-bearing one — Firefox blocklists WebGL when
it does not recognise the renderer, and llvmpipe on a headless runner is
exactly that case; `gfx.webrender.software` and `gfx.webrender.all` put
compositing on the software path rather than leaving it to negotiate with a GPU
process that is not there. Gated rather than unconditional because forcing
software rendering on a workstation would slow every local run while exercising
a path that machine never takes.

`.github/workflows/ci.yml` supplies the other half: `libgl1-mesa-dri` (with
`libglx-mesa0` and `libegl-mesa0`) installed explicitly, since `--with-deps`
installs what Firefox needs to *start*, which is a different set; and
`LIBGL_ALWAYS_SOFTWARE=1` on both the UI suite and the contrast step. Prefs
without the driver would be a browser told to use a rasteriser that is not
installed.

A deviation from the plan, and the reason this is a module rather than a
constant in the config: **`tests/ui/contrast.js` launches its own Firefox.**
`make check-contrast` drives the same routes through `firefox.launch()`
directly, so the config's prefs would never have reached it — on CI it would
have measured the "map could not be displayed" rectangle instead of a map and
reported a number for the wrong thing, silently, because it asserts contrast
rather than the presence of cartography. Both call sites now import the one
definition.

Verified as far as this machine honestly allows, which turned out to be further
than the plan assumed: because the prefs key off `CI`, `CI=1
LIBGL_ALWAYS_SOFTWARE=1` locally exercises the exact configuration CI will use.
Under it, `capabilities.setup.js` reports `llvmpipe, or similar` rather than the
workstation's Radeon — so the software path is genuinely the one under test —
the map specs pass, `map-no-webgl.spec.js` still passes (confirming
`webgl.disabled` beats `webgl.force-enabled`, which that spec asserts on
purpose), `make check-contrast` measures 728 elements all above threshold, and
the full suite is green.

What that does **not** prove is the runner's own state: this machine has a
working Mesa install, which is the very thing that may be missing there. Hence
the verification below.

**Follow-up: the display, which the above missed.** The first push failed in 78
seconds — Milestone 2's guard doing exactly its job, one failure naming WebGL2
instead of 121 naming nothing. Mesa was not the problem: `libgl1-mesa-dri
25.2.8` was already on the image and `libegl-mesa0` installed cleanly. Firefox
reported `WebGL2 renderer: none` anyway.

The missing piece was an **X display**. Headless Firefox on Linux still reaches
its GL context through GLX, so with no `DISPLAY` it gets none at all, whatever
driver is installed and whatever the prefs say. Chromium never needed one
because SwiftShader is in-process — which is precisely why its three gesture
specs passed through all five weeks the Firefox projects were failing, and why
the contrast between them was the clue that started this stage.

Reproduced locally by taking the display away, which turns the workstation into
a fair imitation of the runner after all:

| condition | renderer |
| --- | --- |
| headless, `DISPLAY=:0`, `LIBGL_ALWAYS_SOFTWARE=1` | `llvmpipe, or similar` |
| headless, no `DISPLAY`, `LIBGL_ALWAYS_SOFTWARE=1` | `none` |

One variable, and it is the one nothing in the first attempt supplied. So the
job installs `xvfb` and both browser steps run under `xvfb-run -a`. Headless
stays on: the display is for GLX to bind to, not for anyone to look at.

`capabilities.setup.js`'s failure message and `software-gl.js`'s header were
rewritten to name the display first — the previous text confidently pointed at
Mesa and the prefs, which would have sent the next reader down the path that had
just been ruled out. The message now also carries the `env -u DISPLAY` command
above, so the reproduction is in the failure rather than in this file.

Verified with the display present (`llvmpipe`, the map specs and
`map-no-webgl.spec.js` both passing, `make ci` green) and with it absent (the
guard fires in seconds with the corrected message). `xvfb-run` itself is **not**
verified locally — Xvfb is not installed on this machine and the decision was to
push rather than install it. The bisection above is the argument for it; the run
is the proof.

## Build order

1, 2, 3, in that order, and the order carries an argument: Milestone 1 is the
application bug and stands on its own merits whatever CI does; Milestone 2 is
the instrument that makes Milestone 3 legible; Milestone 3 is the one that can
only be tested by pushing it. Landing the cheap, locally-provable work first
means the branch push at the end is testing exactly one unknown.

## Workflow

The loop from `CLAUDE.md`, unchanged. Per milestone: implement; verify with
`make ci` green plus a real behavioural check (assertions, not screenshots);
add a **Done.** paragraph to that milestone's section here saying what actually
landed and how it was checked; update `plans/todo.md` in both directions;
commit once, with a message that says what and why and how it was verified;
leave `make dev` running; stop and wait.

## Verification

The stage is done when a push to `main` produces a `ci.yml` run whose `ui` job
is green — not when the code looks right. Until that run exists, the stage is
open.
