// Pages get a lifecycle (Stage 48).
//
// The router hands every render an AbortSignal and aborts it when the next
// render starts, and the router itself can be torn down. Each test here is one
// thing that used to outlive the page it belonged to.
//
// The first test reads the seeded scenarios only; the rest own their trips.
import { test, expect } from "@playwright/test";
import { login, gotoRoute, SCENARIO_TITLES, DEMO_USER } from "./helpers/scenarios.js";
import { holdRoute } from "./helpers/gate.js";

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

// A save always finishes -- aborting a request in flight would not undo it on
// the server -- but the redirect after it is skipped once the user has gone
// somewhere else. Each test holds the request open, leaves, then releases it.
test.describe("late saves", () => {
  test.use({ viewport: DESKTOP });

  let tripIds = [];

  async function createTrip(page, title) {
    const res = await page.request.post("/api/trips", { data: { title } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    const trip = await res.json();
    tripIds.push(trip.id);
    return trip;
  }

  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test.afterEach(async ({ page }) => {
    // The delete tests have already removed theirs; a 404 here is fine.
    for (const id of tripIds) await page.request.delete(`/api/trips/${id}`);
    tripIds = [];
  });

  async function openUserSettings(page) {
    await page.locator(".user-menu-slot .menu__trigger").click();
    await page.locator(".user-menu-slot [role=menuitem]").first().click();
    await expect(page).toHaveURL(/\/settings$/);
  }

  test("a location saved after the user left does not pull them back", async ({ page }) => {
    const trip = await createTrip(page, "UI suite: late location save");
    const res = await page.request.post(`/api/trips/${trip.id}/locations`, {
      data: { title: "Late Save", category: "site", tags: [], dates: [] },
    });
    expect(res.status()).toBe(201);
    const location = await res.json();
    await gotoRoute(page, `/trips/${trip.id}/locations/${location.id}/edit`);

    const gate = await holdRoute(page, "**/api/locations/*", { method: "PATCH" });
    const saved = page.waitForResponse((r) => r.request().method() === "PATCH" && r.url().includes(`/api/locations/${location.id}`));
    await page.locator('[data-action="save"]').click();
    await gate.arrived();

    await page.locator(".app-brand").click();
    await expect(page).toHaveURL(/\/trips$/);
    gate.release();
    expect((await saved).status(), "the save itself still lands").toBe(200);
    await page.evaluate(() => new Promise((r) => setTimeout(r, 300)));
    expect(await page.evaluate(() => window.location.pathname)).toBe("/trips");
  });

  test("a trip deleted after the user left it does not pull them back", async ({ page }) => {
    const trip = await createTrip(page, "UI suite: late trip delete");
    await gotoRoute(page, `/trips/${trip.id}/settings`);

    const gate = await holdRoute(page, `**/api/trips/${trip.id}`, { method: "DELETE" });
    await page.locator('.trip-tab-content [data-action="delete"]').click();
    await page.locator("dialog.dialog .btn-danger").click();
    await gate.arrived();

    await openUserSettings(page);
    gate.release();
    await expect.poll(async () => (await page.request.get(`/api/trips/${trip.id}`)).status()).toBe(404);
    await page.evaluate(() => new Promise((r) => setTimeout(r, 300)));
    expect(await page.evaluate(() => window.location.pathname)).toBe("/settings");
  });

  // The other half of the same rule: switching tabs is not leaving the trip,
  // and a trip that is gone has nothing left to show on any of its tabs.
  test("a trip deleted while the user only switched tabs still goes to the list", async ({ page }) => {
    const trip = await createTrip(page, "UI suite: delete then switch tab");
    await gotoRoute(page, `/trips/${trip.id}/settings`);

    const gate = await holdRoute(page, `**/api/trips/${trip.id}`, { method: "DELETE" });
    await page.locator('.trip-tab-content [data-action="delete"]').click();
    await page.locator("dialog.dialog .btn-danger").click();
    await gate.arrived();

    await page.locator('a[data-tab="locations"]').click();
    await expect(page).toHaveURL(/\/locations$/);
    gate.release();
    await expect(page).toHaveURL(/\/trips$/);
  });

  // While a confirm is open the modal blocks every click, so Back is the one
  // way off the page. The dialog used to survive it, and confirming then
  // deleted the trip from the page the user had gone back to.
  test("Back closes an open confirm as a cancel", async ({ page }) => {
    const trip = await createTrip(page, "UI suite: Back over a confirm");
    await gotoRoute(page, `/trips/${trip.id}/locations`);
    // A plain JS click: Settings may sit in the "More" menu at this width,
    // and what is under test is the history entry it pushes.
    await page.evaluate(() => document.querySelector('a[data-tab="settings"]').click());
    await expect(page).toHaveURL(/\/settings$/);

    const deletes = [];
    page.on("request", (req) => {
      if (req.method() === "DELETE") deletes.push(req.url());
    });
    await page.locator('.trip-tab-content [data-action="delete"]').click();
    await expect(page.locator("dialog.dialog")).toBeVisible();

    await page.goBack();
    await expect(page).toHaveURL(/\/locations$/);
    await expect(page.locator("dialog.dialog")).toHaveCount(0);
    expect(deletes, "nothing was deleted").toHaveLength(0);
  });
});

// What a page started outside its own markup stops when the page goes.
test.describe("teardown", () => {
  test.use({ viewport: DESKTOP });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: teardown" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  // Keeps the signal each assist stream was started with, so the test can ask
  // it directly whether leaving aborted it. The request itself is held at the
  // network and never released: the stub would otherwise answer before the
  // test could leave.
  const RECORD_STREAM_SIGNALS = `
    window.__streamSignals = [];
    const recordedFetch = window.fetch;
    window.fetch = function (input, init) {
      if (String(input).includes("/assist/")) window.__streamSignals.push(init?.signal);
      return recordedFetch.apply(this, arguments);
    };
  `;

  async function assistOrSkip(page) {
    const me = await (await page.request.get("/api/auth/me")).json();
    test.skip(!me.capabilities.assist, "needs a server started with CARAVEL_LLM_URL=stub");
  }

  test("leaving the suggest page cancels its stream", async ({ page }) => {
    await assistOrSkip(page);
    await page.addInitScript(RECORD_STREAM_SIGNALS);
    await gotoRoute(page, `/trips/${tripId}/suggest`);

    const gate = await holdRoute(page, "**/api/trips/*/assist/locations");
    await page.locator(".suggest-page__prompt").fill("things to do in Reykjavik");
    await page.locator('[data-action="suggest-run"]').click();
    await gate.arrived();
    expect(await page.evaluate(() => window.__streamSignals.map((s) => s.aborted))).toEqual([false]);

    await page.locator(".app-brand").click();
    await expect(page).toHaveURL(/\/trips$/);
    expect(await page.evaluate(() => window.__streamSignals.map((s) => s.aborted))).toEqual([true]);
  });

  test("leaving the location editor cancels its assist stream", async ({ page }) => {
    await assistOrSkip(page);
    await page.addInitScript(RECORD_STREAM_SIGNALS);
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    const gate = await holdRoute(page, "**/api/trips/*/assist/location");
    await page.locator(".assist__prompt").fill("Harpa concert hall");
    await page.locator('[data-action="assist-run"]').click();
    await gate.arrived();
    expect(await page.evaluate(() => window.__streamSignals.map((s) => s.aborted))).toEqual([false]);

    await page.locator(".app-brand").click();
    await expect(page).toHaveURL(/\/trips$/);
    expect(await page.evaluate(() => window.__streamSignals.map((s) => s.aborted))).toEqual([true]);
  });

  // The @-picker closes itself on resize, through a listener on window. It used
  // to be removed only when the notes tab re-rendered itself, so leaving the
  // editor by switching tabs left it behind -- one more each time.
  test("switching away from the notes editor removes the mention picker's listeners", async ({ page }) => {
    await page.addInitScript(`
      window.__resizeListeners = 0;
      const add = window.addEventListener, remove = window.removeEventListener;
      window.addEventListener = function (type, ...rest) {
        if (type === "resize") window.__resizeListeners++;
        return add.call(this, type, ...rest);
      };
      window.removeEventListener = function (type, ...rest) {
        if (type === "resize") window.__resizeListeners--;
        return remove.call(this, type, ...rest);
      };
    `);
    await gotoRoute(page, `/trips/${tripId}/locations`);
    const baseline = await page.evaluate(() => window.__resizeListeners);

    // A new trip's note is empty, which opens the tab straight in the editor.
    await page.evaluate(() => document.querySelector('a[data-tab="notes"]').click());
    await expect(page.locator(".trip-notes textarea")).toBeVisible();
    expect(await page.evaluate(() => window.__resizeListeners), "the picker is listening").toBe(baseline + 1);

    await page.locator('a[data-tab="locations"]').click();
    await expect(page).toHaveURL(/\/locations$/);
    expect(await page.evaluate(() => window.__resizeListeners), "and stops when the tab goes").toBe(baseline);
  });
});
