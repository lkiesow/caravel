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


// Milestone 5. Address search in the location editor.
//
// /api/geocode is stubbed at the network boundary throughout: the dev server
// is configured with the real Nominatim URL, so an unstubbed test here would
// send live traffic to OpenStreetMap every run. The proxy itself - request
// shape, User-Agent, mapping, timeouts, rate limiting, the disabled case - is
// covered by Go tests in internal/httpapi/geocode_test.go, which never leave
// the process either.
const GEOCODE_RESULTS = [
  { display_name: "Reykjavík, Höfuðborgarsvæðið, Iceland", lat: 64.1466, lng: -21.9426 },
  { display_name: "Reykjavík Airport, Iceland", lat: 64.13, lng: -21.9406 },
];

test.describe("address search in the location editor", () => {
  let tripId;

  test.beforeEach(async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: geocode spec" } });
    expect(res.status(), "create the spec's own trip").toBe(201);
    tripId = (await res.json()).id;
  });

  test.afterEach(async ({ page }) => {
    if (tripId) await page.request.delete(`/api/trips/${tripId}`);
    tripId = null;
  });

  async function stubGeocode(page, handler) {
    await page.route("**/api/geocode?*", handler);
  }

  async function openNewLocation(page) {
    await gotoRoute(page, `/trips/${tripId}/locations/new`);
    await page.waitForFunction(() => document.querySelector(".location-form__map")?.hasAttribute("data-ready"));
  }

  test("finds a place and fills the coordinates and the empty address", async ({ page }) => {
    const queries = [];
    await stubGeocode(page, (route, request) => {
      queries.push(new URL(request.url()).searchParams.get("q"));
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(GEOCODE_RESULTS) });
    });
    await openNewLocation(page);

    const panel = page.locator(".location-search");
    await expect(panel, "the server reports a geocoder, so the control shows").toBeVisible();

    await page.locator('.location-search input[name="placeQuery"]').fill("Reykjavik");
    // Nothing may have been requested yet: a query costs an external service,
    // so this searches on submit and never per keystroke.
    expect(queries, "typing must not search").toEqual([]);

    await page.locator('[data-action="search-place"]').click();
    const results = page.locator(".location-search__result");
    await expect(results).toHaveCount(2);
    expect(queries).toEqual(["Reykjavik"]);
    await expect(results.first()).toHaveText(GEOCODE_RESULTS[0].display_name);

    await results.first().click();
    await expect(page.locator('.location-form input[name="lat"]')).toHaveValue("64.1466");
    await expect(page.locator('.location-form input[name="lng"]')).toHaveValue("-21.9426");
    // An empty address gets the result's formatted name - it is the one thing
    // a geocoder knows that the map click cannot tell you.
    await expect(page.locator('.location-form input[name="address"]')).toHaveValue(GEOCODE_RESULTS[0].display_name);
    await expect(results, "choosing a result should close the list").toHaveCount(0);

    // And the picker followed, which is the whole point of filling the fields.
    await expect(page.locator(".location-form__map")).toHaveAttribute("lat", "64.1466");
    await expect
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".leaflet-marker-icon").length))
      .toBe(1);
  });

  test("does not overwrite an address the user already wrote", async ({ page }) => {
    await stubGeocode(page, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(GEOCODE_RESULTS) })
    );
    await openNewLocation(page);

    const address = page.locator('.location-form input[name="address"]');
    await address.fill("The blue house past the bridge");
    await page.locator('.location-search input[name="placeQuery"]').fill("Reykjavik");
    await page.locator('[data-action="search-place"]').click();
    await page.locator(".location-search__result").first().click();

    // The coordinates are what was asked for; the wording was not.
    await expect(page.locator('.location-form input[name="lat"]')).toHaveValue("64.1466");
    await expect(address).toHaveValue("The blue house past the bridge");
  });

  test("says so when the search finds nothing, and when it is unavailable", async ({ page }) => {
    await stubGeocode(page, (route) => route.fulfill({ status: 200, contentType: "application/json", body: "[]" }));
    await openNewLocation(page);

    const status = page.locator(".location-search__status");
    await page.locator('.location-search input[name="placeQuery"]').fill("zzzzzzzz");
    await page.locator('[data-action="search-place"]').click();
    await expect(status).toBeVisible();
    const noResults = await status.textContent();

    await page.unroute("**/api/geocode?*");
    await stubGeocode(page, (route) => route.fulfill({ status: 502, contentType: "application/json", body: '{"error":"upstream"}' }));
    await page.locator('.location-search input[name="placeQuery"]').fill("Reykjavik");
    await page.locator('[data-action="search-place"]').click();
    await expect(status).toBeVisible();
    // Two different situations must not read identically - "found nothing"
    // and "the search is broken" call for different next actions.
    await expect(status).not.toHaveText(noResults);

    // A failed search must leave the rest of the card working: the map is
    // still the way to set a point.
    await clickPickerAt(page, 0.5, 0.5, ".location-form__map");
    await expect(page.locator('.location-form input[name="lat"]')).not.toHaveValue("");
  });

  test("Enter in the search box searches instead of saving the page", async ({ page }) => {
    // The Location card treats Enter as "save the page" (location-editor's own
    // keydown handler), which in this field would leave the trip mid-edit.
    await stubGeocode(page, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(GEOCODE_RESULTS) })
    );
    await openNewLocation(page);

    await page.locator('.location-search input[name="placeQuery"]').fill("Reykjavik");
    await page.locator('.location-search input[name="placeQuery"]').press("Enter");
    await expect(page.locator(".location-search__result")).toHaveCount(2);
    expect(page.url(), "Enter should not have saved and navigated away").toContain("/locations/new");
  });
});


// Milestone 6. The device's own position.
//
// The happy path is testable at all because http://localhost counts as a
// secure context; over plain HTTP to a phone the API exists and never calls
// back, which is the failure the insecure-context guard exists for and the
// one case below that has to be simulated rather than provoked.
// accuracy matters here: Playwright defaults it to 0, and a zero-accuracy fix
// deliberately draws no ring (see showPosition), so a test that left it out
// would "fail" on correct behaviour. 35m is a plausible phone GPS reading.
const REYKJAVIK = { latitude: 64.1466, longitude: -21.9426, accuracy: 35 };

// The locate button settles by re-enabling itself, whether it succeeded or
// failed. Polling merely for "the status line is visible" is not enough - the
// in-progress "Finding your location…" message satisfies that too, which made
// the first version of the refusal test below pass on a request still in
// flight.
async function waitForLocateSettled(page, selector = "leaflet-map") {
  await page.waitForFunction(
    (sel) => document.querySelector(sel).shadowRoot.querySelector('[data-action="locate"]').disabled === false,
    selector,
    { timeout: 20000 }
  );
}

test.describe("the locate control", () => {
  test.use({ permissions: ["geolocation"], geolocation: REYKJAVIK });

  test("shows where you are, with the accuracy the browser reported", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    await page.evaluate(() => document.querySelector("leaflet-map").shadowRoot.querySelector('[data-action="locate"]').click());
    await waitForLocateSettled(page);

    const here = await page.evaluate(() => {
      const el = document.querySelector("leaflet-map");
      const p = el._hereMarker.getLatLng();
      const c = el._map.getCenter();
      return {
        marker: { lat: p.lat, lng: p.lng },
        centre: { lat: c.lat, lng: c.lng },
        zoom: el._map.getZoom(),
        hasAccuracyRing: Boolean(el._hereCircle),
        radius: el._hereCircle?.getRadius() ?? null,
        status: el.shadowRoot.querySelector(".locate-status").hidden,
      };
    });

    expect(here.marker.lat).toBeCloseTo(REYKJAVIK.latitude, 3);
    expect(here.marker.lng).toBeCloseTo(REYKJAVIK.longitude, 3);
    // Centred, not merely marked - the point of the button is to take you there.
    expect(here.centre.lat).toBeCloseTo(REYKJAVIK.latitude, 2);
    expect(here.zoom, "should zoom in on the position").toBeGreaterThan(10);
    // The ring is not decoration: a 2km fix and a 5m fix look identical
    // without it and only one is worth acting on.
    expect(here.hasAccuracyRing, "an accuracy ring should be drawn").toBe(true);
    expect(here.radius, "the ring should have a real radius").toBeGreaterThan(0);
    expect(here.status, "no error line on success").toBe(true);
  });

  test("in the editor, it sets the point being picked", async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: locate spec" } });
    const tripId = (await res.json()).id;
    try {
      await gotoRoute(page, `/trips/${tripId}/locations/new`);
      await page.waitForFunction(() => document.querySelector(".location-form__map")?.hasAttribute("data-ready"));

      await page.evaluate(() =>
        document.querySelector(".location-form__map").shadowRoot.querySelector('[data-action="locate"]').click()
      );
      await waitForLocateSettled(page, ".location-form__map");

      // The single most useful case: standing somewhere and recording it.
      await expect(page.locator('.location-form input[name="lat"]')).toHaveValue(/64\.14/);
      await expect(page.locator('.location-form input[name="lng"]')).toHaveValue(/-21\.94/);
      // ...and the coordinates flowed on to the pick marker, so the two
      // markers now mean different things on the same map.
      await expect
        .poll(() => page.evaluate(() => Boolean(document.querySelector(".location-form__map")._pickMarker)))
        .toBe(true);
    } finally {
      await page.request.delete(`/api/trips/${tripId}`);
    }
  });
});

test.describe("the locate control when it cannot work", () => {
  test("an unanswered or refused prompt settles instead of hanging", async ({ page, context }) => {
    await context.clearPermissions();
    await login(page);
    await gotoTripMap(page);

    await page.evaluate(() => document.querySelector("leaflet-map").shadowRoot.querySelector('[data-action="locate"]').click());

    await waitForLocateSettled(page);

    const after = await page.evaluate(() => {
      const el = document.querySelector("leaflet-map");
      return {
        message: el.shadowRoot.querySelector(".locate-status").textContent.trim(),
        marker: Boolean(el._hereMarker),
        buttonDisabled: el.shadowRoot.querySelector('[data-action="locate"]').disabled,
      };
    });
    expect(after.marker, "a refusal must not mark a position anyway").toBe(false);
    expect(after.message.length, "the outcome should be explained").toBeGreaterThan(10);
    // Re-enabled afterwards: a refusal can be changed in site settings, so
    // the button has to remain pressable.
    expect(after.buttonDisabled, "the control should be usable again").toBe(false);
    // Not the in-progress line: this is the assertion that fails if the
    // request is left hanging, which is what PositionOptions.timeout alone
    // does when the permission prompt goes unanswered.
    expect(after.message.toLowerCase()).not.toContain("finding your location…");
  });

  test("an insecure context disables the control up front rather than hanging", async ({ page }) => {
    // The case that cannot be provoked on localhost, and the one that matters
    // most: over plain HTTP a phone's browser leaves getCurrentPosition
    // silently uncalled forever. isSecureContext is what the guard reads.
    await page.addInitScript(() => {
      Object.defineProperty(window, "isSecureContext", { get: () => false });
    });
    await login(page);
    await gotoTripMap(page);

    const state = await page.evaluate(() => {
      const el = document.querySelector("leaflet-map");
      const status = el.shadowRoot.querySelector(".locate-status");
      return {
        disabled: el.shadowRoot.querySelector('[data-action="locate"]').disabled,
        message: status.hidden ? null : status.textContent.trim(),
      };
    });
    expect(state.disabled, "the button must not be pressable at all").toBe(true);
    expect(state.message, "and it must say why").toBeTruthy();
    // Specifically about the connection, not a generic failure - it is the
    // only one of these the user can do nothing about from the page.
    expect(state.message.toLowerCase()).toMatch(/secure|http/);
  });

  test("the trip map without the attribute has no locate control at all", async ({ page }) => {
    // The control is opt-in per mount: the single-marker embed on a location's
    // view page has no use for it.
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "view location").path);
    const count = await page.evaluate(
      () => document.querySelector("leaflet-map")?.shadowRoot.querySelectorAll('[data-action="locate"]').length ?? -1
    );
    expect(count, "the view page's embed should not offer a locate button").toBe(0);
  });
});


// Milestone 7. Distance filtering on the locations list.
//
// The seeded `full` trip is Iceland: the hotel is in Reykjavik, the flight is
// ~37km away at Keflavik and Kirkjufell is ~108km up the coast - so a 5km
// radius centred on the hotel is a real filter with an unambiguous answer.
const AT_THE_HOTEL = { latitude: 64.1466, longitude: -21.9426, accuracy: 20 };

async function pickRadius(page, value) {
  const menu = page.locator(".locations-distance-slot .menu");
  await menu.locator('[data-action="toggle"]').click();
  await menu.locator(`[data-value="${value}"]`).click();
}

test.describe("distance filter on the locations list", () => {
  test.use({ permissions: ["geolocation"], geolocation: AT_THE_HOTEL });

  test("narrows the list to what is actually nearby", async ({ page }) => {
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "trip locations").path);

    const cards = page.locator("item-card");
    const before = await cards.count();
    expect(before, "the seeded trip should have several locations").toBeGreaterThan(2);

    await pickRadius(page, "5");

    // Only the hotel is within 5km of the hotel.
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).toHaveAttribute("title", /Foss Hotel/);

    // ...and going back to "any distance" restores the rest, so the filter is
    // a filter and not a one-way door.
    await pickRadius(page, "any");
    await expect(cards).toHaveCount(before);
  });

  test("keeps locations that have no coordinates, and says it did", async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: distance spec" } });
    const tripId = (await res.json()).id;
    try {
      // Near, far, and unmeasurable.
      for (const body of [
        { title: "Right here", category: "site", location: { lat: 64.1466, lng: -21.9426 } },
        { title: "Far away", category: "site", location: { lat: 64.9275, lng: -23.3106 } },
        { title: "No coordinates", category: "site", location: { address: "past the bridge" } },
      ]) {
        expect((await page.request.post(`/api/trips/${tripId}/items`, { data: body })).status()).toBe(201);
      }

      await gotoRoute(page, `/trips/${tripId}/locations`);
      await expect(page.locator("item-card")).toHaveCount(3);

      await pickRadius(page, "5");

      // The far one goes; the one with no coordinates stays. It is not far
      // away, it is unmeasurable - hiding it would make a gap in the data
      // look like a distance result.
      const titles = await page.locator("item-card").evaluateAll((els) => els.map((el) => el.getAttribute("title")));
      expect(titles.sort()).toEqual(["No coordinates", "Right here"]);

      // And the user is told, rather than left to wonder why an unplaced
      // location survived a distance filter.
      const note = page.locator(".locations-distance-note");
      await expect(note).toBeVisible();
      await expect(note).toContainText("1");
    } finally {
      await page.request.delete(`/api/trips/${tripId}`);
    }
  });
});

test.describe("distance filter when the position cannot be had", () => {
  test("a refused position resets the menu instead of showing a filter that is not applied", async ({ page, context }) => {
    await context.clearPermissions();
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "trip locations").path);

    const before = await page.locator("item-card").count();
    await pickRadius(page, "5");

    // Settles rather than hanging - the same own-timer guarantee the locate
    // control relies on. Compared against the in-progress line *exactly*: the
    // timeout message happens to begin with the same words ("Finding your
    // location took too long…"), so a substring match here passes while the
    // request is still in flight, which is the bug this asserts against.
    const IN_PROGRESS = "Finding your location\u2026";
    await expect(page.locator(".locations-distance-status")).toBeVisible({ timeout: 20000 });
    await expect
      .poll(
        () => page.evaluate(() => document.querySelector(".locations-distance-status").textContent.trim()),
        { timeout: 20000 }
      )
      .not.toBe(IN_PROGRESS);

    // The list is untouched and the trigger no longer claims to be filtering.
    await expect(page.locator("item-card")).toHaveCount(before);
    const trigger = page.locator('.locations-distance-slot [data-action="toggle"]');
    await expect(trigger).not.toHaveClass(/menu__trigger--active/);
    await expect(page.locator(".locations-distance-note")).toBeHidden();
  });

  test("the control is absent entirely where locating cannot work", async ({ page }) => {
    // A filter that can only ever fail is worse than no filter, so over plain
    // HTTP the toolbar simply does not offer one.
    await page.addInitScript(() => {
      Object.defineProperty(window, "isSecureContext", { get: () => false });
    });
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "trip locations").path);

    await expect(page.locator(".locations-distance-slot .menu")).toHaveCount(0);
    // The category filter beside it is unaffected.
    await expect(page.locator(".locations-filter-slot .menu")).toHaveCount(1);
  });
});


// Milestone 8 (sweep-up). German at 324x756 over everything this stage added.
//
// German is the longer language and these are the stage's own surfaces, so
// this is the case most likely to overflow a box or shrink a control. Several
// of the lines involved are invisible until something happens - a search
// returns nothing, locating fails, a radius hides something - so they are
// forced visible here rather than left unmeasured. That is the same trick
// settings.spec.js uses on its success line, and it exists because a sweep
// only measures what is actually rendered.
const LONGEST_LOCATE_MESSAGE_DE =
  "Für den Standort ist eine sichere Verbindung nötig. Diese Seite wird über einfaches HTTP ausgeliefert, daher gibt der Browser ihn nicht heraus.";

// Everything visible must fit, and every control must stay tappable.
async function assertFitsAndTappable(page, root) {
  const result = await page.evaluate((rootSelector) => {
    const scope = document.querySelector(rootSelector);
    const wide = [];
    const small = [];
    const walk = (node) => {
      for (const el of node.querySelectorAll("*")) {
        const style = getComputedStyle(el);
        if (style.display === "none" || style.visibility === "hidden") continue;
        // Leaflet's own markup, skipped for both checks exactly as
        // routes.spec.js skips it. Its tiles and panes are *supposed* to
        // extend past the map - .leaflet-container and .map-wrap clip them -
        // so measuring their raw boxes reports the library's internals as our
        // overflow. Measuring the first version of this test without the
        // exclusion is how that got noticed: two leaflet-tile images.
        if (el.closest('[class*="leaflet-"]')) continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        if (r.right > window.innerWidth + 1) wide.push(`${el.localName}.${el.className}`);
        const isControl = ["button", "a", "input", "select", "textarea"].includes(el.localName);
        // A native checkbox/radio is ~14px and its label is the real target.
        const nativeTick = el.localName === "input" && (el.type === "checkbox" || el.type === "radio");
        if (isControl && !nativeTick && (r.height < 44 || r.width < 44)) {
          small.push(`${el.localName}.${el.className} ${Math.round(r.width)}x${Math.round(r.height)}`);
        }
        if (el.shadowRoot) walk(el.shadowRoot);
      }
    };
    walk(scope);
    return { wide, small, docOverflow: document.documentElement.scrollWidth - window.innerWidth };
  }, root);

  expect(result.docOverflow, "document overflow in px").toBeLessThanOrEqual(0);
  expect(result.wide, "elements past the right edge").toEqual([]);
  expect(result.small, "controls under 44px").toEqual([]);
}

test.describe("Stage 13's surfaces in German at 324px", () => {
  test.use({ locale: "de-DE", viewport: MOBILE });

  test("the location editor: picker, address search and its results", async ({ page }) => {
    await page.route("**/api/geocode?*", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        // A long real display_name, which is what a formatted address is.
        body: JSON.stringify([
          { display_name: "Kirkjufell, Grundarfjarðarbær, Vesturland, 350, Ísland", lat: 64.9399, lng: -23.3075 },
          { display_name: "Reykjavík, Höfuðborgarsvæðið, Ísland", lat: 64.1466, lng: -21.9426 },
        ]),
      })
    );
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: de sweep" } });
    const tripId = (await res.json()).id;
    try {
      await gotoRoute(page, `/trips/${tripId}/locations/new`);
      await page.waitForFunction(() => document.querySelector(".location-form__map")?.hasAttribute("data-ready"));

      await page.locator('.location-search input[name="placeQuery"]').fill("Kirkjufell");
      await page.locator('[data-action="search-place"]').click();
      await expect(page.locator(".location-search__result")).toHaveCount(2);

      // ...and the failure line, which no successful search ever shows.
      await page.locator(".location-search__status").evaluate((el, text) => {
        el.hidden = false;
        el.textContent = text;
      }, "Die Adresssuche ist gerade nicht verfügbar. Du kannst den Punkt weiterhin auf der Karte setzen.");

      // The picker's own locate line, at its longest.
      await page.evaluate((text) => {
        const status = document.querySelector(".location-form__map").shadowRoot.querySelector(".locate-status");
        status.hidden = false;
        status.textContent = text;
      }, LONGEST_LOCATE_MESSAGE_DE);

      await assertFitsAndTappable(page, ".location-editor");
    } finally {
      await page.request.delete(`/api/trips/${tripId}`);
    }
  });

  test("the trip map: legend, locate control and its longest message", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    await page.evaluate((text) => {
      const status = document.querySelector("leaflet-map").shadowRoot.querySelector(".locate-status");
      status.hidden = false;
      status.textContent = text;
    }, LONGEST_LOCATE_MESSAGE_DE);

    await assertFitsAndTappable(page, ".trip-detail");
  });

  test("the locations toolbar: three controls, the distance menu and its notes", async ({ page }) => {
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "trip locations").path);

    // Open the dropdown so its rows are measured too - "Beliebige Entfernung"
    // is the longest label in it.
    await page.locator('.locations-distance-slot [data-action="toggle"]').click();
    await expect(page.locator(".locations-distance-slot .menu__dropdown")).toBeVisible();

    await page.evaluate(() => {
      for (const [sel, text] of [
        [".locations-distance-status", "Die Standortermittlung hat zu lange gedauert. Falls dein Browser nach Erlaubnis gefragt hat, beantworte das zuerst und versuche es dann erneut."],
        [".locations-distance-note", "3 Orte ohne Koordinaten werden weiterhin angezeigt."],
      ]) {
        const el = document.querySelector(sel);
        el.hidden = false;
        el.textContent = text;
      }
    });

    await assertFitsAndTappable(page, ".items-tab");
  });
});
