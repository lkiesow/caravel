// Playwright config for Caravel's UI suite.
//
// Firefox only, deliberately: the original note asking for this suite specified
// Firefox, and one browser keeps the CI job cheap. The checks here (heading
// outline, accessible names, overflow, contrast) are about markup and CSS rather
// than engine quirks, so a second engine would mostly buy duplicate failures.
//
// The suite drives a *running* dev server rather than starting one itself, so
// `make test-ui` can be pointed at whatever is already up. CI starts one first.
// It also expects the seeded scenarios from `make dev-reset` to be present —
// see tests/ui/helpers/scenarios.js.
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
    },
  ],
});
