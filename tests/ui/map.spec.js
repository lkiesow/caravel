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
//
// Kept, rather than replaced, now that map.gesture.spec.js drives the same two
// gestures with real fingers on Chromium (Stage 19 Milestone 5). The two
// assert different things and neither subsumes the other: this one proves the
// handlers are *configured* right on the engine the rest of the suite runs,
// and it is the only place that would catch the media query itself being
// broken, since the gesture spec never consults it.
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
    const marker = sr.querySelector(".maplibregl-marker");
    if (!marker) throw new Error("no markers on the trip map - does the seed still give the `full` trip coordinates?");
    for (const type of ["mousedown", "mouseup", "click"]) {
      marker.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, view: window, button: 0 }));
    }
  });
  await page.waitForFunction(
    () => document.querySelector("leaflet-map").shadowRoot.querySelector(".maplibregl-popup-content [data-item-id]") !== null
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
      hintShown: (() => {
        const el = sr.querySelector(".gesture-hint");
        return el ? !el.hidden : false;
      })(),
      // Handler names and shapes are MapLibre's since Stage 30. dragPan is
      // mouse-and-touch panning; cooperativeGestures is what makes touch
      // panning need two fingers.
      dragPan: host._map.dragPan.isEnabled(),
      cooperativeGestures: host._map.cooperativeGestures.isEnabled(),
      touchZoomRotate: host._map.touchZoomRotate.isEnabled(),
      dragRotate: host._map.dragRotate.isEnabled(),
      innerHeight: window.innerHeight,
    };
  });
}

test.describe("the trip map at phone width", () => {
  test.use({ viewport: MOBILE });

  // This test used to assert the opposite -- that the map was *capped* at 20rem
  // and took no more than half the visible screen. That cap was Stage 13's
  // belt-and-braces beside the fix that actually mattered, dragging:
  // !isCoarsePointer(), and it left a map too small to read on the one screen
  // whose entire job is showing a map. Stage 23 Milestone 7 raised it, and what
  // is asserted now is the reason it is safe to: the page is still scrollable
  // and the legend is still above the map.
  test("the map fills the screen it is the subject of", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    const { map, legend, innerHeight } = await readMap(page);

    expect(map.height, "the trip map should be 85vh on a phone").toBeCloseTo(innerHeight * 0.85, -1);

    // The legend used to render *after* the map and land at y=769 - a dozen
    // pixels past the fold, with nothing hinting it was there.
    expect(legend.bottom, "the legend should sit above the map, not below it").toBeLessThanOrEqual(map.top + 1);
    expect(legend.top, "the legend should be above the fold").toBeLessThan(innerHeight);

    // Deliberately not the whole screen: a strip of page below the map is what
    // shows there is more underneath it.
    expect(map.height, "but not the entire screen").toBeLessThan(innerHeight);
    const scrollable = await page.evaluate(
      () => document.documentElement.scrollHeight > window.innerHeight
    );
    expect(scrollable, "the page must still have somewhere to scroll").toBe(true);
  });

  // The other two mounts sit inside a page of other content, and the single
  // blanket rule that capped the trip map was *inflating* both of them to
  // 320px. Each keeps its own height now.
  test("the smaller maps keep their own heights", async ({ page }) => {
    await login(page);

    const res = await page.request.post("/api/trips", { data: { title: "UI suite: map heights" } });
    const tripId = (await res.json()).id;
    try {
      const created = await page.request.post(`/api/trips/${tripId}/items`, {
        data: {
          category: "site",
          tags: ["viewpoint"],
          title: "Somewhere",
          location: { lat: 64.9631, lng: -19.0208, address: null },
        },
      });
      const itemId = (await created.json()).id;

      const heightOf = async (route, selector) => {
        await gotoRoute(page, route);
        await page.waitForFunction((s) => document.querySelector(s)?.hasAttribute("data-ready"), selector);
        return page.evaluate(
          (s) => Math.round(document.querySelector(s).shadowRoot.getElementById("map").getBoundingClientRect().height),
          selector
        );
      };

      // 16rem and 20rem, i.e. what their own desktop rules already say.
      expect(
        await heightOf(`/trips/${tripId}/locations/${itemId}`, "leaflet-map"),
        "a single-marker map on a location page"
      ).toBe(256);
      expect(
        await heightOf(`/trips/${tripId}/locations/new`, ".location-form__map"),
        "the coordinate picker inside a form card"
      ).toBe(320);
    } finally {
      await page.request.delete(`/api/trips/${tripId}`);
    }
  });

  test("a one-finger drag is left to the page on a touch device", async ({ page }) => {
    await pretendCoarsePointer(page);
    await login(page);
    await gotoTripMap(page);
    const { cooperativeGestures, touchZoomRotate, dragRotate, hint } = await readMap(page);

    // What this test asserts changed shape in Stage 30, because the mechanism
    // did. It used to be "Leaflet's Drag handler is off on a coarse pointer",
    // which is how one-finger drags were left to the page. MapLibre routes
    // touch panning of every finger count through dragPan, so turning that off
    // would take two-finger panning with it; cooperativeGestures raises the
    // handler's minimum to two touches instead and marks the canvas
    // touch-action: pan-x pan-y so the browser scrolls the page for the first
    // finger. Same guarantee, stated as the configuration that now provides it.
    expect(
      cooperativeGestures,
      "one finger must belong to the page, which is what cooperative gestures buy"
    ).toBe(true);
    // ...and the map must still be movable with two.
    expect(touchZoomRotate, "two-finger pan/zoom must survive").toBe(true);
    // Rotation is a capability Leaflet never had and this app does not want:
    // north stays up.
    expect(dragRotate, "the map must not rotate").toBe(false);
    // The gesture is still spelled out, but only when it happens (Stage 23
    // Milestone 6): a caption standing under the map at all times explained
    // nothing to the person who never made the gesture, and cost a line of
    // screen to everyone. That it *does* appear on a one-finger drag is
    // asserted in map.gesture.spec.js, which drives real touch.
    const { hintShown } = await readMap(page);
    expect(hintShown, "the hint should wait for the gesture rather than stand there").toBe(false);
  });
});

// The tile layer used to be a literal in leaflet-map.js, which is why the map
// could only ever speak the local language: the standard OSM tiles label
// places in the local script (Tokyo renders as the Japanese for it) and no
// parameter on them changes that. It is configuration now, so what is worth
// asserting is the wiring - that the map is built from whatever
// /api/map/config answers, rather than from a constant that merely happens to
// agree with it today.
//
// Stage 30 had to re-found this test rather than rename its selectors. There
// are no tile <img> elements to read any more: a vector tile is a .pbf fetched
// into a worker and drawn into one canvas. The claim is unchanged, so it is
// asserted from the two places it is now observable - the style document the
// map was actually given, and the requests that went out - which between them
// are a closer reading of "requested from the configured provider" than
// scraping img.src ever was, and which work for a raster provider too.
test.describe("the map follows the server's configuration", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("map data is requested from the configured provider, and it is credited", async ({ page }) => {
    await login(page);

    // Collected across the navigation rather than after it: by the time the
    // map is ready the requests have already gone.
    const requested = [];
    page.on("request", (r) => requested.push(r.url()));

    await gotoTripMap(page);

    const { configured, sources, attribution, attributionText } = await page.evaluate(async () => {
      const res = await fetch("/api/map/config", { credentials: "same-origin" });
      const host = document.querySelector("leaflet-map");
      const sr = host.shadowRoot;
      const style = host._map.getStyle();
      return {
        configured: await res.json(),
        // Every source's endpoint, whichever shape it takes: a vector source
        // names a TileJSON `url`, a raster source lists `tiles` outright.
        sources: Object.values(style.sources).flatMap((src) =>
          src.url ? [src.url] : src.tiles || []
        ),
        attribution: sr.querySelector(".maplibregl-ctrl-attrib-inner")?.innerHTML ?? null,
        attributionText: sr.querySelector(".maplibregl-ctrl-attrib-inner")?.textContent ?? null,
      };
    });

    // Which provider the config points at, in whichever of the two shapes it
    // answered with. style_url means "draw this vector style"; empty means the
    // operator pinned a raster provider through CARAVEL_TILE_URL.
    expect(sources.length, "the style should name at least one source").toBeGreaterThan(0);

    let expectedHosts;
    if (configured.style_url) {
      // The style is vendored and same-origin, so its own sources are what
      // name the third party. Read them from the served document rather than
      // hardcoding OpenFreeMap here: the point of the test is that nothing in
      // the browser holds a second opinion about where map data comes from.
      expectedHosts = await page.evaluate(async (u) => {
        const style = await (await fetch(u)).json();
        return [
          ...new Set(
            Object.values(style.sources)
              .flatMap((src) => (src.url ? [src.url] : src.tiles || []))
              .map((t) => new URL(t, location.href).host)
          ),
        ];
      }, configured.style_url);
    } else {
      // {s} dropped rather than filled in: the subdomain is rotated over
      // a, b and c, so which one any single request used is not worth
      // asserting.
      expectedHosts = [new URL(configured.tile_url.replace("{s}.", "")).host];
    }

    // Suffix rather than equality: a raster URL's {s} is expanded here into
    // a./b./c., which are the configured provider and must count as it.
    const belongs = (host) => expectedHosts.some((h) => host === h || host.endsWith(`.${h}`));
    for (const src of sources) {
      const host = new URL(src, page.url()).host;
      expect(
        belongs(host),
        `source ${src} (${host}) should belong to the configured provider: ${expectedHosts.join(", ")}`
      ).toBe(true);
    }

    // And the wire, not just the wiring: something must actually have been
    // asked of that host. The requests are aborted by blockExternalRequests,
    // but an aborted request is still a request that was made.
    const wentOut = requested.filter((u) => belongs(new URL(u).host));
    expect(
      wentOut.length,
      `the map should have asked ${expectedHosts.join(", ")} for something`
    ).toBeGreaterThan(0);

    // Attribution is served as HTML and rendered unescaped on purpose: every
    // provider's terms require a working link back, so a "fix" that escaped
    // the markup would leave the instance out of compliance with text that
    // still looks right in a screenshot. MapLibre sanitises only <script>, on*
    // handlers and javascript:/data: URLs, so an operator's credit survives.
    // The links are what is asserted, rather than the markup verbatim - the
    // DOM renders `&copy;` back out as `©`, so a string comparison would fail
    // on a correctly rendered credit.
    //
    // Only the configured credit is asserted, deliberately: the provider's own
    // credit arrives with the TileJSON, which never resolves under
    // blockExternalRequests.
    const requiredLinks = [...configured.tile_attribution.matchAll(/href="([^"]+)"/g)].map((m) => m[1]);
    expect(requiredLinks.length, "the configured attribution should carry at least one link").toBeGreaterThan(0);
    for (const href of requiredLinks) {
      expect(attribution, `the provider's credit link ${href} must survive into the DOM`).toContain(
        `href="${href}"`
      );
    }
    // And the visible text, so an anchor with no words in it would still fail.
    expect(attributionText, "the credit should read as text, not just link to somewhere").toContain(
      "OpenStreetMap"
    );
  });
});

test.describe("the trip map with a mouse", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("dragging still works and no hint stands over the map", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    const { dragPan, hintShown } = await readMap(page);
    // Unconditionally true since Stage 30, and correct: cooperative gestures
    // constrain touch, not the mouse, so click-and-drag panning is unaffected
    // on every pointer type.
    expect(dragPan, "a fine pointer should keep click-and-drag panning").toBe(true);
    expect(hintShown, "the hint should not be showing before any gesture").toBe(false);
  });

  // Stage 23 Milestone 6. Reported: "if you scroll down and the mouse cursor
  // lands over the map, you start zooming into the map". Leaflet's wheel
  // handler zooms on any wheel event, so a page scroll that crossed the map
  // became a zoom -- and on a map that is most of the screen, that is most
  // scrolls.
  async function wheelOverMap(page, { ctrl }) {
    return page.evaluate((withCtrl) => {
      const host = document.querySelector("leaflet-map");
      const mapEl = host.shadowRoot.getElementById("map");
      const before = host._map.getZoom();
      mapEl.dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, cancelable: true, deltaY: -240, ctrlKey: withCtrl })
      );
      return before;
    }, ctrl);
  }

  const zoomAndHint = (page) =>
    page.evaluate(() => {
      const host = document.querySelector("leaflet-map");
      const hint = host.shadowRoot.querySelector(".gesture-hint");
      return { zoom: host._map.getZoom(), hintShown: !!hint && !hint.hidden, hintText: hint?.textContent?.trim() };
    });

  test("a plain wheel scrolls the page and says why, instead of zooming", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    const before = await wheelOverMap(page, { ctrl: false });

    // Leaflet debounces the zoom it would have performed, so give it more
    // than wheelDebounceTime to prove the zoom never arrives.
    await page.waitForTimeout(200);
    const after = await zoomAndHint(page);
    expect(after.zoom, "a plain wheel must not zoom the map").toBe(before);
    expect(after.hintShown, "and must explain why nothing happened").toBe(true);
    expect(after.hintText).toContain("Ctrl");
  });

  test("Ctrl and the wheel still zoom, with no hint in the way", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    const before = await wheelOverMap(page, { ctrl: true });
    await expect
      .poll(async () => (await zoomAndHint(page)).zoom, { message: "Ctrl + wheel should zoom" })
      .not.toBe(before);
    expect((await zoomAndHint(page)).hintShown, "the hint is for the gesture that did not work").toBe(false);
  });

  // Reported twice while testing Milestone 6, and the second report is why the
  // wheel is now handled here rather than by Leaflet.
  //
  // First: Ctrl + wheel zoomed the whole site in Firefox. That was ours -- we
  // let the event through and relied on Leaflet's handler to cancel the
  // browser default, which makes cancelling a browser-level action depend on
  // somebody else's handler being reached.
  //
  // Then, with the default cancelled, the map still would not zoom on the
  // reporter's machine, in a build where it zoomed correctly everywhere it
  // could be measured. Rather than keep guessing at a mechanism that cannot be
  // reproduced, the component now performs the zoom itself from the
  // *direction* of the wheel alone. These tests pin that down across the
  // event shapes real devices send.
  const wheelZoom = async (page, init) => {
    const before = await page.evaluate(() => document.querySelector("leaflet-map")._map.getZoom());
    const cancelled = await page.evaluate((i) => {
      const host = document.querySelector("leaflet-map");
      return !host.shadowRoot
        .getElementById("map")
        .dispatchEvent(new WheelEvent("wheel", { bubbles: true, cancelable: true, ...i }));
    }, init);
    // The zoom is animated, so the level settles a few frames later; a
    // synchronous read here would race it and report no change. This waited a
    // flat 350ms until Stage 30 and that became a source of failures under a
    // full parallel run - the ease is 150ms, but only once the browser gets
    // around to running its frames. Waiting for the camera to actually stop is
    // both faster in the common case and not a race in the slow one. A plain
    // wheel starts no animation at all, so this returns immediately for the
    // "left entirely alone" case below.
    await page.waitForTimeout(50);
    await settleMap(page, "leaflet-map");
    const after = await page.evaluate(() => document.querySelector("leaflet-map")._map.getZoom());
    // Rounded, because since Stage 30 the starting zoom is fractional. Leaflet
    // snapped to whole levels, so `after - before` came out exactly 1;
    // MapLibre's fitBounds lands on values like 0.3289 and the same
    // subtraction gives 1.0000000000000004. The claim being tested is "one
    // notch is one level", which is about the step and not about IEEE 754.
    return { zoomed: Number((after - before).toFixed(6)), cancelled };
  };

  test("Ctrl + wheel zooms the map, whatever shape the wheel event has", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    // A mouse notch in Firefox arrives as three *lines*; in Chrome as a
    // hundred pixels. A trackpad or a tilting wheel adds a horizontal
    // component to the same gesture. All of them are the same intent.
    expect(await wheelZoom(page, { ctrlKey: true, deltaY: -3, deltaMode: 1 }), "lines").toEqual({
      zoomed: 1,
      cancelled: true,
    });
    expect(await wheelZoom(page, { ctrlKey: true, deltaY: -100, deltaMode: 0 }), "pixels").toEqual({
      zoomed: 1,
      cancelled: true,
    });
    expect(
      await wheelZoom(page, { ctrlKey: true, deltaY: -100, deltaX: -4, deltaMode: 0 }),
      "a wheel that also reports a horizontal component"
    ).toEqual({ zoomed: 1, cancelled: true });
    expect(await wheelZoom(page, { ctrlKey: true, deltaY: 100, deltaMode: 0 }), "down zooms out").toEqual({
      zoomed: -1,
      cancelled: true,
    });
    // Meta for the Mac, where Ctrl is the operating system's own zoom.
    expect(await wheelZoom(page, { metaKey: true, deltaY: -100, deltaMode: 0 }), "meta").toEqual({
      zoomed: 1,
      cancelled: true,
    });
  });

  test("a plain wheel is left entirely alone, so the page can scroll", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    // Not cancelled is the load-bearing half: cancelling it would stop the
    // page scrolling over the map, which is the complaint that started this.
    expect(await wheelZoom(page, { deltaY: -100, deltaMode: 0 })).toEqual({ zoomed: 0, cancelled: false });
  });

  // A trip with no locations shows "nothing has a location yet" laid over the
  // map -- and from Caravel v1 until Stage 23 that message was absolutely
  // positioned across the whole wrapper and hit-testable, so it swallowed
  // every mouse event the map should have had. An empty map could not be
  // dragged, clicked or interacted with at all, and nothing pointed at the
  // message as the cause: it reads as a label, not as a sheet of glass.
  //
  // Only reachable on a trip with nothing on the map, which is why no earlier
  // spec caught it -- they all use the seeded trip, which has locations.
  test("a map with no locations can still be dragged", async ({ page }) => {
    await login(page);
    const res = await page.request.post("/api/trips", { data: { title: "UI suite: empty map spec" } });
    expect(res.status()).toBe(201);
    const tripId = (await res.json()).id;

    try {
      await gotoRoute(page, `/trips/${tripId}/map`);
      await page.waitForFunction(() => document.querySelector("leaflet-map")?._map);

      const state = await page.evaluate(() => {
        const sr = document.querySelector("leaflet-map").shadowRoot;
        const b = sr.getElementById("map").getBoundingClientRect();
        const el = sr.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
        return {
          emptyShown: !!sr.querySelector(".empty"),
          topElementIsTheMap: !!el?.classList?.contains("maplibregl-canvas"),
        };
      });

      expect(state.emptyShown, "the empty-map message should still be there").toBe(true);
      expect(
        state.topElementIsTheMap,
        "the message must not be what the mouse hits, or the map takes no input at all"
      ).toBe(true);

      // And prove it by actually dragging.
      const box = await page.evaluate(() => {
        const b = document.querySelector("leaflet-map").shadowRoot.getElementById("map").getBoundingClientRect();
        return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
      });
      const centre = () =>
        page.evaluate(() => {
          const c = document.querySelector("leaflet-map")._map.getCenter();
          return `${c.lat.toFixed(4)},${c.lng.toFixed(4)}`;
        });

      const before = await centre();
      await page.mouse.move(box.x, box.y);
      await page.mouse.down();
      for (let i = 1; i <= 10; i++) await page.mouse.move(box.x - i * 12, box.y - i * 6);
      await page.mouse.up();
      await expect.poll(centre, { message: "dragging an empty map should pan it" }).not.toBe(before);
    } finally {
      await page.request.delete(`/api/trips/${tripId}`);
    }
  });

  test("Leaflet is not the one zooming, so it cannot swallow the gesture", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    const enabled = await page.evaluate(() =>
      document.querySelector("leaflet-map")._map.scrollZoom.isEnabled()
    );
    expect(enabled, "Leaflet's wheel handler must stay off; one piece of code owns the wheel").toBe(false);
  });

  test("the hint goes away on its own", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    await wheelOverMap(page, { ctrl: false });
    expect((await zoomAndHint(page)).hintShown).toBe(true);
    await expect
      .poll(async () => (await zoomAndHint(page)).hintShown, {
        message: "the overlay must not sit on the map indefinitely",
        timeout: 5000,
      })
      .toBe(false);
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
      const a = document.querySelector("leaflet-map").shadowRoot.querySelector(".maplibregl-popup-content [data-item-id]");
      return { href: a.getAttribute("href"), text: a.textContent.trim(), itemId: a.dataset.itemId };
    });

    // A real <a href>, so middle-click and "open in new tab" still work.
    expect(link.href, "the popup link should be a real, resolvable route").toBe(
      `${mapPath.replace(/\/map$/, "")}/locations/${link.itemId}`
    );
    expect(link.text, "the in-app link should be labelled").toBeTruthy();

    await page.evaluate(() => {
      document.querySelector("leaflet-map").shadowRoot.querySelector(".maplibregl-popup-content [data-item-id]").click();
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
      const a = document.querySelector("leaflet-map").shadowRoot.querySelector(".maplibregl-popup-content [data-item-id]");
      const e = new MouseEvent("click", { bubbles: true, cancelable: true, view: window, button: 0, ctrlKey: true });
      a.dispatchEvent(e);
      return e.defaultPrevented;
    });
    expect(defaultPrevented, "ctrl-click should fall through to the browser").toBe(false);
    expect(page.url(), "ctrl-click should not navigate this tab").toContain("/map");
  });

  test("the popup links meet the tap target floor on a phone", async ({ page }) => {
    // routes.spec.js's sweep skips anything under a maplibregl-* class, and
    // popup content is rendered inside .maplibregl-popup-content - so these
    // two links are invisible to it and have to be measured here.
    await page.setViewportSize(MOBILE);
    await login(page);
    await gotoTripMap(page);
    await openFirstPopup(page);

    const heights = await page.evaluate(() => {
      const sr = document.querySelector("leaflet-map").shadowRoot;
      return [...sr.querySelectorAll(".maplibregl-popup-content .popup-link")].map((a) => ({
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
// Waits for camera animation to finish. Anything reading a zoom or a centre
// after a gesture needs this: the map eases rather than jumping.
async function settleMap(page, selector) {
  await page.waitForFunction(
    (sel) => {
      const host = document.querySelector(sel);
      return host?._map && !host._map.isMoving() && !host._map.isZooming();
    },
    selector,
    { timeout: 10000 }
  );
}

async function clickPickerAt(page, fractionX, fractionY, selector = "[data-test-picker]") {
  await page.evaluate(
    ({ fx, fy, sel }) => {
      const host = document.querySelector(sel);
      // The canvas, not #map. MapLibre listens on the canvas rather than on
      // the container it was handed, so events dispatched at #map never reach
      // its handler chain and the map registers no click at all - silently,
      // which is what makes this worth stating: the old Leaflet version of
      // this helper targeted #map and worked.
      const canvas = host.shadowRoot.querySelector(".maplibregl-canvas");
      const r = canvas.getBoundingClientRect();
      const clientX = r.left + r.width * fx;
      const clientY = r.top + r.height * fy;
      const opts = { bubbles: true, cancelable: true, view: window, button: 0, clientX, clientY };
      for (const type of ["mousedown", "mouseup", "click"]) {
        canvas.dispatchEvent(new MouseEvent(type, opts));
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
        markers: host.shadowRoot.querySelectorAll(".maplibregl-marker").length,
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
      .poll(() => page.evaluate(() => document.querySelector("[data-test-picker]").shadowRoot.querySelectorAll(".maplibregl-marker").length))
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
      // The canvas stands in for Leaflet's tile pane as the "was the shadow
      // root rebuilt?" sentinel, and is a stronger one: a re-created canvas is
      // literally a new WebGL context.
      window.__canvas = host.shadowRoot.querySelector(".maplibregl-canvas");
      window.__markerEl = host.shadowRoot.querySelector(".maplibregl-marker");
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
          const p = host._pickMarker.getLngLat();
          return { lat: Math.round(p.lat * 1e4) / 1e4, lng: Math.round(p.lng * 1e4) / 1e4 };
        })
      )
      .toEqual({ lat: 48.8584, lng: 2.2945 });

    const survived = await page.evaluate(() => {
      const host = document.querySelector("[data-test-picker]");
      return {
        sameMap: host._map === window.__mapInstance,
        sameTilePane: host.shadowRoot.querySelector(".maplibregl-canvas") === window.__canvas,
        sameMarker: host.shadowRoot.querySelector(".maplibregl-marker") === window.__markerEl,
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
      const r = host.shadowRoot.querySelector(".maplibregl-marker").getBoundingClientRect();
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
        page.evaluate(() => document.querySelector("[data-test-picker]").shadowRoot.querySelectorAll(".maplibregl-marker").length)
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
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".maplibregl-marker").length))
      .toBe(1);

    // A cleared field must remove the attribute, not set it blank: "no
    // coordinate" and "the coordinate 0" are different answers.
    await page.locator('.location-form input[name="lat"]').fill("");
    await expect(picker).not.toHaveAttribute("lat", /.*/);
    await expect
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".maplibregl-marker").length))
      .toBe(0);
  });

  // Stage 23 Milestone 5. The complaint: "if a geo-location is set in the
  // location editor, the location is shown on the map, but the map is not
  // zoomed in to / centered on the set location."
  //
  // The old rule recentred on the first render and afterwards only when the
  // point left the viewport. An editor opened with no coordinates sits at the
  // world view, zoom 2, where every point on Earth is inside the bounds - so
  // typing moved a pin nobody could see.
  const view = (page) =>
    page.evaluate(() => {
      const el = document.querySelector(".location-form__map");
      const c = el._map.getCenter();
      return { zoom: el._map.getZoom(), lat: c.lat, lng: c.lng };
    });

  test("typing coordinates zooms to them instead of moving an invisible pin", async ({ page }) => {
    await openNewLocation(page);

    const before = await view(page);
    expect(before.zoom, "an empty editor should start at the world view").toBe(2);

    await page.locator('.location-form input[name="lat"]').fill("48.8584");
    await page.locator('.location-form input[name="lng"]').fill("2.2945");

    await expect.poll(async () => (await view(page)).zoom, {
      message: "the map should zoom to the typed point, not stay at world view",
    }).toBeGreaterThan(5);

    const after = await view(page);
    expect(after.lat, "and centre on it").toBeCloseTo(48.8584, 1);
    expect(after.lng).toBeCloseTo(2.2945, 1);
  });

  test("but leaves a map the person has already moved where they put it", async ({ page }) => {
    await openNewLocation(page);

    // Ctrl + wheel is the zoom gesture since Milestone 6, and a plain wheel is
    // deliberately not one: it scrolls the page and leaves the map alone, so
    // it must not count as the person having positioned this map either.
    await page.evaluate(() => {
      const mapEl = document.querySelector(".location-form__map").shadowRoot.getElementById("map");
      mapEl.dispatchEvent(new WheelEvent("wheel", { bubbles: true, cancelable: true, deltaY: -100, ctrlKey: true }));
    });
    await expect.poll(async () => (await view(page)).zoom).toBeGreaterThan(2);
    // Wait for the ease to finish before recording the view. Under Leaflet
    // setZoomAround landed on a whole level and the poll above effectively
    // caught the end of it; MapLibre eases over 150ms and the poll catches a
    // fractional zoom mid-flight, so "the view the person chose" has to mean
    // the one they were left with.
    await settleMap(page, ".location-form__map");
    const moved = await view(page);

    await page.locator('.location-form input[name="lat"]').fill("48.8584");
    await page.locator('.location-form input[name="lng"]').fill("2.2945");
    await expect
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".maplibregl-marker").length))
      .toBe(1);

    // The point is inside the visible bounds at this zoom, so nothing should
    // have moved: the person chose this view.
    const after = await view(page);
    expect(after.zoom, "a map the person zoomed must not be re-zoomed under them").toBe(moved.zoom);
  });

  // The two milestones meet here, and the meeting is easy to get wrong: a
  // plain wheel scrolls the page (Milestone 6), so it must not be recorded as
  // the person having positioned the map (Milestone 5). Getting this wrong
  // means typed coordinates silently stop zooming for anyone who scrolled the
  // page past the map first -- which is most people, since the map sits well
  // down the editor.
  test("scrolling the page past the map does not stop typed coordinates zooming", async ({ page }) => {
    await openNewLocation(page);

    await page.evaluate(() => {
      const mapEl = document.querySelector(".location-form__map").shadowRoot.getElementById("map");
      mapEl.dispatchEvent(new WheelEvent("wheel", { bubbles: true, cancelable: true, deltaY: 240 }));
    });
    await page.waitForTimeout(200);
    expect((await view(page)).zoom, "a plain wheel must not have zoomed the map").toBe(2);

    await page.locator('.location-form input[name="lat"]').fill("48.8584");
    await page.locator('.location-form input[name="lng"]').fill("2.2945");

    await expect
      .poll(async () => (await view(page)).zoom, {
        message: "typing should still zoom after a plain wheel over the map",
      })
      .toBeGreaterThan(5);
  });

  test("a point placed by clicking the map keeps the zoom the click was made at", async ({ page }) => {
    await openNewLocation(page);

    const before = await view(page);
    await clickPickerAt(page, 0.5, 0.4, ".location-form__map");
    await expect(page.locator(".location-form__map")).toHaveAttribute("lat", /.+/);

    // Clicking is how you say "there", at the zoom you are already looking at.
    // Zooming to 14 underneath that would throw away the view they chose --
    // and it is only avoided because placing the point took a mousedown on the
    // map, which is what marks the map as moved by the person.
    const after = await view(page);
    expect(after.zoom, "a clicked point must not re-zoom the map").toBe(before.zoom);
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
      .poll(() => page.evaluate(() => document.querySelector(".location-form__map").shadowRoot.querySelectorAll(".maplibregl-marker").length))
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
      const p = el._hereMarker.getLngLat();
      const c = el._map.getCenter();
      // The ring is a GeoJSON source and two layers since Stage 30, not an
      // object with a radius - MapLibre has no metre-radius circle. Its extent
      // is measured off the geometry instead, which is a better assertion
      // anyway: it reads what is on the map rather than what was asked for.
      const src = el._map.getSource("here-accuracy");
      const ring = el._hereRing?.geometry?.coordinates?.[0] ?? null;
      const lats = ring ? ring.map((c2) => c2[1]) : [];
      return {
        marker: { lat: p.lat, lng: p.lng },
        centre: { lat: c.lat, lng: c.lng },
        zoom: el._map.getZoom(),
        hasAccuracyRing: Boolean(src) && Boolean(el._map.getLayer("here-accuracy-fill")),
        // Height of the ring in metres, from its own coordinates.
        spanMetres: lats.length ? (Math.max(...lats) - Math.min(...lats)) * 111320 : null,
        reportedAccuracy: el._hereAccuracy,
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
    expect(here.reportedAccuracy, "the ring should have a real radius").toBeGreaterThan(0);
    // And the drawn geometry must match the number it was drawn from, within
    // the tolerance of a 64-sided approximation of a circle: a ring that is
    // always the same size on screen would satisfy the check above and defeat
    // the purpose entirely.
    expect(here.spanMetres).toBeGreaterThan(here.reportedAccuracy * 1.9);
    expect(here.spanMetres).toBeLessThan(here.reportedAccuracy * 2.1);
    expect(here.status, "no error line on success").toBe(true);
  });

  // Two claims about the ring, and they fail in different ways.
  //
  // The first is that it is the size it says it is. accuracyRing() converts
  // metres to degrees, and the longitude half has to be divided by cos(lat) or
  // the ring is an ellipse that is too narrow everywhere except the equator -
  // at Reykjavik, cos(64 degrees) is about 0.44, so getting that wrong makes
  // the ring less than half as wide as it should be. Measuring the east-west
  // span on the ground is what catches it; the north-south span, which the
  // test above checks, would look perfect either way.
  //
  // The second is that it is anchored to the ground rather than to the screen.
  // MapLibre's circle layer takes a *pixel* radius, which would give a fixed
  // dot silently claiming a different distance at every zoom - and a 2km fix
  // and a 5m fix would look identical again, which is the bug the ring exists
  // to prevent. A ring in degrees grows on screen as you zoom in; a ring in
  // pixels does not.
  test("the accuracy ring is the size it claims, at any zoom", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    await page.evaluate(() =>
      document.querySelector("leaflet-map").shadowRoot.querySelector('[data-action="locate"]').click()
    );
    await waitForLocateSettled(page);

    const measure = async (zoom) => {
      await page.evaluate((z) => {
        const m = document.querySelector("leaflet-map")._map;
        m.jumpTo({ center: m.getCenter(), zoom: z });
      }, zoom);
      await settleMap(page, "leaflet-map");
      return page.evaluate(() => {
        const el = document.querySelector("leaflet-map");
        const ring = el._hereRing.geometry.coordinates[0];
        const lngs = ring.map((c) => c[0]);
        const lats = ring.map((c) => c[1]);
        const midLat = (Math.max(...lats) + Math.min(...lats)) / 2;
        // Degrees of longitude are shorter away from the equator, so the
        // east-west span only converts to metres through cos(latitude).
        const eastWest =
          (Math.max(...lngs) - Math.min(...lngs)) * 111320 * Math.cos((midLat * Math.PI) / 180);
        const ys = ring.map((c) => el._map.project(c).y);
        return {
          eastWest,
          accuracy: el._hereAccuracy,
          pixelSpan: Math.max(...ys) - Math.min(...ys),
        };
      });
    };

    const at12 = await measure(12);
    const at13 = await measure(13);
    const at14 = await measure(14);

    // Diameter, so twice the reported accuracy. The tolerance covers the
    // 64-sided approximation of a circle, nothing more.
    for (const [name, m] of [["z12", at12], ["z13", at13], ["z14", at14]]) {
      expect(m.eastWest, `${name}: the ring should be as wide as the fix it reports`).toBeGreaterThan(
        m.accuracy * 1.9
      );
      expect(m.eastWest, `${name}: and no wider`).toBeLessThan(m.accuracy * 2.1);
    }

    // ...and it is on the ground, not on the glass.
    expect(at12.pixelSpan, "the ring should have a measurable size on screen").toBeGreaterThan(0);
    expect(at13.pixelSpan / at12.pixelSpan, "one zoom level should double it on screen").toBeGreaterThan(1.9);
    expect(at13.pixelSpan / at12.pixelSpan).toBeLessThan(2.1);
    expect(at14.pixelSpan / at13.pixelSpan, "and again").toBeGreaterThan(1.9);
    expect(at14.pixelSpan / at13.pixelSpan).toBeLessThan(2.1);
  });

  // Sources and layers are destroyed by setStyle(); markers and popups are DOM
  // and are not. Nothing in the app restyles a map yet - Milestone 5 will, on
  // every light/dark change - so this drives setStyle directly rather than
  // waiting for a feature to exist. Without applyOverlays() bound to
  // style.load, the ring simply vanishes the first time the theme changes and
  // nothing else reports it.
  test("the accuracy ring survives the style being replaced", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    await page.evaluate(() =>
      document.querySelector("leaflet-map").shadowRoot.querySelector('[data-action="locate"]').click()
    );
    await waitForLocateSettled(page);

    const before = await page.evaluate(() => {
      const el = document.querySelector("leaflet-map");
      const c = el._map.getCenter();
      return {
        ring: Boolean(el._map.getSource("here-accuracy")),
        fill: Boolean(el._map.getLayer("here-accuracy-fill")),
        line: Boolean(el._map.getLayer("here-accuracy-line")),
        markerEl: el.shadowRoot.querySelectorAll(".maplibregl-marker").length,
        centre: { lat: c.lat, lng: c.lng },
        zoom: el._map.getZoom(),
        geometry: el._hereRing.geometry.coordinates[0].length,
      };
    });
    expect(before.ring && before.fill && before.line, "the ring should be there to begin with").toBe(true);

    // The other vendored style, so this is the real operation Milestone 5 runs
    // rather than a re-application of the same document.
    await page.evaluate(async () => {
      const el = document.querySelector("leaflet-map");
      const style = await (await fetch("/js/vendor/map-styles/dark.json")).json();
      el._map.setStyle(style, { diff: false });
    });
    await page.waitForFunction(
      () => document.querySelector("leaflet-map")._map.isStyleLoaded(),
      null,
      { timeout: 20000 }
    );

    const after = await page.evaluate(() => {
      const el = document.querySelector("leaflet-map");
      const c = el._map.getCenter();
      return {
        ring: Boolean(el._map.getSource("here-accuracy")),
        fill: Boolean(el._map.getLayer("here-accuracy-fill")),
        line: Boolean(el._map.getLayer("here-accuracy-line")),
        markerEl: el.shadowRoot.querySelectorAll(".maplibregl-marker").length,
        centre: { lat: c.lat, lng: c.lng },
        zoom: el._map.getZoom(),
        geometry: el._hereRing.geometry.coordinates[0].length,
      };
    });

    expect(after.ring, "the source must be re-added after a restyle").toBe(true);
    expect(after.fill, "and its fill layer").toBe(true);
    expect(after.line, "and its outline").toBe(true);
    expect(after.geometry, "with the same geometry").toBe(before.geometry);
    // Markers are DOM, so they should never have been at risk - asserted so a
    // future change that moves them into the style would be noticed.
    expect(after.markerEl, "markers should survive a restyle untouched").toBe(before.markerEl);
    // And the camera, which setStyle preserves: a restyle must not look like a
    // navigation.
    expect(after.centre.lat).toBeCloseTo(before.centre.lat, 6);
    expect(after.centre.lng).toBeCloseTo(before.centre.lng, 6);
    expect(after.zoom).toBeCloseTo(before.zoom, 6);
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

// Since Stage 26 Milestone 4 the distance filter is a group inside the one
// filter menu rather than a button of its own, so choosing a radius is three
// clicks: open, drill into Distance, pick. The assertions below are unchanged --
// the point of that milestone was that the filtering behaviour did not move.
async function pickRadius(page, value) {
  const menu = page.locator(".locations-filter-slot .menu");
  await menu.locator('[data-action="toggle"]').click();
  await menu.locator('[data-group="distance"]').click();
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

  test("clearing resets the distance filter without asking for the position again", async ({ page }) => {
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "trip locations").path);

    const cards = page.locator("item-card");
    const all = await cards.count();

    // Two filters at once, which is the case Clear exists for: undoing them
    // one at a time means opening the menu once per filter and drilling into
    // each.
    const menu = page.locator(".locations-filter-slot .menu");
    await pickRadius(page, "5");
    await expect(cards).toHaveCount(1);

    await menu.locator('[data-action="toggle"]').click();
    await menu.locator('[data-group="category"]').click();
    await menu.locator('[data-value="stay"]').click();

    await menu.locator('[data-action="toggle"]').click();
    await menu.locator('[data-action="clear"]').click();

    // Both filters are off in one action, and the list is whole again.
    await expect(cards).toHaveCount(all);
    await menu.locator('[data-action="toggle"]').click();
    await expect(menu.locator('[data-group="distance"]')).toHaveText("Any distance");
    await expect(menu.locator('[data-group="category"]')).toHaveText("All categories");

    // Clearing must not re-enter the path that asks the device where it is:
    // a filter being switched off is not a reason to prompt anybody. The
    // status line stays empty, which is what that path writes to.
    await expect(page.locator(".locations-distance-status")).toBeHidden();
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
    // One trigger for every filter since Stage 26 Milestone 4, so "not
    // filtering" is now a claim about the whole menu -- which is right here,
    // since the category filter is untouched and distance fell back to "any".
    const trigger = page.locator('.locations-filter-slot [data-action="toggle"]');
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

    // The menu is still there -- it holds every filter now -- but distance is
    // not one of the rows in it. Omitted rather than shown and disabled: a row
    // that can only ever fail is the thing this test says must not exist.
    await page.locator('.locations-filter-slot [data-action="toggle"]').click();
    await expect(page.locator('[data-group="distance"]')).toHaveCount(0);
    // The category filter beside it is unaffected.
    await expect(page.locator('[data-group="category"]')).toHaveCount(1);
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
        // The map library's own markup, skipped for both checks exactly as
        // routes.spec.js skips it. Under Leaflet this was about tiles and
        // panes measuring wider than the map; MapLibre draws into one canvas
        // and does not, but the exclusion is still wanted for its controls -
        // the attribution toggle and the popup close button are both well
        // under a 44px tap target and neither is ours to size.
        if (el.closest('[class*="maplibregl-"]')) continue;
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

// The reason this stage exists. Raster tiles bake their labels in before
// anyone asks, so an instance had one language for everybody; a vector style
// draws them in the browser, so the label expression can follow the reader.
//
// Asserted against the style object rather than against rendered glyphs,
// deliberately: blockExternalRequests stops the fonts and the tiles, so
// nothing is drawn to read. That the *drawing* follows the expression was
// confirmed by hand against live tiles - Prague/Prag, Warsaw/Warschau,
// Cologne/Köln - and is recorded in plans/stage-30.md rather than pretended at
// here.
test.describe("map labels follow the reader's language", () => {
  const labelChains = (page) =>
    page.evaluate(() => {
      const layers = document.querySelector("leaflet-map")._map.getStyle().layers;
      const withText = layers.filter((l) => l.layout?.["text-field"]);
      return {
        total: withText.length,
        localised: withText
          .filter((l) => l.layout["text-field"][0] === "coalesce")
          .map((l) => l.layout["text-field"].slice(1).map((g) => g[1]).join(",")),
        untouched: withText
          .filter((l) => l.layout["text-field"][0] !== "coalesce")
          .map((l) => JSON.stringify(l.layout["text-field"])),
      };
    });

  test("English asks for English names, and falls back rather than blanking", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    const { total, localised, untouched } = await labelChains(page);

    expect(localised.length, "most label layers should be localised").toBeGreaterThan(10);
    // One chain, every layer: the reader's locale, then whatever Latin-script
    // name exists, then the local name. The last two are what keeps a place
    // labelled at all where there is no translation - a bare ["get","name:en"]
    // would blank every unlabelled feature on the map.
    expect([...new Set(localised)]).toEqual(["name:en,name_en,name:latin,name"]);
    expect(total).toBe(localised.length + untouched.length);
  });

  test("road shields keep their numbers", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    const { untouched } = await labelChains(page);

    // The trap in "rewrite every text-field layer": three layers in positron
    // are motorway shields, whose text is ["to-string", ["get","ref"]]. A
    // shield reads "A1" - not a name, with no translation - so localising it
    // would blank every shield on the map. They must be left exactly as they
    // came.
    expect(untouched.length, "the shield layers should still be there").toBeGreaterThan(0);
    for (const expr of untouched) {
      expect(expr, "a non-name label must not have been rewritten").toBe('["to-string",["get","ref"]]');
    }
  });

  test.describe("in German", () => {
    test.use({ locale: "de-DE" });

    test("the same map asks for German names", async ({ page }) => {
      await login(page);
      await gotoTripMap(page);
      const { localised } = await labelChains(page);
      expect([...new Set(localised)]).toEqual(["name:de,name_de,name:latin,name"]);
    });
  });

  // The locale is a client-side preference that never reaches the server, so
  // nothing refetches the style - the route re-renders and the component
  // rebuilds. Worth asserting rather than assuming: the map is the only part
  // of the app whose *content* comes from a document built at construction
  // time, so a re-render that reused the existing map would leave the labels
  // in the old language with everything around them switched.
  test("switching language in the app relabels the map", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    expect([...new Set((await labelChains(page)).localised)]).toEqual([
      "name:en,name_en,name:latin,name",
    ]);

    await page.evaluate(async () => {
      const { setLocale } = await import("/js/i18n.js");
      await setLocale("de");
    });
    await page.waitForFunction(
      () => document.querySelector("leaflet-map")?.hasAttribute("data-ready"),
      null,
      { timeout: 20000 }
    );
    await expect
      .poll(async () => [...new Set((await labelChains(page)).localised)].join("|"), {
        message: "the map should relabel itself when the app language changes",
        timeout: 15000,
      })
      .toBe("name:de,name_de,name:latin,name");
  });

  // The other two mounts build their own maps, and the editor's picker in
  // particular is constructed by a different page with different attributes.
  test("the location view and the editor picker localise too", async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem("caravel.locale", "de"));
    await login(page);
    const routes = await buildRoutes(page);

    // "new location" rather than "edit location" for the picker: it needs no
    // surviving seeded row, and the specs that write are the reason the suite
    // has an isolation problem (todo.md). Same mount, same claim, less shared
    // state to depend on.
    for (const label of ["view location", "new location"]) {
      const route = routes.find((r) => r.label === label);
      expect(route, `the sweep should know a ${label} route`).toBeTruthy();
      await gotoRoute(page, route.path);
      const chains = await page.evaluate(() => {
        const host = document.querySelector("leaflet-map");
        return host._map
          .getStyle()
          .layers.filter((l) => l.layout?.["text-field"]?.[0] === "coalesce")
          .map((l) => l.layout["text-field"][1][1]);
      });
      expect(chains.length, `${label} should have localised label layers`).toBeGreaterThan(10);
      expect([...new Set(chains)], `${label} should ask for German`).toEqual(["name:de"]);
    }
  });
});

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
    await page.locator('.locations-filter-slot [data-action="toggle"]').click();
    await expect(page.locator(".locations-filter-slot .menu__dropdown")).toBeVisible();
    // Drill into Distance, so the radius rows are on screen when this is
    // measured -- "Beliebige Entfernung" is still the longest label in here.
    await page.locator('[data-group="distance"]').click();

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


// Stage 29 Milestone 1. The outbound Google Maps URL was written out three
// times -- internal/httpapi/map.go, and twice in web/js -- and the three had
// drifted: the Go copy used %f while both JS copies interpolated the raw
// number, so the same place produced a different URL depending on which link
// you clicked. It is one helper per language now, deliberately identical.
//
// This is the assertion that makes that refactor worth having, and it is the
// only place in the suite that compares the three. It deliberately reaches all
// three renderers for *one* location: the trip-map popup (built by the server
// and passed through as item.google_maps_url), the single-marker popup on that
// location's own page, and the location view's own link beside the map. Two of
// those three are in a shadow root.
test.describe("the Google Maps link is built in one place", () => {
  // The single-marker popup, unlike the trip-wide one, has no [data-item-id]
  // link to wait for -- it is on the location's own page, so linking there
  // would link to itself. So this waits for the popup's only anchor instead.
  async function openSingleMarkerPopup(page) {
    await page.evaluate(() => {
      const sr = document.querySelector("leaflet-map").shadowRoot;
      const marker = sr.querySelector(".maplibregl-marker");
      if (!marker) throw new Error("the location page's map embed has no marker");
      for (const type of ["mousedown", "mouseup", "click"]) {
        marker.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, view: window, button: 0 }));
      }
    });
    return page.waitForFunction(() => {
      const a = document.querySelector("leaflet-map").shadowRoot.querySelector(".maplibregl-popup-content a[target=_blank]");
      return a ? a.getAttribute("href") : false;
    });
  }

  test("all three renderers agree, for the same location", async ({ page }) => {
    await login(page);
    const mapPath = await gotoTripMap(page);
    await openFirstPopup(page);

    // The trip-wide popup: this href is the server's, verbatim.
    const fromTripMap = await page.evaluate(() => {
      const sr = document.querySelector("leaflet-map").shadowRoot;
      const links = [...sr.querySelectorAll(".maplibregl-popup-content a")];
      const google = links.find((a) => a.getAttribute("target") === "_blank");
      return { href: google?.getAttribute("href") ?? null, itemId: sr.querySelector("[data-item-id]").dataset.itemId };
    });
    expect(fromTripMap.href, "the trip map popup should offer a Google Maps link").toBeTruthy();

    // Sanity-check the *form* too, not only that the three agree: three
    // identical wrong URLs would pass an equality-only test.
    //
    // Since Milestone 2 a seeded location has a title, so the link must be the
    // named form -- a text search in the path segment, biased by /@lat,lng,17z.
    // The old coordinate query would land on a dropped pin, which is the whole
    // bug this stage exists to fix, so its absence is asserted explicitly.
    expect(fromTripMap.href, "a named place should not get the coordinate query").not.toContain("?api=1&query=");
    expect(fromTripMap.href).toMatch(/^https:\/\/www\.google\.com\/maps\/search\/[^/]+\/@-?[\d.]+,-?[\d.]+,17z$/);
    expect(fromTripMap.href, "%f would leave trailing zeros on the coordinates").not.toMatch(/0{4},|0{4},17z/);

    // The bias must be the path segment. Coordinates inside `query` are read as
    // literal text -- measured during Stage 29 planning, where a name plus a
    // Paris coordinate pair returned results in San Francisco -- so a refactor
    // that moved them back into the query string would silently restore the bug.
    expect(fromTripMap.href.split("/@")[1], "the bias should carry the zoom").toMatch(/,17z$/);

    // Now the same location's own page, which renders the other two.
    const tripId = mapPath.split("/")[2];
    await gotoRoute(page, `/trips/${tripId}/locations/${fromTripMap.itemId}`);
    await page.waitForFunction(() => document.querySelector("leaflet-map")?.hasAttribute("data-ready"));

    // Not a bare .location-view__maps-link: Stage 29 Milestone 3 added an
    // OpenStreetMap link beside the Google one under the same class, which
    // made this locator ambiguous and the test a strict-mode failure on main.
    // Named rather than positional, so a third link would not silently
    // re-point it.
    const fromLocationView = await page
      .locator('.location-view__maps-link[data-i18n="map.viewOnGoogleMaps"]')
      .getAttribute("href");
    const fromSingleMarker = await (await openSingleMarkerPopup(page)).jsonValue();

    expect(fromLocationView, "the location view's link should match the server's").toBe(fromTripMap.href);
    expect(fromSingleMarker, "the single-marker popup's link should match the server's").toBe(fromTripMap.href);
  });
});

// Stage 29 Milestone 2. The fallback, which is the form every link in Caravel
// had before this stage: a place with no usable name has nothing to search for,
// so the coordinate query -- and the dropped pin it produces -- is the honest
// answer rather than a bug. Asserted through the real render path by blanking
// the title on the component that builds the link.
test.describe("a place with no name falls back to the coordinate link", () => {
  test("the single-marker popup drops back to ?api=1&query=", async ({ page }) => {
    await login(page);
    const routes = await buildRoutes(page);
    await gotoRoute(page, routes.find((r) => r.label === "trip map").path);
    await page.waitForFunction(() => document.querySelector("leaflet-map")?.hasAttribute("data-ready"));

    // Mount a single-marker embed with coordinates but no marker-title, which
    // is the state a location saved without a title would render.
    const href = await page.evaluate(async () => {
      const el = document.createElement("leaflet-map");
      el.setAttribute("lat", "64.1");
      el.setAttribute("lng", "-21.9");
      el.dataset.testNoTitle = "1";
      document.querySelector(".page").append(el);
      await new Promise((r) => {
        const done = () => (el.hasAttribute("data-ready") ? r() : requestAnimationFrame(done));
        done();
      });
      const sr = el.shadowRoot;
      const marker = sr.querySelector(".maplibregl-marker");
      for (const type of ["mousedown", "mouseup", "click"]) {
        marker.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, view: window, button: 0 }));
      }
      const a = sr.querySelector(".maplibregl-popup-content a[target=_blank]");
      const out = a.getAttribute("href");
      el.remove();
      return out;
    });

    expect(href).toBe("https://www.google.com/maps/search/?api=1&query=64.1,-21.9");
  });
});
