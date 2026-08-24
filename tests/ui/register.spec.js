// The register form, which no spec rendered before.
//
// It only exists when the instance has open signup on, and the seed
// deliberately leaves it off — so covering it means turning a *global instance
// setting* on and back off again. That was the reason this stayed uncovered:
// against the shared dev database a failed restore left the instance open for
// every later run. Stage 19 Milestone 1 removed that hazard — the database
// belongs to the run and dies with it — which is what makes this spec
// affordable now.
//
// Two things still deserve care, both about the setting being global for the
// duration:
//
//  - `unauthenticated.spec.js` asserts the *absence* of the register link, and
//    skips itself if it finds signup open. Workers run in parallel, so it could
//    in principle skip during this spec's window. That is why the window is
//    kept to one open-and-close per test, in a serial block, rather than one
//    per locale.
//  - The restore is asserted rather than assumed. A silently failing cleanup is
//    the failure mode settings.spec.js was written around.
//
// Registration shares the login rate limiter (10/min/IP, and every worker on
// localhost is one bucket), so exactly one account is registered here. The
// German half asserts the rendered form, which is where the translation lives;
// submitting it again would only spend budget.
import { test, expect } from "@playwright/test";

const MOBILE = { width: 324, height: 756 };
const VISITOR = { storageState: { cookies: [], origins: [] }, viewport: MOBILE };
const NEWCOMER = { username: "uisuite-newcomer", password: "newcomer1234" };

// Serial: both tests own the same global switch, so they must not overlap.
test.describe.configure({ mode: "serial" });

test.describe("registration", () => {
  // The default project session is `demo`, who is the seeded admin — which is
  // what makes the toggle reachable.
  async function setOpenSignup(page, open) {
    const res = await page.request.put("/api/admin/settings/open-signup", {
      data: { open_signup: open },
    });
    expect(res.status(), `set open_signup=${open}`).toBe(200);
    const config = await (await page.request.get("/api/auth/config")).json();
    expect(config.open_signup, `open_signup should now be ${open}`).toBe(open);
  }

  test("the register form appears only while signup is open", async ({ page, browser }) => {
    // Closed to begin with, which is the seeded state and the thing the
    // "appears only while" claim is measured against.
    const before = await (await page.request.get("/api/auth/config")).json();
    expect(before.open_signup, "the seed should leave signup closed").toBe(false);

    const contexts = [];
    try {
      for (const locale of ["en", "de"]) {
        const context = await browser.newContext({ ...VISITOR, locale });
        contexts.push(context);
        const visitor = await context.newPage();
        await visitor.goto("/");
        await expect(visitor.locator(".auth-form")).toBeVisible();
        await expect(
          visitor.locator('[data-action="switch-mode"]'),
          `no way to register while signup is closed (${locale})`
        ).toHaveCount(0);
      }

      await setOpenSignup(page, true);

      for (const [i, locale] of ["en", "de"].entries()) {
        const visitor = contexts[i].pages()[0];
        await visitor.reload();
        // The switch link arrives asynchronously: the page renders assuming
        // signup is closed and re-renders after /auth/config answers. So this
        // waits rather than counting immediately.
        const shows = visitor.locator('[data-action="switch-mode"]');
        await expect(shows, `the register link should appear (${locale})`).toBeVisible();

        await shows.click();

        // Register mode, asserted on structure rather than on text: in German
        // the heading, the submit button and the "log in" link are all the same
        // words, so copy alone would not tell the two modes apart.
        const form = visitor.locator(".auth-form");
        await expect(
          form.locator('input[name="displayName"]'),
          `register mode offers a display name (${locale})`
        ).toBeVisible();
        await expect(form.locator('input[name="password"]')).toHaveAttribute(
          "autocomplete",
          "new-password"
        );

        // And the German form is actually in German, rather than quietly
        // falling back — the guard that stops this half being decoration.
        const expected = locale === "de" ? "Konto erstellen" : "Create an account";
        await expect(form.locator("h2")).toHaveText(expected);
      }
    } finally {
      await setOpenSignup(page, false);
      for (const context of contexts) await context.close();
    }

    // Closed again, and visibly so.
    const context = await browser.newContext(VISITOR);
    try {
      const visitor = await context.newPage();
      await visitor.goto("/");
      await expect(visitor.locator(".auth-form")).toBeVisible();
      await expect(visitor.locator('[data-action="switch-mode"]')).toHaveCount(0);
    } finally {
      await context.close();
    }
  });

  test("registering an account logs the newcomer straight in", async ({ page, browser }) => {
    let userId = null;
    const context = await browser.newContext(VISITOR);
    try {
      await setOpenSignup(page, true);

      const visitor = await context.newPage();
      await visitor.goto("/");
      await expect(visitor.locator('[data-action="switch-mode"]')).toBeVisible();
      await visitor.locator('[data-action="switch-mode"]').click();

      const form = visitor.locator(".auth-form");
      await form.locator('input[name="username"]').fill(NEWCOMER.username);
      await form.locator('input[name="displayName"]').fill("A Newcomer");
      await form.locator('input[name="password"]').fill(NEWCOMER.password);

      // Waiting on the response rather than only on the page: a 429 from the
      // shared login limiter renders as the same generic error a real refusal
      // does, so without this the failure would read as a broken register form.
      const [res] = await Promise.all([
        visitor.waitForResponse("**/api/auth/register"),
        form.locator('button[type="submit"]').click(),
      ]);
      expect(
        res.status(),
        res.status() === 429
          ? "registration was rate limited (429) — it shares the 10/min login bucket"
          : "registration should have succeeded"
      ).toBe(200);

      // No redirect: the SPA swaps in place, so the evidence is the app header
      // being there and the auth form being gone.
      await expect(visitor.locator(".app-header")).toBeVisible();
      await expect(visitor.locator(".auth-form")).toHaveCount(0);

      const users = await (await page.request.get("/api/admin/users")).json();
      const created = users.find((u) => u.username === NEWCOMER.username);
      expect(created, "the account should exist on the instance").toBeTruthy();
      expect(created.display_name).toBe("A Newcomer");
      userId = created.id;
    } finally {
      await context.close();
      await setOpenSignup(page, false);
      // The database is this run's own, but the spec still tidies up: pointed
      // at a dev server through CARAVEL_TEST_URL it would otherwise leave an
      // account behind.
      if (userId) {
        const deleted = await page.request.delete(`/api/admin/users/${userId}`);
        expect([204, 200], "clean up the registered account").toContain(deleted.status());
      }
    }
  });
});
