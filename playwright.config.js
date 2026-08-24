// Playwright config for Caravel's UI suite.
//
// Firefox for everything, deliberately: the original note asking for this suite
// specified Firefox, and one browser keeps the CI job cheap. The checks here
// (heading outline, accessible names, overflow, contrast) are about markup and
// CSS rather than engine quirks, so a second engine would mostly buy duplicate
// failures.
//
// The one exception is gestures. Playwright's `isMobile` - the option that
// flips `(pointer: coarse)` and enables real touch input - is Chromium-only,
// and `hasTouch: true` does not do it. So there is a third project scoped to
// *.gesture.spec.js and nothing else: a Chromium project for the assertions
// that need a finger, not a second full run of the sweeps.
//
// The suite drives a server at CARAVEL_TEST_URL rather than starting one
// itself. `make test-ui` goes through scripts/ui_test.sh, which starts a
// throwaway instance — own port, own database, own seed — and sets that
// variable; setting it yourself points the suite at a server you already run
// instead. Either way the seeded scenarios must be present, since the routes
// are resolved from them — see tests/ui/helpers/scenarios.js.
import { defineConfig, devices } from "@playwright/test";
import { AUTH_STATE_FILE } from "./tests/ui/helpers/scenarios.js";

const baseURL = process.env.CARAVEL_TEST_URL || "http://localhost:8080";

export default defineConfig({
  testDir: "./tests/ui",
  // contrast.js is a standalone measurement script, not a spec.
  testMatch: /.*\.spec\.js/,
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  // Each spec sweeps every route in one test, so the per-test budget covers ~17
  // page loads, not one. The default 30s is far too tight for that.
  timeout: 180_000,
  expect: { timeout: 10_000 },
  // One dev server serves every worker, so unlimited parallelism just makes them
  // queue on it (and on SQLite's write lock).
  workers: process.env.CI ? 2 : 4,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : [["list"]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",
    // Headless is Playwright's default, but stated explicitly so it can't change
    // underneath us and so the headed escape hatch is discoverable:
    //   make test-ui HEADED=1            watch the run in a real window
    //   make test-ui HEADED=1 SLOWMO=300 ...slowly enough to follow
    //   make test-ui UI=1                Playwright's interactive UI mode
    headless: !process.env.CARAVEL_TEST_HEADED,
    launchOptions: {
      slowMo: Number(process.env.CARAVEL_TEST_SLOWMO || 0),
    },
  },
  projects: [
    // One login for the whole run, saved and reused by every spec — see
    // tests/ui/auth.setup.js. Its own testMatch, since the top-level one only
    // picks up *.spec.js.
    // Firefox here too, not just on the project below: a project with no
    // browser named falls back to chromium, which this suite deliberately does
    // not install — so the setup died with "Executable doesn't exist" and took
    // all nine specs down with it ("9 did not run").
    {
      name: "setup",
      testMatch: /.*\.setup\.js/,
      use: { ...devices["Desktop Firefox"] },
    },
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"], storageState: AUTH_STATE_FILE },
      dependencies: ["setup"],
      // Gesture specs need real touch, which Firefox cannot emulate — they
      // belong to the chromium project below. Without this they would run here
      // too, since the top-level testMatch takes every *.spec.js.
      testIgnore: /.*\.gesture\.spec\.js/,
    },
    // Real touch input, and the only place in the suite that has it. The
    // viewport is overridden to the project's own 324×756 convention rather
    // than left at the Pixel 5's 393px, so a gesture is measured at the same
    // width every other mobile assertion uses; everything else about the
    // device profile — isMobile, hasTouch, the scale factor — is what makes
    // this project worth having.
    {
      name: "chromium-gestures",
      testMatch: /.*\.gesture\.spec\.js/,
      use: {
        ...devices["Pixel 5"],
        viewport: { width: 324, height: 756 },
        storageState: AUTH_STATE_FILE,
      },
      dependencies: ["setup"],
    },
  ],
});
