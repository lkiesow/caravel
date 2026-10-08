// Things you navigate to are real links (Stage 47).
//
// Location and trip cards used to be role="button" elements with a click
// handler, so middle-click, Ctrl-click and "Open link in new tab" had no href
// to work with: opening a location in a background tab meant opening it,
// copying the URL and pasting it. Each card is now wrapped in an
// <a data-link>, which the router takes over for a plain click only.
//
// Every assertion here is about which *page* ends up where: a plain click must
// stay a client-side navigation in this tab (the window marker survives, so no
// full load), a modified click must leave this tab alone and open the URL in a
// new one.
//
// The trip tab bar got the same treatment in Milestone 2, below.
//
// Owns its trips, so the seeded scenarios other specs read are never touched.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const DESKTOP = { width: 1280, height: 900 };

test.describe("cards are links", () => {
  test.use({ viewport: DESKTOP });

  let tripIds = [];

  async function createTrip(page, data) {
    const res = await page.request.post("/api/trips", { data });
    expect(res.status(), "create the spec's own trip").toBe(201);
    const trip = await res.json();
    tripIds.push(trip.id);
    return trip;
  }

  async function createItem(page, tripId, title) {
    const res = await page.request.post(`/api/trips/${tripId}/locations`, {
      data: { title, category: "stay", tags: ["hotel"], dates: [] },
    });
    expect(res.status(), "create a location").toBe(201);
    return res.json();
  }

  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test.afterEach(async ({ page }) => {
    for (const id of tripIds) await page.request.delete(`/api/trips/${id}`);
    tripIds = [];
  });

  // The case that was reported: a location card in a trip's list.
  test("a location card opens in a new tab on Ctrl-click and middle-click, and in place on a plain click", async ({
    page,
    context,
  }) => {
    const trip = await createTrip(page, { title: "UI suite: card links" });
    const item = await createItem(page, trip.id, "Hotel Ranga");
    const listPath = `/trips/${trip.id}/locations`;
    const itemPath = `/trips/${trip.id}/locations/${item.id}`;
    await gotoRoute(page, listPath);

    const link = page.getByRole("link", { name: /Hotel Ranga/ });
    await expect(link).toHaveAttribute("href", itemPath);
    // The card is the link's content, not a second control beside it.
    await expect(link.locator("item-card")).toHaveCount(1);
    await expect(page.locator('item-card[role="button"], item-card[tabindex]')).toHaveCount(0);
    // The global link colour must not leak into the card through inheritance.
    expect(await link.evaluate((el) => getComputedStyle(el).textDecorationLine)).toBe("none");
    const titleColor = await page
      .locator("item-card")
      .evaluate((el) => getComputedStyle(el.shadowRoot.querySelector("h2")).color);
    const bodyColor = await page.evaluate(() => getComputedStyle(document.body).color);
    expect(titleColor, "the card title keeps the text colour, not the link colour").toBe(bodyColor);

    for (const how of [{ modifiers: ["ControlOrMeta"] }, { button: "middle" }]) {
      const opened = context.waitForEvent("page");
      await page.locator("item-card").click(how);
      const tab = await opened;
      await tab.waitForLoadState();
      expect(new URL(tab.url()).pathname, `${JSON.stringify(how)} opens the location`).toBe(itemPath);
      await tab.close();
      expect(
        await page.evaluate(() => window.location.pathname),
        `${JSON.stringify(how)} leaves this tab on the list`
      ).toBe(listPath);
    }

    // A plain click is still the router's: same document, new route.
    await page.evaluate(() => (window.__sameDocument = true));
    await page.locator("item-card").click();
    await expect(page.getByRole("heading", { level: 1, name: "Hotel Ranga" })).toBeVisible();
    expect(await page.evaluate(() => window.location.pathname)).toBe(itemPath);
    expect(await page.evaluate(() => window.__sameDocument)).toBe(true);

    // Enter on the focused link, the keyboard path the old keydown handler had.
    await page.goBack();
    await expect(link).toBeVisible();
    await link.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("heading", { level: 1, name: "Hotel Ranga" })).toBeVisible();
    expect(await page.evaluate(() => window.__sameDocument)).toBe(true);
  });

  test("a trip card opens in a new tab on Ctrl-click, and in place on a plain click", async ({ page, context }) => {
    const trip = await createTrip(page, { title: "Cardlinks Alpha", start_date: "2031-05-01", end_date: "2031-05-09" });
    await createTrip(page, { title: "Cardlinks Beta" });
    await gotoRoute(page, "/trips");
    // Both into one grid row, for the height check below.
    await page.locator('input[name="q"]').fill("Cardlinks");
    await expect(page.locator("trip-card")).toHaveCount(2);

    const link = page.getByRole("link", { name: /Cardlinks Alpha/ });
    await expect(link).toHaveAttribute("href", `/trips/${trip.id}`);

    // The equal-height row trip-card.js carries down from the grid: the grid
    // item is now the link, so the stretch has to pass through it. Alpha has a
    // dates line and Beta does not, which is what used to make them differ.
    const boxes = await page
      .locator("trip-card")
      .evaluateAll((els) => els.map((el) => el.shadowRoot.querySelector(".card").getBoundingClientRect()));
    expect(boxes[0].top, "both cards share a row").toBe(boxes[1].top);
    expect(boxes[0].height).toBe(boxes[1].height);

    const opened = context.waitForEvent("page");
    await page.locator('trip-card[title="Cardlinks Alpha"]').click({ modifiers: ["ControlOrMeta"] });
    const tab = await opened;
    await tab.waitForLoadState();
    // The trip URL canonicalises itself to its first tab.
    await expect(tab).toHaveURL(new RegExp(`/trips/${trip.id}(/locations)?$`));
    await tab.close();
    expect(await page.evaluate(() => window.location.pathname)).toBe("/trips");

    await page.evaluate(() => (window.__sameDocument = true));
    await page.locator('trip-card[title="Cardlinks Alpha"]').click();
    await expect(page.getByRole("heading", { level: 1, name: "Cardlinks Alpha" })).toBeVisible();
    expect(await page.evaluate(() => window.location.pathname)).toBe(`/trips/${trip.id}/locations`);
    expect(await page.evaluate(() => window.__sameDocument)).toBe(true);
  });
});

// The trip tab bar, Milestone 2. Tabs are links too, but without data-link:
// a plain click is taken over by trip-detail-page.js, which switches the tab
// with a local re-render instead of a route match -- so the trip must not be
// fetched again. A modified click is the browser's.
test.describe("trip tabs are links", () => {
  test.use({ viewport: DESKTOP });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: tab links" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("every tab has its route as href, and the current one is marked", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations`);
    const tabs = page.locator(".trip-tabs > a[data-tab]");
    await expect(tabs).toHaveCount(9);
    for (const tab of await tabs.all()) {
      const key = await tab.getAttribute("data-tab");
      await expect(tab).toHaveAttribute("href", `/trips/${tripId}/${key}`);
      // The global link styling must not reach the bar.
      expect(await tab.evaluate((el) => getComputedStyle(el).textDecorationLine)).toBe("none");
    }
    await expect(page.locator('.trip-tabs [aria-current="page"]')).toHaveCount(1);
    await expect(page.locator('.trip-tabs [aria-current="page"]')).toHaveAttribute("data-tab", "locations");
  });

  test("a plain click switches in place without refetching the trip; Ctrl-click opens a new tab", async ({
    page,
    context,
  }) => {
    await gotoRoute(page, `/trips/${tripId}/locations`);
    const tripFetches = [];
    page.on("request", (req) => {
      if (new URL(req.url()).pathname === `/api/trips/${tripId}`) tripFetches.push(req.url());
    });

    const notes = page.locator('.trip-tabs > a[data-tab="notes"]');
    const opened = context.waitForEvent("page");
    await notes.click({ modifiers: ["ControlOrMeta"] });
    const tab = await opened;
    await tab.waitForLoadState();
    expect(new URL(tab.url()).pathname).toBe(`/trips/${tripId}/notes`);
    await tab.close();
    expect(await page.evaluate(() => window.location.pathname)).toBe(`/trips/${tripId}/locations`);
    await expect(page.locator('.trip-tabs [aria-current="page"]')).toHaveAttribute("data-tab", "locations");

    await page.evaluate(() => (window.__sameDocument = true));
    await page.locator('.trip-tabs > a[data-tab="itinerary"]').click();
    await expect(page.locator('.trip-tabs [aria-current="page"]')).toHaveAttribute("data-tab", "itinerary");
    expect(await page.evaluate(() => window.location.pathname)).toBe(`/trips/${tripId}/itinerary`);
    expect(await page.evaluate(() => window.__sameDocument)).toBe(true);
    expect(tripFetches, "a tab switch is a local re-render, not a route match").toEqual([]);

    // Back still returns to the previous tab.
    await page.goBack();
    await expect(page.locator('.trip-tabs [aria-current="page"]')).toHaveAttribute("data-tab", "locations");
  });
});
