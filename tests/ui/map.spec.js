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

// Clicks the first marker and waits for its popup. Markers are Leaflet
// divIcons inside the shadow root, so this goes through page.evaluate rather
// than a locator - and dispatches a real MouseEvent, because Leaflet listens
// for pointer/mouse events rather than for .click().
async function openFirstPopup(page) {
  await page.evaluate(() => {
    const sr = document.querySelector("leaflet-map").shadowRoot;
    const marker = sr.querySelector(".leaflet-marker-icon");
    if (!marker) throw new Error("no markers on the trip map - does the seed still give the `full` trip coordinates?");
    for (const type of ["mousedown", "mouseup", "click"]) {
      marker.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, view: window, button: 0 }));
    }
  });
  await page.waitForFunction(
    () => document.querySelector("leaflet-map").shadowRoot.querySelector(".leaflet-popup-content [data-item-id]") !== null
  );
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


// Milestone 2. A marker popup used to offer the title and a Google Maps link
// and nothing else, so the map could show you where a location was but not
// take you to it. The item id was already in the payload.
test.describe("a marker popup links back into the app", () => {
  test("opens the location client-side, without a page load", async ({ page }) => {
    await login(page);
    const mapPath = await gotoTripMap(page);

    // The sentinel is the point of this test, not decoration. A full document
    // load would also end up on the right pathname - which is exactly what
    // would happen if this link relied on the router's [data-link] handler,
    // since that listener cannot see through the shadow boundary. Only a
    // value that survives on `window` proves the SPA handled it.
    await page.evaluate(() => {
      window.__caravelNoReload = "alive";
    });

    await openFirstPopup(page);
    const link = await page.evaluate(() => {
      const a = document.querySelector("leaflet-map").shadowRoot.querySelector(".leaflet-popup-content [data-item-id]");
      return { href: a.getAttribute("href"), text: a.textContent.trim(), itemId: a.dataset.itemId };
    });

    // A real <a href>, so middle-click and "open in new tab" still work.
    expect(link.href, "the popup link should be a real, resolvable route").toBe(
      `${mapPath.replace(/\/map$/, "")}/locations/${link.itemId}`
    );
    expect(link.text, "the in-app link should be labelled").toBeTruthy();

    await page.evaluate(() => {
      document.querySelector("leaflet-map").shadowRoot.querySelector(".leaflet-popup-content [data-item-id]").click();
    });
    await page.waitForFunction((href) => window.location.pathname === href, link.href);

    expect(
      await page.evaluate(() => window.__caravelNoReload),
      "the page reloaded - the click escaped the SPA instead of being handled in the shadow root"
    ).toBe("alive");

    // ...and it actually rendered that location, rather than just changing the URL.
    await expect(page.locator("h1")).toBeVisible();
  });

  test("a modified click is left to the browser", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    await openFirstPopup(page);

    // Ctrl-click means "new tab". The handler must not preventDefault it, or
    // the link loses every affordance it was kept an <a href> for.
    const defaultPrevented = await page.evaluate(() => {
      const a = document.querySelector("leaflet-map").shadowRoot.querySelector(".leaflet-popup-content [data-item-id]");
      const e = new MouseEvent("click", { bubbles: true, cancelable: true, view: window, button: 0, ctrlKey: true });
      a.dispatchEvent(e);
      return e.defaultPrevented;
    });
    expect(defaultPrevented, "ctrl-click should fall through to the browser").toBe(false);
    expect(page.url(), "ctrl-click should not navigate this tab").toContain("/map");
  });

  test("the popup links meet the tap target floor on a phone", async ({ page }) => {
    // routes.spec.js's sweep skips anything under a leaflet-* class, and
    // Leaflet renders popup content inside .leaflet-popup-content - so these
    // two links are invisible to it and have to be measured here.
    await page.setViewportSize(MOBILE);
    await login(page);
    await gotoTripMap(page);
    await openFirstPopup(page);

    const heights = await page.evaluate(() => {
      const sr = document.querySelector("leaflet-map").shadowRoot;
      return [...sr.querySelectorAll(".leaflet-popup-content .popup-link")].map((a) => ({
        text: a.textContent.trim(),
        height: a.getBoundingClientRect().height,
      }));
    });
    expect(heights.length, "the popup should offer both destinations").toBe(2);
    for (const link of heights) {
      expect(link.height, `"${link.text}" is ${Math.round(link.height)}px`).toBeGreaterThanOrEqual(44);
    }
  });
});
