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

// Reverse geocoding: a point becomes an address you accept (Stage 22
// Milestone 5).
//
// Every test here intercepts **Caravel's own** /api/geocode/reverse rather than
// letting the request through. That is not a convenience: `with_server.sh`
// stubs the LLM and the search backend but leaves CARAVEL_GEOCODER_URL at its
// default, which is OpenStreetMap's public Nominatim (see todo.md, "The UI
// suite reaches the real Nominatim"). Asserting this end to end for real would
// widen that dependency, so the client is driven against a canned answer and
// the server half is owned by Go tests -- internal/geocode/geocode_test.go for
// the URL derivation and the mapping, internal/httpapi/geocode_test.go for the
// statuses.
test.describe("looking up an address for a point", () => {
  test.use({ viewport: MOBILE });

  const ADDRESS = "Vonarstræti 4, 101 Reykjavík, Ísland";
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", {
      data: { title: "UI suite: reverse geocoding" },
    });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  // Answers the lookup with `body`, and records every call so a test can assert
  // that nothing left the building when it should not have.
  async function stubReverse(page, { status = 200, body = null } = {}) {
    const calls = [];
    await page.route("**/api/geocode/reverse*", async (route) => {
      calls.push(route.request().url());
      await route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(
          body ?? { display_name: ADDRESS, lat: 64.1466, lng: -21.9426 },
        ),
      });
    });
    return calls;
  }

  test("offers the address and fills the field only when accepted", async ({ page }) => {
    const calls = await stubReverse(page);
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    const button = page.locator('[data-action="lookup-address"]');
    const offer = page.locator(".location-reverse__offer");
    const address = page.locator('.location-form [name="address"]');

    // Nothing to look up yet: visible, so it reads as something that will work
    // once there is a point, but disabled until there is one.
    await expect(button).toBeVisible();
    await expect(button).toBeDisabled();
    await expect(offer).toBeHidden();

    await page.locator('.location-form [name="lat"]').fill("64.1466");
    await page.locator('.location-form [name="lng"]').fill("-21.9426");
    await expect(button).toBeEnabled();
    // Filling coordinates must not fire a lookup by itself: every query costs a
    // volunteer-run service a request, and placing a pin takes several goes.
    expect(calls, "no lookup before the button is pressed").toHaveLength(0);

    await button.click();
    await expect(offer).toBeVisible();
    await expect(page.locator(".location-reverse__value")).toHaveText(ADDRESS);
    // Offered, not applied. A hand-written address is often better than a
    // geocoder's, so nothing is overwritten until Accept is pressed.
    await expect(address).toHaveValue("");
    expect(calls).toHaveLength(1);
    expect(calls[0]).toContain("lat=64.1466");
    expect(calls[0]).toContain("lng=-21.9426");

    await page.locator('[data-action="accept-address"]').click();
    await expect(address).toHaveValue(ADDRESS);
    await expect(offer).toBeHidden();

    // And it is a real value on the form, not decoration: it saves.
    await page.locator('[name="title"]').fill("Harpa");
    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));
    // Read from the item's own endpoint: the trip listing does not carry the
    // address, it lives on the location detail (see itemLocationResponse).
    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items).toHaveLength(1);
    const stored = await (await page.request.get(`/api/items/${items[0].id}`)).json();
    expect(stored.location?.address).toBe(ADDRESS);
  });

  test("drops a stale offer when the point moves", async ({ page }) => {
    await stubReverse(page);
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    await page.locator('.location-form [name="lat"]').fill("64.1466");
    await page.locator('.location-form [name="lng"]').fill("-21.9426");
    await page.locator('[data-action="lookup-address"]').click();
    await expect(page.locator(".location-reverse__offer")).toBeVisible();

    // The offered address belongs to the old point. Accepting it after moving
    // the pin would file an address for somewhere else entirely.
    await page.locator('.location-form [name="lat"]').fill("48.8584");
    await expect(page.locator(".location-reverse__offer")).toBeHidden();
    await expect(page.locator('[data-action="accept-address"]')).toBeHidden();
  });

  test("says so when there is no address there, and when the service is down", async ({ page }) => {
    await stubReverse(page, { status: 404, body: { error: "no address found for that location" } });
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    await page.locator('.location-form [name="lat"]').fill("0");
    await page.locator('.location-form [name="lng"]').fill("0");
    await page.locator('[data-action="lookup-address"]').click();

    // 404 is an answer -- the middle of an ocean has no address -- and reads
    // differently from the service being unreachable.
    const status = page.locator(".location-reverse__status");
    await expect(status).toHaveText("No address found for this point.");
    await expect(page.locator(".location-reverse__offer")).toBeHidden();

    await page.unroute("**/api/geocode/reverse*");
    await stubReverse(page, { status: 502, body: { error: "unreachable" } });
    await page.locator('[data-action="lookup-address"]').click();
    await expect(status).toHaveText(/unavailable right now/);
  });

  test("is absent entirely when the server cannot do it", async ({ page }) => {
    // The capability is faked off rather than a second server being started,
    // the way assist.spec.js does it. reverse_geocoding is its own flag because
    // an instance can have working address search and no reverse endpoint.
    await page.route("**/api/auth/me", async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      await route.fulfill({
        response,
        json: {
          ...body,
          capabilities: { ...body.capabilities, reverse_geocoding: false },
        },
      });
    });

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    // The address search above it is a separate capability and stays.
    await expect(page.locator(".location-search")).toBeVisible();
    await expect(page.locator(".location-reverse")).toBeHidden();
    await expect(page.locator('[data-action="lookup-address"]')).toBeHidden();
  });
});
