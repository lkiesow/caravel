// The About dialog (Stage 50): from the user menu, it says which build is
// running and where the project lives.
//
// The version is checked against what /api/auth/me reports, not a literal: the
// claim is that the dialog shows the server's answer, whatever it is. The
// suite's own server (scripts/with_server.sh) is built without -ldflags, so
// that answer is "dev" with no URL - which is why both shapes are also forced
// through an intercepted /auth/me: otherwise the linked branch would only ever
// be seen by hand.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };
const MIN_TAP_TARGET_PX = 44;

const COPY = {
  en: { title: "About Caravel", version: "Version", links: ["Website & documentation", "Source code on GitHub", "Report a problem"] },
  de: { title: "Über Caravel", version: "Version", links: ["Website & Dokumentation", "Quellcode auf GitHub", "Problem melden"] },
};

const LINK_HREFS = [
  "https://lkiesow.github.io/caravel/",
  "https://github.com/lkiesow/caravel",
  "https://github.com/lkiesow/caravel/issues/new",
];

async function openAbout(page, title) {
  await page.locator(".user-menu-slot .menu__trigger").click();
  await page.getByRole("menuitem", { name: title }).click();
  const dialog = page.getByRole("dialog", { name: title });
  await expect(dialog).toBeVisible();
  return dialog;
}

// Rewrites the build on /auth/me and reloads, so the app boots on it. After
// login() rather than before: login() installs a catch-all route of its own
// (blockExternalRequests), and the route registered last is the one that runs.
async function forceBuild(page, build) {
  await page.route("**/api/auth/me", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    await route.fulfill({ response, json: { ...body, build } });
  });
  await page.reload();
}

for (const locale of ["en", "de"]) {
  test.describe(`about dialog (${locale})`, () => {
    test.use({ viewport: MOBILE, locale });
    const copy = COPY[locale];

    test(`shows the running build and the project links (${locale})`, async ({ page }) => {
      await login(page);
      await gotoRoute(page, "/trips");
      const { build } = await (await page.request.get("/api/auth/me")).json();
      expect(build.version, "/auth/me reports a version").toBeTruthy();

      const dialog = await openAbout(page, copy.title);

      // The lockup is the first thing seen; "About Caravel" is the name only.
      await expect(dialog.locator(".about-dialog__brand")).toBeVisible();
      await expect(dialog.locator(".dialog__message")).toHaveClass(/sr-only/);

      const version = dialog.locator(".about-dialog__version");
      await expect(version).toHaveText(`${copy.version} ${build.version}`);
      await expect(version.locator("a")).toHaveCount(build.url ? 1 : 0);

      const links = dialog.locator(".about-dialog__link");
      await expect(links).toHaveText(copy.links);
      for (const [i, href] of LINK_HREFS.entries()) {
        await expect(links.nth(i)).toHaveAttribute("href", href);
      }
      // Every link out opens a new tab and hands the new page no handle back.
      for (const a of await dialog.locator("a").all()) {
        await expect(a).toHaveAttribute("target", "_blank");
        await expect(a).toHaveAttribute("rel", "noopener noreferrer");
      }

      // Close, not the first link, takes focus: Enter must not open a tab.
      await expect(dialog.locator(".dialog__actions button")).toBeFocused();

      // A standalone line, so a control: it clears the tap floor.
      for (const link of await links.all()) {
        const box = await link.boundingBox();
        expect(box.height, "about link height").toBeGreaterThanOrEqual(MIN_TAP_TARGET_PX);
      }

      // Fits the phone: the dialog inside the viewport, no page overflow.
      const box = await dialog.boundingBox();
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(MOBILE.width);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, "horizontal overflow with the about dialog open").toBeLessThanOrEqual(0);

      await page.keyboard.press("Escape");
      await expect(page.locator("dialog")).toHaveCount(0);
    });
  });
}

test.describe("about dialog version link", () => {
  test.use({ viewport: MOBILE, locale: "en" });

  test("links a build the server can locate", async ({ page }) => {
    const url = "https://github.com/lkiesow/caravel/releases/tag/v1.4.0";
    await login(page);
    await forceBuild(page, { version: "1.4.0", url });
    await gotoRoute(page, "/trips");

    const version = (await openAbout(page, COPY.en.title)).locator(".about-dialog__version");
    await expect(version.getByRole("link", { name: "1.4.0" })).toHaveAttribute("href", url);
  });

  test("leaves a build it cannot locate unlinked", async ({ page }) => {
    await login(page);
    await forceBuild(page, { version: "284e68a-dirty", url: "" });
    await gotoRoute(page, "/trips");

    const version = (await openAbout(page, COPY.en.title)).locator(".about-dialog__version");
    await expect(version).toHaveText("Version 284e68a-dirty");
    await expect(version.locator("a")).toHaveCount(0);
  });
});
