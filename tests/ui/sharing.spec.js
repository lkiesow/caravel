// Two people on one trip, which is the thing Stage 14 built and which no spec
// could reach before: the suite arrived as a single authenticated user.
//
// It drives the whole arc through the real UI — add a member, watch their app go
// read-only, promote them, watch it open up, then leave — because every part of
// that arc is a place where the client and the server can disagree about a role
// and nothing else would notice.
//
// Isolation follows files.spec.js: its own trip, created in beforeEach and
// deleted in afterEach, so the seeded memberships the other specs rely on
// (`other` as editor on `full`, viewer on `one-pin`) are never touched. The
// second user comes from a saved session rather than a login, so this costs
// nothing against the 10/min limiter.
import { test, expect } from "@playwright/test";
import { login, openAs, OTHER_AUTH_STATE_FILE } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

// Hard-coded per locale rather than read from the locale files, for the reason
// menu.spec.js gives: a wrong *translation* should fail, not be mirrored.
const COPY = {
  en: {
    roleEditor: "Editor",
    roleViewer: "Viewer",
    viewerBadge: "View only",
    leave: "Leave trip",
    noSuchUser: 'No user called “nobody-at-all” on this Caravel.',
    readOnlyHint: "You can view this trip but not change it.",
  },
  de: {
    roleEditor: "Bearbeiter",
    roleViewer: "Betrachter",
    viewerBadge: "Nur Lesen",
    leave: "Reise verlassen",
    noSuchUser: "Kein Benutzer namens „nobody-at-all“ in diesem Caravel.",
    readOnlyHint: "Du kannst diese Reise ansehen, aber nicht ändern.",
  },
};

for (const locale of ["en", "de"]) {
  test.describe(`sharing a trip (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    let tripId;

    test.beforeEach(async ({ page }) => {
      await login(page);
      const res = await page.request.post("/api/trips", { data: { title: `UI suite: sharing spec ${locale}` } });
      expect(res.status(), "create the spec's own trip").toBe(201);
      tripId = (await res.json()).id;
      // One location and one checklist, so the read-only assertions below have
      // something to *not* offer controls for. A tab with no content renders no
      // controls either way, which is the trap Milestone 4 walked into.
      const item = await page.request.post(`/api/trips/${tripId}/items`, {
        data: { title: "Somewhere", category: "site", tags: ["landmark"] },
      });
      expect(item.status()).toBe(201);
      const list = await page.request.post(`/api/trips/${tripId}/checklists`, { data: { title: "Shared list" } });
      expect(list.status()).toBe(201);
      const listId = (await list.json()).id;
      const listItem = await page.request.post(`/api/checklists/${listId}/items`, { data: { text: "something" } });
      expect(listItem.status()).toBe(201);
    });

    test.afterEach(async ({ page }) => {
      // Cascades to the items, checklists and memberships. Runs even on failure,
      // so a red run leaves nothing for the next one to trip over.
      if (tripId) await page.request.delete(`/api/trips/${tripId}`);
      tripId = null;
    });

    test(`adds a viewer, who sees a read-only trip, then promotes and leaves (${locale})`, async ({ page, browser }) => {
      const copy = COPY[locale];
      await page.goto(`/trips/${tripId}/members`);

      // Only the owner to begin with, and their row carries no controls: there
      // is nothing to do to yourself here.
      const rows = page.locator(".member-card");
      await expect(rows).toHaveCount(1);
      await expect(rows.first().locator(".member-card__trigger")).toHaveCount(0);

      // A username that does not exist gets its own message rather than a
      // generic failure — the whole reason the API returns an error code.
      const form = page.locator(".members-add");
      await form.locator('[name="member"]').fill("nobody-at-all");
      await form.locator('button[type="submit"]').click();
      await expect(page.locator(".members-add__error")).toHaveText(copy.noSuchUser);
      await expect(rows).toHaveCount(1);

      // Now add the real one, as a viewer.
      await form.locator('[name="member"]').fill("other");
      await form.locator('[name="role"]').selectOption("viewer");
      await form.locator('button[type="submit"]').click();
      await expect(rows).toHaveCount(2);
      const theirRow = rows.filter({ hasText: "@other" });
      await expect(theirRow.locator(".member-card__role")).toHaveText(copy.roleViewer);

      // --- what the viewer sees, in their own session ---
      const { context: theirContext, page: theirPage } = await openAs(browser, OTHER_AUTH_STATE_FILE, MOBILE);
      try {
        await theirPage.goto(`/trips/${tripId}/locations`);
        await expect(theirPage.locator(".trip-detail__role")).toHaveText(copy.viewerBadge);
        // The content is there; the controls are not. Asserting both halves,
        // because zero controls on an empty page proves nothing.
        await expect(theirPage.locator("item-card")).toHaveCount(1);
        await expect(theirPage.locator('[data-action="new-item"]')).toHaveCount(0);

        await theirPage.goto(`/trips/${tripId}/checklists`);
        await expect(theirPage.locator(".checklist-card")).toHaveCount(1);
        await expect(theirPage.locator(".checklist-item")).toHaveCount(1);
        await expect(theirPage.locator(".checklist-new-form")).toHaveCount(0);
        await expect(theirPage.locator(".checklist-item-form")).toHaveCount(0);
        await expect(theirPage.locator(".checklist-item input[type=checkbox]")).toBeDisabled();

        await theirPage.goto(`/trips/${tripId}/files`);
        await expect(theirPage.locator(".file-drop")).toHaveCount(0);

        await theirPage.goto(`/trips/${tripId}/settings`);
        await expect(theirPage.locator(".editor-card__hint")).toContainText(copy.readOnlyHint);
        await expect(theirPage.locator(".trip-form")).toHaveCount(0);
        await expect(theirPage.locator('[data-action="delete"]')).toHaveCount(0);

        // --- promoted to editor, the same pages open up ---
        await theirRow.locator(".member-card__trigger").click();
        await theirRow.getByRole("menuitemradio", { name: copy.roleEditor }).click();
        await expect(theirRow.locator(".member-card__role")).toHaveText(copy.roleEditor);

        await theirPage.goto(`/trips/${tripId}/locations`);
        await expect(theirPage.locator(".trip-detail__role")).toHaveCount(0);
        await expect(theirPage.locator('[data-action="new-item"]')).toHaveCount(1);

        await theirPage.goto(`/trips/${tripId}/checklists`);
        await expect(theirPage.locator(".checklist-new-form")).toHaveCount(1);
        await expect(theirPage.locator(".checklist-item input[type=checkbox]")).toBeEnabled();

        // --- and they can leave under their own steam ---
        await theirPage.goto(`/trips/${tripId}/members`);
        const leave = theirPage.locator('[data-action="leave"]');
        await expect(leave).toHaveCount(1);
        // A non-owner gets no add form and no per-row menus: they can see who is
        // on the trip and remove only themselves.
        await expect(theirPage.locator(".members-add")).toHaveCount(0);
        await expect(theirPage.locator(".member-card__trigger")).toHaveCount(0);

        await leave.click();
        const dialog = theirPage.locator("dialog.dialog");
        await expect(dialog).toBeVisible();

        // The confirm button is red, because leaving is irreversible and does
        // destroy your personal files here -- but it is not a bin. confirmDialog
        // used to pick the icon from `danger` alone, so an action that removes
        // you from a trip advertised itself as deleting the trip (Stage 23
        // Milestone 8).
        const confirm = dialog.locator(".btn-danger");
        await expect(confirm.locator("use")).toHaveAttribute("href", /#lucide-log-out$/);

        await confirm.click();

        // Landed on the trips list, and *this* trip is gone from it. Named by id
        // rather than counted: they still hold the seeded memberships on the
        // demo trips, so a total count would assert something about the seed
        // rather than about leaving.
        //
        // The first version of this line read
        // `toHaveCount(await locator.count())`, which snapshots the count and
        // then asserts the count equals the snapshot — vacuous, and racy enough
        // to fail on whichever locale happened to lose the race.
        await expect(theirPage).toHaveURL(/\/trips$/);
        await expect(theirPage.locator(`trip-card[trip-id="${tripId}"]`)).toHaveCount(0);
        const reachable = await theirPage.request.get(`/api/trips/${tripId}`);
        expect(reachable.status(), "a trip you left should be gone, not merely hidden").toBe(404);
      } finally {
        await theirContext.close();
      }

      // The owner is alone again.
      await page.reload();
      await expect(rows).toHaveCount(1);
    });

    // The suggestion popup under the username field. It used to be a native
    // <datalist>, which Firefox for Android does not render at all, so it was
    // replaced by components/suggest-input.js — a hand-rolled listbox needs its
    // own coverage, because nothing about it comes from the browser any more.
    test(`suggests users to add, by keyboard and by tap (${locale})`, async ({ page }) => {
      await page.goto(`/trips/${tripId}/members`);
      const form = page.locator(".members-add");
      const field = form.locator('[name="member"]');
      const options = form.locator(".suggest__option");
      const rows = page.locator(".member-card");

      // Below the two-character floor nothing is asked for and nothing opens.
      await field.fill("o");
      await expect(options).toHaveCount(0);
      await expect(field).toHaveAttribute("aria-expanded", "false");

      // toHaveCount auto-waits, which covers the 200ms debounce.
      await field.fill("oth");
      await expect(options).toHaveCount(1);
      await expect(options.first()).toContainText("@other");
      await expect(field).toHaveAttribute("aria-expanded", "true");

      // Escape closes without taking the suggestion.
      await field.press("Escape");
      await expect(options).toHaveCount(0);
      await expect(field).toHaveValue("oth");

      // ...and so does a tap anywhere else, which is the only way off the list
      // on a phone that has no Escape key.
      await field.fill("oth");
      await expect(options).toHaveCount(1);
      await page.locator(".editor-card h2").first().click();
      await expect(options).toHaveCount(0);

      // Arrow keys move an active option, and aria-activedescendant is what
      // announces it — focus stays in the text field throughout.
      await field.fill("oth");
      await expect(options).toHaveCount(1);
      await field.press("ArrowDown");
      const activeId = await field.getAttribute("aria-activedescendant");
      expect(activeId).toBeTruthy();
      await expect(page.locator(`#${activeId}`)).toHaveClass(/suggest__option--active/);

      // Enter takes the suggestion and is swallowed: it must not submit the
      // form, or choosing a name would add them in the same keystroke.
      await field.press("Enter");
      await expect(field).toHaveValue("other");
      await expect(options).toHaveCount(0);
      await expect(rows).toHaveCount(1);

      // Tapping an option fills the field the same way (this is the path that
      // was broken on the phone, so it is asserted through a real click).
      await field.fill("");
      await field.fill("oth");
      await expect(options).toHaveCount(1);
      await options.first().click();
      await expect(field).toHaveValue("other");
      await expect(options).toHaveCount(0);

      await form.locator('button[type="submit"]').click();
      await expect(rows).toHaveCount(2);

      // Someone already on the trip is no longer worth suggesting: their only
      // outcome would be an "already a member" error.
      await field.fill("oth");
      await expect(options).toHaveCount(0);
      await expect(field).toHaveAttribute("aria-expanded", "false");
    });
  });
}
