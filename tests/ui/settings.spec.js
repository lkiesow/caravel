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
import { login, gotoRoute, resolveScenarioTrips, OTHER_USER } from "./helpers/scenarios.js";

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

test.describe("appearance: the choices lay out sanely at both widths", () => {
  test("share one row on desktop and stack full-width on a phone", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await login(page);
    await gotoRoute(page, "/settings");

    const choices = page.locator(".setting-choice");
    await expect(choices).toHaveCount(3);
    const boxes = async () => Promise.all((await choices.all()).map((c) => c.boundingBox()));

    // Desktop: one row - three different x positions, one shared y.
    const wide = await boxes();
    expect(new Set(wide.map((b) => Math.round(b.y))).size, "desktop rows").toBe(1);

    // Phone: the reverse. Each choice gets its own line at full card width,
    // rather than being sized by its own label - which is how this first
    // rendered, with "Follow my device" alone on line one and Light + Dark
    // sharing line two.
    await page.setViewportSize({ width: 324, height: 756 });
    const narrow = await boxes();
    expect(new Set(narrow.map((b) => Math.round(b.y))).size, "phone rows").toBe(3);
    const widths = narrow.map((b) => Math.round(b.width));
    expect(new Set(widths).size, `phone widths ${widths.join("/")}`).toBe(1);
    const group = await page.locator(".setting-choices").boundingBox();
    expect(widths[0], "choice fills the group's width").toBe(Math.round(group.width));
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

// The Language control (Stage 12 Milestone 4), and the app-wide re-render it
// needs. setLocale() has existed since Stage 01 with no callers; translatePage
// only rewrites declarative data-i18n attributes, so everything built through
// t() in JS would keep the old language until the next navigation. The
// assertions below deliberately include one of those strings.
const LOCALE_KEY = "caravel.locale";

const storedLocale = (page) => page.evaluate((key) => localStorage.getItem(key), LOCALE_KEY);
const languageTrigger = (page) => page.locator(".language-slot .menu__trigger");

async function pickLanguage(page, label) {
  await languageTrigger(page).click();
  await page.locator(`.language-slot .menu__dropdown [role="menuitemradio"]`).filter({ hasText: label }).click();
}

test.describe("language: the dropdown opens where the trigger is", () => {
  test("aligns under its trigger rather than at the card's far edge", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await login(page);
    await gotoRoute(page, "/settings");

    const trigger = languageTrigger(page);
    const dropdown = page.locator(".language-slot .menu__dropdown");
    await trigger.click();
    await expect(dropdown).toBeVisible();

    const t = await trigger.boundingBox();
    const d = await dropdown.boundingBox();

    // This is the whole bug: `.menu` is a flex child everywhere else in the app
    // and shrink-wraps, but in a settings card it was a plain block spanning
    // the card, so `right: 0` put the popup against the card's right edge -
    // ~800px from the trigger on a wide screen. Left edges within a pixel, and
    // directly below.
    expect(Math.abs(d.x - t.x), `dropdown x ${d.x} vs trigger x ${t.x}`).toBeLessThan(2);
    expect(d.y, "dropdown sits below the trigger").toBeGreaterThanOrEqual(t.y + t.height - 1);
    expect(d.y - (t.y + t.height), "and close below it").toBeLessThan(12);
  });
});

test.describe("settings: a way back out", () => {
  test("links to the trips list", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");

    // The screen is reached from the header menu, so nothing else on it
    // navigates anywhere - without this link it was a dead end.
    const back = page.locator(".settings-page .back-link");
    await expect(back).toBeVisible();
    await back.click();
    await expect(page).toHaveURL("/trips");
  });
});

test.describe("language: switching to German", () => {
  // An English browser, so "Automatic" and "English" are two distinct rows that
  // happen to resolve to the same locale - the case the old setLocale's
  // "already active, bail out" guard made unreachable.
  test.use({ locale: "en-GB" });

  test("re-translates the whole app, including strings built in JS", async ({ page }) => {
    await login(page);
    const trips = await resolveScenarioTrips(page);
    await gotoRoute(page, "/settings");

    // Auto is the default, and it says what it resolved to rather than just
    // "Automatic".
    await expect(languageTrigger(page)).toHaveText("Automatic (English)");
    expect(await storedLocale(page)).toBeNull();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    await pickLanguage(page, "Deutsch");

    // Declarative copy, the <html lang>, and the stored preference.
    await expect(page.locator("html")).toHaveAttribute("lang", "de");
    await expect(page.locator("h1")).toHaveText("Kontoeinstellungen");
    expect(await storedLocale(page)).toBe("de");
    // The control itself is rebuilt by the re-render, and the Auto row is now
    // German too - which is only true if t() ran again, not just translatePage.
    await expect(languageTrigger(page)).toHaveText("Deutsch");

    // The assertions that catch a missing re-render, and they have to be about
    // what is *already on screen*: a fresh page load or an in-app navigation
    // renders every t() call against the new locale anyway, so asserting after
    // one proves nothing about the listener. These two are strings composed in
    // JS by t() in markup that was built before the language changed:
    //
    //   - the route: the Auto row's label, whose *translated* half must follow
    //     ("Automatic" -> "Automatisch") while the resolved language stays
    //     English, because this browser's language is what Auto follows;
    //   - the header: the user menu's items, which live outside the router
    //     entirely and are the reason the listener re-renders it too.
    await languageTrigger(page).click();
    await expect(
      page.locator('.language-slot .menu__dropdown [role="menuitemradio"]').first()
    ).toContainText("Automatisch (English)");
    await page.keyboard.press("Escape");

    const userMenu = page.locator(".user-menu-slot");
    await userMenu.locator(".menu__trigger").click();
    await expect(userMenu.locator('[role="menuitem"]').first()).toContainText("Kontoeinstellungen");
    await expect(userMenu.locator('[role="menuitem"]').last()).toContainText("Abmelden");
    await page.keyboard.press("Escape");

    // And the rest of the app follows on the next navigation - the locations
    // filter's trigger is another t()-built label.
    await gotoRoute(page, `/trips/${trips.full}/locations`);
    await expect(page.locator(".locations-toolbar .menu__trigger")).toContainText("Alle");

    // German survives a reload, from storage rather than from the browser.
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("lang", "de");
  });
});

test.describe("language: back to Auto", () => {
  // A German browser this time, so Auto resolving correctly is visible: picking
  // English and then Auto has to land back on German.
  test.use({ locale: "de-DE" });

  test("clears the stored preference and follows the browser again", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");

    await expect(page.locator("html")).toHaveAttribute("lang", "de");
    await expect(languageTrigger(page)).toHaveText("Automatisch (Deutsch)");

    await pickLanguage(page, "English");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.locator("h1")).toHaveText("Account settings");
    expect(await storedLocale(page)).toBe("en");

    // Back to Auto: German returns *and* the key is gone. A stored "auto"
    // string would make this row silently sticky.
    // "Automatic (Deutsch)" in English copy: the language is always named in
    // its own language (LOCALE_NAMES), which is the point of that map.
    await pickLanguage(page, "Automatic (Deutsch)");
    await expect(page.locator("html")).toHaveAttribute("lang", "de");
    await expect(page.locator("h1")).toHaveText("Kontoeinstellungen");
    expect(await storedLocale(page)).toBeNull();
  });
});

// Changing a password, end to end (Stage 12 Milestone 5) - a genuinely mutating
// flow, and the awkward kind: it cannot use the demo user, because the change
// deletes every session that user has and auth.setup.js's shared session state
// is one of them. So it drives the *other* seeded account and puts its password
// back afterwards, the same "leave the seed as you found it" contract
// files.spec.js keeps by deleting the trip it created.
const TEMP_PASSWORD = "temporary-password-1";

// Whether the test got as far as actually changing the password, so afterEach
// knows what to restore without spending a login probing for it.
//
// Login attempts are a budget here, not free: /api/auth/login and
// /api/auth/password share one limiter (10 per minute per IP, router.go), and
// this spec, its cleanup and auth.setup.js all draw on it. Overspending shows
// up as a 429 - which is how the first version of this cleanup failed silently
// and left the seeded password wrong.
let leftTempPassword = false;

// Logs in as the other user in this context, by API rather than through the
// login form - the form is not what is under test here.
async function loginAs(page, password) {
  const res = await page.request.post("/api/auth/login", {
    data: { username: OTHER_USER.username, password },
  });
  return res.status();
}

test.describe("password change", () => {
  test.afterEach(async ({ page }) => {
    // Put the seed back from whichever password the test left behind, so a
    // failure half-way through doesn't leave the account broken for the next
    // run - and *assert* that it worked. The first version of this restored
    // silently, so when it didn't work (the change POST is rate limited like
    // login, and nothing checked its status) the seed's documented password
    // quietly stopped being true, which cost a debugging session. A noisy
    // failure here points straight at `make dev-seed`, which since Stage 12
    // Milestone 6 resets passwords instead of leaving an existing user's alone.
    if (!leftTempPassword) return;

    expect(
      await loginAs(page, TEMP_PASSWORD),
      `${OTHER_USER.username} should still be on this spec's temporary password — run \`make dev-seed\``
    ).toBe(200);
    const res = await page.request.post("/api/auth/password", {
      data: { current_password: TEMP_PASSWORD, new_password: OTHER_USER.password },
    });
    expect(
      res.status(),
      `could not restore the seeded password for ${OTHER_USER.username} (HTTP ${res.status()}) — run \`make dev-seed\``
    ).toBe(200);
    leftTempPassword = false;
  });

  test("rejects a wrong current password, then changes it and logs other devices out", async ({
    page,
    browser,
  }) => {
    await login(page, OTHER_USER);
    await gotoRoute(page, "/settings");

    // A second device for the same account, to prove the change reaches it.
    const otherDevice = await browser.newContext();
    const otherPage = await otherDevice.newPage();
    expect(
      (
        await otherPage.request.post("/api/auth/login", {
          data: { username: OTHER_USER.username, password: OTHER_USER.password },
        })
      ).status()
    ).toBe(200);

    const form = page.locator(".password-form");
    // The card is only rendered for an account that has a password at all
    // (/auth/me's has_password), so its presence is itself an assertion.
    await expect(form).toBeVisible();
    const error = page.locator(".password-form__error");
    const success = page.locator(".password-form__success");

    // 1. Mistyped confirmation: caught client-side, nothing sent.
    await form.locator('input[name="current"]').fill(OTHER_USER.password);
    await form.locator('input[name="next"]').fill(TEMP_PASSWORD);
    await form.locator('input[name="confirm"]').fill("something-else-entirely");
    await form.locator('button[type="submit"]').click();
    await expect(error).toBeVisible();
    await expect(success).toBeHidden();

    // 2. Too short, also client-side.
    await form.locator('input[name="next"]').fill("short");
    await form.locator('input[name="confirm"]').fill("short");
    await form.locator('button[type="submit"]').click();
    await expect(error).toBeVisible();
    await expect(success).toBeHidden();

    // 3. Wrong current password: this one reaches the server, which answers 401
    // - and that must read as "wrong password", not as "you are logged out".
    await form.locator('input[name="current"]').fill("not-my-password");
    await form.locator('input[name="next"]').fill(TEMP_PASSWORD);
    await form.locator('input[name="confirm"]').fill(TEMP_PASSWORD);
    await form.locator('button[type="submit"]').click();
    await expect(error).toBeVisible();
    await expect(success).toBeHidden();
    await expect(page).toHaveURL("/settings");
    // That the password is *unchanged* after each rejected attempt is asserted
    // server-side in internal/httpapi/password_test.go, where it costs no
    // login attempts against the limiter.

    // 4. The real thing.
    await form.locator('input[name="current"]').fill(OTHER_USER.password);
    await form.locator('input[name="next"]').fill(TEMP_PASSWORD);
    await form.locator('input[name="confirm"]').fill(TEMP_PASSWORD);
    await form.locator('button[type="submit"]').click();
    await expect(success).toBeVisible();
    await expect(error).toBeHidden();
    // The form clears, so the old value can't be resubmitted by accident.
    await expect(form.locator('input[name="current"]')).toHaveValue("");
    leftTempPassword = true;

    // The browser that made the change is still logged in - the endpoint
    // re-issues its session, which is the whole reason it has to.
    await gotoRoute(page, "/trips");
    await expect(page.locator("h1")).toBeVisible();

    // The other device is not.
    expect((await otherPage.request.get("/api/auth/me")).status()).toBe(401);
    await otherDevice.close();

    // The old password is gone. That the new one works is asserted by
    // afterEach, which has to log in with it anyway to put the seed back.
    expect(await loginAs(page, OTHER_USER.password)).toBe(401);
  });
});

// The settings screen in German at phone width.
//
// The suite's route sweeps (routes.spec.js) run in one locale, and German is the
// longer language - the case most likely to overflow a box or shrink a control.
// This screen is the stage's new surface and its copy is the longest in the app
// ("Passwort geändert. Deine anderen Geräte wurden abgemeldet."), so it gets the
// sweep in the other locale rather than waiting on the suite-wide version
// todo.md still asks for.
test.describe("settings in German at 324px", () => {
  test.use({ locale: "de-DE", viewport: { width: 324, height: 756 } });

  test("neither overflows nor shrinks a control below the tap floor", async ({ page }) => {
    await login(page, OTHER_USER);
    await gotoRoute(page, "/settings");
    await expect(page.locator("h1")).toHaveText("Kontoeinstellungen");
    // The longest string on the screen only renders once a change succeeds, so
    // it is unhidden here rather than left unmeasured - the box has to hold it.
    await page.locator(".password-form__success").evaluate((el) => {
      el.hidden = false;
    });

    const result = await page.evaluate(() => {
      const doc = document.documentElement;
      const controls = [...document.querySelectorAll(".settings-page button, .settings-page a, .settings-page input, .settings-page label")];
      const small = [];
      for (const el of controls) {
        // Same exclusion the sweep makes: a native radio is ~14px and the
        // label around it is the real target (see routes.spec.js).
        if (el.localName === "input" && el.type === "radio") continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        if (r.height < 44 || r.width < 44) {
          small.push(`${el.localName}.${el.className} ${Math.round(r.width)}x${Math.round(r.height)}`);
        }
      }
      // Anything sticking out past the viewport, measured per element so the
      // failure names the offender rather than just the page.
      const wide = [...document.querySelectorAll(".settings-page *")]
        .filter((el) => el.getBoundingClientRect().right > window.innerWidth + 1)
        .map((el) => `${el.localName}.${el.className}`);
      return { overflow: doc.scrollWidth - window.innerWidth, small, wide };
    });

    expect(result.overflow, "document overflow in px").toBeLessThanOrEqual(0);
    expect(result.wide, "elements past the right edge").toEqual([]);
    expect(result.small, "controls under 44px").toEqual([]);
  });
});
