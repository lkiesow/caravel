// A second press during a request must do nothing.
//
// The bug: on a slow network, double-clicking Save in the location editor
// created the location twice. Nothing in web/js stopped a mutating handler from
// being re-entered while its request was in flight, so this spec is the one
// that proves the guard (web/js/busy.js) rather than the individual screens.
//
// Every case asserts three things, because each catches a different wrong fix:
//
//   1. exactly one request reached the gate -- the count the bug made two;
//   2. while it is held, the control is disabled and says aria-busy="true", so
//      the user can see it is working;
//   3. after release, the server holds exactly one row -- which is what catches
//      a guard that freezes the UI while the server still got both requests.
//
// Requests are held open (helpers/gate.js), not delayed: with a fixed delay the
// assertions would be racing the app instead of describing it.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";
import { holdRoute, doubleClick } from "./helpers/gate.js";

// A location needs a trip to live in, and these tests must not touch the
// seeded scenarios the read-only specs count rows in.
async function ownTrip(page, title) {
  const res = await page.request.post("/api/trips", { data: { title } });
  expect(res.status(), "create the spec's own trip").toBe(201);
  return (await res.json()).id;
}

const MOBILE = { width: 324, height: 756 };

test.describe("a second press while a write is in flight", () => {
  test.use({ viewport: MOBILE });

  // Set whenever a test might leave a trip behind; the cleanup is a safety net,
  // not the assertion.
  let strayTripIds;

  test.beforeEach(async ({ page }) => {
    await login(page);
    strayTripIds = [];
  });

  test.afterEach(async ({ page }) => {
    for (const id of strayTripIds) await page.request.delete(`/api/trips/${id}`);
    strayTripIds = [];
  });

  // The trip create page is the hard case for the primitive, not the easy one:
  // its Create button lives outside the form that owns the submit handler, so
  // the two share one flag through renderTripForm's returned guard. A per-button
  // fix would have left the form's own submit path open.
  test("creating a trip twice creates one trip", async ({ page }) => {
    const title = `UI suite: double-submit ${Date.now()}`;

    await gotoRoute(page, "/trips/new");
    await page.locator('.trip-form input[name="title"]').fill(title);

    const gate = await holdRoute(page, "**/api/trips");
    const create = page.locator('[data-action="create"]');

    await doubleClick(page, '[data-action="create"]');
    await gate.arrived(1);

    // The count first, because it is the bug: the second press must not have
    // reached the network at all.
    expect(gate.seen, "POST /api/trips should have been sent once").toHaveLength(1);
    // And the press is visible as work in progress.
    await expect(create).toBeDisabled();
    await expect(create).toHaveAttribute("aria-busy", "true");

    gate.release();
    await expect(page).toHaveURL(/\/trips\/[0-9a-f-]+\/locations$/);
    strayTripIds.push(page.url().match(/\/trips\/([0-9a-f-]+)\//)[1]);

    // The server's own count, not the page's: a guard that only tidied up the
    // UI would still leave two rows here.
    const trips = await (await page.request.get("/api/trips")).json();
    expect(trips.filter((t) => t.title === title), `exactly one trip named ${title}`).toHaveLength(1);
  });

  // The reported bug, as reported: Save pressed twice on a new location.
  test("saving a new location twice creates one location", async ({ page }) => {
    const tripId = await ownTrip(page, "UI suite: double-submit locations");
    strayTripIds.push(tripId);

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('.item-form input[name="title"]').fill("Held waterfall");

    const gate = await holdRoute(page, `**/api/trips/${tripId}/items`);
    const save = page.locator('[data-action="save"]');

    await doubleClick(page, '[data-action="save"]');
    await gate.arrived(1);

    expect(gate.seen, "POST .../items should have been sent once").toHaveLength(1);
    await expect(save).toBeDisabled();
    await expect(save).toHaveAttribute("aria-busy", "true");

    gate.release();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));

    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.filter((i) => i.title === "Held waterfall"), "exactly one location").toHaveLength(1);
  });

  // Why the flag lives on the guard and not on the button. This page reaches
  // one save() from three controls, and disabling the button alone would leave
  // the two Enter paths open - press Save, then hit Enter in either card while
  // it is still going, and the location is created twice again.
  test("the location editor's three save paths share one flag", async ({ page }) => {
    const tripId = await ownTrip(page, "UI suite: double-submit three doors");
    strayTripIds.push(tripId);

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('.item-form input[name="title"]').fill("Three doors");

    const gate = await holdRoute(page, `**/api/trips/${tripId}/items`);

    // The button, then Enter in Basic info, then Enter in the Location card -
    // all in one synchronous turn, so all three land while the first request
    // is still held.
    await page.evaluate(() => {
      document.querySelector('[data-action="save"]').click();
      document.querySelector(".item-form").requestSubmit();
      document.querySelector(".location-form").requestSubmit();
    });
    await gate.arrived(1);

    expect(gate.seen, "three controls, one request").toHaveLength(1);

    gate.release();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));

    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.filter((i) => i.title === "Three doors"), "exactly one location").toHaveLength(1);
  });

  // The only path the re-enable code ever runs on. On success the page
  // navigates and the button is thrown away, so a guard that never restored
  // anything would pass every test above and still leave a dead button behind
  // after one failed save.
  test("a failed write hands the button back, focus included", async ({ page }) => {
    await gotoRoute(page, "/trips/new");
    await page.locator('.trip-form input[name="title"]').fill("UI suite: double-submit failure");

    const gate = await holdRoute(page, "**/api/trips", { status: 500 });
    const create = page.locator('[data-action="create"]');

    await create.focus();
    await doubleClick(page, '[data-action="create"]');
    await gate.arrived(1);
    await expect(create).toBeDisabled();

    gate.release();

    await expect(create).toBeEnabled();
    await expect(create).not.toHaveAttribute("aria-busy", "true");
    // Disabling the pressed button drops focus to <body>; the guard puts it
    // back, so the keyboard user who pressed Enter can press it again.
    await expect(create).toBeFocused();
    await expect(page.locator(".trip-form__error")).toBeVisible();
    expect(gate.seen).toHaveLength(1);
  });
});
