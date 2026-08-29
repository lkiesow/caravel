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
    await page.locator('.date-form button[type="submit"]').click();
    await expect(dates).toHaveCount(1);
    // Formatted, not the ISO strings that were typed in.
    await expect(dates.first()).toContainText("Aug 20");

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
    await expect(page.locator(".date-list li")).toContainText("Aug 20");

    // Everything above could still be a page that never asked the server. This
    // is the line that says it reached the database.
    await page.reload();
    await expect(page.locator("h1")).toHaveText("Hotel Ranga");
    await expect(page.locator(".link-list a")).toHaveAttribute("href", "https://example.com/booking");
    await expect(page.locator(".date-list li")).toContainText("Aug 20");

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

  // The reason Stage 25 exists. Dates set on a location used to be stored
  // beside it and shown on its own page, and the itinerary knew nothing about
  // them - so a stay of 20-22 August left those three days empty. They are now
  // the same fact seen from two sides.
  test("dates set on a location put it on those itinerary days", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations/new`);

    await page.locator('.item-form input[name="title"]').fill("Hotel Ranga");
    await page.locator('.item-form select[name="category"]').selectOption("stay");
    await page.locator('.date-form input[name="startDate"]').fill("2026-08-20");
    await page.locator('.date-form input[name="endDate"]').fill("2026-08-22");
    await page.locator('.date-form button[type="submit"]').click();
    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));

    // Read through the API rather than the tab, so this asserts what was
    // stored and not merely what one screen chose to draw.
    const itinerary = await (await page.request.get(`/api/trips/${tripId}/itinerary`)).json();
    const onIt = itinerary
      .filter((d) => d.entries.some((e) => e.item_title === "Hotel Ranga"))
      .map((d) => d.date);
    // Inclusive of both ends: checking out on the 22nd still means being there
    // on the 22nd.
    expect(onIt).toEqual(["2026-08-20", "2026-08-21", "2026-08-22"]);

    // And the itinerary tab really shows it, three days running.
    await gotoRoute(page, `/trips/${tripId}/itinerary`);
    await expect(page.locator(".itinerary-day", { hasText: "Hotel Ranga" })).toHaveCount(3);
  });

  // The hazard the datesDirty flag exists for. Sending the dates asserts the
  // location complete set of itinerary days, so a save that merely renamed it
  // would roll the itinerary back to whatever the editor happened to load -
  // silently undoing a day somebody else added in the meantime.
  //
  // Resending an unchanged set is harmless, because the reconcile is a diff and
  // does nothing when the sets match. The damage needs the itinerary to move
  // under an open editor, which is what this test arranges.
  test("renaming a location does not undo an itinerary change made while it was open", async ({ page }) => {
    const created = await page.request.post(`/api/trips/${tripId}/items`, {
      data: {
        title: "Hotel Ranga",
        category: "stay",
        type: "hotel",
        dates: [{ start_date: "2026-08-20", end_date: "2026-08-21" }],
      },
    });
    expect(created.status(), "create the location with dates").toBe(201);
    const itemId = (await created.json()).id;

    // Open the editor, which reads the two days it currently has.
    await gotoRoute(page, `/trips/${tripId}/locations/${itemId}/edit`);
    await expect(page.locator(".date-list li")).toHaveCount(1);

    // Someone else extends the stay by a day, through the itinerary.
    const day = await page.request.put(`/api/trips/${tripId}/itinerary/days/2026-08-22`, {
      data: { notes: null },
    });
    expect(day.status()).toBe(200);
    const added = await page.request.post(`/api/itinerary/days/${(await day.json()).id}/entries`, {
      data: { item_id: itemId, note: "late checkout" },
    });
    expect(added.status()).toBe(201);

    // The open editor still believes in two days. Rename, touching nothing else.
    await page.locator('.item-form input[name="title"]').fill("Hotel Ranga Reykjavik");
    await page.locator('[data-action="save"]').click();
    await expect(page.locator("h1")).toHaveText("Hotel Ranga Reykjavik");

    const itinerary = await (await page.request.get(`/api/trips/${tripId}/itinerary`)).json();
    const third = itinerary.find((d) => d.date === "2026-08-22");
    expect(third, "the day added while the editor was open must survive").toBeTruthy();
    expect(third.entries.map((e) => e.item_id)).toEqual([itemId]);
    expect(third.entries[0].note).toBe("late checkout");

    // And the location now reports all three days, as one range.
    const item = await (await page.request.get(`/api/items/${itemId}`)).json();
    expect(item.dates).toEqual([{ start_date: "2026-08-20", end_date: "2026-08-22" }]);
  });

  // Tags, added in Stage 26 Milestone 2. The interesting cases are not "a chip
  // appears" but the three ways this field could quietly lose what was typed:
  // Enter reaching the form and saving the page instead of adding the tag, a
  // typed-but-uncommitted tag vanishing on Save, and the trip vocabulary not
  // reaching the second location that would use it.
  test("tags: committing, correcting, suggesting, and surviving a reload", async ({ page }) => {
    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('.item-form input[name="title"]').fill("Hallgrimskirkja");

    const input = page.locator(".tag-field__input");
    const chips = page.locator(".tag-field__chip");

    // Enter commits a tag. It must NOT submit the form -- the form treats Enter
    // in any single-line field as Save, so this only works because the field
    // stops the event once it has consumed it. Staying on /new is the assertion.
    await input.fill("Reykjavik");
    await input.press("Enter");
    await expect(chips).toHaveCount(1);
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/new$`));

    // A comma is a separator too, so a pasted list becomes several chips and
    // the fragment after the last comma stays in the box.
    await input.fill("church, landmark, ico");
    await expect(chips).toHaveCount(3);
    await expect(input).toHaveValue(" ico");

    // Case-insensitive within one location: the server would fold these
    // together, so a chip that looked accepted here would vanish on save.
    await input.fill("REYKJAVIK");
    await input.press("Enter");
    await expect(chips).toHaveCount(3);

    // Backspace on an empty box removes the last chip.
    await input.fill("");
    await input.press("Backspace");
    await expect(chips).toHaveCount(2);

    // Each remove button names its own tag rather than being one of a row of
    // identical "Remove" buttons.
    const removeReykjavik = page.getByRole("button", { name: "Remove tag Reykjavik" });
    await expect(removeReykjavik).toBeVisible();

    // And it meets the tap-target guideline. routes.spec.js sweeps this route
    // but cannot catch this one: no seeded location carries a tag, so no chip
    // -- and no remove button -- exists while it runs. Measured here instead,
    // because the button is icon-only and the blanket rule in base.css gives
    // such a button its height but not its width (it measured 22x44).
    const box = await removeReykjavik.boundingBox();
    expect(Math.round(box.width), "remove-tag button width").toBeGreaterThanOrEqual(44);
    expect(Math.round(box.height), "remove-tag button height").toBeGreaterThanOrEqual(44);

    // A tag typed but never committed must still be saved: losing it silently
    // is the worst outcome this field has available.
    await input.fill("uncommitted");
    await page.locator('[data-action="save"]').click();
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/[0-9a-f-]+$`));

    const viewChips = page.locator(".location-view__tags .tag-chip");
    await expect(viewChips).toHaveText(["Reykjavik", "church", "uncommitted"]);

    // ...and it reached the database, not just the page.
    await page.reload();
    await expect(page.locator(".location-view__tags .tag-chip")).toHaveText([
      "Reykjavik",
      "church",
      "uncommitted",
    ]);

    // The card in the list carries them too. The custom element keeps them in
    // its shadow root, so this reads through it.
    await gotoRoute(page, `/trips/${tripId}/locations`);
    const cardTags = await page.locator("item-card").first().evaluate((el) =>
      [...el.shadowRoot.querySelectorAll(".tag")].map((t) => t.textContent)
    );
    expect(cardTags).toEqual(["Reykjavik", "church", "uncommitted"]);

    // A second location is offered the first one's vocabulary. This is the
    // whole defence against Museum and museum both existing, since the server
    // deduplicates within a location but not across the trip.
    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.locator('.item-form input[name="title"]').fill("Second");
    await page.locator(".tag-field__input").pressSequentially("rey");
    const options = page.locator(".tag-field .suggest__list [role=option]");
    await expect(options).toHaveText(["Reykjavik"]);

    // Picking with the keyboard commits the suggestion and, again, does not
    // save the page.
    await page.locator(".tag-field__input").press("ArrowDown");
    await page.locator(".tag-field__input").press("Enter");
    await expect(page.locator(".tag-field__chip")).toHaveText(["Reykjavik"]);
    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/new$`));
  });

  // Stage 26 Milestone 3: the locations list carries the itinerary dates, so a
  // card can say when the location is without the tab asking per card.
  test("dates reach the locations list and show on the card", async ({ page }) => {
    const mk = async (title, dates) =>
      (
        await page.request.post(`/api/trips/${tripId}/items`, {
          data: { title, category: "site", type: "", dates },
        })
      ).json();

    await mk("Three days running", [{ start_date: "2026-09-05", end_date: "2026-09-07" }]);
    // Two separate stretches: the card shows the first and counts the rest.
    await mk("Twice", [
      { start_date: "2026-09-05", end_date: "2026-09-06" },
      { start_date: "2026-09-20", end_date: "2026-09-20" },
    ]);
    await mk("Unscheduled", []);

    // One request for the whole list. If dates were fetched per card this
    // would be four, which is the regression the Go test also guards.
    const itemRequests = [];
    page.on("request", (req) => {
      const u = new URL(req.url());
      if (u.pathname.startsWith("/api/") && req.method() === "GET") itemRequests.push(u.pathname);
    });

    await gotoRoute(page, `/trips/${tripId}/locations`);
    await expect(page.locator("item-card")).toHaveCount(3);

    const read = async (title) =>
      page
        .locator(`item-card[title="${title}"]`)
        .evaluate((el) => el.shadowRoot.querySelector(".dates")?.textContent ?? null);

    // Formatted, not the ISO strings, and collapsed into one range.
    expect(await read("Three days running")).toContain("5");
    expect(await read("Three days running")).toContain("7");
    // The second stretch is a count, not a second line.
    expect(await read("Twice")).toContain("+1");
    // A location with no itinerary days shows no date line at all, rather than
    // an empty one or a dash.
    expect(await read("Unscheduled")).toBeNull();

    expect(
      itemRequests.filter((p) => p.endsWith("/items")),
      "the list is one request; dates must not be fetched per card"
    ).toHaveLength(1);

    // The meta line under the title stacks on a phone, where there is no room
    // for anything else, and runs together as one separated row above 640px,
    // where three short stacked lines left most of a full-width card empty.
    const layout = async () =>
      page.locator(`item-card[title="Twice"]`).evaluate((el) => {
        const meta = el.shadowRoot.querySelector(".meta");
        return {
          direction: getComputedStyle(meta).flexDirection,
          separatorsShown: [...el.shadowRoot.querySelectorAll(".meta__sep")].every(
            (s) => getComputedStyle(s).display !== "none"
          ),
        };
      });

    expect(await layout(), "stacked at 324px").toMatchObject({ direction: "column" });

    await page.setViewportSize({ width: 1024, height: 800 });
    expect(await layout(), "one row on a wide screen").toMatchObject({
      direction: "row",
      separatorsShown: true,
    });
    await page.setViewportSize(MOBILE);
  });

  // A separator is only ever drawn *between* two parts that exist. The failure
  // this guards is a card leading with a stray dot because it has tags but no
  // dates, which is the common shape for somewhere not yet on the itinerary.
  test("a card with no dates does not lead with a separator", async ({ page }) => {
    await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Tags only", category: "site", type: "", tags: ["alpha"] },
    });
    await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Nothing at all", category: "site", type: "", tags: [] },
    });

    await page.setViewportSize({ width: 1024, height: 800 });
    await gotoRoute(page, `/trips/${tripId}/locations`);

    const seps = async (title) =>
      page
        .locator(`item-card[title="${title}"]`)
        .evaluate((el) => el.shadowRoot.querySelectorAll(".meta__sep").length);

    expect(await seps("Tags only"), "one part means no separator").toBe(0);

    // And a location with nothing to say under its title renders no meta row
    // at all, rather than an empty one taking up space.
    const hasMeta = await page
      .locator(`item-card[title="Nothing at all"]`)
      .evaluate((el) => Boolean(el.shadowRoot.querySelector(".meta")));
    expect(hasMeta, "no meta row when there is nothing to put in it").toBe(false);

    await page.setViewportSize(MOBILE);
  });

  // Stage 26 Milestone 5: sorting. Together with the date line on the card,
  // this closes the "Dates on the locations list" backlog entry, which named
  // both.
  //
  // The trip is seeded so that no two of the three orders agree, which is the
  // only way an assertion here proves the sort is doing anything.
  test("sorts by name and by date, and keeps undated locations last", async ({ page }) => {
    const mk = (title, dates) =>
      page.request.post(`/api/trips/${tripId}/items`, {
        data: { title, category: "site", type: "", dates },
      });

    // Insertion order: neither alphabetical nor chronological.
    await mk("Zebra crossing", [{ start_date: "2026-09-05", end_date: "2026-09-05" }]);
    await mk("Älvsborg", [{ start_date: "2026-09-20", end_date: "2026-09-20" }]);
    await mk("apple orchard", []);
    await mk("Hut 10", [{ start_date: "2026-09-01", end_date: "2026-09-01" }]);
    await mk("Hut 2", []);

    await gotoRoute(page, `/trips/${tripId}/locations`);

    const menu = page.locator(".locations-sort-slot .menu");
    const titles = () =>
      page.locator("item-card").evaluateAll((els) => els.map((e) => e.getAttribute("title")));
    const choose = async (label) => {
      await menu.locator('[data-action="toggle"]').click();
      await page.getByRole("menuitemradio", { name: label, exact: true }).click();
      await expect(menu.locator(".menu__dropdown")).toBeHidden();
    };

    const added = await titles();
    expect(added, "the default is the order the API returned").toEqual([
      "Zebra crossing",
      "Älvsborg",
      "apple orchard",
      "Hut 10",
      "Hut 2",
    ]);
    // The default order is not "sorted", so the trigger stays neutral.
    await expect(menu.locator('[data-action="toggle"]')).not.toHaveClass(/menu__trigger--active/);

    await choose("By name");
    expect(await titles(), "collated: Ä with A, case-insensitive, 2 before 10").toEqual([
      "Älvsborg",
      "apple orchard",
      "Hut 2",
      "Hut 10",
      "Zebra crossing",
    ]);
    // A non-default order tints the trigger, which under 640px is the only
    // cue left once the label is hidden.
    await expect(menu.locator('[data-action="toggle"]')).toHaveClass(/menu__trigger--active/);

    await choose("By date");
    expect(await titles(), "earliest first; the two undated go last, not first").toEqual([
      "Hut 10",
      "Zebra crossing",
      "Älvsborg",
      "apple orchard",
      "Hut 2",
    ]);

    // Back to the default restores the fetched order exactly. Note what this
    // does and does not show: it proves "as added" is recoverable, not that
    // sorted() copies -- applyFilters already passes it a fresh array from
    // .filter(), so this assertion passes even with the spread removed. It was
    // checked that way rather than assumed.
    await choose("As added");
    expect(await titles()).toEqual(added);
    await expect(menu.locator('[data-action="toggle"]')).not.toHaveClass(/menu__trigger--active/);
  });

  // Sorting orders what the filters left, rather than replacing them.
  test("sorting composes with a filter and with the search box", async ({ page }) => {
    const mk = (title, category) =>
      page.request.post(`/api/trips/${tripId}/items`, {
        data: { title, category, type: "", dates: [] },
      });
    await mk("Zulu inn", "stay");
    await mk("Alpha inn", "stay");
    await mk("Mike museum", "site");

    await gotoRoute(page, `/trips/${tripId}/locations`);

    await page.locator(".locations-filter-slot [data-action=\"toggle\"]").click();
    await page.locator('[data-group="category"]').click();
    await page.locator('[data-value="stay"]').click();

    await page.locator('.locations-sort-slot [data-action="toggle"]').click();
    await page.getByRole("menuitemradio", { name: "By name", exact: true }).click();

    const titles = await page
      .locator("item-card")
      .evaluateAll((els) => els.map((e) => e.getAttribute("title")));
    expect(titles, "the filter still narrows, the sort still orders").toEqual([
      "Alpha inn",
      "Zulu inn",
    ]);

    // And the search box narrows the already-sorted list rather than resetting
    // the order.
    await page.locator('input[name="q"]').fill("inn");
    expect(
      await page.locator("item-card").evaluateAll((els) => els.map((e) => e.getAttribute("title")))
    ).toEqual(["Alpha inn", "Zulu inn"]);
  });

  // Stage 26 Milestone 6: filtering by tag and by date.
  test("filters by tag, and offers the group only once the trip has tags", async ({ page }) => {
    const menu = page.locator(".locations-filter-slot .menu");
    const open = async () => menu.locator('[data-action="toggle"]').click();

    // An untagged trip does not get a filter that could only ever say "any":
    // this is the one group whose options can legitimately be empty.
    await gotoRoute(page, `/trips/${tripId}/locations`);
    await open();
    await expect(menu.locator('[data-group="tags"]')).toHaveCount(0);

    const mk = (title, tags) =>
      page.request.post(`/api/trips/${tripId}/items`, {
        data: { title, category: "site", type: "", tags },
      });
    await mk("Blue lagoon", ["south", "spa"]);
    await mk("Grey lagoon", ["south"]);
    await mk("Far north", ["north"]);

    await gotoRoute(page, `/trips/${tripId}/locations`);
    await open();
    await menu.locator('[data-group="tags"]').click();

    // Sorted case-insensitively, with the neutral option first. The options
    // come from the list already in memory -- the tab holds every location, so
    // asking the server for a projection of what it is looking at would be a
    // request for nothing.
    await expect(menu.locator("[data-value]")).toHaveText(["Any tag", "north", "south", "spa"]);

    await menu.locator('[data-value="south"]').click();
    await expect(page.locator("item-card")).toHaveCount(2);
    await open();
    await expect(menu.locator('[data-group="tags"]')).toHaveText("south");
    await expect(menu.locator('[data-action="toggle"]')).toHaveClass(/menu__trigger--active/);

    // Back to Any tag restores the list.
    await menu.locator('[data-group="tags"]').click();
    await menu.locator('[data-value="any"]').click();
    await expect(page.locator("item-card")).toHaveCount(3);
  });

  test("filters by date: not scheduled, scheduled, and a range that overlaps", async ({ page }) => {
    const mk = (title, dates) =>
      page.request.post(`/api/trips/${tripId}/items`, {
        data: { title, category: "site", type: "", dates },
      });
    // A stay spanning three days, a single day before it, and two with no
    // itinerary days at all.
    await mk("Long stay", [{ start_date: "2026-09-05", end_date: "2026-09-07" }]);
    await mk("One day", [{ start_date: "2026-09-01", end_date: "2026-09-01" }]);
    await mk("Someday", []);
    await mk("Maybe", []);

    await gotoRoute(page, `/trips/${tripId}/locations`);
    const menu = page.locator(".locations-filter-slot .menu");
    const open = async () => menu.locator('[data-action="toggle"]').click();
    const titles = () =>
      page.locator("item-card").evaluateAll((els) => els.map((e) => e.getAttribute("title")));

    // "Not scheduled" is why this is a preset list and not only a range
    // picker: while planning, "what have I not placed yet" is the question
    // asked most, and no range can express it.
    await open();
    await menu.locator('[data-group="date"]').click();
    await menu.locator('[data-value="unscheduled"]').click();
    expect((await titles()).sort()).toEqual(["Maybe", "Someday"]);
    await open();
    await expect(menu.locator('[data-group="date"]')).toHaveText("Not scheduled");

    await menu.locator('[data-group="date"]').click();
    await menu.locator('[data-value="scheduled"]').click();
    expect((await titles()).sort()).toEqual(["Long stay", "One day"]);

    // A range that falls *inside* the long stay must find it: overlap, not
    // containment. A hotel booked the 5th to the 7th is part of what happens
    // on the 6th.
    await open();
    await menu.locator('[data-group="date"]').click();
    await menu.locator('.date-filter input[name="from"]').fill("2026-09-06");
    await menu.locator('.date-filter input[name="to"]').fill("2026-09-06");
    await menu.locator('.date-filter button[type="submit"]').click();
    expect(await titles()).toEqual(["Long stay"]);

    // The row shows the range itself, formatted -- its state is not one of a
    // fixed set of options, so it computes its own label.
    await open();
    await expect(menu.locator('[data-group="date"]')).toContainText("6");

    // An earlier window finds the other one and not the stay.
    await menu.locator('[data-group="date"]').click();
    await menu.locator('.date-filter input[name="from"]').fill("2026-09-01");
    await menu.locator('.date-filter input[name="to"]').fill("2026-09-02");
    await menu.locator('.date-filter button[type="submit"]').click();
    expect(await titles()).toEqual(["One day"]);

    // Apply with both ends empty is not a range, it is the neutral state.
    await open();
    await menu.locator('[data-group="date"]').click();
    await menu.locator('.date-filter input[name="from"]').fill("");
    await menu.locator('.date-filter input[name="to"]').fill("");
    await menu.locator('.date-filter button[type="submit"]').click();
    await expect(page.locator("item-card")).toHaveCount(4);
    await open();
    await expect(menu.locator('[data-group="date"]')).toHaveText("Any date");
    await expect(menu.locator('[data-action="toggle"]')).not.toHaveClass(/menu__trigger--active/);
  });

  // The date group's neutral state is a range rather than one of its options,
  // so Clear has to reach it through onClear. Worth its own case: this is the
  // group the hook was added for.
  test("clearing resets a date range as well as the other filters", async ({ page }) => {
    await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Dated", category: "site", type: "", tags: ["x"], dates: [{ start_date: "2026-09-05", end_date: "2026-09-05" }] },
    });
    await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Undated", category: "site", type: "", tags: [], dates: [] },
    });

    await gotoRoute(page, `/trips/${tripId}/locations`);
    const menu = page.locator(".locations-filter-slot .menu");
    const open = async () => menu.locator('[data-action="toggle"]').click();

    await open();
    await menu.locator('[data-group="date"]').click();
    await menu.locator('.date-filter input[name="from"]').fill("2026-09-05");
    await menu.locator('.date-filter button[type="submit"]').click();
    await expect(page.locator("item-card")).toHaveCount(1);

    await open();
    await menu.locator('[data-action="clear"]').click();
    await expect(page.locator("item-card")).toHaveCount(2);
    await open();
    await expect(menu.locator('[data-group="date"]')).toHaveText("Any date");
    await expect(menu.locator('[data-action="toggle"]')).not.toHaveClass(/menu__trigger--active/);
  });

  // Omitting the tags must not clear them -- the same absent-versus-empty rule
  // the API keeps for links and dates, checked from the browser because the
  // editor is what decides whether to send the key at all.
  test("tags: a location with none stays clean, and editing another field keeps them", async ({ page }) => {
    const created = await page.request.post(`/api/trips/${tripId}/items`, {
      data: { title: "Bare", category: "site", type: "", tags: ["kept"] },
    });
    const item = await created.json();

    await gotoRoute(page, `/trips/${tripId}/locations/${item.id}/edit`);
    await expect(page.locator(".tag-field__chip")).toHaveText(["kept"]);

    await page.locator('.item-form input[name="title"]').fill("Bare, retitled");
    await page.locator('[data-action="save"]').click();
    await expect(page.locator("h1")).toHaveText("Bare, retitled");
    await expect(page.locator(".location-view__tags .tag-chip")).toHaveText(["kept"]);

    // And a location with no tags shows no empty chip row at all.
    const plain = await (
      await page.request.post(`/api/trips/${tripId}/items`, {
        data: { title: "Untagged", category: "site", type: "", tags: [] },
      })
    ).json();
    await gotoRoute(page, `/trips/${tripId}/locations/${plain.id}`);
    await expect(page.locator(".location-view__tags")).toHaveCount(0);
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
