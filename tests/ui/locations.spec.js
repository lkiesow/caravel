// The location editor, end to end: create, edit, delete.
//
// Owns its trip (files.spec.js's shape, Stage 11 Milestone 5) so the seeded
// scenarios every other spec reads are never touched.
//
// What this does *not* cover, deliberately: the coordinate picker. map.spec.js
// already drives it properly - a real DOM click inside the map's shadow root,
// then a Save, then a read back through the API to prove the stored point is
// the one the fields showed. Repeating that here would buy a second copy of the
// same evidence. So coordinates are typed, which covers the other half: that
// lat, lng and the address ride along with the rest of the form and come back
// out on the view page.
//
// What is new is everything else the editor writes - title, category, type,
// notes, links and dates - none of which had an assertion before, in either
// direction: no spec had ever pressed Save on this form and then looked at what
// came back.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

test.describe("the location editor, end to end", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: locations spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    // Cascades to the locations, their links and their dates. Runs even after a
    // failure, so a red run leaves nothing behind for the next one.
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("creates a location with links and dates, and the view page shows them", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    // Create mode says so on the button: "Save" would mean the page had
    // decided it was editing something.
    const save = page.locator('[data-action="save"]');
    await expect(save).toHaveText("Create location");
    // And offers no way to delete a thing that does not exist yet.
    await expect(page.locator('[data-action="delete"]')).toHaveCount(0);

    await page.locator('.item-form input[name="title"]').fill("Hotel Ranga");
    await page.locator('.item-form select[name="category"]').selectOption("stay");
    await page.locator('.item-form input[name="type"]').fill("hotel");
    await page.locator('.item-form textarea[name="notes"]').fill("Check in **after 15:00**.");

    await page.locator('.location-form input[name="lat"]').fill("63.8333");
    await page.locator('.location-form input[name="lng"]').fill("-20.3167");
    await page.locator('.location-form input[name="address"]').fill("Sudurlandsvegur, 851 Hella");

    // Links and dates are staged in memory by their own little forms and only
    // written by the Save below - so the list growing here is not yet evidence
    // that anything persisted. That is what the reload at the end is for.
    const links = page.locator(".link-list li");
    await expect(page.locator(".link-list li.empty")).toBeVisible();

    await page.locator('.link-form input[name="url"]').fill("https://example.com/booking");
    await page.locator('.link-form input[name="label"]').fill("Booking");
    await page.locator('.link-form button[type="submit"]').click();
    await page.locator('.link-form input[name="url"]').fill("https://example.com/wrong");
    await page.locator('.link-form button[type="submit"]').click();
    await expect(links).toHaveCount(2);

    // Remove the second one again. A staged row that cannot be taken back off
    // the list is the failure worth catching before Save, not after.
    await page.locator('.link-list button[data-action="delete-link"]').nth(1).click();
    await expect(links).toHaveCount(1);
    await expect(links.first()).toContainText("Booking");

    const dates = page.locator(".date-list li");
    await expect(page.locator(".date-list li.empty")).toBeVisible();
    await page.locator('.date-form input[name="startDate"]').fill("2026-08-20");
    await page.locator('.date-form input[name="endDate"]').fill("2026-08-22");
    await page.locator('.date-form input[name="label"]').fill("Two nights");
    await page.locator('.date-form button[type="submit"]').click();
    await expect(dates).toHaveCount(1);

    await save.click();

    // A successful create lands on the new location's own view page, which is
    // also how the spec learns its id.
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));

    await expect(page.locator("h1")).toHaveText("Hotel Ranga");
    await expect(page.locator(".category-label")).toHaveText("Stay");
    await expect(page.locator(".type-label")).toHaveText("hotel");
    // Server-rendered markdown, so the emphasis is a real element rather than
    // asterisks printed on the page.
    await expect(page.locator(".location-view__notes strong")).toHaveText("after 15:00");
    await expect(page.locator(".location-view__address")).toHaveText("Sudurlandsvegur, 851 Hella");
    await expect(page.locator(".link-list li")).toHaveCount(1);
    await expect(page.locator(".link-list a")).toHaveAttribute("href", "https://example.com/booking");
    await expect(page.locator(".date-list li")).toContainText("Two nights");

    // Everything above could still be a page that never asked the server. This
    // is the line that says it reached the database.
    await page.reload();
    await expect(page.locator("h1")).toHaveText("Hotel Ranga");
    await expect(page.locator(".link-list a")).toHaveAttribute("href", "https://example.com/booking");
    await expect(page.locator(".date-list li")).toContainText("Two nights");

    // And the list the trip shows now has it, under the title the form gave it.
    await gotoRoute(page, `/trips/${tripId}/locations`);
    await expect(page.locator("item-card")).toHaveCount(1);
    await expect(page.locator("item-card")).toHaveAttribute("title", "Hotel Ranga");
  });

  test("edits an existing location, and the form opens on what is already there", async ({ page }) => {
    const created = await page.request.post(`/api/trips/${tripId}/items`, {
      data: {
        title: "Skogafoss",
        category: "site",
        type: "waterfall",
        notes: "Bring a raincoat.",
        links: [{ url: "https://example.com/skogafoss", label: "Info" }],
      },
    });
    expect(created.status(), "create the location to edit").toBe(201);
    const itemId = (await created.json()).id;

    await gotoRoute(page, `/trips/${tripId}/locations/${itemId}/edit`);

    // Edit mode: the heading names the thing, the button says Save, and the
    // delete card exists. All three are the create/edit branch being taken.
    await expect(page.locator("h1")).toHaveText("Edit Skogafoss");
    await expect(page.locator('[data-action="save"]')).toHaveText("Save");
    await expect(page.locator('[data-action="delete"]')).toBeVisible();

    // The form opens on the stored values rather than empty - an editor that
    // silently blanked a field would erase it on the next Save.
    await expect(page.locator('.item-form input[name="title"]')).toHaveValue("Skogafoss");
    await expect(page.locator('.item-form select[name="category"]')).toHaveValue("site");
    await expect(page.locator('.item-form input[name="type"]')).toHaveValue("waterfall");
    await expect(page.locator('.item-form textarea[name="notes"]')).toHaveValue("Bring a raincoat.");
    await expect(page.locator(".link-list li")).toHaveCount(1);

    await page.locator('.item-form input[name="title"]').fill("Skogafoss waterfall");
    await page.locator('.item-form select[name="category"]').selectOption("transport");
    await page.locator('.item-form textarea[name="notes"]').fill("");
    await page.locator('.link-list button[data-action="delete-link"]').first().click();

    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(`/trips/${tripId}/locations/${itemId}`);

    await expect(page.locator("h1")).toHaveText("Skogafoss waterfall");
    await expect(page.locator(".category-label")).toHaveText("Transport");
    // Emptied, not merely re-rendered: the sections the view page has nothing
    // to say about are omitted rather than left standing empty.
    await expect(page.locator(".location-view__notes")).toHaveCount(0);
    await expect(page.locator(".link-list")).toHaveCount(0);

    const detail = await (await page.request.get(`/api/items/${itemId}`)).json();
    expect(detail.title).toBe("Skogafoss waterfall");
    expect(detail.category).toBe("transport");
    expect(detail.links, "clearing the last link should clear it server-side too").toEqual([]);
  });

  test("deletes a location, but only once the confirmation is accepted", async ({ page }) => {
    const created = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Delete me", category: "site" },
    });
    expect(created.status(), "create the location to delete").toBe(201);
    const itemId = (await created.json()).id;

    await gotoRoute(page, `/trips/${tripId}/locations/${itemId}/edit`);

    // Cancel first. A destructive action that fires anyway is the bug this
    // half of the test exists for.
    await page.locator('[data-action="delete"]').click();
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();
    await expect(page).toHaveURL(`/trips/${tripId}/locations/${itemId}/edit`);
    expect((await page.request.get(`/api/items/${itemId}`)).status()).toBe(200);

    await page.locator('[data-action="delete"]').click();
    await page.locator(".dialog__actions button", { hasText: "Delete" }).click();

    // Back to the trip, because the page that was being edited is gone.
    // The trip detail page canonicalises /trips/{id} to its first tab, so this
    // is the locations list with nothing left on it.
    await expect(page).toHaveURL(`/trips/${tripId}/locations`);
    await expect(page.locator(".items-empty:not(.items-empty--no-matches)")).toBeVisible();
    expect(
      (await page.request.get(`/api/items/${itemId}`)).status(),
      "the location should be gone, not merely hidden"
    ).toBe(404);
  });
});
