// Getting a WebGL2 context out of Firefox on a machine with no GPU.
//
// MapLibre v6 needs one, and without it roughly half the UI suite fails: every
// route mounting a <map-view> waits on data-ready and never gets it. That is
// what had CI's ui job red for five weeks (Stage 40) -- Chromium ships
// SwiftShader and its gesture specs passed throughout, while Firefox, which has
// no software fallback of its own, failed 121 tests on the same runner.
//
// `webgl.force-enabled` is the load-bearing pref: Firefox blocklists WebGL when
// it does not recognise the renderer, and llvmpipe on a headless runner is
// exactly that case. The other two put compositing on the software path rather
// than leaving it to negotiate with a GPU process that is not there.
//
// Applied only under CI, because this is a workaround for the absence of a GPU
// and not the configuration a developer's run should have: forcing software
// rendering on a workstation would make every local run slower while exercising
// a path that machine never takes. `CI=1 make test-ui` opts in locally, which is
// how it was verified before being pushed, and is the same variable the workers,
// reporter and forbidOnly settings in playwright.config.js already key off.
//
// Mesa itself comes from the `ui` job, which installs libgl1-mesa-dri and sets
// LIBGL_ALWAYS_SOFTWARE=1. Prefs without the driver would be a browser told to
// use a rasteriser that is not installed, so the two halves belong together --
// see .github/workflows/ci.yml.
//
// Its own module because there are two callers and they are easy to skew: the
// Playwright config, and tests/ui/contrast.js, which launches Firefox itself. A
// contrast run without these would measure the "map could not be displayed"
// rectangle instead of a map and report a number for the wrong thing -- quietly,
// since it asserts contrast rather than the presence of cartography.
export const SOFTWARE_GL_PREFS = process.env.CI
  ? {
      "webgl.force-enabled": true,
      "gfx.webrender.software": true,
      "gfx.webrender.all": true,
    }
  : {};
