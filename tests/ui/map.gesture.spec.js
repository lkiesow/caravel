// The two map gestures, with actual fingers.
//
// map.spec.js asserts the same rule from the other side: it stubs
// `(pointer: coarse)` through addInitScript and then reads the map's handler
// state — cooperative gestures on, rotation off. That is honest as far as it
// goes, and it
// is all Firefox can do, because Playwright's `isMobile` (the option that flips
// the media query and enables touch emulation) is Chromium-only and
// `hasTouch: true` does not do it. But it proves the handlers are configured,
// not that the gestures behave: no assertion anywhere had ever moved a finger.
//
// So this file runs in the chromium-gestures project (see playwright.config.js)
// and drives real touch input through CDP's Input.dispatchTouchEvent. Synthetic
// TouchEvents dispatched from page.evaluate would not do: the map would see
// them, but the *browser* would not, and "one finger scrolls the page" is a
// claim about the browser's own scrolling. Untrusted events never scroll.
//
// The stubbed versions in map.spec.js stay. They assert a different thing — the
// configuration, on the engine the rest of the suite runs — and they would have
// caught a regression in the media query itself, which these two would not.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute } from "./helpers/scenarios.js";

// One finger down, dragged, and lifted. Several small steps rather than one
// jump, because both the map and the browser's scroller work from deltas
// between moves.
async function drag(cdp, points, dx, dy, steps = 10) {
  // Each finger needs its own id, or CDP treats the two points as one touch
  // and the map never sees a second finger at all.
  const at = (i) =>
    points.map((p, id) => ({ id, x: p.x + (dx * i) / steps, y: p.y + (dy * i) / steps }));
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: at(0) });
  for (let i = 1; i <= steps; i++) {
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: at(i) });
  }
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
}

// The map sits well down the trip page — at 324x756 its box starts around
// y=617 and runs to y=937, i.e. mostly *below the fold*. CDP dispatches touch
// at viewport coordinates and silently delivers nothing outside them, so a
// gesture aimed at the untouched box lands nowhere and every assertion about
// "the map did not move" passes for the wrong reason. That happened here: the
// first version of the one-finger test was green with zero touch events
// reaching the page. So the map is scrolled into view first, and the point
// being touched is checked to be on screen.
async function showMap(page) {
  await page.evaluate(() => {
    document.querySelector("map-view").scrollIntoView({ block: "center" });
  });
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
}

function mapState(page) {
  return page.evaluate(() => {
    const host = document.querySelector("map-view");
    const c = host._map.getCenter();
    const box = host.shadowRoot.getElementById("map").getBoundingClientRect();
    return {
      lat: c.lat,
      lng: c.lng,
      scrollY: window.scrollY,
      box: { x: box.x, y: box.y, width: box.width, height: box.height },
      innerHeight: window.innerHeight,
    };
  });
}

test.describe("map gestures on a real touch device", () => {
  let cdp;

  test.beforeEach(async ({ page, context }) => {
    cdp = await context.newCDPSession(page);
    await login(page);
    const routes = await buildRoutes(page);
    const mapRoute = routes.find((r) => r.label === "trip map");
    expect(mapRoute, "the route sweep should know a trip map route").toBeTruthy();
    await gotoRoute(page, mapRoute.path);
    await page.waitForFunction(() => document.querySelector("map-view")?._map);

    // The premise of the whole file: this really is a coarse pointer, with no
    // stub involved. If Chromium ever stopped reporting it, both tests below
    // would still pass for the wrong reason.
    const coarse = await page.evaluate(() => window.matchMedia("(pointer: coarse)").matches);
    expect(coarse, "the chromium-gestures project must emulate a touch device").toBe(true);
  });

  test("one finger on the map scrolls the page and leaves the map where it was", async ({ page }) => {
    // The page has to have somewhere to scroll to, or this asserts nothing.
    const scrollable = await page.evaluate(
      () => document.documentElement.scrollHeight > window.innerHeight
    );
    expect(scrollable, "the trip map page should be taller than the screen").toBe(true);

    await showMap(page);
    const before = await mapState(page);
    const centre = { x: before.box.x + before.box.width / 2, y: before.box.y + before.box.height / 2 };
    expect(centre.y, "the finger must land inside the viewport").toBeGreaterThan(0);
    expect(centre.y).toBeLessThan(before.innerHeight);

    // Drag whichever way the page can still move: showMap() centres the map,
    // which may already have consumed the scroll room in one direction.
    const room = await page.evaluate(() => ({
      y: window.scrollY,
      max: document.documentElement.scrollHeight - window.innerHeight,
    }));
    const dy = room.y < room.max - 20 ? -120 : 120;

    // Diagonal, not straight up: the vertical component is what the page
    // scrolls on, and the horizontal component is what makes the assertion
    // below mean anything. Since Stage 30 the map cannot pan vertically at
    // this zoom at all -- MapLibre pins the camera when the whole world
    // already fits the viewport, which at the trip map's fitBounds zoom it
    // exactly does -- so a purely vertical drag would leave the centre
    // unchanged whether or not the map had swallowed the gesture. Longitude
    // is unconstrained, so a sideways component turns "the map must not pan"
    // back into a claim that can fail.
    await drag(cdp, [centre], -60, dy);
    await page.waitForFunction((y) => window.scrollY !== y, before.scrollY);

    const after = await mapState(page);
    expect(after.scrollY, "a one-finger drag should scroll the page").not.toBe(before.scrollY);
    // And the map must not have moved with it. This is the bug the whole
    // arrangement exists to prevent: a map that eats the scroll gesture.
    expect(after.lng, "the map must not pan under one finger").toBeCloseTo(before.lng, 6);
    expect(after.lat).toBeCloseTo(before.lat, 6);
  });

  // Stage 23 Milestone 6. The one-finger drag correctly does nothing to the
  // map -- and used to say so with a caption standing permanently under it,
  // which explained nothing to anyone who never made the gesture and cost a
  // line of screen to everybody. Now it is said when it happens, and this is
  // the only project that can prove it: the hint is driven by a real
  // touchmove.
  test("a one-finger drag explains the gesture it did not perform", async ({ page }) => {
    await showMap(page);
    const before = await mapState(page);
    const centre = { x: before.box.x + before.box.width / 2, y: before.box.y + before.box.height / 2 };
    expect(centre.y, "the finger must land inside the viewport").toBeGreaterThan(0);
    expect(centre.y).toBeLessThan(before.innerHeight);

    const hintShown = () =>
      page.evaluate(() => {
        const el = document.querySelector("map-view").shadowRoot.querySelector(".gesture-hint");
        return { shown: !!el && !el.hidden, text: el?.textContent?.trim() };
      });

    expect((await hintShown()).shown, "nothing should be over the map before the gesture").toBe(false);

    await drag(cdp, [centre], 0, -120);

    const seen = await hintShown();
    expect(seen.shown, "a one-finger drag should say why the map did not move").toBe(true);
    expect(seen.text).toMatch(/two fingers/i);

    // And it must take itself away again.
    await expect
      .poll(async () => (await hintShown()).shown, {
        message: "the overlay must not sit on the map indefinitely",
        timeout: 5000,
      })
      .toBe(false);
  });

  test("two fingers pan the map and leave the page where it was", async ({ page }) => {
    await showMap(page);
    const before = await mapState(page);
    const cx = before.box.x + before.box.width / 2;
    const cy = before.box.y + before.box.height / 2;
    expect(cy, "the fingers must land inside the viewport").toBeGreaterThan(0);
    expect(cy).toBeLessThan(before.innerHeight);
    // Kept well inside the map box so neither finger lands on a control or
    // outside the element.
    const fingers = [
      { x: cx - 30, y: cy - 20 },
      { x: cx + 30, y: cy + 20 },
    ];

    // Sideways, and longitude is what is asserted. The obvious version of this
    // test drags upwards and watches latitude, which is what it did until
    // Stage 30 -- but MapLibre will not pan vertically when the world already
    // fits the viewport, and at the trip map's fitBounds zoom the world height
    // and the container height agree to within a pixel (measured: 643 and
    // 643). That is correct behaviour and an improvement on Leaflet, which
    // would happily drag the world off the top of the screen; it just means
    // latitude is the one axis that cannot demonstrate a pan here.
    await drag(cdp, fingers, -60, 0);

    // The pan is animated, so the centre settles a frame or two later.
    await expect
      .poll(async () => (await mapState(page)).lng, {
        message: "two fingers should pan the map",
      })
      .not.toBeCloseTo(before.lng, 6);

    const after = await mapState(page);
    expect(after.scrollY, "a two-finger gesture belongs to the map, not the page").toBe(before.scrollY);
  });
});
