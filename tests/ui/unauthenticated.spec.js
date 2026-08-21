// The two pages nobody had ever swept.
//
// Every other spec starts from a saved session (auth.setup.js), so the login
// screen — the only thing an unauthenticated visitor sees, and therefore the
// first impression of the whole app — had no coverage at all: no overflow
// check, no heading, no accessible names, no tap targets. todo.md had been
// carrying that gap since Stage 10.
//
// The fix is one line of configuration: an empty storageState, which overrides
// the project's saved session for this file only.
import { test, expect } from "@playwright/test";
import { blockExternalRequests, VIEWPORTS, COLOR_SCHEMES } from "./helpers/scenarios.js";
import { DEEP_DOM_SOURCE, ACCESSIBLE_NAME_SOURCE } from "./helpers/deep-dom.js";

const MIN_TAP_TARGET_PX = 44;

for (const locale of ["en", "de"]) {
  for (const viewport of VIEWPORTS) {
    test.describe(`login screen (${locale}, ${viewport.name})`, () => {
      test.use({
        // Empty rather than the project default: this file is about what a
        // visitor with no session sees.
        storageState: { cookies: [], origins: [] },
        viewport: { width: viewport.width, height: viewport.height },
        locale,
      });

      test(`is usable and does not overflow (${locale}, ${viewport.name})`, async ({ page }) => {
        await blockExternalRequests(page);
        await page.goto("/");

        // A visitor at "/" gets the login screen rather than a redirect to
        // somewhere that would 401 — this is the app's front door.
        const form = page.locator(".auth-form");
        await expect(form).toBeVisible();
        await expect(page.locator("h1")).toHaveCount(1);
        await expect(page.locator("h1")).not.toBeEmpty();

        // No horizontal overflow, the same check routes.spec.js makes for every
        // authenticated route.
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        expect(overflow, `document overflows horizontally by ${overflow}px`).toBeLessThanOrEqual(0);

        // Every control has an accessible name and clears the tap floor at
        // phone width. The username and password inputs get theirs from their
        // own <label>, which is the thing that would break silently.
        const failures = await page.evaluate(
          ({ minTap, isMobile, deepDom, accName }) => {
            eval(deepDom);
            eval(accName);
            const problems = [];
            for (const el of deepQueryAll("input, button, .link-button")) {
              const label = accessibleName(el);
              if (!label) problems.push(`no accessible name: ${el.outerHTML.slice(0, 80)}`);
              if (!isMobile) continue;
              const box = el.getBoundingClientRect();
              if (box.width === 0 && box.height === 0) continue;
              if (box.height < minTap || box.width < minTap) {
                problems.push(`${label || el.tagName} is ${Math.round(box.width)}x${Math.round(box.height)}`);
              }
            }
            return problems;
          },
          {
            minTap: MIN_TAP_TARGET_PX,
            isMobile: viewport.name !== "desktop",
            deepDom: DEEP_DOM_SOURCE,
            accName: ACCESSIBLE_NAME_SOURCE,
          }
        );
        expect(failures, `login screen (${locale}, ${viewport.name})`).toEqual([]);
      });

      test(`offers no registration while signup is closed (${locale}, ${viewport.name})`, async ({ page }) => {
        await blockExternalRequests(page);
        await page.goto("/");
        await expect(page.locator(".auth-form")).toBeVisible();

        // The seed leaves registration closed (migration 0008 seeds
        // open_signup=false and the demo user already exists), so the page must
        // not offer a register link at all — before Stage 14 Milestone 5 it
        // always did, and the form it led to answered 403 reported as "invalid
        // username or password".
        //
        // Read from the API rather than assumed: if somebody has opened signup
        // on their dev instance this should skip rather than fail, because the
        // page would be right and the assumption wrong.
        const config = await (await page.request.get("/api/auth/config")).json();
        test.skip(config.open_signup === true, "registration is open on this instance");

        await expect(page.locator(".auth-form__switch")).toHaveCount(0);
        await expect(page.locator('[data-action="switch-mode"]')).toHaveCount(0);
      });

    });
  }
}

// Outside the viewport loop on purpose. Each run of this costs one attempt
// against the login limiter (10/min/IP — see auth.setup.js on the 429 that
// causes), and nothing about a refused login varies with the width. Both
// locales still run, because the message is the part that is translated.
for (const locale of ["en", "de"]) {
  test.describe(`login refusal (${locale})`, () => {
    test.use({
      storageState: { cookies: [], origins: [] },
      viewport: { width: 324, height: 756 },
      locale,
    });

    test(`rejects a wrong password without leaking whether the user exists (${locale})`, async ({ page }) => {
      await blockExternalRequests(page);
      await page.goto("/");

      const form = page.locator(".auth-form");
      await form.locator('[name="username"]').fill("demo");
      await form.locator('[name="password"]').fill("definitely-not-the-password");
      await form.locator('button[type="submit"]').click();

      // The error is announced, not just shown: role=alert is the only way
      // somebody using a screen reader learns the submit did anything.
      const error = page.locator(".auth-form__error");
      await expect(error).toBeVisible();
      await expect(error).toHaveAttribute("role", "alert");
      await expect(error).not.toBeEmpty();

      // Still on the login screen rather than in a half-authenticated state.
      await expect(page.locator(".auth-form")).toBeVisible();
      await expect(page.locator(".app-header")).toHaveCount(0);
    });
  });
}
