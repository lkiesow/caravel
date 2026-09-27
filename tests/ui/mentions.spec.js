// Typing @ in the trip notepad, end to end.
//
// The feature is three separable things and each is asserted rather than
// inferred from a screenshot: the trigger rule (when the list opens, and the
// four ways it closes again), the insertion (exactly what text lands in the
// textarea, including the escaping a bracketed title needs), and the
// positioning (the list sits next to the caret, not under a field that may be
// a screenful tall).
//
// Positioning is the part a screenshot would be tempting for and the part
// where a screenshot proves least: what matters is a number -- the distance
// from the caret's line -- and whether the box is inside the viewport. Both
// are measurable, so both are measured.
//
// Owns its trip and its locations, like notes.spec.js: this writes a note and
// creates places, and the seeded trips are read by other specs and by the
// screenshot run.
import { test, expect } from "@playwright/test";
import { login } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

const PLACES = [
  { title: "Kex Hostel", category: "stay" },
  { title: "Keflavik Airport", category: "transport" },
  { title: "Blue Lagoon", category: "site" },
  // The title that breaks a naive insertion: its brackets would close the
  // markdown link's label and leave the link as literal text.
  { title: "Museum [north]", category: "site" },
];

test.describe("linking a location from the notepad", () => {
  let tripId;
  const ids = {};

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", {
      data: { title: "UI suite: mentions spec" },
    });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
    for (const place of PLACES) {
      const made = await page.request.post(`/api/trips/${tripId}/items`, { data: place });
      expect(made.status(), `create ${place.title}`).toBe(201);
      ids[place.title] = (await made.json()).id;
    }
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  const options = (page) => page.locator("#trip-notes-mentions .suggest__option");

  test("offers the trip's locations and inserts a link to the one picked", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.click();

    // Prefix matches lead: both places start with "Ke", and Kex is typed for.
    await textarea.pressSequentially("We stayed at @kex");
    await expect(options(page)).toHaveText([/Kex Hostel/]);

    // Enter picks without an ArrowDown first -- autoActivateFirst, which is
    // what makes the keyboard path work on a phone.
    await textarea.press("Enter");
    await expect(textarea).toHaveValue(
      `We stayed at [Kex Hostel](/trips/${tripId}/locations/${ids["Kex Hostel"]})`,
    );
    await expect(options(page)).toHaveCount(0);

    // And the link is a link once rendered, not four literal square brackets.
    await page.locator('.trip-notes__form button[type="submit"]').click();
    const link = page.locator(".trip-notes__rendered a");
    await expect(link).toHaveText("Kex Hostel");
    await expect(link).toHaveAttribute(
      "href",
      `/trips/${tripId}/locations/${ids["Kex Hostel"]}`,
    );
  });

  test("a query with no @ prefix match still narrows, and two hits both show", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.click();

    await textarea.pressSequentially("@ke");
    await expect(options(page)).toHaveCount(2);
    // The category is the second line, so a list of similar names is
    // distinguishable.
    await expect(options(page).first().locator(".suggest__hint")).toHaveText("Stay");

    // A title with a space in it is reachable by typing the space.
    await textarea.fill("");
    await textarea.pressSequentially("@blue la");
    await expect(options(page)).toHaveText([/Blue Lagoon/]);
  });

  test("a bracketed title is escaped, so the link survives rendering", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.click();

    await textarea.pressSequentially("@museum");
    await expect(options(page)).toHaveText([/Museum \[north\]/]);
    await textarea.press("Enter");
    await expect(textarea).toHaveValue(
      `[Museum \\[north\\]](/trips/${tripId}/locations/${ids["Museum [north]"]})`,
    );

    // Through the real renderer: one anchor, brackets in the text, none left
    // over as literal markdown.
    await page.locator('.trip-notes__form button[type="submit"]').click();
    const rendered = page.locator(".trip-notes__rendered");
    await expect(rendered.locator("a")).toHaveCount(1);
    await expect(rendered.locator("a")).toHaveText("Museum [north]");
    await expect(rendered).not.toContainText("\\");
  });

  test("closes on Escape, on a third space, and on backspacing past the @", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.click();

    await textarea.pressSequentially("@kex");
    await expect(options(page)).toHaveCount(1);
    await textarea.press("Escape");
    await expect(options(page)).toHaveCount(0);
    // Escape closed the list and nothing else: the draft is untouched.
    await expect(textarea).toHaveValue("@kex");

    // A run of three spaces is prose, not a query.
    await textarea.fill("");
    await textarea.pressSequentially("@blue");
    await expect(options(page)).toHaveCount(1);
    await textarea.pressSequentially(" a b c");
    await expect(options(page)).toHaveCount(0);

    // Backspacing over the @ leaves no run to complete.
    await textarea.fill("");
    await textarea.pressSequentially("@ke");
    await expect(options(page)).toHaveCount(2);
    for (let i = 0; i < 3; i++) await textarea.press("Backspace");
    await expect(options(page)).toHaveCount(0);
  });

  test("an email address in a note does not open the list", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.click();

    // The @ is mid-word, so it is not a trigger.
    await textarea.pressSequentially("write to lars@kex");
    await expect(options(page)).toHaveCount(0);
  });

  test("the list follows the caret rather than the bottom of a tall field", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");

    // Enough text below the caret that the field is far taller than the list's
    // distance from it: with the stylesheet's `top: 100%` this assertion is
    // off by hundreds of pixels.
    await textarea.fill(`${"filler line\n".repeat(30)}end`);
    // Control+Home rather than a click: a click lands the caret wherever the
    // pointer fell, which is mid-word, and an @ mid-word is deliberately not a
    // trigger. The caret has to be somewhere known for this to measure
    // anything.
    await textarea.click();
    await page.keyboard.press("Control+Home");
    await textarea.pressSequentially("@kex");
    await expect(options(page)).toHaveCount(1);

    const list = await page.locator("#trip-notes-mentions").boundingBox();
    const field = await textarea.boundingBox();
    // `line-height: normal` computes as the string "normal" in Firefox, not a
    // length -- the same NaN the picker's caret measurement has to fall back
    // from, so the test falls back identically.
    const lineHeight = await textarea.evaluate((el) => {
      const cs = getComputedStyle(el);
      return parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.2;
    });

    // The caret is on the first line. The list is within a line or two of it,
    // and nowhere near the bottom of a field this tall -- which is exactly
    // where `top: 100%` would have put it.
    expect(
      Math.abs(list.y - field.y),
      "list sits at the caret's line, not at the foot of the field",
    ).toBeLessThan(lineHeight * 3);
    expect(
      field.height,
      "the field really is tall enough for this to mean something",
    ).toBeGreaterThan(400);
    expect(list.y, "nowhere near the bottom of the field").toBeLessThan(field.y + field.height / 2);
  });

  test("at 324px the list stays inside the viewport", async ({ page }) => {
    await page.setViewportSize(MOBILE);
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");

    // A caret at the very end of a long note, which the browser scrolls to the
    // bottom of the screen: the list has to flip above it to stay visible.
    await textarea.fill(`${"filler line\n".repeat(30)}`);
    await textarea.click();
    await page.keyboard.press("Control+End");
    await textarea.pressSequentially("@ke");
    await expect(options(page)).toHaveCount(2);

    const list = await page.locator("#trip-notes-mentions").boundingBox();
    expect(list.x, "not off the left edge").toBeGreaterThanOrEqual(0);
    expect(list.x + list.width, "not off the right edge").toBeLessThanOrEqual(MOBILE.width + 1);
    const inViewport = await page
      .locator("#trip-notes-mentions")
      .evaluate((el) => {
        const r = el.getBoundingClientRect();
        return r.top >= 0 && r.bottom <= window.innerHeight;
      });
    expect(inViewport, "fully inside the viewport").toBe(true);
  });
  test("a location link in a rendered note navigates without reloading", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.fill(
      [
        `Sleep: [Kex Hostel](/trips/${tripId}/locations/${ids["Kex Hostel"]})`,
        "",
        "Read: [OpenStreetMap](https://www.openstreetmap.org/)",
      ].join("\n"),
    );
    await page.locator('.trip-notes__form button[type="submit"]').click();

    const rendered = page.locator(".trip-notes__rendered");
    const internal = rendered.locator('a[href^="/trips/"]');
    const external = rendered.locator('a[href^="https://"]');

    // Marked for the router, and wearing a pin so it reads as a place rather
    // than as any other link in the same paragraph.
    await expect(internal).toHaveAttribute("data-link", "");
    await expect(internal.locator("svg.rendered-link__pin")).toHaveCount(1);
    // The external link is left exactly as the sanitizer produced it.
    await expect(external).not.toHaveAttribute("data-link", "");
    await expect(external.locator("svg")).toHaveCount(0);

    // A flag that only survives if the document is never replaced. This is the
    // whole assertion: a full page load would boot the app again and lose it.
    await page.evaluate(() => {
      window.__sameDocument = true;
    });
    await internal.click();

    await expect(page).toHaveURL(new RegExp(`/trips/${tripId}/locations/${ids["Kex Hostel"]}$`));
    await expect(page.locator(".location-view")).toBeVisible();
    expect(
      await page.evaluate(() => window.__sameDocument),
      "navigated client-side, without a page load",
    ).toBe(true);
  });

  test("a modified click is still the browser's to handle", async ({ page }) => {
    await page.goto(`/trips/${tripId}/notes`);
    const textarea = page.locator("#trip-notes-body");
    await textarea.fill(`[Kex Hostel](/trips/${tripId}/locations/${ids["Kex Hostel"]})`);
    await page.locator('.trip-notes__form button[type="submit"]').click();

    const link = page.locator('.trip-notes__rendered a[href^="/trips/"]');
    await expect(link).toHaveAttribute("data-link", "");

    // Ctrl+click asks for a new tab. The router must not swallow it -- and the
    // current page must stay where it is.
    const before = page.url();
    await link.click({ modifiers: ["ControlOrMeta"] });
    expect(page.url(), "the notes page did not navigate").toBe(before);
    await expect(page.locator(".trip-notes__rendered")).toBeVisible();
  });
});
