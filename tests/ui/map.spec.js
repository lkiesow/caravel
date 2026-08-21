// The map's own behaviour, as opposed to the route sweeps that merely render
// it. Stage 13's subject: leaflet-map.js had not changed shape since Stage 01
// and was strictly read-only, which is what several backlog entries were stuck
// behind.
//
// Milestone 1 is the confirmed bug from Stage 07 - "the mobile map swallows
// vertical scrolling" - which is really two faults with one symptom: the map
// took a flat 50vh and the legend sat below it off the fold, so almost the
// whole visible screen was map; and Leaflet's drag handler consumed a
// one-finger drag that the user meant for the page. They are asserted
// separately below, because the fixes are independent (CSS vs. a map option)
// and one passing should not hide the other regressing.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

// Playwright drives Firefox here (see playwright.config.js) and its device
// emulation cannot flip `(pointer: coarse)` - only Chromium's isMobile does.
// So the touch device is emulated at the one place the component actually
// asks the question. This is a stub of the *input*, not of the behaviour
// under test: everything asserted afterwards is the component's real
// response to it.
async function pretendCoarsePointer(page) {
  await page.addInitScript(() => {
    const real = window.matchMedia.bind(window);
    window.matchMedia = (query) =>
      query === "(pointer: coarse)"
        ? {
            matches: true,
            media: query,
            onchange: null,
            addEventListener() {},
            removeEventListener() {},
            addListener() {},
            removeListener() {},
            dispatchEvent: () => false,
          }
        : real(query);
  });
}

async function gotoTripMap(page) {
  const routes = await buildRoutes(page);
  const mapRoute = routes.find((r) => r.label === "trip map");
  expect(mapRoute, "the route sweep should know a trip map route").toBeTruthy();
  await gotoRoute(page, mapRoute.path);
  await page.waitForFunction(() => document.querySelector("leaflet-map")?._map);
  return mapRoute.path;
}

// Reaches into the component's shadow root, which is where every part of this
// lives - the map element, the legend and the hint are all behind it.
function readMap(page) {
  return page.evaluate(() => {
    const host = document.querySelector("leaflet-map");
    const sr = host.shadowRoot;
    const box = (el) => (el ? el.getBoundingClientRect().toJSON() : null);
    return {
      map: box(sr.getElementById("map")),
      legend: box(sr.querySelector(".legend")),
      hint: sr.querySelector(".gesture-hint")?.textContent?.trim() ?? null,
      dragging: host._map.dragging.enabled(),
      touchZoom: host._map.touchZoom.enabled(),
      innerHeight: window.innerHeight,
    };
  });
}

test.describe("the trip map at phone width", () => {
  test.use({ viewport: MOBILE });

  test("the map leaves most of the screen to the page", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    const { map, legend, innerHeight } = await readMap(page);

    // Capped, rather than a flat 50vh: 50vh of 756 is 378, and with the old
    // rule the measured map was 424px tall starting at y=383.
    expect(map.height, "the mobile map should be capped at 20rem").toBeLessThanOrEqual(320);

    // The legend used to render *after* the map and land at y=769 - a dozen
    // pixels past the fold, with nothing hinting it was there.
    expect(legend.bottom, "the legend should sit above the map, not below it").toBeLessThanOrEqual(map.top + 1);
    expect(legend.top, "the legend should be above the fold").toBeLessThan(innerHeight);

    // The symptom itself: how much of what you can actually see is map. A
    // touch drag has to start somewhere, and before this it could only start
    // on the map from y=383 down.
    const visibleMap = Math.min(map.bottom, innerHeight) - Math.max(map.top, 0);
    expect(
      visibleMap,
      `${Math.round(visibleMap)}px of the ${innerHeight}px screen is map - a drag has nowhere else to start`
    ).toBeLessThanOrEqual(innerHeight / 2);
  });

  test("a one-finger drag is left to the page on a touch device", async ({ page }) => {
    await pretendCoarsePointer(page);
    await login(page);
    await gotoTripMap(page);
    const { dragging, touchZoom, hint } = await readMap(page);

    // Leaflet's Drag handler is what was eating the gesture. Off, a one-finger
    // touchmove is never consumed and the page scrolls normally.
    expect(dragging, "Leaflet's drag handler should be off on a coarse pointer").toBe(false);
    // ...but the map must still be movable, and it is: touchZoom applies the
    // pinch centre's delta even at scale 1, so two fingers pan as well as zoom
    // (TouchZoom._onTouchMove in the vendored leaflet.esm.js).
    expect(touchZoom, "two-finger pan/zoom must survive").toBe(true);
    // A silently changed gesture is a broken map as far as the user knows.
    expect(hint, "the changed gesture should be spelled out").toBeTruthy();
  });
});

test.describe("the trip map with a mouse", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("dragging still works and no touch hint appears", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    const { dragging, hint } = await readMap(page);
    expect(dragging, "a fine pointer should keep click-and-drag panning").toBe(true);
    expect(hint, "the two-finger hint is touch-only").toBeNull();
  });
});
