// Logs the demo user in once per run and saves the session cookie, which every
// spec then starts from (see playwright.config.js: the firefox project depends
// on this one and points `storageState` at the file below).
//
// Why: login is rate limited to 10 per minute per IP
// (newLoginLimiter in internal/httpapi/router.go), and the suite used to log in
// inside every spec — 9 per run. Two runs inside a minute, or one run alongside
// a hand-written Playwright script, therefore hit HTTP 429; the specs then
// rendered the login page and failed on unrelated assertions, with a message
// blaming the seed ("has `make dev-reset FORCE=1` been run?") when the seed was
// fine. One login per run leaves the limiter's headroom for the person driving
// the browser at the same time.
//
// The file is rewritten on every run rather than reused if present, because
// `make dev-reset` wipes the sessions table: a cached token from an earlier run
// would authenticate nothing and every spec would fail as if logged out.
import { test as setup, expect } from "@playwright/test";
import { DEMO_USER, AUTH_STATE_FILE } from "./helpers/scenarios.js";

setup("log in as the demo user", async ({ page }) => {
  // Through the API rather than the login form: one request instead of a page
  // load plus a form fill, and a broken login form should fail the login page's
  // own coverage, not every other spec in the suite. page.request shares the
  // browser context's cookie jar, so the session lands where storageState will
  // pick it up.
  const res = await page.request.post("/api/auth/login", { data: DEMO_USER });

  expect(
    res.status(),
    res.status() === 429
      ? "login was rate limited (HTTP 429) — more than 10 logins from this IP in the last minute. " +
          "Wait a minute and re-run; the seed is not the problem."
      : `login as ${DEMO_USER.username} failed — has \`make dev-reset FORCE=1\` been run?`
  ).toBe(200);

  await page.context().storageState({ path: AUTH_STATE_FILE });
});
