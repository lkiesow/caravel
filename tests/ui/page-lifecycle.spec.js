// Pages get a lifecycle (Stage 48).
//
// The router hands every render an AbortSignal and aborts it when the next
// render starts, and the router itself can be torn down. Each test here is one
// thing that used to outlive the page it belonged to.
//
// Reads the seeded scenarios only; nothing here writes.
import { test, expect } from "@playwright/test";
import { login, gotoRoute, SCENARIO_TITLES, DEMO_USER } from "./helpers/scenarios.js";

const DESKTOP = { width: 1280, height: 900 };

test.describe("page lifecycle", () => {
  test.use({ viewport: DESKTOP });

  // Logging out and back in used to leave the first router's popstate and
  // click listeners behind, so from then on every navigation was rendered twice
  // -- once for real, once into the old router's detached <main> -- and every
  // route's fetches went out twice.
  //
  // Logout and login are faked at the network: a real logout would end the
  // session the whole suite shares (see auth.setup.js), and a real login would
  // spend the 10/min budget. What is under test is the client re-mounting, which
  // happens the same either way.
  test("logging out and back in leaves one router", async ({ page }) => {
    await login(page);
    const me = await (await page.request.get("/api/auth/me")).json();
    await page.route("**/api/auth/logout", (route) => route.fulfill({ status: 204 }));
    await page.route("**/api/auth/login", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(me) }));

    await gotoRoute(page, "/trips");
    await page.locator(".user-menu-slot .menu__trigger").click();
    await page.locator(".user-menu-slot [role=menuitem]").last().click();
    await page.locator('input[name="username"]').fill(DEMO_USER.username);
    await page.locator('input[name="password"]').fill(DEMO_USER.password);
    await page.locator('button[type="submit"]').click();
    const card = page.locator(`trip-card[title="${SCENARIO_TITLES.full}"]`);
    await expect(card).toBeVisible();

    // Counted from here on: one per route render. Both of the router's
    // listeners are covered -- the card is a [data-link] click, Back is a
    // popstate.
    const tripFetches = [];
    const listFetches = [];
    page.on("request", (req) => {
      if (req.method() !== "GET") return;
      const path = new URL(req.url()).pathname;
      if (/^\/api\/trips\/[^/]+$/.test(path)) tripFetches.push(path);
      if (path === "/api/trips") listFetches.push(path);
    });

    await card.click();
    await expect(page).toHaveURL(/\/trips\/[^/]+\/locations$/);
    await expect(page.locator(".trip-tab-content")).toBeVisible();
    await page.evaluate(() => new Promise((r) => setTimeout(r, 300)));
    expect(tripFetches, "one trip fetch for one click").toHaveLength(1);

    await page.goBack();
    await expect(card).toBeVisible();
    await page.evaluate(() => new Promise((r) => setTimeout(r, 300)));
    expect(listFetches, "one list fetch for one Back").toHaveLength(1);
  });
});
