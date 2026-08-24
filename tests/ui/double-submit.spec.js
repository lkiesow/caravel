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
