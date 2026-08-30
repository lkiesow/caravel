// A stored link with a dangerous scheme must not become a clickable link.
//
// The server refuses anything but http and https on the way in (Stage 27), so
// this cannot be set up through the API -- which is the point. The rows this
// protects are the ones written before that check existed and still sitting in
// somebody's database, so the fixture is an intercepted API response rather
// than a real location: exactly the shape a pre-fix row arrives in.
//
// Two assertions, and the second is the one that matters. That no anchor
// carries the URL is easy to satisfy by accident; that the text is still on
// the page proves the guard renders it inertly rather than dropping it, which
// is what lets somebody see what is stored and go and remove it.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const DANGEROUS = "javascript:alert(document.domain)";

test.describe("links with an unsafe scheme", () => {
  let tripId;
  let itemId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const trip = await page.request.post("/api/trips", { data: { title: "UI suite: link safety" } });
    expect(trip.status(), "create the spec's own trip").toBe(201);
    tripId = (await trip.json()).id;

    const item = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Kex Hostel", category: "stay" },
    });
    expect(item.status(), "create the location").toBe(201);
    itemId = (await item.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  // The server half: it cannot be stored in the first place.
  test("the API refuses to store one", async ({ page }) => {
    const res = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Planted", category: "site", links: [{ url: DANGEROUS }] },
    });
    expect(res.status(), "a javascript: link is refused").toBe(400);

    const standalone = await page.request.post(`/api/items/${itemId}/links`, {
      data: { url: DANGEROUS },
    });
    expect(standalone.status(), "and refused by the standalone link endpoint too").toBe(400);
  });

  // The client half: a row that predates the server check still renders safely.
  test("the location page renders a stored one as text, not a link", async ({ page }) => {
    await plantLink(page, `**/api/items/${itemId}`);

    await gotoRoute(page, `/trips/${tripId}/locations/${itemId}`);

    await expect(page.locator(`a[href="${DANGEROUS}"]`), "no clickable javascript: link").toHaveCount(0);
    await expect(page.locator(".link-list__unsafe"), "shown as inert text instead").toHaveText(DANGEROUS);
  });

  test("the editor renders a stored one as text, not a link", async ({ page }) => {
    await plantLink(page, `**/api/items/${itemId}`);

    await gotoRoute(page, `/trips/${tripId}/locations/${itemId}/edit`);

    await expect(page.locator(`a[href="${DANGEROUS}"]`), "no clickable javascript: link").toHaveCount(0);
    await expect(page.locator(".link-list__unsafe"), "shown as inert text instead").toHaveText(DANGEROUS);
  });

  // Answers the item detail request with the real response plus one link the
  // server would no longer accept, which is what a row written before the
  // check looks like coming out of the database.
  async function plantLink(page, pattern) {
    await page.route(pattern, async (route) => {
      if (route.request().method() !== "GET") return route.continue();
      const response = await route.fetch();
      const body = await response.json();
      body.links = [{ id: "planted", url: DANGEROUS, label: null, sort_order: 0 }];
      await route.fulfill({ response, json: body });
    });
  }
});
