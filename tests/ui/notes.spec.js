// The trip notepad, end to end.
//
// The generic sweeps already cover this tab as a route -- routes.spec.js for
// overlap and tap targets, headings.spec.js for the outline, a11y-names.spec.js
// for the controls, menu.spec.js for where the tab sits in the bar. None of
// them press a button. What is left, and what this file is, is the one thing
// the tab actually does: which of its two modes you land in, and that the
// markdown you type comes back rendered and comes back editable.
//
// The mode rule is the whole feature and is easy to get subtly wrong, so each
// transition is asserted rather than inferred: empty opens the editor, saved
// opens the read view, Edit goes back with the source intact, Cancel throws a
// draft away, and clearing returns to the editor.
//
// Owns its trips, like checklists.spec.js: the seeded `full` trip carries a
// note that other specs and the screenshot run read, so writing to it here
// would make this file's order matter.
import { test, expect } from "@playwright/test";
import { login, openAs, OTHER_AUTH_STATE_FILE } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

const SOURCE = ["## Ferry", "", "Book by *May*.", "", "- passport", "- paper licence"].join("\n");

test.describe("trip notes, end to end", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: notes spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("opens in the editor when empty, and renders the markdown once saved", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);

    // Nothing written: straight into the editor, with no Cancel, because there
    // is no saved note to cancel back to.
    const textarea = page.locator(".trip-notes textarea");
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue("");
    await expect(page.locator(".trip-notes__edit")).toHaveCount(0);
    await expect(page.locator(".trip-notes__cancel")).toHaveCount(0);
    await expect(page.locator(".trip-notes__rendered")).toHaveCount(0);

    await textarea.fill(SOURCE);
    await page.locator('.trip-notes__form button[type="submit"]').click();

    // Saved: the read view, with real markup rather than the source text. The
    // heading and the list are the two the server's renderer is actually being
    // trusted for -- a client that inserted the markdown as text would still
    // "contain" the words.
    const rendered = page.locator(".trip-notes__rendered");
    await expect(rendered).toBeVisible();
    await expect(rendered.locator("h2")).toHaveText("Ferry");
    await expect(rendered.locator("li")).toHaveText(["passport", "paper licence"]);
    await expect(rendered.locator("em")).toHaveText("May");
    await expect(page.locator(".trip-notes textarea")).toHaveCount(0);
    await expect(page.locator(".trip-notes__edit")).toBeVisible();

    // And it is the mode you come back to, not just the mode you were left in.
    await page.reload();
    await expect(page.locator(".trip-notes__rendered h2")).toHaveText("Ferry");
    await expect(page.locator(".trip-notes textarea")).toHaveCount(0);
  });

  test("Edit gives the source back, and Cancel throws away an unsaved draft", async ({ page }) => {
    await page.request.put(`/api/trips/${tripId}/notes`, { data: { body: SOURCE } });
    await page.goto(`/trips/${tripId}/notes`);

    await page.locator(".trip-notes__edit").click();

    // The markdown as typed, not the rendered text: this is the assertion that
    // fails if the tab ever starts round-tripping through the HTML.
    const textarea = page.locator(".trip-notes textarea");
    await expect(textarea).toHaveValue(SOURCE);

    await textarea.fill("thrown away");
    await page.locator(".trip-notes__cancel").click();

    await expect(page.locator(".trip-notes__rendered h2")).toHaveText("Ferry");
    await expect(page.locator(".trip-notes__rendered")).not.toContainText("thrown away");

    // Re-opening must not resurrect the discarded draft.
    await page.locator(".trip-notes__edit").click();
    await expect(page.locator(".trip-notes textarea")).toHaveValue(SOURCE);
  });

  test("clearing the note puts you back where a fresh trip starts", async ({ page }) => {
    await page.request.put(`/api/trips/${tripId}/notes`, { data: { body: SOURCE } });
    await page.goto(`/trips/${tripId}/notes`);

    await page.locator(".trip-notes__edit").click();
    await page.locator(".trip-notes textarea").fill("   ");
    await page.locator('.trip-notes__form button[type="submit"]').click();

    // Whitespace is not a note: the row is deleted, so this is the same state
    // as a trip nobody has written on -- the editor, and no Cancel.
    await expect(page.locator(".trip-notes textarea")).toHaveValue("");
    await expect(page.locator(".trip-notes__rendered")).toHaveCount(0);
    await expect(page.locator(".trip-notes__cancel")).toHaveCount(0);

    await page.reload();
    await expect(page.locator(".trip-notes textarea")).toBeVisible();
    await expect(page.locator(".trip-notes__rendered")).toHaveCount(0);
  });

  test("a viewer reads the note and is offered no way to change it", async ({ page, browser }) => {
    await page.request.put(`/api/trips/${tripId}/notes`, { data: { body: SOURCE } });
    const added = await page.request.post(`/api/trips/${tripId}/members`, {
      data: { username: "other", role: "viewer" },
    });
    expect(added.status(), "add the viewer").toBe(201);

    const { context, page: theirPage } = await openAs(browser, OTHER_AUTH_STATE_FILE, MOBILE);
    try {
      await theirPage.goto(`/trips/${tripId}/notes`);
      // Both halves: the note is there, and the controls are not. Asserting
      // only the absence would pass on a tab that failed to load at all.
      await expect(theirPage.locator(".trip-notes__rendered h2")).toHaveText("Ferry");
      await expect(theirPage.locator(".trip-notes textarea")).toHaveCount(0);
      await expect(theirPage.locator(".trip-notes__edit")).toHaveCount(0);
    } finally {
      await context.close();
    }
  });
});
