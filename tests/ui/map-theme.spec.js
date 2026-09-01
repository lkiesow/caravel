// How the map is lit, which Stage 30 Milestone 5 made a separate question from
// how the app is lit.
//
// Four states rather than the interface's three, because "follow the app" is
// itself a choice here rather than the only behaviour: a bright map inside a
// dark app is a legitimate preference, and "day / night" follows the sun where
// the reader is, which is not what the operating system's dark mode tracks.
//
// Its own file rather than more of map.spec.js, which is already ~1900 lines,
// and deliberately using its own trip where it writes nothing - the suite's
// isolation problem (plans/todo.md) is worst in the specs that lean hardest on
// the shared seed.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute } from "./helpers/scenarios.js";

const MOBILE = { width: 324, height: 756 };

// The two vendored cartographies, by the one thing that tells them apart
// without fetching anything: their background layer.
const LIGHT_BG = "rgb(242,243,240)";
const DARK_BG = "rgb(12,12,12)";

async function gotoTripMap(page) {
  const routes = await buildRoutes(page);
  const route = routes.find((r) => r.label === "trip map");
  expect(route, "the sweep should know a trip map route").toBeTruthy();
  await gotoRoute(page, route.path);
  await page.waitForFunction(() => document.querySelector("leaflet-map")?._map, null, { timeout: 20000 });
}

const setMode = async (page, mode) => {
  await page.evaluate(async (m) => {
    const { setMapTheme } = await import("/js/map-theme.js");
    setMapTheme(m);
  }, mode);
};

const mapState = (page) =>
  page.evaluate(() => {
    const host = document.querySelector("leaflet-map");
    // getStyle() is undefined *during* a swap - MapLibre drops the old style
    // before the new one has parsed - so this has to survive being called mid
    // transition. It is polled until the map settles, and a null background is
    // exactly the "not yet" the poll is waiting out. Without the guard the
    // helper throws instead of returning "not yet", which showed up only under
    // a full parallel run, where the window is wide enough to land in.
    const bg = host._map.getStyle()?.layers?.find((l) => l.type === "background");
    const marker = host.shadowRoot.querySelector(".maplibregl-marker");
    const centre = host._map.getCenter();
    return {
      scheme: host.dataset.scheme,
      background: bg?.paint?.["background-color"] ?? null,
      markerColor: marker ? getComputedStyle(marker).backgroundColor : null,
      legendDot: (() => {
        const d = host.shadowRoot.querySelector(".legend .dot");
        return d ? getComputedStyle(d).backgroundColor : null;
      })(),
      markers: host.shadowRoot.querySelectorAll(".maplibregl-marker").length,
      lat: centre.lat,
      lng: centre.lng,
      zoom: host._map.getZoom(),
    };
  });

// Waits for the restyle to land: setMapTheme announces, the component fetches
// a style and swaps it, so the scheme attribute changes a beat before the
// style does.
const waitForScheme = async (page, scheme, background) => {
  await expect
    .poll(async () => `${(await mapState(page)).scheme}/${(await mapState(page)).background}`, {
      message: `the map should settle on the ${scheme} cartography`,
      timeout: 20000,
    })
    .toBe(`${scheme}/${background}`);
};

test.describe("the map's own light and dark", () => {
  test("light and dark are explicit choices, whatever the app is doing", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    await setMode(page, "dark");
    await waitForScheme(page, "dark", DARK_BG);

    await setMode(page, "light");
    await waitForScheme(page, "light", LIGHT_BG);
  });

  // The point of the whole arrangement: a restyle is not a rebuild. Somebody
  // who has panned to the far side of a trip and switched the map to dark
  // should still be looking at the same place.
  test("switching keeps the view and the markers", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    await page.evaluate(() =>
      document.querySelector("leaflet-map")._map.jumpTo({ center: [12.57, 55.68], zoom: 9 })
    );
    const before = await mapState(page);
    expect(before.markers, "the trip map should have markers to lose").toBeGreaterThan(0);

    await setMode(page, "dark");
    await waitForScheme(page, "dark", DARK_BG);
    const after = await mapState(page);

    expect(after.lat, "the camera must survive a restyle").toBeCloseTo(before.lat, 6);
    expect(after.lng).toBeCloseTo(before.lng, 6);
    expect(after.zoom).toBeCloseTo(before.zoom, 6);
    // Markers are DOM and belong to the map, not to the style - asserted so a
    // future change that moves them into the style is noticed here rather than
    // by a reader whose pins vanished.
    expect(after.markers, "markers must survive a restyle").toBe(before.markers);
  });

  // A colour chosen to read on near-white paper does not read on near-black.
  // The palette is two sets of custom properties on the host, so the markers
  // already on the map recolour without being rebuilt.
  test("markers and their legend keys recolour together", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    await setMode(page, "light");
    await waitForScheme(page, "light", LIGHT_BG);
    const light = await mapState(page);

    await setMode(page, "dark");
    await waitForScheme(page, "dark", DARK_BG);
    const dark = await mapState(page);

    expect(light.markerColor, "a marker should be coloured at all").toBeTruthy();
    expect(dark.markerColor, "the dark map needs its own marker colours").not.toBe(light.markerColor);
    // The legend names the markers, so a legend key in a different colour from
    // the pin it describes would be a lie. This is the assertion that catches
    // one of the two being changed alone.
    expect(dark.legendDot, "the legend key must match its marker").toBe(dark.markerColor);
    expect(light.legendDot).toBe(light.markerColor);
  });

  test("follow-app is the default, and it follows the app", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    // Nothing stored: the default is the absence of a key, as theme.js does it.
    const stored = await page.evaluate(() => localStorage.getItem("caravel.mapTheme"));
    expect(stored, "the default should not be written to storage").toBeNull();

    await page.evaluate(async () => {
      const { setTheme } = await import("/js/theme.js");
      setTheme("dark");
    });
    await waitForScheme(page, "dark", DARK_BG);

    await page.evaluate(async () => {
      const { setTheme } = await import("/js/theme.js");
      setTheme("light");
    });
    await waitForScheme(page, "light", LIGHT_BG);
  });

  // The map is the only surface with a fourth mode, and this is why it exists:
  // the app's "auto" follows the operating system, which is a switch somebody
  // set at home. Where the sun actually is is a fact about right now and about
  // where the map is looking.
  test("day / night follows the sun rather than the operating system", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    // The app is left in light mode throughout, so anything that happens below
    // cannot be it following the interface.
    await page.evaluate(async () => {
      const { setTheme } = await import("/js/theme.js");
      setTheme("light");
    });

    // A remembered fix is what "auto" prefers, and the locate control is what
    // normally writes one. Written directly here so the test can put the
    // reader somewhere with a known sun without a geolocation prompt - which
    // is also the guarantee being relied on in production: this mode never
    // asks.
    const place = async (lat, lng) => {
      await page.evaluate(
        async ([la, ln]) => {
          const { rememberPosition } = await import("/js/map-theme.js");
          rememberPosition({ lat: la, lng: ln });
        },
        [lat, lng]
      );
    };

    await setMode(page, "auto");

    // Two points on opposite sides of the planet: whatever the hour, one of
    // them is in daylight and the other is not, so this asserts a real
    // difference without depending on when the suite runs.
    await place(0, 0);
    const atNull = (await mapState(page)).scheme;
    await page.waitForTimeout(200);
    await place(0, 180);
    await expect
      .poll(async () => (await mapState(page)).scheme, {
        message: "the antipode should be lit the other way",
        timeout: 20000,
      })
      .not.toBe(atNull);

    // And it must agree with the module that does the arithmetic, rather than
    // merely differing.
    const agrees = await page.evaluate(async () => {
      const { isDaylight } = await import("/js/sun.js");
      const host = document.querySelector("leaflet-map");
      return { expected: isDaylight(0, 180) ? "light" : "dark", actual: host.dataset.scheme };
    });
    expect(agrees.actual, "the map should be lit the way the sun says").toBe(agrees.expected);
  });
});

// The solar arithmetic on its own. This is the kind of code that is silently
// wrong for a year, so it is pinned against places and moments whose answers
// are not in dispute - and in particular against the polar cases, which are
// the reason it computes an altitude rather than solving for a sunrise time.
test.describe("the sun", () => {
  const at = (page, lat, lng, iso) =>
    page.evaluate(
      async ([la, ln, s]) => {
        const { solarAltitude, isDaylight } = await import("/js/sun.js");
        const d = new Date(s);
        return { altitude: solarAltitude(la, ln, d), daylight: isDaylight(la, ln, d) };
      },
      [lat, lng, iso]
    );

  test("knows day from night, including where the sun does not set", async ({ page }) => {
    await login(page);

    // Ordinary places, both hemispheres.
    expect((await at(page, 51.5, -0.13, "2026-06-21T12:00:00Z")).daylight, "London, midsummer noon").toBe(true);
    expect((await at(page, 51.5, -0.13, "2026-06-21T00:00:00Z")).daylight, "London, midsummer midnight").toBe(false);
    expect((await at(page, -33.87, 151.21, "2026-06-21T02:00:00Z")).daylight, "Sydney, local noon").toBe(true);
    expect((await at(page, -33.87, 151.21, "2026-06-21T14:00:00Z")).daylight, "Sydney, local midnight").toBe(false);

    // The polar cases. A formula that solves for a sunrise *time* has no
    // answer here and typically returns NaN, which reads as night and would
    // give a Norwegian summer a dark map at noon.
    expect((await at(page, 69.65, 18.96, "2026-06-21T23:00:00Z")).daylight, "Tromso, midnight sun").toBe(true);
    expect((await at(page, -77.85, 166.67, "2026-12-21T15:00:00Z")).daylight, "McMurdo, polar day").toBe(true);
    // Svalbard in December is below civil twilight all day, so it is dark even
    // at local noon...
    expect((await at(page, 78.22, 15.65, "2026-12-21T11:00:00Z")).daylight, "Longyearbyen, polar night").toBe(false);
    // ...while Tromso on the same day never gets the sun above the horizon but
    // does reach civil twilight, which is a dim blue daylight and the
    // brightest part of its day. Light is the right answer there, and the -6
    // degree threshold is what separates the two.
    const tromso = await at(page, 69.65, 18.96, "2026-12-21T11:00:00Z");
    expect(tromso.altitude, "the sun really is below the horizon").toBeLessThan(0);
    expect(tromso.daylight, "but it is civil twilight, not night").toBe(true);
  });

  test("computes an altitude that matches the almanac", async ({ page }) => {
    await login(page);

    // Solar noon at Greenwich on the equinox, and midsummer noon in London:
    // both cross-checked against NOAA's solar calculator.
    expect((await at(page, 51.4778, 0, "2026-03-20T12:00:00Z")).altitude).toBeCloseTo(38.4, 0);
    expect((await at(page, 51.5, -0.13, "2026-06-21T12:00:00Z")).altitude).toBeCloseTo(61.9, 0);

    // The equator on the equinox is nearly overhead - but at 12:08 UTC, not at
    // 12:00: the equation of time puts solar noon about eight minutes late in
    // late March, which is 2 degrees of rotation. Asserting it at 12:00 and
    // expecting 90 is how this test was wrong the first time it was written.
    const noon = await at(page, 0, 0, "2026-03-20T12:08:00Z");
    expect(noon.altitude, "equator, equinox, actual solar noon").toBeGreaterThan(89);
  });
});

test.describe("the map appearance setting", () => {
  test.use({ viewport: MOBILE });

  test("offers four choices and remembers the one picked", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");

    const choices = page.locator('.map-appearance-slot input[name="map-theme"]');
    await expect(choices).toHaveCount(4);
    await expect(page.locator('.map-appearance-slot [name="map-theme"][value="app"]')).toBeChecked();

    await page.locator('.map-appearance-slot [name="map-theme"][value="dark"]').check();
    expect(await page.evaluate(() => localStorage.getItem("caravel.mapTheme"))).toBe("dark");

    // And it survives a reload, which is the whole point of storing it.
    await page.reload();
    await expect(page.locator('.map-appearance-slot [name="map-theme"][value="dark"]')).toBeChecked();

    // Back to the default, which is stored as the absence of a key so that a
    // browser told "follow the app" and one never told anything are the same
    // state.
    await page.locator('.map-appearance-slot [name="map-theme"][value="app"]').check();
    expect(await page.evaluate(() => localStorage.getItem("caravel.mapTheme"))).toBeNull();
  });

  test("its controls are reachable at 324px", async ({ page }) => {
    await login(page);
    await gotoRoute(page, "/settings");

    const labels = page.locator(".map-appearance-slot .setting-choice");
    await expect(labels).toHaveCount(4);
    for (let i = 0; i < 4; i++) {
      const box = await labels.nth(i).boundingBox();
      expect(box.height, "a settings choice is a tap target").toBeGreaterThanOrEqual(40);
      expect(box.x + box.width, "and must not run off a 324px screen").toBeLessThanOrEqual(324);
    }
  });
});
