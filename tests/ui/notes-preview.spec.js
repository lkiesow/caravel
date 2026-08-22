// The location notes field's Write/Preview toggle (Stage 15 Milestone 3).
//
// Writes nothing: it types into the new-location form and never saves, so the
// shared seed is untouched and this spec needs none of files.spec.js's
// create-own-trip isolation. The one thing it must not do is press Save.
//
// What it is really asserting is that the preview is *server-rendered*: the
// markup it checks for (a real <h1>, a real <strong>, a hard-wrap <br>) is
// goldmark's output, and a client-side renderer that happened to disagree would
// show up here. The stronger form of that claim - that the preview matches the
// saved item byte for byte - is a Go test, since it needs two API responses to
// compare.
import { test, expect } from "@playwright/test";
import { login, gotoRoute, resolveScenarioTrips } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

const COPY = {
  en: {
    write: "Write",
    preview: "Preview",
    empty: "Nothing to preview yet — the note is empty.",
  },
  de: {
    write: "Schreiben",
    preview: "Vorschau",
    empty: "Nichts in der Vorschau – die Notiz ist leer.",
  },
};

const SOURCE =
  "# Day one\n\nDrive to **Vik**, then:\n\n- check in\n- eat\n\nsoft wrap\nsecond line";

for (const locale of ["en", "de"]) {
  test.describe(`location notes preview (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });

    test(`renders the markdown server-side and keeps the source (${locale})`, async ({
      page,
    }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      await gotoRoute(page, `/trips/${trips.full}/locations/new`);

      const field = page.locator(".notes-field");
      const notes = field.locator("textarea");
      const preview = field.locator(".notes-field__preview");
      const writeTab = field.getByRole("button", { name: COPY[locale].write });
      const previewTab = field.getByRole("button", {
        name: COPY[locale].preview,
      });

      // Starts in Write, and says so rather than leaving both tabs unpressed.
      await expect(writeTab).toHaveAttribute("aria-pressed", "true");
      await expect(previewTab).toHaveAttribute("aria-pressed", "false");
      await expect(notes).toBeVisible();
      await expect(preview).toBeHidden();

      await notes.fill(SOURCE);
      // Captured before the switch, so the two positions can be compared.
      const textareaTop = await notes.evaluate((el) =>
        Math.round(el.getBoundingClientRect().top),
      );
      await previewTab.click();

      await expect(preview).toBeVisible();
      await expect(notes).toBeHidden();
      await expect(previewTab).toHaveAttribute("aria-pressed", "true");

      // Real elements, not escaped text: this is what says the round trip
      // happened and the HTML was inserted rather than printed.
      await expect(preview.locator("h1")).toHaveText("Day one");
      await expect(preview.locator("strong")).toHaveText("Vik");
      await expect(preview.locator("li")).toHaveText(["check in", "eat"]);
      // Hard wraps are internal/markdown's deliberate deviation from
      // CommonMark. Without them every multi-line note would preview as one
      // run-on paragraph and disagree with the view page.
      await expect(preview.locator("br")).toHaveCount(1);

      // The preview stands exactly where the textarea stood, so switching mode
      // does not shift the rest of the form under the reader.
      const previewTop = await preview.evaluate((el) =>
        Math.round(el.getBoundingClientRect().top),
      );
      expect(
        Math.abs(previewTop - textareaTop),
        "the preview should replace the textarea in place",
      ).toBeLessThanOrEqual(1);

      // Back to Write: the source survived the trip, untouched.
      await writeTab.click();
      await expect(notes).toBeVisible();
      await expect(preview).toBeHidden();
      await expect(notes).toHaveValue(SOURCE);
      await expect(writeTab).toHaveAttribute("aria-pressed", "true");
    });

    test(`says so instead of previewing an empty note (${locale})`, async ({
      page,
    }) => {
      await login(page);
      const trips = await resolveScenarioTrips(page);
      await gotoRoute(page, `/trips/${trips.full}/locations/new`);

      const field = page.locator(".notes-field");
      // Whitespace only, not merely empty: trimmed, this is still nothing to
      // render, and previewing it would show a blank box that reads like a
      // note whose markdown produced no output.
      await field.locator("textarea").fill("   ");

      // Counted rather than trusted: an empty note must cost no round trip.
      const requests = [];
      page.on("request", (req) => {
        if (req.url().includes("/api/markdown/preview"))
          requests.push(req.url());
      });

      await field.getByRole("button", { name: COPY[locale].preview }).click();
      await expect(field.locator(".notes-field__empty")).toHaveText(
        COPY[locale].empty,
      );
      await expect(field.locator(".notes-field__preview")).toBeHidden();
      expect(
        requests,
        "an empty note should not be sent to the server",
      ).toHaveLength(0);
    });
  });
}

test.describe("location notes preview: one request per change", () => {
  test.use({ viewport: MOBILE });

  test("does not re-render text it has already previewed", async ({ page }) => {
    await login(page);
    const trips = await resolveScenarioTrips(page);
    await gotoRoute(page, `/trips/${trips.full}/locations/new`);

    const field = page.locator(".notes-field");
    const requests = [];
    page.on("request", (req) => {
      if (req.url().includes("/api/markdown/preview")) requests.push(req.url());
    });

    const writeTab = field.getByRole("button", { name: COPY.en.write });
    const previewTab = field.getByRole("button", { name: COPY.en.preview });

    await field.locator("textarea").fill("## First");
    await previewTab.click();
    await expect(field.locator(".notes-field__preview h2")).toHaveText("First");
    expect(requests).toHaveLength(1);

    // Toggling twice over unchanged text: still one request in total.
    await writeTab.click();
    await previewTab.click();
    await expect(field.locator(".notes-field__preview h2")).toHaveText("First");
    expect(
      requests,
      "unchanged text should be served from the last render",
    ).toHaveLength(1);

    // Changed text: exactly one more.
    await writeTab.click();
    await field.locator("textarea").fill("## Second");
    await previewTab.click();
    await expect(field.locator(".notes-field__preview h2")).toHaveText(
      "Second",
    );
    expect(requests).toHaveLength(2);
  });
});
