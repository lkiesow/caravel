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
        data: { title: "Somewhere", category: "site", type: "landmark" },
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
      await form.locator('[name="username"]').fill("nobody-at-all");
      await form.locator('button[type="submit"]').click();
      await expect(page.locator(".members-add__error")).toHaveText(copy.noSuchUser);
      await expect(rows).toHaveCount(1);

      // Now add the real one, as a viewer.
      await form.locator('[name="username"]').fill("other");
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
  });
}
