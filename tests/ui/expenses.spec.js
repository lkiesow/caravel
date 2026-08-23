// The expenses tab, end to end (Stage 17 Milestone 7).
//
// Isolation follows files.spec.js and sharing.spec.js: its own trip, created in
// beforeEach and deleted in afterEach, so the seeded scenarios every other spec
// reads are never touched. The seeded `full` trip does carry a ledger, but it is
// there for a human to look at -- a spec that mutated it would break the counts
// the sweeps assert.
//
// What this covers is the part no other assertion reaches: money surviving a
// round trip through a form. The Go tests own the arithmetic (see
// internal/httpapi/expense_split_test.go and expense_balances_test.go); this
// owns the claim that what you type is what gets stored and what you read back.
import { test, expect } from "@playwright/test";
import { login, openAs, OTHER_AUTH_STATE_FILE, OTHER_USER } from "./helpers/scenarios.js";

// 324px on purpose: every row here stacks three or four lines at that width,
// and the amount column is the one thing that must never be the part that
// truncates.
const MOBILE = { width: 324, height: 756 };

test.describe("expenses tab, end to end", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", {
      data: { title: "UI suite: expenses spec", currency: "EUR" },
    });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    // Cascades to the expenses and their shares. Runs even when the test
    // failed, so a red run leaves nothing behind.
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("adds, edits and deletes an expense, keeping the total right", async ({ page }) => {
    await page.goto(`/trips/${tripId}/expenses`);

    const rows = page.locator(".expenses__row");
    const total = page.locator(".expenses__total");
    const form = page.locator(".expenses__form");

    // Empty to begin with: the empty line, no rows, and a zero total in the
    // trip's own currency.
    await expect(page.locator(".expenses__empty")).toBeVisible();
    await expect(rows).toHaveCount(0);
    await expect(total).toHaveText("€0.00");
    // On a solo trip none of the who-paid machinery renders: the answer is
    // always you, so a select of one and a table of one row say nothing.
    await expect(page.locator('[name="payer"]')).toHaveCount(0);
    await expect(page.locator(".expenses__summary-card")).toBeHidden();

    // Typed the way a person types it, decimal point and all.
    await form.locator('[name="title"]').fill("Hostel");
    await form.locator('[name="amount"]').fill("45.50");
    await form.locator('[name="spentOn"]').fill("2026-08-18");
    await form.locator('button[type="submit"]').click();

    await expect(rows).toHaveCount(1);
    await expect(total).toHaveText("€45.50");
    await expect(rows.first().locator(".expenses__row-amount")).toHaveText("€45.50");
    await expect(page.locator(".expenses__day-heading")).toHaveCount(1);

    // A comma is a decimal separator too: a German keyboard produces one, and
    // being strict about it would be a bug rather than a validation.
    await form.locator('[name="title"]').fill("Dinner");
    await form.locator('[name="amount"]').fill("12,05");
    await form.locator('[name="spentOn"]').fill("2026-08-19");
    await form.locator('button[type="submit"]').click();

    await expect(rows).toHaveCount(2);
    await expect(total).toHaveText("€57.55");
    // Newest day first, so the dinner leads.
    await expect(rows.first().locator(".expenses__row-title")).toHaveText("Dinner");
    await expect(rows.first().locator(".expenses__row-amount")).toHaveText("€12.05");

    // Edit: the form loads the row, and the amount comes back as a plain
    // decimal rather than a formatted string.
    await rows.first().locator(".expenses__row-trigger").click();
    await rows.first().locator('button[data-value="edit"]').click();
    await expect(form.locator('[name="title"]')).toHaveValue("Dinner");
    await expect(form.locator('[name="amount"]')).toHaveValue("12.05");
    await form.locator('[name="amount"]').fill("20");
    await form.locator('button[type="submit"]').click();

    await expect(rows).toHaveCount(2);
    await expect(total).toHaveText("€65.50");
    await expect(rows.first().locator(".expenses__row-amount")).toHaveText("€20.00");

    // Delete, behind its confirm dialog.
    await rows.first().locator(".expenses__row-trigger").click();
    await rows.first().locator('button[data-value="delete"]').click();
    await page.locator("dialog button.btn-danger").click();

    await expect(rows).toHaveCount(1);
    await expect(total).toHaveText("€45.50");
  });

  test("refuses an amount it cannot parse, and writes nothing", async ({ page }) => {
    await page.goto(`/trips/${tripId}/expenses`);
    const form = page.locator(".expenses__form");
    const error = form.locator(".item-form__error");

    await form.locator('[name="title"]').fill("Nonsense");
    await form.locator('[name="amount"]').fill("about twelve");
    await form.locator('button[type="submit"]').click();

    await expect(error).toBeVisible();
    // Reported beside the field rather than arriving as a failed request, and
    // the example in the message comes from the currency.
    await expect(error).toContainText("12.50");
    await expect(page.locator(".expenses__row")).toHaveCount(0);
    await expect(page.locator(".expenses__total")).toHaveText("€0.00");

    // More decimals than the currency has is a refusal, not a silent rounding:
    // "12.567" euros is not 12.57 with any confidence.
    await form.locator('[name="amount"]').fill("12.567");
    await form.locator('button[type="submit"]').click();
    await expect(error).toBeVisible();
    await expect(page.locator(".expenses__row")).toHaveCount(0);
  });

  test("the trip currency drives every amount on the tab", async ({ page }) => {
    // JPY has no minor unit, which is the case a hardcoded divide-by-100 gets
    // wrong: 1200 yen is ¥1,200 and not ¥12.00.
    const res = await page.request.patch(`/api/trips/${tripId}`, {
      data: { title: "UI suite: expenses spec", currency: "JPY" },
    });
    expect(res.status()).toBe(200);

    await page.goto(`/trips/${tripId}/expenses`);
    const form = page.locator(".expenses__form");

    await expect(form.locator('[name="amount"]')).toHaveAttribute("placeholder", "0");
    await expect(page.locator(".expenses__amount-label")).toContainText("JPY");

    await form.locator('[name="title"]').fill("Ramen");
    await form.locator('[name="amount"]').fill("1200");
    await form.locator('[name="spentOn"]').fill("2026-08-18");
    await form.locator('button[type="submit"]').click();

    const amount = page.locator(".expenses__row-amount");
    await expect(amount).toHaveCount(1);
    await expect(amount).toContainText("1,200");
    await expect(amount).not.toContainText("12.00");

    // And a decimal is refused for a currency that has none.
    await form.locator('[name="title"]').fill("Coffee");
    await form.locator('[name="amount"]').fill("4.50");
    await form.locator('button[type="submit"]').click();
    await expect(form.locator(".item-form__error")).toBeVisible();
    await expect(page.locator(".expenses__row")).toHaveCount(1);
  });

  test("a shared trip shows who paid, a subset share, and a balance that follows from the rows", async ({ page, browser }) => {
    // A real second person on the trip, added the way the Members tab does it.
    const added = await page.request.post(`/api/trips/${tripId}/members`, {
      data: { username: OTHER_USER.username, role: "editor" },
    });
    expect(added.status(), "add the second member").toBe(201);
    const otherId = (await added.json()).user_id;

    await page.goto(`/trips/${tripId}/expenses`);
    const form = page.locator(".expenses__form");

    // Now the payer select exists, defaulting to the person filling the form,
    // and the share picker is collapsed behind its toggle.
    await expect(form.locator('[name="payer"]')).toBeVisible();
    await expect(form.locator('[name="shareAll"]')).toBeChecked();
    await expect(page.locator(".expenses__shares-group")).toBeHidden();

    // One expense for the whole trip: 30.00 between two people.
    await form.locator('[name="title"]').fill("Groceries");
    await form.locator('[name="amount"]').fill("30.00");
    await form.locator('[name="spentOn"]').fill("2026-08-18");
    await form.locator('button[type="submit"]').click();
    await expect(page.locator(".expenses__row")).toHaveCount(1);

    // And one for the other person only, which is the case that made the
    // balances hard to follow before the rows said so.
    await form.locator('[name="title"]').fill("Their museum ticket");
    await form.locator('[name="amount"]').fill("10.00");
    await form.locator('[name="spentOn"]').fill("2026-08-19");
    await form.locator('[name="shareAll"]').uncheck();
    await expect(page.locator(".expenses__shares-group")).toBeVisible();
    // Opening the picker starts from everyone, so narrowing means unticking.
    await form.locator(`[name="share"][value="${otherId}"]`).check();
    await page.locator('.expenses__shares-group [name="share"]:not([value="' + otherId + '"])').uncheck();
    await form.locator('button[type="submit"]').click();

    const rows = page.locator(".expenses__row");
    await expect(rows).toHaveCount(2);
    await expect(page.locator(".expenses__total")).toHaveText("€40.00");

    // The subset row says who it was for; the whole-trip row says nothing
    // extra, which is what keeps the common case quiet.
    const subset = rows.filter({ hasText: "Their museum ticket" });
    const shared = rows.filter({ hasText: "Groceries" });
    await expect(subset.locator(".expenses__row-shares")).toBeVisible();
    await expect(subset.locator(".expenses__row-shares")).toContainText("Other User");
    await expect(shared.locator(".expenses__row-shares")).toBeHidden();
    // The reader paid for the ticket but is not among the people it was for.
    await expect(subset.locator(".expenses__row-share")).toContainText("not shared with you");
    await expect(shared.locator(".expenses__row-share")).toContainText("€15.00");

    // The summary is one card at the bottom, and its numbers follow from the
    // rows above: demo paid 40.00 and owes 15.00, so is owed 25.00.
    const card = page.locator(".expenses__summary-card");
    await expect(card).toBeVisible();
    await expect(card.locator(".expenses__payer")).toHaveCount(1);
    await expect(card.locator(".expenses__payer-paid")).toHaveText("€40.00");
    const balances = card.locator(".expenses__balance");
    await expect(balances).toHaveCount(2);
    await expect(balances.filter({ hasText: "Demo User" })).toContainText("is owed €25.00");
    await expect(balances.filter({ hasText: "Other User" })).toContainText("owes €25.00");
    // One transfer settles it, with its amount in its own column.
    const transfers = card.locator(".expenses__transfer");
    await expect(transfers).toHaveCount(1);
    await expect(transfers.locator(".expenses__transfer-who")).toHaveText("Other User pays Demo User");
    await expect(transfers.locator(".expenses__transfer-amount")).toHaveText("€25.00");

    // The other person reads the same ledger from their own session, with the
    // shares seen from their side: they owe for the ticket that is only theirs.
    const { context: theirContext, page: otherPage } = await openAs(browser, OTHER_AUTH_STATE_FILE, MOBILE);
    try {
      await otherPage.goto(`/trips/${tripId}/expenses`);
      await expect(otherPage.locator(".expenses__total")).toHaveText("€40.00");
      const theirRows = otherPage.locator(".expenses__row");
      await expect(theirRows.filter({ hasText: "Their museum ticket" }).locator(".expenses__row-share")).toContainText(
        "€10.00"
      );
      await expect(
        otherPage.locator(".expenses__summary-card .expenses__balance").filter({ hasText: "Other User" })
      ).toContainText("owes €25.00");
    } finally {
      await theirContext.close();
    }
  });

  test("a viewer reads the ledger and is offered nothing that writes", async ({ page, browser }) => {
    await page.request.post(`/api/trips/${tripId}/members`, {
      data: { username: OTHER_USER.username, role: "viewer" },
    });
    const created = await page.request.post(`/api/trips/${tripId}/expenses`, {
      data: { title: "Hostel", amount_minor: 4500, spent_on: "2026-08-18" },
    });
    expect(created.status()).toBe(201);

    const { context: viewerContext, page: viewerPage } = await openAs(browser, OTHER_AUTH_STATE_FILE, MOBILE);
    try {
      await viewerPage.goto(`/trips/${tripId}/expenses`);

      // Reads everything.
      await expect(viewerPage.locator(".expenses__row")).toHaveCount(1);
      await expect(viewerPage.locator(".expenses__total")).toHaveText("€45.00");
      await expect(viewerPage.locator(".expenses__summary-card")).toBeVisible();

      // And is offered nothing that writes: no form, no row menu.
      await expect(viewerPage.locator(".expenses__form")).toHaveCount(0);
      await expect(viewerPage.locator(".expenses__row-trigger")).toHaveCount(0);

      // Hiding the controls is a courtesy; the server is the boundary.
      const refused = await viewerPage.request.post(`/api/trips/${tripId}/expenses`, {
        data: { title: "Sneaky", amount_minor: 100, spent_on: "2026-08-18" },
      });
      expect(refused.status(), "a viewer's write is refused by the server").toBe(403);
    } finally {
      await viewerContext.close();
    }
  });
});
