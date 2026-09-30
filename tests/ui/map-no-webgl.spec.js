// What a map does in a browser that cannot give it a WebGL2 context.
//
// MapLibre v6 needs one, and the component has always meant to say so rather
// than to leave a blank rectangle -- `map.unavailable` has been in every locale
// file since Stage 30. It could not, though: the guard was a try/catch around
// the constructor, and MapLibre does not throw when the context fails. It emits
// an `error` event and returns a half-built Map, so the first property access
// after it threw an uncaught TypeError, render() died there, and the `load`
// handler that sets data-ready was never registered.
//
// That is not a cosmetic bug. data-ready is what gotoRoute blocks on, so every
// route carrying a map hung for 15s instead of rendering a message -- which is
// what had CI's ui job red with 121 identical timeouts from Stage 30 until
// Stage 40 found it, and what any reader with WebGL off or blocklisted saw.
//
// Its own file because it needs its own browser: firefoxUserPrefs is a launch
// option, so turning WebGL off means a separate instance, and test.use() below
// gets one for this file alone rather than for the suite.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute, waitForMapInstance } from "./helpers/scenarios.js";

// webgl.force-enabled is set alongside webgl.disabled deliberately: the CI job
// turns force-enabled ON to get a software context out of a GPU-less runner, and
// this asserts that `disabled` still wins over it. Without that, this spec would
// quietly stop testing anything the day those prefs were tuned.
test.use({
  launchOptions: { firefoxUserPrefs: { "webgl.disabled": true, "webgl.force-enabled": true } },
});

// Chromium ships SwiftShader and would render a map regardless of the prefs
// above, which are Firefox's. The gesture project is the only Chromium one and
// does not match this file, but the skip states the dependency rather than
// leaving it to the config.
test.skip(({ browserName }) => browserName !== "firefox", "firefoxUserPrefs is Firefox's");

test.describe("a browser with no WebGL2", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test("says so, rather than hanging or leaving a blank rectangle", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));

    const routes = await buildRoutes(page);
    const route = routes.find((r) => r.label === "trip map");
    expect(route, "the sweep should know a trip map route").toBeTruthy();

    // The assertion hiding in plain sight: gotoRoute waits for data-ready, so
    // reaching the next line at all is the component having finished its render
    // instead of timing out. Before the fix this threw after 15s.
    await gotoRoute(page, route.path);

    // There really is no context -- otherwise everything below would pass for
    // the wrong reason.
    const webgl2 = await page.evaluate(() => !!document.createElement("canvas").getContext("webgl2"));
    expect(webgl2, "the prefs above should have taken WebGL2 away").toBe(false);

    const host = page.locator("map-view").first();
    await expect(host).toHaveAttribute("data-ready", "");

    // The sentence, from the locale file rather than hard-coded, so this does
    // not have to be edited when the copy is.
    const message = host.locator(".map-wrap .empty");
    await expect(message).toBeVisible();
    await expect(message).not.toBeEmpty();

    // A context-less canvas is a full-size rectangle drawing nothing, sitting
    // under the sentence that explains why there is no map. The component
    // empties the container rather than trusting a half-built map to survive
    // remove(), which on this path it does not.
    await expect(host.locator("#map canvas")).toHaveCount(0);

    // The TypeError is the whole bug. Its absence is the regression test.
    expect(errors, "the failed context must not leave an uncaught error").toEqual([]);
  });

  // The other half of Milestone 1's consequence, and the reason
  // waitForMapInstance exists: data-ready and _map now disagree on this path.
  // A spec that drives the map still has to fail here -- there is no map to
  // drive -- but it should do so in seconds with a sentence, not by burning the
  // test's whole 180s budget waiting for something that is never coming, which
  // is what one map in ~80 cost on CI before this helper.
  test("a spec waiting for the map instance fails fast and says why", async ({ page }) => {
    const routes = await buildRoutes(page);
    const route = routes.find((r) => r.label === "trip map");
    await gotoRoute(page, route.path);

    const started = Date.now();
    // A short budget on purpose: the point is the diagnosis, and this asserts
    // that the wait is bounded by the helper rather than by the test timeout.
    const error = await waitForMapInstance(page, "map-view", 3000).then(
      () => null,
      (e) => e
    );
    const elapsed = Date.now() - started;

    expect(error, "waiting for an instance that cannot exist must reject").toBeTruthy();
    expect(error.message).toContain("map unavailable");
    expect(error.message).toContain("WebGL2");
    expect(elapsed, "the helper should bound the wait, not the test timeout").toBeLessThan(30000);
  });
});
