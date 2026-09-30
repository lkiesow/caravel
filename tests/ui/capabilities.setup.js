// What this suite needs from a browser, asserted before any spec runs.
//
// Stage 40's diagnosis cost an afternoon for a reason worth not repeating: CI's
// Firefox has no WebGL2 on a GPU-less runner, MapLibre v6 requires one, and the
// symptom was 121 tests failing in 22 minutes with a 15s timeout each. Nothing
// in that output said "this browser cannot render a map". It read as 121
// unrelated bugs in routing, accessibility, layout and the location editor,
// because every one of those specs walks through a route that mounts a map.
//
// So the environment states its own preconditions, the way two others in this
// repository already do and for the same reason: ci.yml's "confirm the tests
// are really running against Postgres", and with_server.sh refusing to start
// when the server came up without the assistant enabled. A silent environmental
// miss otherwise reads as a code failure.
//
// A setup project rather than a fixture or a beforeEach, deliberately:
//
//   - it runs once per run, not once per test;
//   - the firefox and chromium-gestures projects both depend on it, so a
//     failure here stops them rather than letting them produce a pile of
//     confusing output ("N did not run");
//   - and it gets the project's default launch options. map-no-webgl.spec.js
//     turns WebGL off on purpose through its own test.use, and a per-test guard
//     would fire on that spec and fail the thing it is testing.
import { test as setup, expect } from "@playwright/test";

setup("the browser can render a map", async ({ page, browserName }) => {
  // about:blank is enough - this asks the browser about itself and needs
  // nothing from the app, so it runs even when the server is unreachable.
  const gl = await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    let context = null;
    // getContext throws in some configurations rather than returning null, and
    // a throw here would report as a broken setup rather than as the capability
    // that is missing.
    try {
      context = canvas.getContext("webgl2");
    } catch {
      context = null;
    }
    if (!context) return { webgl2: false, renderer: null };
    // The renderer string is not asserted on - a software rasteriser is a
    // perfectly good answer, and is what CI is configured to use. It is
    // captured so that a future failure says which stack was in play.
    const info = context.getExtension("WEBGL_debug_renderer_info");
    return {
      webgl2: true,
      renderer: info ? context.getParameter(info.UNMASKED_RENDERER_WEBGL) : "unknown",
    };
  });

  // Reported on the happy path too: "which GL stack did this run use" is the
  // first question a map failure raises, and it should not need a rerun to
  // answer.
  console.log(`capabilities: ${browserName} WebGL2 renderer: ${gl.renderer ?? "none"}`);

  expect(
    gl.webgl2,
    [
      `${browserName} cannot create a WebGL2 context.`,
      "",
      "MapLibre GL requires one, so every route that mounts a <map-view> would",
      "hang for 15s waiting for data-ready and then fail. That is roughly half",
      "this suite, and none of those failures would mention WebGL.",
      "",
      "The likeliest cause is no X display. Headless Firefox on Linux still",
      "reaches its GL context through GLX, so without a DISPLAY it gets none",
      "at all -- whatever Mesa is installed and whatever the prefs say. The ui",
      "job therefore runs under `xvfb-run -a` as well as installing",
      "libgl1-mesa-dri, setting LIBGL_ALWAYS_SOFTWARE=1, and turning on",
      "webgl.force-enabled and gfx.webrender.software (see software-gl.js).",
      "All of those are needed; the display is the one that is easy to miss,",
      "and is what this check caught on its first run.",
      "",
      "Reproduce it on a machine that has a display by taking the display away:",
      "  env -u DISPLAY -u WAYLAND_DISPLAY CI=1 LIBGL_ALWAYS_SOFTWARE=1 make test-ui",
      "",
      "This check is tests/ui/capabilities.setup.js, added in Stage 40 because",
      "the same failure previously read as 121 unrelated broken tests.",
    ].join("\n")
  ).toBe(true);
});
