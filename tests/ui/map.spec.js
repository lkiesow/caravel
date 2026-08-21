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


// Milestone 3. Pick mode: the first time leaflet-map.js has been anything but
// read-only. No page mounts it yet (the location editor picks it up in
// Milestone 4), so these tests mount one themselves.
//
// The component is registered by any route that renders a map, so the trip map
// is used as a host page and the picker is appended beside it.
async function mountPicker(page, attrs = {}) {
  await page.evaluate((attributes) => {
    document.querySelectorAll("[data-test-picker]").forEach((el) => el.remove());
    const el = document.createElement("leaflet-map");
    el.setAttribute("pick", "");
    el.dataset.testPicker = "1";
    for (const [k, v] of Object.entries(attributes)) el.setAttribute(k, v);
    // A definite box: :host([pick]) sets the height, and the width comes from
    // the block context, which document.body gives it.
    el.style.width = "400px";
    document.body.appendChild(el);
    window.__picks = [];
    el.addEventListener("location-picked", (e) => window.__picks.push(e.detail));
  }, attrs);
  await page.waitForFunction(() => document.querySelector("[data-test-picker]")?._map);
  // Leaflet sizes itself from the container; give it the frame it needs.
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
}

// A real DOM click at a point inside the map, which is what Leaflet turns into
// a latlng. Going through map.fire("click") instead would test nothing.
async function clickPickerAt(page, fractionX, fractionY, selector = "[data-test-picker]") {
  await page.evaluate(
    ({ fx, fy, sel }) => {
      const host = document.querySelector(sel);
      const mapEl = host.shadowRoot.getElementById("map");
      const r = mapEl.getBoundingClientRect();
      const clientX = r.left + r.width * fx;
      const clientY = r.top + r.height * fy;
      const opts = { bubbles: true, cancelable: true, view: window, button: 0, clientX, clientY };
      for (const type of ["mousedown", "mouseup", "click"]) {
        mapEl.dispatchEvent(new MouseEvent(type, opts));
      }
    },
    { fx: fractionX, fy: fractionY, sel: selector }
  );
}

test.describe("pick mode", () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
  });

  test("a click reports a coordinate and places the marker", async ({ page }) => {
    await mountPicker(page);

    // With no coordinates it opens on the world view, like the empty trip map,
    // and has no marker to show.
    const before = await page.evaluate(() => {
      const host = document.querySelector("[data-test-picker]");
      return {
        zoom: host._map.getZoom(),
        markers: host.shadowRoot.querySelectorAll(".leaflet-marker-icon").length,
      };
    });
    expect(before.zoom, "an empty picker should open on the world view").toBe(2);
    expect(before.markers, "nothing is chosen yet, so there is nothing to mark").toBe(0);

    await clickPickerAt(page, 0.5, 0.5);

    const picks = await page.evaluate(() => window.__picks);
    expect(picks.length, "a click should report exactly one coordinate").toBe(1);
    const [{ lat, lng }] = picks;
    expect(Number.isFinite(lat) && Number.isFinite(lng), `got ${lat},${lng}`).toBe(true);
    expect(Math.abs(lat), "latitude must be a real latitude").toBeLessThanOrEqual(90);
    // wrapLatLng: panning the world sideways would otherwise report longitudes
    // past 180, which no tile server or database column wants.
    expect(Math.abs(lng), "longitude should be wrapped into [-180, 180]").toBeLessThanOrEqual(180);
    // 6 decimals is ~11cm; without the rounding this is a 17-digit float going
    // straight into a number input.
    for (const n of [lat, lng]) {
      expect(String(n).split(".")[1]?.length ?? 0, `${n} carries more precision than a form field wants`).toBeLessThanOrEqual(6);
    }

    // The component does not move its own marker on click - the page owns the
    // coordinates and feeds them back as attributes (Milestone 4). Doing that
    // here is what makes the marker appear.
    await page.evaluate(({ lat, lng }) => {
      const host = document.querySelector("[data-test-picker]");
      host.setAttribute("lat", String(lat));
      host.setAttribute("lng", String(lng));
    }, picks[0]);
    await expect
      .poll(() => page.evaluate(() => document.querySelector("[data-test-picker]").shadowRoot.querySelectorAll(".leaflet-marker-icon").length))
      .toBe(1);
  });

  test("a coordinate change moves the marker without rebuilding the map", async ({ page }) => {
    await mountPicker(page, { lat: "64.9631", lng: "-19.0208" });

    // Identity, not just "a map is still there": load() used to rebuild the
    // whole shadow root on any attribute change, which in an editor means a
    // teardown and a fresh tile fetch per keystroke in the coordinate fields.
    await page.evaluate(() => {
      const host = document.querySelector("[data-test-picker]");
      window.__mapInstance = host._map;
      window.__tilePane = host.shadowRoot.querySelector(".leaflet-tile-pane");
      window.__markerEl = host.shadowRoot.querySelector(".leaflet-marker-icon");
    });

    await page.evaluate(() => {
      const host = document.querySelector("[data-test-picker]");
      host.setAttribute("lat", "48.8584");
      host.setAttribute("lng", "2.2945");
    });

    await expect
      .poll(async () =>
        page.evaluate(() => {
          const host = document.querySelector("[data-test-picker]");
          const p = host._pickMarker.getLatLng();
          return { lat: Math.round(p.lat * 1e4) / 1e4, lng: Math.round(p.lng * 1e4) / 1e4 };
        })
      )
      .toEqual({ lat: 48.8584, lng: 2.2945 });

    const survived = await page.evaluate(() => {
      const host = document.querySelector("[data-test-picker]");
      return {
        sameMap: host._map === window.__mapInstance,
        sameTilePane: host.shadowRoot.querySelector(".leaflet-tile-pane") === window.__tilePane,
        sameMarker: host.shadowRoot.querySelector(".leaflet-marker-icon") === window.__markerEl,
      };
    });
    expect(survived.sameMap, "the Leaflet map was re-created").toBe(true);
    expect(survived.sameTilePane, "the shadow root was re-rendered").toBe(true);
    expect(survived.sameMarker, "the marker was torn down instead of moved").toBe(true);
  });

  test("dragging the marker reports the new coordinate", async ({ page }) => {
    // The other half of "pick": a click places it, a drag adjusts it. Leaflet's
    // Draggable binds mousedown on the marker and mousemove/mouseup on the
    // document (see START in the vendored leaflet.esm.js), so the drag has to
    // be dispatched across both.
    await mountPicker(page, { lat: "64.9631", lng: "-19.0208" });
    await page.evaluate(() => {
      window.__picks = [];
    });

    // Real input through page.mouse rather than dispatched MouseEvents:
    // Leaflet's Draggable is picky about which synthetic events it accepts,
    // and a genuine drag is the thing being claimed anyway.
    const from = await page.evaluate(() => {
      const host = document.querySelector("[data-test-picker]");
      host.scrollIntoView({ block: "center" });
      const r = host.shadowRoot.querySelector(".leaflet-marker-icon").getBoundingClientRect();
      return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
    });
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    // Two steps: Leaflet ignores movement inside its click tolerance, and a
    // single jump can be treated as one.
    await page.mouse.move(from.x + 20, from.y + 20);
    await page.mouse.move(from.x + 60, from.y + 40);
    await page.mouse.up();

    await expect.poll(() => page.evaluate(() => window.__picks.length)).toBe(1);
    const [pick] = await page.evaluate(() => window.__picks);
    // Dragged down and to the right: south and east of where it started.
    expect(pick.lat, "dragging down should decrease latitude").toBeLessThan(64.9631);
    expect(pick.lng, "dragging right should increase longitude").toBeGreaterThan(-19.0208);
  });

  test("clearing a coordinate removes the marker rather than dropping it at 0,0", async ({ page }) => {
    // Number("") is 0, which is a real place in the Gulf of Guinea. An
    // emptied field must mean "nothing chosen", not "chosen: null island".
    await mountPicker(page, { lat: "64.9631", lng: "-19.0208" });
    await page.evaluate(() => document.querySelector("[data-test-picker]").setAttribute("lat", ""));
    await expect
      .poll(() =>
        page.evaluate(() => document.querySelector("[data-test-picker]").shadowRoot.querySelectorAll(".leaflet-marker-icon").length)
      )
      .toBe(0);
  });

  test("carries none of the trip map's chrome", async ({ page }) => {
    await mountPicker(page);
    const chrome = await page.evaluate(() => {
      const sr = document.querySelector("[data-test-picker]").shadowRoot;
      return { legend: sr.querySelectorAll(".legend").length, empty: sr.querySelectorAll(".empty").length };
    });
    // The legend filters trip-wide markers and the empty line reports there
    // are none; a picker has neither concept.
    expect(chrome.legend, "a picker has no categories to filter").toBe(0);
    expect(chrome.empty, "a picker is not an empty trip map").toBe(0);
  });
});


// Milestone 4. The picker in the location editor - the first page to mount
// pick mode, and the reason it exists. A mutating flow, so it takes the
// isolation route files.spec.js and settings.spec.js established: its own
// trip, created and deleted around each test, so the shared seed is never
// touched.
test.describe("the location editor's coordinate picker", () => {
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: map picker spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  async function openNewLocation(page) {
    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.waitForFunction(() => document.querySelector(".location-form__map")?.hasAttribute("data-ready"));
  }

  test("a click fills both coordinate fields, and Save persists them", async ({ page }) => {
    await openNewLocation(page);

    const lat = page.locator('.location-form input[name="lat"]');
    const lng = page.locator('.location-form input[name="lng"]');
    await expect(lat, "a new location starts with no coordinates").toHaveValue("");
    await expect(lng).toHaveValue("");

    // The "Show on map" hint is shown precisely while the box is ticked and
    // the coordinates are empty, so it doubles as a check that picking on the
    // map runs the same sync typing does.
    await expect(page.locator(".location-form__hint")).toBeVisible();

    await page.locator('.item-form input[name="title"]').fill("Picked by map");
    await clickPickerAt(page, 0.5, 0.4, ".location-form__map");

    await expect(lat, "a map click should fill the latitude field").not.toHaveValue("");
    await expect(lng, "a map click should fill the longitude field").not.toHaveValue("");
    await expect(page.locator(".location-form__hint"), "the coordinates are no longer missing").toBeHidden();

    const typed = { lat: await lat.inputValue(), lng: await lng.inputValue() };

    await page.locator('[data-action="save"]').click();
    await page.waitForFunction(() => !window.location.pathname.endsWith("/new"));

    // The fields are the source of truth, so what the server stored has to
    // match what they showed - not merely "something was saved". The list
    // endpoint returns a summary with no nested location, so this reads the
    // item's own route.
    const items = await (await page.request.get(`/api/trips/${tripId}/items`)).json();
    expect(items.length, "the location should have been created").toBe(1);
    const detail = await (await page.request.get(`/api/items/${items[0].id}`)).json();
    expect(detail.location, "the picked coordinates should have been saved with it").toBeTruthy();
    expect(detail.location.lat).toBeCloseTo(Number(typed.lat), 6);
    expect(detail.location.lng).toBeCloseTo(Number(typed.lng), 6);
  });

  test("typing a coordinate moves the marker, and clearing it removes it", async ({ page }) => {
    await openNewLocation(page);

    const picker = page.locator(".location-form__map");
    await page.locator('.location-form input[name="lat"]').fill("48.8584");
    await page.locator('.location-form input[name="lng"]').fill("2.2945");

    await expect(picker).toHaveAttribute("lat", "48.8584");
    await expect(picker).toHaveAttribute("lng", "2.2945");
    await expect
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".leaflet-marker-icon").length))
      .toBe(1);

    // A cleared field must remove the attribute, not set it blank: "no
    // coordinate" and "the coordinate 0" are different answers.
    await page.locator('.location-form input[name="lat"]').fill("");
    await expect(picker).not.toHaveAttribute("lat", /.*/);
    await expect
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".leaflet-marker-icon").length))
      .toBe(0);
  });

  test("an existing location opens on its own point, not the world view", async ({ page }) => {
    const created = await page.request.post(`/api/trips/${tripId}/items`, {
      data: {
        title: "Already placed",
        category: "site",
        location: { lat: 64.9631, lng: -19.0208, address: null },
      },
    });
    expect(created.status(), "create a located item to edit").toBe(201);
    const itemId = (await created.json()).id;

    await gotoRoute(page, `/trips/${tripId}/locations/${itemId}/edit`);
    await page.waitForFunction(() => document.querySelector(".location-form__map")?.hasAttribute("data-ready"));

    // Rendered onto the element in the page template rather than pushed after
    // mount, so the map never shows the world view and then jumps.
    const view = await page.evaluate(() => {
      const el = document.querySelector(".location-form__map");
      const c = el._map.getCenter();
      return { zoom: el._map.getZoom(), lat: c.lat, lng: c.lng };
    });
    expect(view.zoom, "an existing point should open zoomed in, not at world view").toBeGreaterThan(5);
    expect(view.lat).toBeCloseTo(64.9631, 1);
    expect(view.lng).toBeCloseTo(-19.0208, 1);
  });
});
