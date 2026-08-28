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

// A 1x1 PNG, the same fixture trip-editor.spec.js uses for a cover photo.
const PIXEL = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64"
);

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

// Pasting a Google Maps link (Stage 22 Milestone 6).
//
// The same field and the same button as the address search: what is in your
// clipboard is somebody else's idea of how to name a place, and asking the user
// to notice which kind it is would be the app's problem becoming theirs.
//
// The endpoint is intercepted for the same reason the reverse-geocoding specs
// intercept theirs -- and here there is a second reason: a full Maps URL is
// resolved by the server with no outbound request at all, but letting the suite
// paste a *short* link would mean reaching maps.app.goo.gl for real. The
// resolver's own tests own that half (internal/geocode/maplink_test.go).
test.describe("pasting a Google Maps link", () => {
  test.use({ viewport: MOBILE });

  const SHORT_LINK = "https://maps.app.goo.gl/xfB9TzpFos2N4oAW8";
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", {
      data: { title: "UI suite: map links" },
    });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  async function stubLink(page, { status = 200, body = null } = {}) {
    const calls = [];
    await page.route("**/api/geocode/link*", async (route) => {
      calls.push(route.request().url());
      await route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(
          body ?? { display_name: "Hallgrímskirkja", lat: 64.1418, lng: -21.9266 },
        ),
      });
    });
    return calls;
  }

  test("fills the coordinates and the title, and leaves the address alone", async ({ page }) => {
    const linkCalls = await stubLink(page);
    // Nothing may reach the address search: a link is not a search term, and
    // sending it to Nominatim as one finds nothing.
    const searchCalls = [];
    await page.route("**/api/geocode?*", async (route) => {
      searchCalls.push(route.request().url());
      await route.fulfill({ status: 200, contentType: "application/json", body: "[]" });
    });

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('[name="placeQuery"]').fill(SHORT_LINK);
    await page.locator('[data-action="search-place"]').click();

    await expect(page.locator('.location-form [name="lat"]')).toHaveValue("64.1418");
    await expect(page.locator('.location-form [name="lng"]')).toHaveValue("-21.9266");
    // The name the URL carries is the name of the *place*, so it goes in the
    // title -- which is in the card above this one. Putting it in the address
    // field is what the first version did, and "Brandenburg Gate" is not an
    // address.
    await expect(page.locator('.item-form [name="title"]')).toHaveValue("Hallgrímskirkja");
    // And the address is left empty rather than filled with something that is
    // not one. A Maps link carries no address (measured: the expanded page's
    // og: tags say "Google Maps" and the street address is not in the HTML at
    // all), so the honest answer is the Look up address button, one press away
    // and enabled by the coordinates this just set.
    await expect(page.locator('.location-form [name="address"]')).toHaveValue("");
    // The message names what happened, because the title it changed is off
    // screen at this width.
    await expect(page.locator(".location-search__status")).toHaveText(/Hallgrímskirkja.*used as the title/);
    // The field is emptied: the link has been consumed, and leaving it there
    // invites a second press that does the same thing again.
    await expect(page.locator('[name="placeQuery"]')).toHaveValue("");

    expect(linkCalls, "the link should have gone to the resolver").toHaveLength(1);
    expect(linkCalls[0]).toContain(encodeURIComponent(SHORT_LINK));
    expect(searchCalls, "a link must not be sent to the address search").toHaveLength(0);
  });

  test("leaves a title somebody typed alone", async ({ page }) => {
    await stubLink(page);
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    const title = page.locator('.item-form [name="title"]');
    await title.fill("The gate we meet at");
    await page.locator('[name="placeQuery"]').fill(SHORT_LINK);
    await page.locator('[data-action="search-place"]').click();

    await expect(page.locator('.location-form [name="lat"]')).toHaveValue("64.1418");
    // The coordinates are what was asked for; the name is a guess about what to
    // call the place, and it does not get to overwrite what somebody typed.
    await expect(title).toHaveValue("The gate we meet at");
    // The message says only what it did, so it does not claim a title it left
    // alone.
    await expect(page.locator(".location-search__status")).toHaveText("Coordinates taken from the link.");
  });

  test("says which way a link failed", async ({ page }) => {
    await stubLink(page, { status: 404, body: { error: "not a single place" } });
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    await page.locator('[name="placeQuery"]').fill(SHORT_LINK);
    await page.locator('[data-action="search-place"]').click();
    // 404 is "that link names no single place" -- a search results page, say --
    // and it is worth saying, because the user can go back and pick the pin.
    await expect(page.locator(".location-search__status")).toHaveText(/does not point at a single place/);
    await expect(page.locator('.location-form [name="lat"]')).toHaveValue("");

    await page.unroute("**/api/geocode/link*");
    await stubLink(page, { status: 502, body: { error: "unreachable" } });
    await page.locator('[name="placeQuery"]').fill(SHORT_LINK);
    await page.locator('[data-action="search-place"]').click();
    await expect(page.locator(".location-search__status")).toHaveText(/could not be read/);
  });

  // The regression this guards. Four of the five ways the coordinates can
  // change write the fields directly, firing no input event, so a control that
  // watches for one is wrong for most of them. Found by testing a real short
  // link by hand: the fields filled and "Look up address" stayed disabled.
  test("leaves the address lookup usable however the coordinates arrived", async ({ page }) => {
    await stubLink(page);
    await page.route("**/api/geocode?*", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify([{ display_name: "Reykjavík, Iceland", lat: 64.1466, lng: -21.9426 }]),
      });
    });

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    const lookup = page.locator('[data-action="lookup-address"]');
    await expect(lookup).toBeDisabled();

    // A resolved map link.
    await page.locator('[name="placeQuery"]').fill(SHORT_LINK);
    await page.locator('[data-action="search-place"]').click();
    await expect(page.locator('.location-form [name="lat"]')).toHaveValue("64.1418");
    await expect(lookup, "a resolved link must enable the lookup").toBeEnabled();

    // A chosen address-search result. This half was broken before the map-link
    // work existed -- the button shipped watching two of the five writers.
    await page.locator('.location-form [name="lat"]').fill("");
    await page.locator('.location-form [name="lng"]').fill("");
    await expect(lookup).toBeDisabled();
    await page.locator('[name="placeQuery"]').fill("Reykjavik");
    await page.locator('[data-action="search-place"]').click();
    await page.locator(".location-search__result").first().click();
    await expect(page.locator('.location-form [name="lat"]')).toHaveValue("64.1466");
    await expect(lookup, "a chosen search result must enable the lookup").toBeEnabled();
  });

  test("still searches for something that is not a link", async ({ page }) => {
    const linkCalls = await stubLink(page);
    const searchCalls = [];
    await page.route("**/api/geocode?*", async (route) => {
      searchCalls.push(route.request().url());
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify([{ display_name: "Reykjavík, Iceland", lat: 64.1466, lng: -21.9426 }]),
      });
    });

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('[name="placeQuery"]').fill("Reykjavik");
    await page.locator('[data-action="search-place"]').click();

    // The ordinary path is untouched: a result list to choose from.
    await expect(page.locator(".location-search__result")).toHaveCount(1);
    expect(searchCalls).toHaveLength(1);
    expect(linkCalls, "a search term must not be sent to the link resolver").toHaveLength(0);
  });
});

// Creating a location is one request, and either all of it happens or none of
// it does (Stage 23 Milestones 3-4).
test.describe("creating a location is atomic", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: atomic create" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  // The bug this replaced, and the reason the count is what gets asserted.
  //
  // Before Stage 23 Milestones 3-4, Create wrote the location first and then
  // attached the cover. A cover the server could not fetch failed *after* the
  // location existed, and the page never adopted the item it had just made --
  // so it was still in create mode, and the obvious thing to do next (fix the
  // picture, press Create again) posted a second location. Once per retry.
  //
  // So the assertion here is not "an error is shown". It is that the trip has
  // no locations after the failure, and exactly one after the retry.
  test("a cover the server cannot fetch creates no location at all, and the retry creates exactly one", async ({
    page,
  }) => {
    const count = async () => {
      const res = await page.request.get(`/api/trips/${tripId}/items`);
      expect(res.status()).toBe(200);
      return (await res.json()).length;
    };

    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('.item-form input[name="title"]').fill("Hotel Ranga");
    await page.locator('.item-form select[name="category"]').selectOption("stay");
    await page.locator('.item-form input[name="type"]').fill("hotel");

    // Port 1 is not listening, so the server's fetch fails at dial without
    // involving anybody else's host. The browser cannot load the preview
    // either, which is realistic -- but staging does not depend on that, so
    // the URL is still carried into the create.
    await page.locator('.image-field input[name="url"]').fill("http://127.0.0.1:1/cover.png");
    await page.locator(".image-field__url-form button[type=submit]").click();

    expect(await count(), "nothing should exist before Create is pressed").toBe(0);

    await page.locator('[data-action="save"]').click();

    // The failure is reported where the form's other failures are reported.
    await expect(page.locator(".item-form__error")).toBeVisible();

    // The whole point. Before this change it was 1, and 2 after the retry.
    expect(await count(), "a failed create must leave no location behind").toBe(0);

    // The page must still be in create mode -- same button, no delete card --
    // because that is the truth: nothing was created.
    await expect(page.locator('[data-action="save"]')).toHaveText("Create location");
    await expect(page.locator('[data-action="delete"]')).toHaveCount(0);

    // Fix the picture and try again, which is exactly what a person would do.
    await page.locator('.image-field input[type="file"]').setInputFiles({
      name: "cover.png",
      mimeType: "image/png",
      buffer: PIXEL,
    });
    await page.locator('[data-action="save"]').click();

    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[^/]+$`));
    expect(await count(), "the retry must create one location, not a second one").toBe(1);

    // And the cover landed with it, in the same request.
    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items[0].image_url, "the cover must have ridden along with the create").toBeTruthy();
  });

  // The other half: everything a create can carry, carried in one request.
  test("create sends the cover and the files in the same request as the item", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('.item-form input[name="title"]').fill("Hotel Ranga");
    await page.locator('.item-form select[name="category"]').selectOption("stay");
    await page.locator('.item-form input[name="type"]').fill("hotel");

    await page.locator('.image-field input[type="file"]').setInputFiles({
      name: "cover.png",
      mimeType: "image/png",
      buffer: PIXEL,
    });
    await page.locator(".file-drop input[type=file]").setInputFiles({
      name: "booking.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("confirmation 12345"),
    });

    // One POST to the items collection, and no separate media or file writes:
    // that is what "one request" means, and it is checkable from here.
    const posts = [];
    page.on("request", (req) => {
      if (req.method() === "POST" && req.url().includes("/api/")) posts.push(new URL(req.url()).pathname);
    });

    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[^/]+$`));

    expect(posts, "the create must be a single POST carrying everything").toEqual([`/api/trips/${tripId}/items`]);

    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items).toHaveLength(1);
    expect(items[0].image_url, "the cover landed").toBeTruthy();

    const files = await (await page.request.get(`/api/items/${items[0].id}/files`)).json();
    expect(files.map((f) => f.filename), "the file landed").toEqual(["booking.txt"]);
  });
});
