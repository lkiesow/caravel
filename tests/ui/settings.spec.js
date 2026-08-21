// The account settings screen's controls (Stage 12).
//
// These are mutating flows in the sense that matters here - they write to
// localStorage and change what every page looks like - but they touch no
// database rows, so unlike files.spec.js they need no per-test trip and no
// cleanup. Each test starts from a fresh browser context, which is where the
// preference lives, so they can't leak into each other either.
//
// Assertions are on computed styles and on the stored value rather than on
// screenshots: "the background actually changed" is the claim, and a matching
// screenshot would prove it only for as long as nobody regenerates it.
import { test, expect } from "@playwright/test";
import { login, gotoRoute } from "./helpers/scenarios.js";

const STORAGE_KEY = "caravel.theme";

// The two palettes' page background, from base.css's :root / [data-theme=dark].
const LIGHT_BG = "rgb(255, 255, 255)";
const DARK_BG = "rgb(24, 24, 27)";

const choice = (page, value) => page.locator(`.setting-choice input[value="${value}"]`);
const bodyBackground = (page) => page.evaluate(() => getComputedStyle(document.body).backgroundColor);
const storedTheme = (page) => page.evaluate((key) => localStorage.getItem(key), STORAGE_KEY);

test.describe("appearance: dark chosen on a light device", () => {
  test.use({ colorScheme: "light" });

  test("overrides the OS, persists across a reload, and hands control back on Auto", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");

    // Auto is the default, and it resolves to the emulated OS scheme - the
    // attribute is always one of light/dark, never the word "auto".
    await expect(choice(page, "auto")).toBeChecked();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await bodyBackground(page)).toBe(LIGHT_BG);
    expect(await storedTheme(page)).toBeNull();

    // Dark on a light device: the whole point of the control.
    await choice(page, "dark").check();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await bodyBackground(page)).toBe(DARK_BG);
    expect(await storedTheme(page)).toBe("dark");

    // Survives a reload, and the control comes back showing the right row.
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await bodyBackground(page)).toBe(DARK_BG);
    await expect(choice(page, "dark")).toBeChecked();

    // Back to Auto: the OS wins again *and* the key is gone. A lingering
    // "auto" string in storage would make this row silently sticky.
    await choice(page, "auto").check();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await bodyBackground(page)).toBe(LIGHT_BG);
    expect(await storedTheme(page)).toBeNull();
  });
});

test.describe("appearance: light chosen on a dark device", () => {
  test.use({ colorScheme: "dark" });

  test("overrides the OS in the other direction too", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");

    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await bodyBackground(page)).toBe(DARK_BG);

    await choice(page, "light").check();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await bodyBackground(page)).toBe(LIGHT_BG);
    expect(await storedTheme(page)).toBe("light");

    // The theme is the app's, not the settings page's: it has to hold on every
    // route, including one rendered after the choice was made.
    await gotoRoute(page, "/trips");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await bodyBackground(page)).toBe(LIGHT_BG);
  });
});

test.describe("appearance: Auto follows the device live", () => {
  test.use({ colorScheme: "light" });

  test("switches with the OS while the tab stays open", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");
    await expect(choice(page, "auto")).toBeChecked();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

    // CSS used to get this for free: with the OS flipping at sunset and the tab
    // already open, an app that only reads the preference at boot would sit
    // there in the wrong theme until reloaded. theme.js's matchMedia listener
    // is what keeps it honest, and this is the only test that touches it.
    await page.emulateMedia({ colorScheme: "dark" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await bodyBackground(page)).toBe(DARK_BG);

    // ...but only while the preference is Auto. An explicit choice must not be
    // undone by the OS.
    await choice(page, "light").check();
    await page.emulateMedia({ colorScheme: "dark" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await bodyBackground(page)).toBe(LIGHT_BG);
  });
});

test.describe("appearance: no flash of the wrong theme", () => {
  test.use({ colorScheme: "light" });

  test("index.html applies the stored theme without any app JS", async ({ page }) => {
    await page.addInitScript(
      ([key]) => localStorage.setItem(key, "dark"),
      [STORAGE_KEY]
    );

    // The app's own modules never load, so the inline <head> script in
    // index.html is the only thing that can have set the attribute. That is
    // exactly the pre-paint path a real load takes, and the reason a
    // dark-themed app doesn't flash white on the way in.
    await page.route("**/js/**", (route) => route.abort());
    await page.goto("/");

    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await bodyBackground(page)).toBe(DARK_BG);
  });
});
