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
// The count comes from the page's own in-flight fetch counter as well as from
// the gate, because the gate alone is a moment behind: see helpers/gate.js.
//
// Requests are held open (helpers/gate.js), not delayed: with a fixed delay the
// assertions would be racing the app instead of describing it.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";
import { holdRoute, doubleClick, pressAll } from "./helpers/gate.js";

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

    const inFlight = await doubleClick(page, '[data-action="create"]');
    await gate.arrived(1);

    // The count first, because it is the bug: the second press must not have
    // reached the network at all.
    expect(inFlight, "the second press must not have started a request").toBe(1);
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

    const inFlight = await doubleClick(page, '[data-action="save"]');
    await gate.arrived(1);

    expect(inFlight, "the second press must not have started a request").toBe(1);
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
    const inFlight = await pressAll(page, ['[data-action="save"]', ".item-form", ".location-form"]);
    await gate.arrived(1);

    expect(inFlight, "three controls, one request in flight").toBe(1);
    expect(gate.seen, "three controls, one request").toHaveLength(1);

    gate.release();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));

    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.filter((i) => i.title === "Three doors"), "exactly one location").toHaveLength(1);
  });

  // The overflow menus are guarded in menu.js itself, so this one case stands
  // for twelve call sites: delete a checklist or a file, change a role or a
  // visibility, duplicate a list. The ⋮ closes on the first pick, so the way to
  // fire a second one is to open it again - which is why the *trigger* is what
  // the guard disables.
  test("a menu action cannot be picked twice", async ({ page }) => {
    const tripId = await ownTrip(page, "UI suite: double-submit menu");
    strayTripIds.push(tripId);

    const checklist = await (await page.request.post(`/api/trips/${tripId}/checklists`, { data: { title: "Packing" } })).json();
    const item = await (await page.request.post(`/api/checklists/${checklist.id}/items`, { data: { text: "Passport" } })).json();

    await gotoRoute(page, `/trips/${tripId}/checklists`);

    const row = page.locator(".checklist-item", { hasText: "Passport" });
    const trigger = row.locator(".menu__trigger");
    const gate = await holdRoute(page, `**/api/checklists/${checklist.id}/items/${item.id}`, { method: "DELETE" });

    await trigger.click();
    await row.getByRole("menuitem", { name: "Remove" }).click();
    await gate.arrived(1);

    // Reopen and pick it again, in one synchronous turn so neither press can
    // wait for the first request to finish. The row is still there: the DELETE
    // it would be removed by is the one being held.
    const rowSelector = `.checklist-item[data-item-id="${item.id}"]`;
    const inFlight = await pressAll(page, [`${rowSelector} .menu__trigger`, `${rowSelector} [data-value="remove"]`]);

    expect(inFlight, "the second pick must not have started a request").toBe(1);
    expect(gate.seen, "DELETE should have been sent once").toHaveLength(1);
    // Asserted after the second attempt rather than before it, so that a run
    // against an unguarded menu reports the request count -- the actual bug --
    // instead of timing out on the busy state first.
    await expect(trigger).toBeDisabled();
    await expect(trigger).toHaveAttribute("aria-busy", "true");

    gate.release();
    await expect(row).toHaveCount(0);

    const after = await (await page.request.get(`/api/trips/${tripId}/checklists`)).json();
    expect(after.find((c) => c.id === checklist.id).items, "the list is empty, not doubly-deleted").toHaveLength(0);
  });

  // The row forms: an add-item field with a submit button beside it, which is
  // the shape eight of these conversions share. Worth its own case because
  // guardForm resolves the button itself, and because this form refocuses its
  // input after each add - the guard must not fight that.
  test("adding a checklist item twice adds one item", async ({ page }) => {
    const tripId = await ownTrip(page, "UI suite: double-submit row form");
    strayTripIds.push(tripId);

    const checklist = await (await page.request.post(`/api/trips/${tripId}/checklists`, { data: { title: "Packing" } })).json();

    await gotoRoute(page, `/trips/${tripId}/checklists`);
    await page.locator(".checklist-item-form input[name='text']").fill("Passport");

    const gate = await holdRoute(page, `**/api/checklists/${checklist.id}/items`);
    const submit = page.locator(".checklist-item-form button[type='submit']");

    const inFlight = await doubleClick(page, ".checklist-item-form button[type='submit']");
    await gate.arrived(1);

    expect(inFlight, "the second press must not have started a request").toBe(1);
    expect(gate.seen, "POST .../items should have been sent once").toHaveLength(1);
    await expect(submit).toBeDisabled();

    gate.release();
    await expect(page.locator(".checklist-item")).toHaveCount(1);

    const after = await (await page.request.get(`/api/trips/${tripId}/checklists`)).json();
    expect(after.find((c) => c.id === checklist.id).items, "one item, not two").toHaveLength(1);
  });

  // The toggles, which are the shape with the nastiest failure: two PATCHes for
  // the same box can be answered in either order, so the loser silently wins
  // the checkbox and the UI ends up disagreeing with the server. The box goes
  // disabled instead, which makes the second flip impossible rather than
  // merely dropped.
  test("a checkbox cannot be flipped into a second request", async ({ page }) => {
    const tripId = await ownTrip(page, "UI suite: double-submit toggle");
    strayTripIds.push(tripId);

    const checklist = await (await page.request.post(`/api/trips/${tripId}/checklists`, { data: { title: "Packing" } })).json();
    const item = await (await page.request.post(`/api/checklists/${checklist.id}/items`, { data: { text: "Passport" } })).json();

    await gotoRoute(page, `/trips/${tripId}/checklists`);

    const box = page.locator('.checklist-item input[type="checkbox"]');
    const gate = await holdRoute(page, `**/api/checklists/${checklist.id}/items/${item.id}`, { method: "PATCH" });

    // Ticked, then a second click on the same box in the same turn.
    const inFlight = await doubleClick(page, '.checklist-item input[type="checkbox"]');
    await gate.arrived(1);

    expect(inFlight, "the second flip must not have started a request").toBe(1);
    expect(gate.seen, "PATCH should have been sent once").toHaveLength(1);
    await expect(box).toBeDisabled();

    gate.release();
    await expect(box).toBeEnabled();
    await expect(box).toBeChecked();

    const after = await (await page.request.get(`/api/trips/${tripId}/checklists`)).json();
    expect(after.find((c) => c.id === checklist.id).items[0].checked, "the server agrees with the box").toBe(true);
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
