// Checklists, end to end: the mutations.
//
// Two specs already look at this tab and neither writes. menu.spec.js asserts
// the visibility *grouping* and exactly which moves each card's menu offers,
// read-only, against the shared seed; sharing.spec.js asserts what a viewer is
// not offered. So what was missing is every action those menus lead to - and
// the one behaviour with no assertion anywhere, that a duplicated list starts
// unticked (Stage 15 Milestone 1).
//
// Owns its trips, files.spec.js's shape, because the seeded `full` trip's three
// lists are what menu.spec.js counts.
import { test, expect } from "@playwright/test";
import { login, openAs, OTHER_AUTH_STATE_FILE } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

test.describe("checklists, end to end", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: checklists spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("creates a list, adds items, ticks one, rewords it and removes it", async ({ page }) => {
    await page.goto(`/trips/${tripId}/checklists`);

    const empty = page.locator(".checklists-empty");
    await expect(empty).toBeVisible();

    // On a solo trip there is no visibility choice to make, so the form should
    // not offer one - a control that cannot mean anything yet.
    await expect(page.locator(".checklist-new__visibility")).toHaveCount(0);

    const newForm = page.locator(".checklist-new-form");
    await newForm.locator('input[name="title"]').fill("Packing");
    await newForm.locator('button[type="submit"]').click();

    const card = page.locator(".checklist-card");
    await expect(card).toHaveCount(1);
    await expect(card.locator("h2")).toHaveText("Packing");
    await expect(empty).toBeHidden();

    // The item form refocuses itself after each add, which is what makes
    // typing a list in one go work; two in a row is the cheap way to assert it
    // without a focus check that would pass on a re-rendered blank form.
    const itemForm = card.locator(".checklist-item-form");
    for (const text of ["Passport", "Adapter"]) {
      await itemForm.locator('input[name="text"]').fill(text);
      await itemForm.locator('button[type="submit"]').click();
    }

    const items = page.locator(".checklist-item");
    await expect(items).toHaveCount(2);

    // Ticking updates the row's class in place rather than re-rendering, so
    // both halves matter: the box, and the struck-through text.
    const passport = items.filter({ hasText: "Passport" });
    const box = passport.locator('input[type="checkbox"]');
    await expect(box).not.toBeChecked();
    await box.check();
    await expect(box).toBeChecked();
    await expect(passport.locator(".checklist-item__text--done")).toHaveCount(1);

    await page.reload();
    await expect(page.locator(".checklist-item").filter({ hasText: "Passport" }).locator('input[type="checkbox"]')).toBeChecked();

    // ⋮ → Edit, which is a prompt dialog prefilled with the current text.
    await passport.locator(".menu__trigger").click();
    await passport.getByRole("menuitem", { name: "Edit" }).click();
    const promptInput = page.locator(".dialog__input");
    await expect(promptInput).toHaveValue("Passport");
    await promptInput.fill("Passport and visa");
    await page.locator(".dialog__actions button", { hasText: "Save" }).click();

    await expect(page.locator(".checklist-item").filter({ hasText: "Passport and visa" })).toHaveCount(1);
    // Rewording must not clear the tick: text and checked are separate
    // endpoints precisely so that one cannot quietly overwrite the other.
    await expect(
      page.locator(".checklist-item").filter({ hasText: "Passport and visa" }).locator('input[type="checkbox"]'),
      "rewording an item should leave it ticked"
    ).toBeChecked();

    // ⋮ → Remove, which has no confirmation by design.
    const adapter = page.locator(".checklist-item").filter({ hasText: "Adapter" });
    await adapter.locator(".menu__trigger").click();
    await adapter.getByRole("menuitem", { name: "Remove" }).click();
    await expect(page.locator(".checklist-item")).toHaveCount(1);

    await page.reload();
    await expect(page.locator(".checklist-item")).toHaveCount(1);
  });

  test("duplicates a list, and the copy starts unticked", async ({ page }) => {
    const list = await page.request.post(`/api/trips/${tripId}/checklists`, { data: { title: "Packing" } });
    expect(list.status()).toBe(201);
    const listId = (await list.json()).id;
    for (const text of ["Passport", "Adapter"]) {
      const created = await page.request.post(`/api/checklists/${listId}/items`, { data: { text } });
      expect(created.status()).toBe(201);
      const itemId = (await created.json()).id;
      const ticked = await page.request.patch(`/api/checklists/${listId}/items/${itemId}`, { data: { checked: true } });
      expect(ticked.status()).toBe(200);
    }

    await page.goto(`/trips/${tripId}/checklists`);
    const cards = page.locator(".checklist-card");
    await expect(cards).toHaveCount(1);
    await expect(cards.first().locator('input[type="checkbox"]:checked')).toHaveCount(2);

    // ⋮ → Duplicate. One click, no dialog. Scoped to the card's own menu:
    // every item row carries a .menu__trigger too.
    await cards.first().locator(".checklist-card__actions .menu__trigger").click();
    await page.getByRole("menuitem", { name: "Duplicate" }).click();

    await expect(cards).toHaveCount(2);
    const copy = cards.filter({ hasText: "Packing (copy)" });
    await expect(copy).toHaveCount(1);

    // The point of the whole feature: same items, none of them ticked, and the
    // original untouched.
    await expect(copy.locator(".checklist-item")).toHaveCount(2);
    await expect(
      copy.locator('input[type="checkbox"]:checked'),
      "a duplicated list should start with nothing ticked"
    ).toHaveCount(0);
    await expect(cards.first().locator('input[type="checkbox"]:checked')).toHaveCount(2);

    await page.reload();
    await expect(page.locator(".checklist-card")).toHaveCount(2);
    await expect(
      page.locator(".checklist-card").filter({ hasText: "Packing (copy)" }).locator('input[type="checkbox"]:checked')
    ).toHaveCount(0);
  });

  test("renames a list, then deletes it behind its confirmation", async ({ page }) => {
    const list = await page.request.post(`/api/trips/${tripId}/checklists`, { data: { title: "Route plan" } });
    expect(list.status()).toBe(201);

    await page.goto(`/trips/${tripId}/checklists`);
    const card = page.locator(".checklist-card");
    const menu = () => card.locator(".checklist-card__actions .menu__trigger");

    await menu().click();
    await page.getByRole("menuitem", { name: "Rename" }).click();
    const promptInput = page.locator(".dialog__input");
    await expect(promptInput).toHaveValue("Route plan");
    await promptInput.fill("Ferry plan");
    await page.locator(".dialog__actions button", { hasText: "Save" }).click();
    await expect(card.locator("h2")).toHaveText("Ferry plan");

    // Cancel first: a destructive action that fires anyway is the bug worth
    // catching here.
    await menu().click();
    await page.getByRole("menuitem", { name: "Delete" }).click();
    await page.locator(".dialog__actions button", { hasText: "Cancel" }).click();
    await expect(card).toHaveCount(1);

    await menu().click();
    await page.getByRole("menuitem", { name: "Delete" }).click();
    await page.locator(".dialog__actions button", { hasText: "Delete" }).click();

    await expect(card).toHaveCount(0);
    await expect(page.locator(".checklists-empty")).toBeVisible();
  });

  test("on a shared trip, a list can be moved between visibilities", async ({ page, browser }) => {
    const added = await page.request.post(`/api/trips/${tripId}/members`, {
      data: { username: "other", role: "editor" },
    });
    expect(added.status(), "add the second member").toBe(201);

    await page.goto(`/trips/${tripId}/checklists`);

    // Now that somebody else is on the trip, the choice exists and defaults to
    // "everyone can tick".
    const visibility = page.locator(".checklist-new__visibility");
    await expect(visibility).toHaveCount(1);
    await expect(visibility.locator('input[value="shared"]')).toBeChecked();

    const newForm = page.locator(".checklist-new-form");
    await newForm.locator('input[name="title"]').fill("Shopping");
    await newForm.locator('button[type="submit"]').click();

    const card = page.locator(".checklist-card");
    await expect(card).toHaveCount(1);
    await expect(page.locator('.checklist-section[data-visibility="shared"] .checklist-card')).toHaveCount(1);

    // The other editor can see it and tick it, because "shared" is the one
    // visibility that is not author-only.
    const { context: theirContext, page: theirPage } = await openAs(browser, OTHER_AUTH_STATE_FILE, MOBILE);
    try {
      await theirPage.goto(`/trips/${tripId}/checklists`);
      await expect(theirPage.locator(".checklist-card")).toHaveCount(1);
      await expect(theirPage.locator(".checklist-item-form")).toHaveCount(1);

      // ⋮ → Make private. There is no badge on a card: the section it sits in
      // is the whole of the state, which is why the assertion is on the group.
      await card.locator(".checklist-card__actions .menu__trigger").click();
      await page.getByRole("menuitem", { name: "Make private" }).click();
      await expect(page.locator('.checklist-section[data-visibility="personal"] .checklist-card')).toHaveCount(1);
      await expect(page.locator('.checklist-section[data-visibility="shared"]')).toHaveCount(0);

      // And it disappears for the other editor entirely - private means unseen,
      // not merely uneditable.
      await theirPage.reload();
      await expect(
        theirPage.locator(".checklist-card"),
        "a personal list should be invisible to everyone else"
      ).toHaveCount(0);
      await expect(theirPage.locator(".checklists-empty")).toBeVisible();
    } finally {
      await theirContext.close();
    }
  });
});
