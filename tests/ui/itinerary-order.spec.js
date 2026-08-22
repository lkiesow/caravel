// Reordering entries inside an itinerary day (Stage 15 Milestone 4).
//
// This one *writes*, so it follows files.spec.js: its own trip, created in
// beforeEach and deleted in afterEach, so the seeded itineraries the route
// sweeps depend on are never reordered underneath them.
//
// The trip deliberately has no dates. itinerary-tab.js opens every day on a
// trip with no date range (there is no "today" to measure against), which saves
// the spec from having to expand a <details> before it can reach the controls.
import { test, expect } from "@playwright/test";
import { login } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };
const DAY = "2026-08-20";
const TITLES = ["Breakfast", "Museum", "Dinner"];

test.describe("itinerary entry order", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const trip = await page.request.post("/api/trips", {
      data: { title: "UI suite: itinerary order" },
    });
    expect(trip.status(), "create the spec's own trip").toBe(201);
    tripId = (await trip.json()).id;

    // A real day row: days inside a trip's range are synthesized and carry no
    // id, so there would be nothing for the entry routes to aim at.
    const day = await page.request.put(
      `/api/trips/${tripId}/itinerary/days/${DAY}`,
      { data: { notes: null } },
    );
    expect(day.status(), "create the day").toBe(200);
    const dayId = (await day.json()).id;

    // Added one at a time and in order, which is also what makes the rendered
    // order meaningful below.
    for (const title of TITLES) {
      const item = await page.request.post(`/api/trips/${tripId}/items`, {
        data: { title, category: "site" },
      });
      expect(item.status(), `create item ${title}`).toBe(201);
      const entry = await page.request.post(
        `/api/itinerary/days/${dayId}/entries`,
        {
          data: { item_id: (await item.json()).id },
        },
      );
      expect(entry.status(), `add ${title} to the day`).toBe(201);
    }
  });

  test.afterEach(async ({ page }) => {
    // Cascades to the day, its entries and the items. Runs even on failure, so
    // a red run leaves no litter for the next one.
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
  });

  function entryTitles(page) {
    return page
      .locator(
        ".itinerary-day__entries li .itinerary-entry__link span:not(.dot)",
      )
      .allTextContents();
  }

  test("moves an entry, persists it, and disables the ends", async ({
    page,
  }) => {
    await page.goto(`/trips/${tripId}/itinerary`);
    await expect(page.locator(".itinerary-day__entries li")).toHaveCount(3);

    // The order entries were added in. Before this milestone every row carried
    // sort_order 0 and this was whatever the database chose.
    expect(await entryTitles(page)).toEqual(TITLES);

    const rows = page.locator(".itinerary-day__entries li");
    // The ends are disabled rather than missing, so the controls do not shift
    // position between rows.
    await expect(rows.nth(0).locator('[data-action="move-up"]')).toBeDisabled();
    await expect(
      rows.nth(0).locator('[data-action="move-down"]'),
    ).toBeEnabled();
    await expect(rows.nth(2).locator('[data-action="move-up"]')).toBeEnabled();
    await expect(
      rows.nth(2).locator('[data-action="move-down"]'),
    ).toBeDisabled();

    // Move the last one to the top, two presses, without re-aiming between
    // them: focus follows the entry, so the second press is on whatever the
    // first one left focused.
    await page.getByRole("button", { name: "Move Dinner earlier" }).click();
    await expect(page.locator(".itinerary-day__entries li")).toHaveCount(3);
    expect(await entryTitles(page)).toEqual(["Breakfast", "Dinner", "Museum"]);

    await page.getByRole("button", { name: "Move Dinner earlier" }).click();
    expect(await entryTitles(page)).toEqual(["Dinner", "Breakfast", "Museum"]);

    // Focus did not fall on the floor. Dinner is now at the top, where its own
    // "earlier" button is disabled, so focus should have gone to the enabled
    // one on the same entry rather than nowhere.
    const focused = await page.evaluate(() =>
      document.activeElement?.getAttribute("aria-label"),
    );
    expect(focused, "focus should stay on the entry that moved").toBe(
      "Move Dinner later",
    );

    // And it was actually saved, not just drawn.
    await page.reload();
    await expect(page.locator(".itinerary-day__entries li")).toHaveCount(3);
    expect(
      await entryTitles(page),
      "the new order should survive a reload",
    ).toEqual(["Dinner", "Breakfast", "Museum"]);
  });

  test("keeps three 44px controls and the title on one row at 324px", async ({
    page,
  }) => {
    await page.goto(`/trips/${tripId}/itinerary`);
    await expect(page.locator(".itinerary-day__entries li")).toHaveCount(3);

    const geometry = await page
      .locator(".itinerary-day__entries li")
      .first()
      .evaluate((li) => ({
        rowScrolls: li.scrollWidth > li.clientWidth,
        buttons: [...li.querySelectorAll("button")].map((b) => {
          const r = b.getBoundingClientRect();
          return {
            action: b.dataset.action,
            w: Math.round(r.width),
            h: Math.round(r.height),
          };
        }),
        docOverflow:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      }));

    expect(geometry.buttons.map((b) => b.action)).toEqual([
      "move-up",
      "move-down",
      "remove",
    ]);
    // Every one of the three, including the disabled ones - a disabled control
    // is still something a finger lands on.
    for (const b of geometry.buttons) {
      expect(b.w, `${b.action} is ${b.w}x${b.h}`).toBeGreaterThanOrEqual(44);
      expect(b.h, `${b.action} is ${b.w}x${b.h}`).toBeGreaterThanOrEqual(44);
    }
    expect(
      geometry.rowScrolls,
      "the entry row must not scroll horizontally",
    ).toBe(false);
    expect(
      geometry.docOverflow,
      "the page must not scroll horizontally",
    ).toBeLessThanOrEqual(0);
  });
});
