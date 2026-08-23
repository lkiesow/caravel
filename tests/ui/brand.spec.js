// The brand's shipping surface: the icon set, the self-hosted wordmark face,
// and the tokens both are declared with (Stage 18 Milestone 1).
//
// Every claim here is one a person cannot see going wrong. A favicon that 404s
// still leaves a working app with a blank tab; a font that falls back silently
// still renders every word; an icon served as application/octet-stream loads
// fine in the browser somebody tested in. So they are asserted rather than
// eyeballed.
//
// One caveat, since it is the kind of thing that reads as covered when it is
// not: the woff2 content type passes here whether or not router.go registers
// it, because Go's mime package falls back to the system /etc/mime.types and a
// developer machine has one. The explicit registration exists for the image,
// which does not - so this assertion is the guard for whatever environment the
// suite runs against, and Milestone 7 has to check the container itself.
import { test, expect } from "@playwright/test";
import { blockExternalRequests, APP_ORIGIN } from "./helpers/scenarios.js";

// Everything the browser and an installed app ask for, with the content type
// each must arrive as.
const ASSETS = [
  ["/icons/favicon.svg", "image/svg+xml"],
  ["/icons/favicon-32.png", "image/png"],
  ["/icons/favicon-16.png", "image/png"],
  ["/icons/apple-touch-icon.png", "image/png"],
  ["/icons/icon-192.png", "image/png"],
  ["/icons/icon-512.png", "image/png"],
  ["/icons/icon-maskable-512.png", "image/png"],
  ["/brand/og-card.png", "image/png"],
  ["/brand/mark.svg", "image/svg+xml"],
  ["/fonts/montserrat-500.woff2", "font/woff2"],
  ["/fonts/montserrat-700.woff2", "font/woff2"],
];

test.describe("brand assets", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("every icon, card and font is served with the right content type", async ({ page }) => {
    for (const [path, contentType] of ASSETS) {
      const res = await page.request.get(path);
      expect(res.status(), `${path} should be served`).toBe(200);
      expect(res.headers()["content-type"], `${path} content type`).toContain(contentType);
    }
  });

  test("the wordmark face loads from this instance and is not the fallback", async ({ page }) => {
    // Aborts anything off-origin, so a face that somehow came from a font CDN
    // would fail to load here rather than pass by accident.
    await blockExternalRequests(page);
    await page.goto("/");

    const result = await page.evaluate(async () => {
      const faces = await document.fonts.load("700 24px Montserrat", "CARAVEL Größe");
      const measure = (family) => {
        const ctx = document.createElement("canvas").getContext("2d");
        ctx.font = `700 24px ${family}`;
        // German umlauts and typographic punctuation on purpose: they are the
        // characters a too-narrow subset drops, and the app ships a German
        // locale.
        return ctx.measureText("CARAVEL Größe – „quotes“").width;
      };
      return {
        statuses: faces.map((f) => f.status),
        brand: measure("Montserrat"),
        fallback: measure("system-ui"),
      };
    });

    expect(result.statuses, "the 700 face should resolve").toEqual(["loaded"]);
    // If the subset were missing the umlauts or the face had not loaded at all,
    // the browser would fall back and the two would measure the same.
    expect(
      Math.abs(result.brand - result.fallback),
      "Montserrat measures the same as the fallback, so it did not actually apply"
    ).toBeGreaterThan(1);
  });

  test("the page declares the icons, the social card and the brand tokens", async ({ page }) => {
    await blockExternalRequests(page);
    await page.goto("/");

    await expect(page.locator('link[rel="icon"][type="image/svg+xml"]')).toHaveAttribute(
      "href",
      "/icons/favicon.svg"
    );
    await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", /og-card\.png$/);

    // The description is one string in three places (the meta tag, og, and the
    // manifest); they drifted apart before this stage and the tag is the one a
    // reader sees.
    const description = await page.locator('meta[name="description"]').getAttribute("content");
    const og = await page.locator('meta[property="og:description"]').getAttribute("content");
    expect(og).toBe(description);

    const manifest = await page.request.get("/manifest.webmanifest");
    const parsed = await manifest.json();
    expect(parsed.description).toBe(description);

    // The chrome colour is written in two files and means one thing, so it can
    // drift silently: the installed app would frame in one colour and the
    // browser tab tint in another. Navy is the brand's, deliberately not the
    // in-app accent.
    const themeColor = await page.locator('meta[name="theme-color"]').getAttribute("content");
    expect(themeColor.toLowerCase()).toBe("#23304f");
    expect(parsed.theme_color.toLowerCase()).toBe(themeColor.toLowerCase());
    expect(parsed.background_color.toLowerCase()).toBe("#faf7f2");
  });

  test("brand ink resolves per theme", async ({ page }) => {
    await blockExternalRequests(page);
    await page.goto("/");

    // Navy on light, the lightened navy on dark: the token exists so callers
    // never pick between the two, which means it has to actually change.
    const inkFor = (theme) =>
      page.evaluate((t) => {
        document.documentElement.dataset.theme = t;
        return getComputedStyle(document.documentElement).getPropertyValue("--brand-ink").trim();
      }, theme);

    expect(await inkFor("light")).toBe("#23304f");
    expect(await inkFor("dark")).toBe("#5470a8");
  });

  test("nothing on the front door is fetched from a third party", async ({ page }) => {
    const offOrigin = [];
    page.on("request", (request) => {
      const origin = new URL(request.url()).origin;
      if (origin !== APP_ORIGIN && !request.url().startsWith("data:")) offOrigin.push(request.url());
    });

    // No blockExternalRequests here on purpose: that helper would abort the
    // very requests this test exists to notice.
    await page.goto("/");
    await page.locator(".auth-form").waitFor();

    expect(offOrigin, `the login screen fetched ${offOrigin.length} off-origin URL(s)`).toEqual([]);
  });
});

// The header lockup and the login hero (Stage 18 Milestone 2). The hero is the
// app's front door, so what is asserted here is what a stranger sees: the
// headline, the mark behind it being decorative, and the brand face actually
// reaching the two places that are branding rather than chrome.
test.describe("the login hero", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  for (const theme of ["light", "dark"]) {
    test(`renders the lockup, headline and watermark (${theme})`, async ({ page }) => {
      await blockExternalRequests(page);
      await page.addInitScript((t) => localStorage.setItem("caravel.theme", t), theme);
      await page.goto("/");

      // One h1, and it is the hero's headline - the form's own title is an h2,
      // so the page has a single top-level heading rather than two competing
      // ones.
      const h1 = page.locator("h1");
      await expect(h1).toHaveCount(1);
      await expect(h1).toHaveClass(/auth-hero__title/);
      await expect(h1).not.toBeEmpty();
      await expect(page.locator(".auth-form h2")).not.toBeEmpty();

      // The mark appears twice - in the lockup and as the watermark - and
      // neither may contribute an accessible name, since the wordmark beside
      // it already says "Caravel". A screen reader announcing the app three
      // times is the failure this prevents.
      const marks = page.locator(".auth-hero .brand-mark");
      await expect(marks).toHaveCount(2);
      for (let i = 0; i < 2; i++) {
        await expect(marks.nth(i)).toHaveAttribute("aria-hidden", "true");
      }

      const rendered = await page.evaluate((expectedTheme) => {
        const title = document.querySelector(".auth-hero__title");
        const wordmark = document.querySelector(".app-brand--hero .app-brand__wordmark");
        const watermark = document.querySelector(".auth-hero__watermark");
        const wm = getComputedStyle(watermark);
        return {
          theme: document.documentElement.dataset.theme,
          expectedTheme,
          titleFont: getComputedStyle(title).fontFamily,
          wordmarkFont: getComputedStyle(wordmark).fontFamily,
          wordmarkTracking: getComputedStyle(wordmark).letterSpacing,
          // The mask is what paints the mark, and it takes its colour from
          // background-color: a missing mask would leave a filled rectangle.
          watermarkMask: wm.maskImage || wm.webkitMaskImage,
          watermarkOpacity: Number(wm.opacity),
          watermarkWidth: watermark.getBoundingClientRect().width,
        };
      }, theme);

      expect(rendered.theme).toBe(theme);
      expect(rendered.titleFont).toContain("Montserrat");
      expect(rendered.wordmarkFont).toContain("Montserrat");
      // The tracked wordmark from the brand rules (~0.17em at 18px).
      expect(parseFloat(rendered.wordmarkTracking)).toBeGreaterThan(2);
      expect(rendered.watermarkMask).toContain("mark.svg");
      // Decorative, so it must stay faint - and present, so it must not be
      // invisible either. Both directions have been wrong during this
      // milestone: the mark's own `width: 1em` first collapsed it to 16px.
      expect(rendered.watermarkOpacity).toBeGreaterThan(0.05);
      expect(rendered.watermarkOpacity).toBeLessThan(0.6);
      expect(rendered.watermarkWidth).toBeGreaterThan(100);
    });
  }
});

test.describe("the app header", () => {
  test("carries the lockup and links home", async ({ page }) => {
    await blockExternalRequests(page);
    await page.goto("/trips");

    const brand = page.locator(".app-header .app-brand");
    // The link's accessible name comes from the wordmark alone: the mark is
    // aria-hidden, so this is "Caravel" and not "Caravel Caravel".
    await expect(brand).toHaveAccessibleName("Caravel");
    await expect(brand).toHaveAttribute("href", "/trips");
    await expect(page.locator(".app-header .brand-mark")).toHaveAttribute("aria-hidden", "true");

    const font = await page.evaluate(
      () => getComputedStyle(document.querySelector(".app-brand__wordmark")).fontFamily
    );
    expect(font).toContain("Montserrat");
  });

  test("navigates without a full page load", async ({ page }) => {
    await blockExternalRequests(page);
    await page.goto("/settings");
    // data-link hands the click to the router; without it the browser would
    // reload the whole app, which is slower and loses in-page state. Proven by
    // the document surviving the navigation.
    await page.evaluate(() => {
      window.__stillTheSameDocument = true;
    });
    await page.locator(".app-header .app-brand").click();
    await expect(page).toHaveURL(/\/trips$/);
    expect(await page.evaluate(() => window.__stillTheSameDocument)).toBe(true);
  });
});
