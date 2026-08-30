// The trip-level assistant: several places at once, reviewed and added
// together.
//
// Driven by the stub provider's suggest script (internal/assist/stub.go),
// which proposes three Reykjavik places -- one with a cover and coordinates,
// one with coordinates and no cover, and one deliberately thin: no address, no
// link, nothing to geocode. That third one is the point of the fixture. A
// script where every candidate was complete would never show whether a sparse
// card renders, and sparse is the common case.
//
// Owns its trip, like every other spec that writes, so the seeded scenarios
// are untouched.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

test.describe("suggesting several locations", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);

    // Server configuration rather than seed data, so a developer running the
    // suite against their own dev server gets a skip rather than a failure --
    // the same reasoning assist.spec.js gives.
    const me = await (await page.request.get("/api/auth/me")).json();
    test.skip(!me.capabilities.assist, "needs a server started with CARAVEL_LLM_URL=stub");

    const res = await page.request.post("/api/trips", { data: { title: "UI suite: suggest spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("is reached from the New menu, and adds the ticked places in one go", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations`);

    // The entry point: New is a menu now, not a plain button, because the
    // toolbar had no room for a fifth control.
    await page.locator(".locations-new-slot .menu__trigger").click();
    const suggestRow = page.locator('.locations-new-slot [role="menu"] button', { hasText: "Suggest locations" });
    await expect(suggestRow).toBeVisible();
    await suggestRow.click();

    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/suggest$`));

    await page.locator(".suggest-page__prompt").fill("things to do in Reykjavik");
    await page.locator('[data-action="suggest-run"]').click();

    const cards = page.locator(".suggest-card");
    await expect(cards).toHaveCount(3, { timeout: 30_000 });

    // Every card is ticked to start with: the reviewing act is removing the
    // wrong ones, not picking the right ones out of a blank list.
    await expect(page.locator(".suggest-card input[type=checkbox]:checked")).toHaveCount(3);
    await expect(page.locator('[data-action="suggest-add"]')).toHaveText("Add 3 locations");

    // The run trace is an account of what it did, and it is shared with the
    // editor's panel -- so this also proves the extraction reaches both.
    await expect(page.locator(".assist-trace")).toBeVisible();
    // The pages it read, likewise shared.
    await expect(page.locator(".assist-sources a").first()).toBeVisible();

    // The thin candidate has no cover and no links, and still renders.
    const thin = cards.filter({ hasText: "Braud and Co" });
    await expect(thin).toHaveCount(1);
    await expect(thin.locator(".suggest-card__cover")).toHaveCount(0);
    await expect(thin.locator(".suggest-card__links")).toHaveCount(0);

    // Untick one, and the button counts down with it.
    await thin.locator("input[type=checkbox]").uncheck();
    await expect(page.locator('[data-action="suggest-add"]')).toHaveText("Add 2 locations");

    await page.locator('[data-action="suggest-add"]').click();

    // Back on the locations tab, holding exactly the two that were ticked.
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations$`));
    await expect(page.locator("item-card")).toHaveCount(2);

    // Read back through the API rather than off the cards: what matters is
    // that the whole candidate was written, not that a title was rendered.
    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.map((i) => i.title).sort()).toEqual(["Hallgrimskirkja", "Kex Hostel"]);

    const church = items.find((i) => i.title === "Hallgrimskirkja");
    expect(church.category, "the proposed category was written").toBe("site");
    expect(church.tags, "the proposed tags were split into a list").toContain("church");
    expect(typeof church.lat, "the geocoded position was written").toBe("number");

    const detail = await (await page.request.get(`/api/items/${church.id}`)).json();
    expect(detail.links.length, "the proposed link was written").toBe(1);
    expect(detail.notes, "the proposed notes were written").toContain("church");
  });

  // Nothing is written until the button is pressed. The whole feature rests on
  // that, so it gets an assertion rather than a comment.
  test("writes nothing to the trip until the button is pressed", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/suggest`);

    await page.locator(".suggest-page__prompt").fill("things to do in Reykjavik");
    await page.locator('[data-action="suggest-run"]').click();
    await expect(page.locator(".suggest-card")).toHaveCount(3, { timeout: 30_000 });

    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items, "the trip is still empty while the candidates are on screen").toHaveLength(0);
  });

  // A place already on the trip is dropped by the server, and the page says so
  // rather than silently offering two where it offered three.
  test("says when a suggestion was skipped for being on the trip already", async ({ page }) => {
    const existing = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Kex Hostel", category: "stay" },
    });
    expect(existing.status()).toBe(201);

    await gotoRoute(page, `/trips/${tripId}/suggest`);
    await page.locator(".suggest-page__prompt").fill("things to do in Reykjavik");
    await page.locator('[data-action="suggest-run"]').click();

    await expect(page.locator(".suggest-card")).toHaveCount(2, { timeout: 30_000 });
    await expect(page.locator(".suggest-page__note")).toContainText("already has it");
    await expect(page.locator(".suggest-card", { hasText: "Kex Hostel" })).toHaveCount(0);
  });

  test("refuses to run with no prompt", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/suggest`);

    await page.locator('[data-action="suggest-run"]').click();

    await expect(page.locator(".suggest-page__error")).toBeVisible();
    await expect(page.locator(".suggest-card")).toHaveCount(0);
  });

  // With no assistant there is only one way to add a location, so the button
  // is a plain button that goes straight there -- not a menu with a single
  // row, which is a tap in front of the thing you asked for. The default
  // instance has no assistant, so this is the shape most people see.
  test("is a plain button, not a one-row menu, where there is no assistant", async ({ page }) => {
    await page.route("**/api/auth/me", async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      // The nested object is spread on its own: a shallow spread of the
      // payload would carry the original capabilities through untouched.
      await route.fulfill({
        response,
        json: { ...body, capabilities: { ...body.capabilities, assist: false } },
      });
    });

    await gotoRoute(page, `/trips/${tripId}/locations`);

    await expect(page.locator(".locations-new-slot .menu__trigger")).toHaveCount(0);
    const button = page.locator('.locations-new-slot [data-action="new-item"]');
    await expect(button).toHaveCount(1);

    // And it opens the editor directly, with no menu in between.
    await button.click();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/new$`));
  });

  // The toolbar is a deliberately non-wrapping row that fits 324px exactly,
  // and this milestone put a menu where a button was. A regression here is
  // invisible in every other assertion.
  test("the toolbar still does not wrap at 324px", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations`);

    const wrapped = await page.locator(".list-toolbar").evaluate((el) => {
      const rows = new Set();
      for (const child of el.children) rows.add(Math.round(child.getBoundingClientRect().top));
      return rows.size > 1;
    });
    expect(wrapped, "the toolbar wrapped onto a second row").toBe(false);

    const overflows = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    expect(overflows, "the page scrolls horizontally").toBe(false);
  });
});
