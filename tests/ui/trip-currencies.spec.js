// The exchange-rate editor in trip settings (Stage 32 Milestone 4).
//
// Isolation follows expenses.spec.js: its own trip, created in beforeEach and
// deleted in afterEach, so the seeded scenarios every other spec reads are
// never touched.
//
// What this covers is the part no Go test can reach: the exponent fold. The
// server stores a minor-unit-to-minor-unit integer and has no idea how many
// decimal places a currency has -- the browser folds that in on the way down
// and unfolds it on the way back. So the claim under test is that what you
// type is what you read back, with the right integer in between.
import { test, expect } from "@playwright/test";
import { login } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

test.describe("trip currencies", () => {
  test.use({ viewport: MOBILE });

  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", {
      data: { title: "UI suite: currencies spec", currency: "EUR" },
    });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  test("a typed rate survives the fold to minor units and back", async ({ page }) => {
    await page.goto(`/trips/${tripId}/settings`);

    const slot = page.locator(".trip-currencies-slot");
    await expect(slot.locator(".trip-currencies__empty")).toContainText("EUR");
    await expect(slot.locator(".trip-currencies__row")).toHaveCount(0);

    await slot.getByRole("button", { name: /add a currency/i }).click();
    const row = slot.locator(".trip-currencies__row").first();

    // The trip's own currency must not be offered: a rate from EUR to EUR is a
    // second, contradictory answer to what a euro is worth.
    await expect(row.locator('[data-role="code"]').locator("option[value=EUR]")).toHaveCount(0);

    await row.locator('[data-role="code"]').selectOption("JPY");
    await row.locator('[data-role="rate"]').fill("0.0058");
    await slot.getByRole("button", { name: /save/i }).click();

    // 1 yen (no minor unit) is 0.58 cents, so the stored integer is 580000000.
    // This is the assertion the whole design rests on.
    await expect
      .poll(async () => {
        const body = await (await page.request.get(`/api/trips/${tripId}/currencies`)).json();
        return body.currencies;
      })
      .toEqual([{ code: "JPY", rate_ppb: 580000000 }]);

    // And back out again, through a fresh load rather than the live DOM.
    await page.reload();
    await expect(page.locator('.trip-currencies__row [data-role="rate"]')).toHaveValue("0.0058");
    await expect(page.locator('.trip-currencies__row [data-role="code"]')).toHaveValue("JPY");
  });

  test("refuses an unparseable rate without asking the server", async ({ page }) => {
    await page.goto(`/trips/${tripId}/settings`);
    const slot = page.locator(".trip-currencies-slot");

    await slot.getByRole("button", { name: /add a currency/i }).click();
    await slot.locator('[data-role="code"]').selectOption("JPY");
    await slot.locator('[data-role="rate"]').fill("not a number");
    await slot.getByRole("button", { name: /save/i }).click();

    // The example in the message is derived from the currency pair rather than
    // written into the copy, so a JPY row is not shown a EUR-shaped example.
    await expect(slot.locator(".trip-currencies__error")).toContainText("JPY");
    const stored = await (await page.request.get(`/api/trips/${tripId}/currencies`)).json();
    expect(stored.currencies, "nothing reached the server").toEqual([]);
  });

  test("surfaces the server's refusal when a currency is still in use", async ({ page }) => {
    await page.request.put(`/api/trips/${tripId}/currencies`, {
      data: { currencies: [{ code: "JPY", rate_ppb: 580000000 }] },
    });
    await page.request.post(`/api/trips/${tripId}/expenses`, {
      data: { title: "Ramen", amount_minor: 1200, currency: "JPY", spent_on: "2026-08-19" },
    });

    await page.goto(`/trips/${tripId}/settings`);
    const slot = page.locator(".trip-currencies-slot");
    await slot.locator(".icon-remove").click();
    await slot.getByRole("button", { name: /save/i }).click();

    // The server's own wording, not a client-side paraphrase: it names the code
    // and the count, which is what tells the reader where to go and fix it.
    await expect(slot.locator(".trip-currencies__error")).toContainText("JPY");
    await expect(slot.locator(".trip-currencies__error")).toContainText("1 expense");

    const stored = await (await page.request.get(`/api/trips/${tripId}/currencies`)).json();
    expect(stored.currencies, "the refused save changed nothing").toEqual([
      { code: "JPY", rate_ppb: 580000000 },
    ]);
  });

  // The fold, exhaustively, in the browser that actually performs it. Every
  // ordered pair of supported currencies, because the shift depends on both
  // exponents and a pair whose exponents differ is exactly where an off-by-one
  // hides.
  test("every currency pair round-trips exactly", async ({ page }) => {
    await page.goto(`/trips/${tripId}/settings`);

    const failures = await page.evaluate(async () => {
      const { parseRate, formatRate, CURRENCIES } = await import("/js/format.js");
      const rates = ["1", "0.5", "0.0058", "172", "12.345", "0.01", "999.99"];
      const bad = [];
      for (const foreign of CURRENCIES) {
        for (const main of CURRENCIES) {
          if (foreign === main) continue;
          for (const typed of rates) {
            const ppb = parseRate(typed, foreign, main);
            if (ppb === null || !Number.isSafeInteger(ppb) || ppb <= 0) {
              bad.push(`${foreign}->${main} ${typed}: parsed to ${ppb}`);
              continue;
            }
            const back = formatRate(ppb, foreign, main);
            if (back !== typed) bad.push(`${foreign}->${main} ${typed} -> ${ppb} -> ${back}`);
          }
        }
      }
      return bad;
    });

    expect(failures).toEqual([]);
  });

  // The expenses tab's half of the feature: the picker, the dual row, and the
  // promise that a single-currency trip is untouched by any of it.
  test("records an expense in another currency and shows both figures", async ({ page }) => {
    await page.request.put(`/api/trips/${tripId}/currencies`, {
      data: { currencies: [{ code: "JPY", rate_ppb: 580000000 }] },
    });
    await page.request.post(`/api/trips/${tripId}/expenses`, {
      data: { title: "Train", amount_minor: 4500, spent_on: "2026-08-20" },
    });

    await page.goto(`/trips/${tripId}/expenses`);
    const form = page.locator(".expenses__form");

    // The label and the placeholder follow the picker, exponent included: yen
    // have no minor unit, so the field stops offering decimals.
    await expect(page.locator(".expenses__amount-label")).toHaveText("Amount (EUR)");
    await form.locator('[name="currency"]').selectOption("JPY");
    await expect(page.locator(".expenses__amount-label")).toHaveText("Amount (JPY)");
    await expect(form.locator('[name="amount"]')).toHaveAttribute("placeholder", "0");

    // The live preview, before anything is saved.
    await form.locator('[name="title"]').fill("Ryokan");
    await form.locator('[name="amount"]').fill("12000");
    await form.locator('[name="spentOn"]').fill("2026-08-19");
    await expect(page.locator(".expenses__converted-preview")).toHaveText("≈ €69.60");

    await form.getByRole("button", { name: /add expense/i }).click();

    // Original first, converted after: what was paid is the fact, the
    // conversion is an approximation of it.
    const yenRow = page.locator(".expenses__row", { hasText: "Ryokan" });
    await expect(yenRow.locator(".expenses__row-amount")).toContainText("¥12,000");
    await expect(yenRow.locator(".expenses__row-converted")).toHaveText("≈ €69.60");

    // The euro row keeps one figure, and the total is the converted sum.
    const eurRow = page.locator(".expenses__row", { hasText: "Train" });
    await expect(eurRow.locator(".expenses__row-converted")).toHaveCount(0);
    await expect(page.locator(".expenses__total")).toHaveText("€114.60");
  });

  test("reopens a foreign expense in the currency it was paid in", async ({ page }) => {
    await page.request.put(`/api/trips/${tripId}/currencies`, {
      data: { currencies: [{ code: "JPY", rate_ppb: 580000000 }] },
    });
    await page.request.post(`/api/trips/${tripId}/expenses`, {
      data: { title: "Ryokan", amount_minor: 12000, currency: "JPY", spent_on: "2026-08-19" },
    });

    await page.goto(`/trips/${tripId}/expenses`);
    const row = page.locator(".expenses__row", { hasText: "Ryokan" });
    await row.locator(".expenses__row-trigger").click();
    await row.locator('button[data-value="edit"]').click();

    const form = page.locator(".expenses__form");
    // 12000, not 120.00: the amount is typed back in its own currency, whose
    // exponent is zero.
    await expect(form.locator('[name="amount"]')).toHaveValue("12000");
    await expect(form.locator('[name="currency"]')).toHaveValue("JPY");
    // And the preview is there on open, not only once the field is touched.
    await expect(page.locator(".expenses__converted-preview")).toHaveText("≈ €69.60");
  });

  // The regression that would otherwise go unnoticed: every trip that has not
  // asked for a second currency must look exactly as it did before Stage 32.
  test("a single-currency trip is offered no picker at all", async ({ page }) => {
    await page.request.post(`/api/trips/${tripId}/expenses`, {
      data: { title: "Hostel", amount_minor: 4500, spent_on: "2026-08-18" },
    });

    await page.goto(`/trips/${tripId}/expenses`);
    await expect(page.locator(".expenses__row")).toHaveCount(1);
    await expect(page.locator('.expenses__form [name="currency"]')).toHaveCount(0);
    await expect(page.locator(".expenses__row-converted")).toHaveCount(0);
    await expect(page.locator(".expenses__converted-preview")).toBeHidden();
    await expect(page.locator(".expenses__row-amount")).toHaveText("€45.00");
  });
});
