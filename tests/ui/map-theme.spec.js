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
//
// Compared as parsed colours rather than as strings, because the two styles
// write theirs differently -- liberty uses "#f8f4f0" and dark uses
// "rgb(12,12,12)" -- and a string comparison would break on a restyle that
// changed nothing but the notation.
const LIGHT_BG = [248, 244, 240];
const DARK_BG = [12, 12, 12];

async function gotoTripMap(page) {
  const routes = await buildRoutes(page);
  const route = routes.find((r) => r.label === "trip map");
  expect(route, "the sweep should know a trip map route").toBeTruthy();
  await gotoRoute(page, route.path);
  await page.waitForFunction(() => document.querySelector("map-view")?._map, null, { timeout: 20000 });
}

// Clicks the first marker and waits for its popup. Markers live in the shadow
// root and the library listens for mouse events rather than for .click(), so
// this dispatches real ones - the same approach map.spec.js takes.
async function openFirstPopup(page) {
  await page.evaluate(() => {
    const host = document.querySelector("map-view");
    const sr = host.shadowRoot;
    // Clicking a marker *toggles* its popup, so an already-open one would be
    // closed by the click below rather than reopened. Restyling keeps popups
    // (they are DOM), which is exactly when this bites.
    host._markers?.forEach((m) => m.getPopup()?.remove());
    const marker = sr.querySelector(".maplibregl-marker");
    if (!marker) throw new Error("no markers on the trip map - does the seed still give the trip coordinates?");
    for (const type of ["mousedown", "mouseup", "click"]) {
      marker.dispatchEvent(new MouseEvent(type, { bubbles: true, cancelable: true, view: window, button: 0 }));
    }
  });
  await page.waitForFunction(
    () => document.querySelector("map-view").shadowRoot.querySelector(".maplibregl-popup-content a") !== null
  );
}

// Two points the sun is unambiguously up over and down under, right now.
//
// Both tests below need "somewhere lit" and "somewhere dark" and used to use
// the equator's antipodes, 0,0 and 0,180, on the reasoning that one of them is
// always in daylight. That is false, and it failed the suite twice a day for a
// window of about 48 minutes. At the equator the two altitudes are exact
// negatives of each other, and sun.js thresholds at civil twilight rather than
// at the horizon -- so whenever |altitude| is under 6 degrees, *both* points
// are "daylight" and the test asserting they differ fails. Observed at 18:13Z:
// -3.3 and +3.3 degrees, both light.
//
// Derived from the clock instead. The sun is overhead near longitude
// (12 - UTC hours) * 15, so that point is at local noon and its antipode at
// local midnight: altitudes near +83 and -83 whenever the suite runs, which is
// the margin the fixed pair only had for part of the day. Latitude 0 keeps it
// away from the polar cases, which have their own test.
function sunProbes(now = new Date()) {
  const hours = now.getUTCHours() + now.getUTCMinutes() / 60;
  const wrap = (lng) => ((((lng + 180) % 360) + 360) % 360) - 180;
  const noon = wrap((12 - hours) * 15);
  return { lit: [0, noon], dark: [0, wrap(noon + 180)] };
}

const setMode = async (page, mode) => {
  await page.evaluate(async (m) => {
    const { setMapTheme } = await import("/js/map-theme.js");
    setMapTheme(m);
  }, mode);
};

const mapState = (page) =>
  page.evaluate(() => {
    const host = document.querySelector("map-view");
    // getStyle() is undefined *during* a swap - MapLibre drops the old style
    // before the new one has parsed - so this has to survive being called mid
    // transition. It is polled until the map settles, and a null background is
    // exactly the "not yet" the poll is waiting out. Without the guard the
    // helper throws instead of returning "not yet", which showed up only under
    // a full parallel run, where the window is wide enough to land in.
    const bg = host._map.getStyle()?.layers?.find((l) => l.type === "background");
    // "#f8f4f0" or "rgb(12,12,12)" -> [r,g,b], so the assertion does not care
    // which notation a style happens to use.
    const rgb = (v) => {
      if (typeof v !== "string") return null;
      const hex = v.match(/^#([0-9a-f]{6})$/i);
      if (hex) return [0, 2, 4].map((i) => parseInt(hex[1].slice(i, i + 2), 16));
      const fn = v.match(/(\d+)\s*[ ,]\s*(\d+)\s*[ ,]\s*(\d+)/);
      return fn ? [Number(fn[1]), Number(fn[2]), Number(fn[3])] : null;
    };
    const marker = host.shadowRoot.querySelector(".maplibregl-marker");
    const centre = host._map.getCenter();
    return {
      scheme: host.dataset.scheme,
      background: rgb(bg?.paint?.["background-color"]),
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
      document.querySelector("map-view")._map.jumpTo({ center: [12.57, 55.68], zoom: 9 })
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

  // The popup a marker opens sits *on* the map, so it is the map's scheme it
  // has to obey - not the app's, and certainly not MapLibre's stylesheet,
  // which paints white paper and sets no text colour at all. On a dark map
  // that produced an unreadable pane: the title inherited the app's light
  // --color-text through the shadow boundary and disappeared into the white,
  // and so did the close button. Asserted as contrast rather than as literal
  // colours, so the palette can be retuned without rewriting the test.
  test("a marker popup is dressed in the map's scheme", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    // The interface is left dark throughout: the light map below therefore
    // cannot be the popup borrowing app tokens, which is the actual bug.
    await page.evaluate(async () => {
      const { setTheme } = await import("/js/theme.js");
      setTheme("dark");
    });

    const read = async () => {
      await openFirstPopup(page);
      return page.evaluate(() => {
        const sr = document.querySelector("map-view").shadowRoot;
        const content = sr.querySelector(".maplibregl-popup-content");
        const cs = getComputedStyle(content);
        const link = content.querySelector("a");
        const tip = sr.querySelector(".maplibregl-popup-tip");
        const tipStyle = getComputedStyle(tip);
        // The tip is a bordered zero-size box: whichever side is painted is
        // the arrow, and the other three are transparent or absent. Comparing
        // only the painted sides is what makes this independent of the anchor
        // MapLibre happened to pick.
        const painted = ["Top", "Right", "Bottom", "Left"]
          .filter((s) => parseFloat(tipStyle[`border${s}Width`]) > 0)
          .map((s) => tipStyle[`border${s}Color`])
          .filter((c) => c !== "rgba(0, 0, 0, 0)" && c !== "transparent");
        return {
          background: cs.backgroundColor,
          text: cs.color,
          link: link ? getComputedStyle(link).color : null,
          tip: painted,
        };
      });
    };

    // Relative luminance and the WCAG ratio, on "rgb(r, g, b)" strings -
    // computed here rather than in the page so a failure prints the numbers.
    const lum = (css) => {
      const [r, g, b] = css.match(/\d+/g).slice(0, 3).map(Number);
      const c = [r, g, b].map((v) => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
    };
    const contrast = (a, b) => {
      const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
      return (hi + 0.05) / (lo + 0.05);
    };

    const check = (state, scheme) => {
      expect(contrast(state.text, state.background), `the ${scheme} popup title must be readable`).toBeGreaterThanOrEqual(4.5);
      expect(state.link, `the ${scheme} popup should have a link to colour`).toBeTruthy();
      expect(contrast(state.link, state.background), `the ${scheme} popup links must be readable`).toBeGreaterThanOrEqual(4.5);
      // A white arrow under a dark box is the failure mode of styling the
      // content and forgetting the tip, and it is the one that looks broken
      // rather than merely low-contrast.
      expect(state.tip.length, `the ${scheme} popup should paint exactly one arrow`).toBe(1);
      expect(state.tip[0], `the ${scheme} popup arrow must match its box`).toBe(state.background);
    };

    await setMode(page, "dark");
    await waitForScheme(page, "dark", DARK_BG);
    const dark = await read();
    check(dark, "dark");

    await setMode(page, "light");
    await waitForScheme(page, "light", LIGHT_BG);
    const light = await read();
    check(light, "light");

    expect(light.background, "the two schemes must not share one popup colour").not.toBe(dark.background);
    // The map is dark here and the app is dark too, so this is the assertion
    // that the popup followed the map: dark paper under a dark interface is
    // ambiguous, near-black paper under a *light* map is not.
    expect(lum(dark.background), "a dark map wants dark paper").toBeLessThan(0.2);
    expect(lum(light.background), "a light map wants light paper").toBeGreaterThan(0.7);
  });

  test("follow-app tracks the app, once chosen", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);
    await setMode(page, "app");

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

  // Day/night is the default, so it has to work for a browser that has never
  // used the locate control -- which is most of them, and asking for a
  // position merely to tint a map is out of the question.
  //
  // The fallback is the device's own clock, read as a longitude: whatever the
  // map is pointed at, the answer is the same, because the mode is about the
  // light in the reader's room. This used to fall back to the place on screen,
  // which lit a trip to Vienna and a trip to Japan differently at the same
  // moment on the same sofa.
  test("day / night ignores the place on screen and follows the device", async ({ page }) => {
    await login(page);
    await gotoTripMap(page);

    // Light interface throughout, and nothing remembered: whatever happens
    // below cannot be the app's theme leaking through.
    await page.evaluate(async () => {
      const { setTheme } = await import("/js/theme.js");
      setTheme("light");
      localStorage.removeItem("caravel.lastPosition");
    });

    // The route's own map goes first, so the probes below are the only
    // <map-view> on the page and nothing races them.
    await page.evaluate(() => document.querySelector("map-view")?.remove());

    const schemeFor = async (lat, lng) =>
      page.evaluate(
        async ([la, ln]) => {
          const el = document.createElement("map-view");
          el.setAttribute("lat", String(la));
          el.setAttribute("lng", String(ln));
          el.setAttribute("marker-title", "probe");
          document.body.appendChild(el);
          await new Promise((r) => {
            const check = () => (el.hasAttribute("data-ready") ? r() : requestAnimationFrame(check));
            check();
          });
          const scheme = el.dataset.scheme;
          el.remove();
          return scheme;
        },
        [lat, lng]
      );

    // One point under the midday sun and one under the midnight one -- see
    // sunProbes for why this is not a fixed pair. Both maps must come out the
    // same, which is the whole assertion: the subject of the map is not an
    // input any more.
    const { lit, dark } = sunProbes();
    const atLit = await schemeFor(...lit);
    const atDark = await schemeFor(...dark);
    expect(
      atLit,
      `a map of noon at ${lit[1]} and one of midnight at ${dark[1]} must be lit the same way`
    ).toBe(atDark);

    // And that one answer must be the sun over the *device*, which the module
    // estimates from the browser's UTC offset.
    const expected = await page.evaluate(async () => {
      const { isDaylight } = await import("/js/sun.js");
      const { observerPosition } = await import("/js/map-theme.js");
      const where = observerPosition();
      return isDaylight(where.lat, where.lng) ? "light" : "dark";
    });
    expect(atLit, "the map should follow the sun where the reader is").toBe(expected);
  });

  // The map is the only surface with a fourth mode, and this is why it exists:
  // the app's "auto" follows the operating system, which is a switch somebody
  // set at home. Whether the sun is up where the reader is standing is a fact
  // about right now.
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

    // One point under the midday sun and one under the midnight one, worked
    // out from the clock -- see sunProbes for why this is not a fixed pair.
    const { lit, dark } = sunProbes();
    await place(...lit);
    const atLit = (await mapState(page)).scheme;
    expect(atLit, "the point the sun is overhead should be lit").toBe("light");
    await page.waitForTimeout(200);
    await place(...dark);
    await expect
      .poll(async () => (await mapState(page)).scheme, {
        message: "the point on the night side should be lit the other way",
        timeout: 20000,
      })
      .toBe("dark");

    // And it must agree with the module that does the arithmetic, rather than
    // merely differing.
    const agrees = await page.evaluate(async (d) => {
      const { isDaylight } = await import("/js/sun.js");
      const host = document.querySelector("map-view");
      return { expected: isDaylight(...d) ? "light" : "dark", actual: host.dataset.scheme };
    }, dark);
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
    // Day/night is the default, and it is stored as the absence of a key the
    // way theme.js does it.
    await expect(page.locator('.map-appearance-slot [name="map-theme"][value="auto"]')).toBeChecked();
    expect(await page.evaluate(() => localStorage.getItem("caravel.mapTheme"))).toBeNull();

    await page.locator('.map-appearance-slot [name="map-theme"][value="dark"]').check();
    expect(await page.evaluate(() => localStorage.getItem("caravel.mapTheme"))).toBe("dark");

    // And it survives a reload, which is the whole point of storing it.
    await page.reload();
    await expect(page.locator('.map-appearance-slot [name="map-theme"][value="dark"]')).toBeChecked();

    // "Follow app" is a real stored choice now that it is no longer the
    // default...
    await page.locator('.map-appearance-slot [name="map-theme"][value="app"]').check();
    expect(await page.evaluate(() => localStorage.getItem("caravel.mapTheme"))).toBe("app");

    // ...and returning to the default clears the key, so a browser told
    // "day / night" and one never told anything are the same state.
    await page.locator('.map-appearance-slot [name="map-theme"][value="auto"]').check();
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
