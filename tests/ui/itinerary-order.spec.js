// The itinerary tab, end to end: reordering entries inside a day (Stage 15
// Milestone 4), and the rest of what the tab writes (Stage 19 Milestone 3).
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

// Used by both describes below, so it sits at module scope rather than inside
// one of them.
//
// The `> li` child combinator throughout this file is load-bearing, not style:
// since Stage 22 each entry row carries an overflow menu, whose dropdown is a
// <ul> of its own, so a descendant `li` selector counts two menu items per row
// as if they were entries.
function entryTitles(page) {
  return page
    .locator(".itinerary-day__entries > li .itinerary-entry__link span:not(.dot)")
    .allTextContents();
}

// Opens one entry row's overflow menu and picks an item from it. Remove lives
// there since Stage 22, alongside Move to another day.
async function entryMenu(row, label) {
  await row.locator(".itinerary-entry__menu .menu__trigger").click();
  await row.locator(`.menu__dropdown button:has-text("${label}")`).click();
}

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

  test("moves an entry, persists it, and disables the ends", async ({
    page,
  }) => {
    await page.goto(`/trips/${tripId}/itinerary`);
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(3);

    // The order entries were added in. Before this milestone every row carried
    // sort_order 0 and this was whatever the database chose.
    expect(await entryTitles(page)).toEqual(TITLES);

    const rows = page.locator(".itinerary-day__entries > li");
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
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(3);
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
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(3);
    expect(
      await entryTitles(page),
      "the new order should survive a reload",
    ).toEqual(["Dinner", "Breakfast", "Museum"]);
  });

  test("keeps three 44px controls and the title on one row at 324px", async ({
    page,
  }) => {
    await page.goto(`/trips/${tripId}/itinerary`);
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(3);

    const geometry = await page
      .locator(".itinerary-day__entries > li")
      .first()
      .evaluate((li) => ({
        rowScrolls: li.scrollWidth > li.clientWidth,
        // Direct controls only: the menu's own dropdown buttons are inside
        // this row too, and they are not what the row lays out.
        buttons: [...li.querySelectorAll(".itinerary-entry__actions > button, .itinerary-entry__actions .menu__trigger")].map((b) => {
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

    // The third control is the overflow menu since Stage 22; Remove moved
    // inside it rather than becoming a fourth icon in a 324px row.
    expect(geometry.buttons.map((b) => b.action)).toEqual([
      "move-up",
      "move-down",
      "toggle",
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

// The rest of the tab's writes. A second describe rather than a second file:
// the setup is the same trip with the same shape, and the reorder tests above
// are the only reason this file was ever narrower than the tab.
//
// There is still nothing named "unschedule": removing the entry is what that
// would mean, and the location itself is untouched by it, which is asserted
// below. Moving an entry to another day *does* exist as of Stage 22 and has
// its own describe at the end of this file.
test.describe("the itinerary tab, end to end", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: itinerary tab" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("adds a day, puts a location on it, and removing the entry keeps the location", async ({ page }) => {
    const item = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Blue Lagoon", category: "site" },
    });
    expect(item.status(), "create a location to schedule").toBe(201);
    const itemId = (await item.json()).id;

    await page.goto(`/trips/${tripId}/itinerary`);

    // A dateless trip synthesises no days at all, so the tab starts with none
    // and the Add a day form is the only way in.
    await expect(page.locator(".itinerary-day")).toHaveCount(0);
    await page.locator('.itinerary-add-day input[name="date"]').fill(DAY);
    await page.locator('.itinerary-add-day button[type="submit"]').click();

    const day = page.locator(".itinerary-day");
    await expect(day).toHaveCount(1);
    await expect(day.locator(".itinerary-day__empty")).toBeVisible();

    // The picker lists the trip's locations. It is populated when the tab
    // loads, which is why the location above is created before the goto.
    const add = day.locator(".itinerary-day__add-item");
    await add.locator('select[name="itemId"]').selectOption(itemId);
    await add.locator('button[type="submit"]').click();

    await expect(day.locator(".itinerary-day__entries > li")).toHaveCount(1);
    await expect(day.locator(".itinerary-day__empty")).toBeHidden();
    expect(await entryTitles(page)).toEqual(["Blue Lagoon"]);

    // Settle on the count before reading the titles: a bare read straight after
    // reload races the tab's own fetch and comes back empty, which reads as a
    // persistence failure rather than as a test that asked too early.
    await page.reload();
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(1);
    expect(await entryTitles(page), "the entry should have reached the database").toEqual(["Blue Lagoon"]);

    // Removing the entry unschedules it - the location itself survives, which
    // is the half a delete-cascade bug would get wrong and the list count
    // alone would not notice.
    await entryMenu(page.locator(".itinerary-day__entries > li").first(), "Remove");
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(0);

    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.map((i) => i.title), "unscheduling must not delete the location").toEqual(["Blue Lagoon"]);
  });

  test("removing a day with entries on it asks first", async ({ page }) => {
    const item = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Geysir", category: "site" },
    });
    expect(item.status()).toBe(201);
    const itemId = (await item.json()).id;
    const day = await page.request.put(`/api/trips/${tripId}/itinerary/days/${DAY}`, { data: { notes: null } });
    expect(day.status()).toBe(200);
    const dayId = (await day.json()).id;
    expect((await page.request.post(`/api/itinerary/days/${dayId}/entries`, { data: { item_id: itemId } })).status()).toBe(201);

    await page.goto(`/trips/${tripId}/itinerary`);
    await expect(page.locator(".itinerary-day__entries > li")).toHaveCount(1);

    // Cancel first. This one confirms only because the day has something on
    // it, so the dialog appearing at all is part of what is being asserted.
    await page.locator('[data-action="remove-day"]').click();
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();
    await expect(page.locator(".itinerary-day")).toHaveCount(1);

    await page.locator('[data-action="remove-day"]').click();
    // "Remove", not "Delete": this dialog passes its own confirmKey, because
    // the day is a container rather than the content.
    await page.locator(".dialog__actions button", { hasText: "Remove" }).click();
    await expect(page.locator(".itinerary-day")).toHaveCount(0);

    // The day and its entry are gone; the location is not.
    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.map((i) => i.title)).toEqual(["Geysir"]);
  });
});

// Moving an entry to another day (Stage 22 Milestone 2).
//
// The point of the feature is that the entry survives the move intact, so the
// assertions are about the note rather than only about which list it is in:
// remove-and-re-add, which is what people had to do before, loses it.
test.describe("moving an entry to another day", () => {
  test.use({ viewport: MOBILE });

  const OTHER_DAY = "2026-08-22";
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: itinerary move" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;

    // Two real days, and one entry with a note on the first of them.
    for (const date of [DAY, OTHER_DAY]) {
      const day = await page.request.put(`/api/trips/${tripId}/itinerary/days/${date}`, { data: { notes: null } });
      expect(day.status(), `create day ${date}`).toBe(200);
      if (date !== DAY) continue;
      const item = await page.request.post(`/api/trips/${tripId}/items`, { data: { title: "Museum", category: "site" } });
      expect(item.status()).toBe(201);
      const entry = await page.request.post(`/api/itinerary/days/${(await day.json()).id}/entries`, {
        data: { item_id: (await item.json()).id, note: "book ahead" },
      });
      expect(entry.status(), "add the entry with a note").toBe(201);
    }
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("moves the entry, keeps its note, and saves it", async ({ page }) => {
    await page.goto(`/trips/${tripId}/itinerary`);

    const firstDay = page.locator(".itinerary-day").first();
    const secondDay = page.locator(".itinerary-day").nth(1);
    await expect(firstDay.locator(".itinerary-day__entries > li")).toHaveCount(1);
    await expect(secondDay.locator(".itinerary-day__entries > li")).toHaveCount(0);

    await entryMenu(firstDay.locator(".itinerary-day__entries > li").first(), "Move to another day");

    // The dialog offers the *other* days, never the one the entry is already
    // on, and it defaults to the next one along.
    const select = page.locator("dialog.dialog .dialog__select");
    await expect(select).toBeVisible();
    await expect(select.locator("option")).toHaveCount(1);
    await expect(select).toHaveValue(OTHER_DAY);

    await page.locator('dialog.dialog .dialog__actions button[value="confirm"]').click();

    await expect(firstDay.locator(".itinerary-day__entries > li")).toHaveCount(0);
    await expect(secondDay.locator(".itinerary-day__entries > li")).toHaveCount(1);
    await expect(firstDay.locator(".itinerary-day__empty")).toBeVisible();

    // The note came with it. This is the assertion the whole milestone is for.
    await expect(secondDay.locator(".itinerary-entry__note")).toHaveText("book ahead");

    // And it was saved, not merely drawn.
    await page.reload();
    await expect(page.locator(".itinerary-day").nth(1).locator(".itinerary-day__entries > li")).toHaveCount(1);
    expect(await entryTitles(page), "the move should survive a reload").toEqual(["Museum"]);
    await expect(page.locator(".itinerary-entry__note")).toHaveText("book ahead");
  });

  test("cancelling the dialog moves nothing", async ({ page }) => {
    await page.goto(`/trips/${tripId}/itinerary`);
    const firstDay = page.locator(".itinerary-day").first();
    await expect(firstDay.locator(".itinerary-day__entries > li")).toHaveCount(1);

    await entryMenu(firstDay.locator(".itinerary-day__entries > li").first(), "Move to another day");
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();

    await expect(page.locator("dialog.dialog")).toHaveCount(0);
    await expect(firstDay.locator(".itinerary-day__entries > li")).toHaveCount(1);
    const itinerary = await (await page.request.get(`/api/trips/${tripId}/itinerary`)).json();
    const withEntries = itinerary.filter((d) => (d.entries || []).length);
    expect(
      withEntries.map((d) => d.date),
      "a cancelled dialog must not have written anything",
    ).toEqual([DAY]);
  });
});
