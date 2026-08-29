// Creating a trip, and editing one from its Settings tab.
//
// trips.spec.js is deliberately read-only - its assertions turn on the seeded
// trips' count and order, so it cannot create anything. This is the spec that
// writes, and it owns its trips: the one it creates through the UI is deleted
// through the UI at the end of the same test, which is how the delete flow gets
// covered without a second fixture.
//
// The two halves are genuinely different code. `/trips/new` renders
// trip-editor-page.js with the submit button *outside* the form and stages a
// cover photo in memory until the trip exists; the Settings tab renders the
// same trip-form.js with its own actions row and writes immediately. A spec
// that only drove one of them would leave the other's wiring unasserted.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

// A 1x1 PNG, enough for the upload path to have something real to store.
const PIXEL = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64"
);

test.describe("the trip editor, end to end", () => {
  test.use({ viewport: MOBILE });

  // Only set when a test creates a trip the UI might not finish deleting; the
  // cleanup is a safety net, not the assertion.
  let strayTripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    strayTripId = null;
  });

  test.afterEach(async ({ page }) => {
    if (strayTripId) await page.request.delete(`/api/trips/${strayTripId}`);
    strayTripId = null;
  });

  test("creates a trip from the form, then deletes it from its settings", async ({ page }) => {
    await gotoRoute(page, "/trips/new");

    const form = page.locator(".trip-form");
    const error = page.locator(".trip-form__error");
    await expect(error).toBeHidden();

    // The end-before-start guard is client-side, so it must fire without the
    // server ever being asked. Checked first, while the title is still blank -
    // if the two validations were reordered the message would be about the
    // title instead.
    await form.locator('input[name="startDate"]').fill("2026-09-10");
    await form.locator('input[name="endDate"]').fill("2026-09-01");
    await page.locator('[data-action="create"]').click();
    await expect(error).toHaveText("The end date can't be before the start date.");

    await form.locator('input[name="endDate"]').fill("2026-09-20");
    await form.locator('input[name="title"]').fill("UI suite: trip editor spec");
    await form.locator('input[name="subtitle"]').fill("Two weeks of nothing");
    await form.locator('select[name="currency"]').selectOption("ISK");

    // The cover photo is staged, not uploaded: on this page there is no trip to
    // attach it to yet. So the preview appearing is the whole of the evidence
    // available before Save.
    await page.locator('.image-field input[type="file"]').setInputFiles({
      name: "cover.png",
      mimeType: "image/png",
      buffer: PIXEL,
    });
    await expect(page.locator(".image-field__preview")).toBeVisible();

    await page.locator('[data-action="create"]').click();

    // A successful create lands on the trip, which canonicalises itself to its
    // first tab.
    await expect(page).toHaveURL(/\/trips\/[0-9a-f-]+\/locations$/);
    const tripId = page.url().match(/\/trips\/([0-9a-f-]+)\//)[1];
    strayTripId = tripId;

    // The header is rendered from what the server returned, so this is the
    // round trip and not the form echoing itself back.
    await expect(page.locator(".page__header h1")).toHaveText("UI suite: trip editor spec");
    await expect(page.locator(".trip-summary__subtitle")).toHaveText("Two weeks of nothing");
    await expect(page.locator(".trip-summary__dates")).toContainText("2026");
    // The staged photo rode along in the create request itself.
    await expect(page.locator(".trip-detail__cover")).toBeVisible();

    const created = await (await page.request.get(`/api/trips/${tripId}`)).json();
    expect(created.subtitle).toBe("Two weeks of nothing");
    expect(created.currency, "the currency chosen on the form").toBe("ISK");
    expect(created.start_date).toBe("2026-09-10");
    expect(created.preview_image_id, "the staged cover should have been attached").toBeTruthy();

    // Delete it again, from the Settings tab, behind the confirmation. Cancel
    // first: a destructive action that fires anyway is the bug worth catching.
    await gotoRoute(page, `/trips/${tripId}/settings`);
    await page.locator('[data-action="delete"]').click();
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();
    await expect(page).toHaveURL(`/trips/${tripId}/settings`);

    await page.locator('[data-action="delete"]').click();
    await page.locator(".dialog__actions button", { hasText: "Delete" }).click();

    await expect(page).toHaveURL("/trips");
    expect(
      (await page.request.get(`/api/trips/${tripId}`)).status(),
      "the trip should be gone, not merely hidden"
    ).toBe(404);
    strayTripId = null;
  });

  test("edits a trip from its settings tab, and the header follows", async ({ page }) => {
    const res = await page.request.post("/api/trips", {
      data: { title: "UI suite: settings spec", subtitle: "before", start_date: "2026-05-01", end_date: "2026-05-08" },
    });
    expect(res.status(), "create the spec's own trip").toBe(201);
    const tripId = (await res.json()).id;
    strayTripId = tripId;

    await gotoRoute(page, `/trips/${tripId}/settings`);
    const form = page.locator(".trip-form");

    // Opens on the stored values. A settings form that rendered blank would
    // erase the subtitle and both dates on the next Save.
    await expect(form.locator('input[name="title"]')).toHaveValue("UI suite: settings spec");
    await expect(form.locator('input[name="subtitle"]')).toHaveValue("before");
    await expect(form.locator('input[name="startDate"]')).toHaveValue("2026-05-01");
    await expect(form.locator('input[name="endDate"]')).toHaveValue("2026-05-08");

    // Cancel discards rather than saves: it re-renders from the last saved trip.
    await form.locator('input[name="title"]').fill("Typed then abandoned");
    await form.locator('.trip-form__actions [data-action="cancel"]').click();
    await expect(page.locator('.trip-form input[name="title"]')).toHaveValue("UI suite: settings spec");

    await form.locator('input[name="title"]').fill("Renamed in settings");
    await form.locator('input[name="subtitle"]').fill("after");
    await form.locator('input[name="endDate"]').fill("2026-05-15");
    await form.locator('.trip-form__actions button[type="submit"]').click();

    // No navigation - the whole detail page re-renders around the saved trip,
    // so the header is where the save becomes visible.
    await expect(page.locator(".page__header h1")).toHaveText("Renamed in settings");
    await expect(page.locator(".trip-summary__subtitle")).toHaveText("after");

    await page.reload();
    await expect(page.locator(".page__header h1")).toHaveText("Renamed in settings");

    const updated = await (await page.request.get(`/api/trips/${tripId}`)).json();
    expect(updated.title).toBe("Renamed in settings");
    expect(updated.subtitle).toBe("after");
    expect(updated.end_date).toBe("2026-05-15");
  });

  // The point of Stage 24 Milestone 5-6: the cover goes in the same request as
  // the trip, so a cover the server cannot fetch creates nothing at all.
  // Before that the trip was created first and the failure arrived afterwards,
  // in a dialog, having already left a trip behind with no picture.
  test("a cover URL the server cannot fetch creates no trip", async ({ page }) => {
    // Keyed on this title rather than on a count: the suite runs fully
    // parallel, so sibling tests create and delete trips while this one runs.
    const title = "UI suite: should not exist";

    await gotoRoute(page, "/trips/new");
    const form = page.locator(".trip-form");
    await form.locator('input[name="title"]').fill(title);

    // Port 1 on loopback: nothing is listening, so the server's fetch fails.
    // The browser cannot preview it either, which is the pre-existing
    // client-side warning; the create is what this test is about.
    await page.locator('.image-field__url-form input[name="url"]').fill("http://127.0.0.1:1/cover.png");
    await page.locator(".image-field__url-form button[type=submit]").click();

    await page.locator('[data-action="create"]').click();

    // The error lands on the form, and the page stays put rather than
    // navigating to a trip that should not exist.
    await expect(page.locator(".trip-form__error")).toBeVisible();
    await expect(page).toHaveURL(/\/trips\/new$/);

    const trips = await (await page.request.get("/api/trips")).json();
    expect(
      trips.filter((t) => t.title === title),
      "a failed cover must not leave a trip behind",
    ).toEqual([]);
  });

  test("refuses a trip with no title, and says so", async ({ page }) => {
    await gotoRoute(page, "/trips/new");

    // The form is novalidate, so this reaches the server and the message is the
    // API's own words rendered into the form's error line.
    await page.locator('.trip-form input[name="subtitle"]').fill("no title though");
    await page.locator('[data-action="create"]').click();

    await expect(page.locator(".trip-form__error")).toBeVisible();
    await expect(page).toHaveURL("/trips/new");
  });
});
