// The fallback face has to land where Inter will (Stage 41 Milestone 2).
//
// base.css uses font-display: swap, so the platform font paints the first
// frame of every cold load and Inter replaces it a moment later. Measured
// before this was addressed, that cost 77 to 94 percent of the elements on a
// 324px route moving when Inter arrived -- the worst single element by 68px,
// and whole pages changing height by as much. The reader watched the layout
// rearrange itself.
//
// scripts/gen_font_fallbacks.py answers that with a metric-adjusted stand-in
// per platform font: size-adjust scales the glyphs to Inter's average
// character width over the app's own locale copy, and the ascent/descent
// overrides replace the line box. This spec is what stops those numbers
// rotting -- they are generated from font files, and nothing else in the suite
// would notice if a regenerated size-adjust drifted or a local() name stopped
// matching.
//
// HOW IT MEASURES, and why it does not do the obvious thing. It renders one
// page, reads the computed --font-ui, and re-renders with Inter removed from
// the front of that stack. It does NOT block the font over the network, which
// is what the first two versions did, because that route led through three
// separate false results:
//
//   - a page.route abort that the service worker served straight past from its
//     own cache, reporting a flawless zero shift that meant nothing;
//   - a fix for that which navigated twice, passed alone, and failed in the
//     parallel suite claiming a 76.5px shift -- another worker had created and
//     deleted trips between the two navigations, so it was comparing two
//     different pages and blaming the font;
//   - and a same-page version that aborted and un-aborted the font URL per
//     route, which deadlocked under parallel load and timed out at 180s.
//
// Switching the CSS variable has none of those failure modes: no interception,
// no service worker, no second navigation, and no data that another worker can
// change underneath it. What it gives up is proof that the files load over the
// network, which brand.spec.js already asserts.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute, VIEWPORTS } from "./helpers/scenarios.js";

// The phone viewport only. Overflow and wrapping are width problems, 324px is
// the tightest case the suite tests, and a second viewport would double the
// runtime to re-measure the same font metrics.
const MOBILE = VIEWPORTS.find((v) => v.name === "mobile");

// Three routes rather than every route: the exploratory sweep ran all 25 and
// every one told the same story. These are the dense ones -- a list, a long
// form, and a table with tabular figures.
const SAMPLE = ["trips list", "edit location", "trip expenses"];

// Worst observed residual after the adjustment was 5.4px at this viewport,
// against 68px before it. The ceiling is set above that with room for
// rounding, not at it: a guard against the adjustment breaking, not a lock on
// the exact pixel.
const MAX_SHIFT_PX = 12;

// The fallback should measure within 1% of Inter on the same string. It came
// out at 280.5 against 280.2, which is 0.1%.
const MAX_WIDTH_DRIFT = 0.01;

const PROBE = "Hamburgefonstiv Größe 0123456789";

// Measures the page as it stands, optionally with the UI stack overridden.
//
// Returns the stack it actually used, so the caller can prove the override did
// something rather than assuming it.
const SNAPSHOT = ({ override, probe }) => {
  const root = document.documentElement;
  if (override === null) root.style.removeProperty("--font-ui");
  else if (override !== undefined) root.style.setProperty("--font-ui", override);
  // Force layout before reading anything back.
  void root.offsetHeight;

  const boxes = [];
  const walk = (node) => {
    for (const el of node.querySelectorAll("*")) {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) continue;
      boxes.push({ y: r.y, h: r.height });
      if (el.shadowRoot) walk(el.shadowRoot);
    }
  };
  walk(document);

  const ctx = document.createElement("canvas").getContext("2d");
  const used = getComputedStyle(document.body).fontFamily;
  ctx.font = `400 16px ${used}`;
  return { boxes, used, width: ctx.measureText(probe).width };
};

test("the metric-adjusted fallback lands where Inter will", async ({ page }) => {
  test.setTimeout(120000);
  await page.setViewportSize({ width: MOBILE.width, height: MOBILE.height });
  await login(page);
  const routes = (await buildRoutes(page)).filter((r) => SAMPLE.includes(r.label));
  expect(routes.map((r) => r.label).sort(), "the sampled routes should all exist").toEqual(
    [...SAMPLE].sort()
  );

  for (const { path, label } of routes) {
    await gotoRoute(page, path);
    await page.evaluate(() => document.fonts.ready);

    // The stack with Inter taken off the front. Read from the page rather than
    // written down here, so this does not quietly drift from base.css.
    const withoutInter = await page.evaluate(() => {
      const stack = getComputedStyle(document.documentElement)
        .getPropertyValue("--font-ui")
        .trim();
      return { stack, without: stack.replace(/^"?Inter"?\s*,\s*/, "") };
    });
    expect(
      withoutInter.without,
      `${label}: --font-ui does not start with Inter, so this test is not removing what ` +
        `it thinks it is. It reads: ${withoutInter.stack}`
    ).not.toBe(withoutInter.stack);

    const inter = await page.evaluate(SNAPSHOT, { probe: PROBE });
    const fallback = await page.evaluate(SNAPSHOT, {
      override: withoutInter.without,
      probe: PROBE,
    });
    await page.evaluate(SNAPSHOT, { override: null, probe: PROBE });

    const drift = Math.abs(fallback.width - inter.width) / inter.width;
    expect(
      drift,
      `${label}: the fallback renders ${fallback.width.toFixed(1)}px where Inter renders ` +
        `${inter.width.toFixed(1)}px. Either size-adjust has drifted, or the fallback face ` +
        "did not resolve at all and this is the raw platform font. Regenerate with " +
        `scripts/gen_font_fallbacks.py. Fallback stack: ${fallback.used}`
    ).toBeLessThan(MAX_WIDTH_DRIFT);

    const n = Math.min(fallback.boxes.length, inter.boxes.length);
    expect(n, `${label}: nothing was measured`).toBeGreaterThan(10);
    let worst = 0;
    let moved = 0;
    for (let i = 0; i < n; i++) {
      const delta = Math.max(
        Math.abs(inter.boxes[i].y - fallback.boxes[i].y),
        Math.abs(inter.boxes[i].h - fallback.boxes[i].h)
      );
      if (delta > 0.05) moved++;
      worst = Math.max(worst, delta);
    }

    // Two-sided on purpose. A zero shift is not a perfect score, it is the
    // signature of a measurement that did not measure anything -- which is how
    // the first attempt at this failed, twice.
    expect(
      moved,
      `${label}: not one box moved when Inter left the stack, so the override did not ` +
        "take effect and the number below proves nothing."
    ).toBeGreaterThan(0);

    expect(
      worst,
      `${label}: the worst element moves ${worst.toFixed(1)}px between the fallback and ` +
        "Inter. The overrides in base.css are generated by scripts/gen_font_fallbacks.py; " +
        "see its header for what they mean."
    ).toBeLessThan(MAX_SHIFT_PX);

    console.log(`font-metrics: ${label} -- worst ${worst.toFixed(1)}px, ${moved}/${n} boxes moved`);
  }
});
