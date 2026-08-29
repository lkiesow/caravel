#!/usr/bin/env node
// Measures colour contrast on a route and reports it against the WCAG thresholds.
//
// Two modes. By default a measurement tool: its output is numbers to read, and
// the judgement is yours. With --strict it is a gate, and `make check-contrast`
// runs it that way in CI.
//
//   node tests/ui/contrast.js --route /trips
//   node tests/ui/contrast.js --route /trips --scheme dark
//   node tests/ui/contrast.js --route /trips --selector ".btn-primary" --selector ".error"
//   node tests/ui/contrast.js --route /trips --route /settings --scheme both --strict
//
// --strict holds every measured element to *its own* WCAG threshold -- 4.5 for
// normal text, 3.0 for large text and for non-text -- rather than to one flat
// floor. That distinction is why this stayed a measurement tool for four
// stages: a single --min number is wrong for at least a third of what it
// measures, and picking per element is the whole decision.
//
// EXEMPT is the other half of that decision, and it is deliberately short.
//
// Two parts are the whole reason this is written down rather than retyped, because
// both silently produce nonsense when done the obvious way:
//
//   1. Translucent backgrounds must be FLATTENED over whatever is painted behind
//      them. Caravel's danger tint is an `rgba(...)`, so reading
//      `backgroundColor` and comparing against it measures the text against a
//      partly-transparent colour — a number that means nothing. This walks up the
//      ancestor chain (crossing shadow boundaries), collecting layers until it
//      reaches an opaque one, then composites them.
//   2. Elements inside SHADOW ROOTS have to be reachable, or the components most
//      worth checking (cards, menus, the map legend) are invisible to the tool.
//
// This is the script that found Stage 07's 2.54:1 primary buttons and 3.08:1 error
// text, and that proved light mode was untouched by the fix.
import { firefox } from "@playwright/test";

const DEFAULT_SELECTORS = [
  ".btn-primary",
  ".btn-secondary",
  ".btn-danger",
  "a",
  "h1",
  "h2",
  "p",
  "label",
  "input",
  ".error",
  ".form-error",
  ".muted",
  ".subtitle",
];

const WCAG = { AA_NORMAL: 4.5, AA_LARGE: 3.0, AA_NON_TEXT: 3.0 };

// Elements --strict does not judge, each with the reason it is not judged.
// Nothing goes in here to make a number go away: an exemption is a claim that
// the guideline does not apply, and if that claim is wrong the fix is the
// colour, not this list.
const EXEMPT = [
  {
    // WCAG 1.4.3 exempts logotypes outright: "text that is part of a logo or
    // brand name has no minimum contrast requirement". The header lockup is
    // the brand mark plus the wordmark, drawn in --brand-wordmark, which is
    // lightened navy on the dark ground and measures 3.59:1. Changing it to
    // clear 4.5 would mean the app not using its own brand colour.
    selector: ".app-brand",
    why: "brand lockup — WCAG 1.4.3 exempts logotypes",
  },
];

function parseArgs(argv) {
  const opts = { routes: [], scheme: "light", selectors: [], min: null, strict: false, selfTest: false, url: process.env.CARAVEL_TEST_URL || "http://localhost:8080" };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const next = () => {
      const v = argv[++i];
      if (v === undefined) {
        console.error(`contrast: ${arg} needs a value`);
        process.exit(2);
      }
      return v;
    };
    if (arg === "--route") opts.routes.push(next());
    else if (arg === "--strict") opts.strict = true;
    else if (arg === "--scheme") opts.scheme = next();
    else if (arg === "--selector") opts.selectors.push(next());
    else if (arg === "--min") opts.min = Number(next());
    else if (arg === "--url") opts.url = next();
    else if (arg === "--self-test") opts.selfTest = true;
    else if (arg === "--help" || arg === "-h") {
      console.log(
        [
          "usage: node tests/ui/contrast.js [options]",
          "  --route <path>       route to measure; repeatable (default /trips).",
          "                       {trip} and {item} are filled from the seeded",
          "                       demo data, e.g. /trips/{trip}/map",
          "  --scheme <s>         light | dark | both (default light)",
          "  --selector <sel>     add a selector; repeatable. Default: a common set",
          "  --min <ratio>        exit non-zero if any element falls below this flat floor",
          "  --strict             exit non-zero if any element falls below ITS OWN WCAG",
          "                       threshold (4.5 text, 3.0 large/non-text), minus EXEMPT",
          "  --url <base>         app base URL (default $CARAVEL_TEST_URL or localhost:8080)",
          "  --self-test          verify the translucent-flattening math on known input, then exit",
        ].join("\n")
      );
      process.exit(0);
    } else {
      console.error(`contrast: unknown argument ${arg}`);
      process.exit(2);
    }
  }
  if (!opts.selectors.length) opts.selectors = DEFAULT_SELECTORS;
  if (!opts.routes.length) opts.routes = ["/trips"];
  if (!["light", "dark", "both"].includes(opts.scheme)) {
    console.error(`contrast: --scheme must be light, dark or both`);
    process.exit(2);
  }
  return opts;
}

// Runs in the page. Kept as one self-contained function so page.evaluate can take
// it whole.
const MEASURE = ({ selectors }) => {
  const parseColor = (str) => {
    const m = String(str).match(/rgba?\(([^)]+)\)/);
    if (!m) return null;
    const parts = m[1].split(/[,\s/]+/).filter(Boolean).map(Number);
    const [r, g, b] = parts;
    const a = parts.length > 3 ? parts[3] : 1;
    return { r, g, b, a };
  };

  // sRGB relative luminance, per WCAG.
  const luminance = ({ r, g, b }) => {
    const chan = (v) => {
      const s = v / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * chan(r) + 0.7152 * chan(g) + 0.0722 * chan(b);
  };

  const ratio = (fg, bg) => {
    const l1 = luminance(fg);
    const l2 = luminance(bg);
    const [hi, lo] = l1 > l2 ? [l1, l2] : [l2, l1];
    return (hi + 0.05) / (lo + 0.05);
  };

  // src over dst, both premultiplied-free rgba.
  const over = (src, dst) => ({
    r: src.r * src.a + dst.r * (1 - src.a),
    g: src.g * src.a + dst.g * (1 - src.a),
    b: src.b * src.a + dst.b * (1 - src.a),
    a: 1,
  });

  // Walks up through ancestors AND out of shadow roots, collecting background
  // layers until an opaque one is found, then composites them back down. Reading
  // getComputedStyle(el).backgroundColor alone is the mistake this avoids: for a
  // translucent tint it returns the tint, not what you actually see.
  const effectiveBackground = (el) => {
    const layers = [];
    let node = el;
    while (node) {
      if (node.nodeType === 1) {
        const bg = parseColor(getComputedStyle(node).backgroundColor);
        if (bg && bg.a > 0) {
          layers.push(bg);
          if (bg.a >= 1) break;
        }
      }
      const parent = node.parentNode;
      node = parent && parent.host ? parent.host : parent;
    }
    // Anything still translucent at the root composites onto the canvas, which is
    // white unless the page says otherwise.
    let result = { r: 255, g: 255, b: 255, a: 1 };
    for (let i = layers.length - 1; i >= 0; i--) result = over(layers[i], result);
    return { color: result, layerCount: layers.length, translucent: layers.some((l) => l.a < 1) };
  };

  const deepWalk = (root, out = []) => {
    for (const child of root.children || []) {
      out.push(child);
      if (child.shadowRoot) deepWalk(child.shadowRoot, out);
      deepWalk(child, out);
    }
    return out;
  };

  const describe = (el) => {
    let s = el.localName;
    if (el.id) s += `#${el.id}`;
    else if (el.classList.length) s += `.${[...el.classList].slice(0, 2).join(".")}`;
    return el.getRootNode() !== document ? `[shadow] ${s}` : s;
  };

  const all = deepWalk(document.documentElement);
  const results = [];
  const seen = new Set();

  for (const selector of selectors) {
    const matches = all.filter((el) => el.matches && el.matches(selector));
    for (const el of matches) {
      if (seen.has(el)) continue;
      const style = getComputedStyle(el);
      if (style.display === "none" || style.visibility === "hidden") continue;
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) continue;
      const text = (el.textContent || "").replace(/\s+/g, " ").trim();
      seen.add(el);

      const fg = parseColor(style.color);
      const bg = effectiveBackground(el);
      if (!fg) continue;
      // Text colour can itself be translucent.
      const flatFg = fg.a < 1 ? over(fg, bg.color) : fg;

      const fontSize = parseFloat(style.fontSize);
      const fontWeight = Number(style.fontWeight) || 400;
      // WCAG "large text": >=18.66px bold, or >=24px.
      const isLarge = fontSize >= 24 || (fontSize >= 18.66 && fontWeight >= 700);

      results.push({
        selector,
        what: describe(el),
        text: text.slice(0, 34),
        hasText: text.length > 0,
        fg: `rgb(${[flatFg.r, flatFg.g, flatFg.b].map((v) => Math.round(v)).join(" ")})`,
        bg: `rgb(${[bg.color.r, bg.color.g, bg.color.b].map((v) => Math.round(v)).join(" ")})`,
        translucentBg: bg.translucent,
        bgLayers: bg.layerCount,
        fontSize: Math.round(fontSize * 10) / 10,
        isLarge,
        ratio: Math.round(ratio(flatFg, bg.color) * 100) / 100,
      });
    }
  }
  return results;
};

// The seeded trip the templated routes point at. Same title tests/ui/helpers
// /scenarios.js calls the "full" scenario -- the one with locations, days,
// expenses and files, so a tab actually has content to measure. Matching on the
// title rather than taking the first trip keeps this stable when the seeder
// adds scenarios.
const SCENARIO_TRIP_TITLE = "Demo: Iceland Ring Road";

// Fills {trip} and {item} in a route. Routes worth measuring are trip tabs and
// the location editor, and their ids are not knowable when the Makefile writes
// the route list down -- so the list carries holes and they are filled here,
// after login, from the seed.
async function resolveRoute(page, route) {
  if (!route.includes("{")) return route;

  const data = await page.evaluate(async (title) => {
    const trips = await (await fetch("/api/trips")).json();
    if (!Array.isArray(trips)) return { error: "could not list trips" };
    const trip = trips.find((t) => t.title === title);
    if (!trip) return { error: `no seeded trip titled ${title}` };
    const items = await (await fetch(`/api/trips/${trip.id}/items`)).json();
    if (!Array.isArray(items) || !items.length) return { error: `trip ${title} has no locations` };
    return { trip: trip.id, item: items[0].id };
  }, SCENARIO_TRIP_TITLE);

  if (data.error) {
    console.error(`contrast: ${data.error} — run \`make dev-reset FORCE=1\` first`);
    process.exit(1);
  }
  return route.replace("{trip}", data.trip).replace("{item}", data.item);
}

async function measureScheme(browser, opts, scheme, route) {
  const context = await browser.newContext({ colorScheme: scheme, viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  // Keep the measurement off the public internet: map tiles would otherwise be
  // fetched from OSM on the map route.
  const appOrigin = new URL(opts.url).origin;
  await page.route("**/*", (route) => {
    const u = new URL(route.request().url());
    return u.origin === appOrigin ? route.continue() : route.abort();
  });

  await page.goto(opts.url + "/");
  const status = await page.evaluate(async () => {
    const res = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username: "demo", password: "demo1234" }),
    });
    return res.status;
  });
  if (status !== 200) {
    console.error(`contrast: could not log in as demo (${status}) — run \`make dev-reset FORCE=1\` first`);
    process.exit(1);
  }

  const path = await resolveRoute(page, route);

  await page.goto(opts.url + path);
  await page.waitForFunction(
    () => {
      const app = document.getElementById("app");
      return app && app.children.length > 0 && !/starting up/i.test(app.textContent);
    },
    undefined,
    { timeout: 15000 }
  );
  const landed = await page.evaluate(() => window.location.pathname);
  // Compared against the *substituted* path, not the template, or every
  // templated route would look like a redirect.
  if (landed !== path) {
    // Same trap the suite guards: unmatched paths silently redirect to /trips.
    console.error(`contrast: asked for ${path} but landed on ${landed} — that route probably doesn't exist`);
    process.exit(1);
  }
  await page.waitForTimeout(400);

  const results = await page.evaluate(MEASURE, { selectors: opts.selectors });
  await context.close();
  return results;
}

function isExempt(what) {
  return EXEMPT.find((e) => what.includes(e.selector.replace(/^\./, "")));
}

function report(scheme, results, min, route) {
  console.log(`\n=== ${route} — ${scheme} — ${results.length} element(s) measured ===`);
  if (!results.length) {
    console.log("  (nothing matched — check --selector and --route)");
    return [];
  }

  const pad = (s, n) => String(s).padEnd(n);
  console.log(
    `  ${pad("ratio", 7)}${pad("thr", 6)}${pad("ok", 4)}${pad("element", 34)}${pad("fg on bg", 34)}text`
  );

  const below = [];
  for (const r of results.sort((a, b) => a.ratio - b.ratio)) {
    const threshold = !r.hasText ? WCAG.AA_NON_TEXT : r.isLarge ? WCAG.AA_LARGE : WCAG.AA_NORMAL;
    const ok = r.ratio >= threshold;
    const exempt = isExempt(r.what);
    // Exempt elements are still measured and still printed — the number is
    // worth seeing — they just do not fail the build.
    if (!ok && !exempt) below.push({ ...r, threshold, route, scheme });
    const flag = ok ? "ok " : exempt ? "sk " : "FAIL";
    const bgNote =
      (r.translucentBg ? ` (${r.bgLayers} layers, flattened)` : "") +
      (exempt && !ok ? ` [exempt: ${exempt.why}]` : "");
    console.log(
      `  ${pad(r.ratio.toFixed(2), 7)}${pad(threshold.toFixed(1), 6)}${pad(flag, 4)}${pad(r.what, 34)}${pad(
        `${r.fg} on ${r.bg}`,
        34
      )}${r.text}${bgNote}`
    );
  }

  const worst = results.reduce((a, b) => (a.ratio < b.ratio ? a : b));
  console.log(`  worst: ${worst.ratio.toFixed(2)}:1 on ${worst.what}`);
  if (min !== null) {
    const failures = results.filter((r) => r.ratio < min);
    if (failures.length) console.log(`  ${failures.length} element(s) below --min ${min}`);
  }
  return below;
}

// Proves the compositing math on known input.
//
// Worth having because the app's translucent surfaces (`--color-danger-tint`,
// an rgba at 0.08/0.14 alpha) only appear in error states, so a normal run never
// exercises the flattening path — and a flattener that silently returned the raw
// tint would look perfectly plausible in the output. The expected values here are
// computed by hand: src over dst = src.rgb * a + dst.rgb * (1 - a).
async function selfTest(browser) {
  const tint = "rgba(220, 38, 38, 0.08)"; // --color-danger-tint, light mode
  // 220*0.08 + 255*0.92 = 252.2 ; 38*0.08 + 255*0.92 = 237.6
  const expectedOverWhite = "rgb(252 238 238)";
  // Two stacked layers: the same tint twice over white.
  // pass 1 -> (252.2, 237.6, 237.6); pass 2 over that:
  // 220*0.08 + 252.2*0.92 = 249.6 ; 38*0.08 + 237.6*0.92 = 221.6
  const expectedTwoLayers = "rgb(250 222 222)";

  const html = `<!doctype html><html><body style="background: rgb(255,255,255); margin:0">
    <div id="single" style="background: ${tint}; color: rgb(0,0,0)">single layer</div>
    <div style="background: ${tint}">
      <div id="double" style="background: ${tint}; color: rgb(0,0,0)">two layers</div>
    </div>
    <div id="opaque" style="background: rgb(37,99,235); color: rgb(255,255,255)">opaque</div>
  </body></html>`;

  const page = await browser.newPage();
  await page.setContent(html);
  const results = await page.evaluate(MEASURE, { selectors: ["#single", "#double", "#opaque"] });
  await page.close();

  const byId = Object.fromEntries(results.map((r) => [r.what.replace(/^\[shadow\] /, ""), r]));
  // Layer counts include the opaque `body` background the walk terminates on —
  // so one more than the number of translucent tints. #opaque stops at itself.
  const checks = [
    ["div#single", expectedOverWhite, true, 2],
    ["div#double", expectedTwoLayers, true, 3],
    ["div#opaque", "rgb(37 99 235)", false, 1],
  ];

  let failed = 0;
  console.log("=== self-test: translucent background flattening ===");
  for (const [key, expectedBg, expectTranslucent, expectLayers] of checks) {
    const r = byId[key];
    if (!r) {
      console.log(`  FAIL ${key}: not measured`);
      failed++;
      continue;
    }
    const bgOk = r.bg === expectedBg;
    const flagOk = r.translucentBg === expectTranslucent;
    const layersOk = r.bgLayers === expectLayers;
    const ok = bgOk && flagOk && layersOk;
    if (!ok) failed++;
    console.log(
      `  ${ok ? "ok  " : "FAIL"} ${key}: bg=${r.bg} (want ${expectedBg}), ` +
        `translucent=${r.translucentBg} (want ${expectTranslucent}), layers=${r.bgLayers} (want ${expectLayers}), ` +
        `ratio=${r.ratio}`
    );
  }
  if (failed) {
    console.error(`self-test: ${failed} check(s) failed — the flattening logic is wrong, so every ratio it reports is suspect`);
    return 1;
  }
  console.log("  all checks passed — layers composite correctly and the raw tint is never used directly\n");
  return 0;
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  const schemes = opts.scheme === "both" ? ["light", "dark"] : [opts.scheme];

  if (opts.selfTest) {
    const browser = await firefox.launch();
    // Close before exiting rather than inside a finally: process.exit() does not
    // run finally blocks, so this would otherwise rely on Playwright's own
    // exit handler to reap the browser.
    let code;
    try {
      code = await selfTest(browser);
    } finally {
      await browser.close();
    }
    process.exit(code);
  }

  const browser = await firefox.launch();
  let anyBelowMin = false;
  const failures = [];
  let measured = 0;
  try {
    for (const route of opts.routes) {
      for (const scheme of schemes) {
        const results = await measureScheme(browser, opts, scheme, route);
        measured += results.length;
        failures.push(...report(scheme, results, opts.min, route));
        if (opts.min !== null && results.some((r) => r.ratio < opts.min)) anyBelowMin = true;
      }
    }
  } finally {
    await browser.close();
  }

  if (opts.strict) {
    // A run that matched nothing would otherwise report a clean sweep. The
    // floor is deliberately low: it only has to catch "the selectors stopped
    // matching", not assert a particular page.
    if (measured < 20) {
      console.error(`\ncontrast: only ${measured} element(s) measured across ${opts.routes.length} route(s) — the selectors are probably not matching`);
      process.exit(1);
    }
    if (failures.length) {
      console.error(`\ncontrast: ${failures.length} element(s) below their own WCAG threshold`);
      for (const f of failures) {
        console.error(`  ${f.ratio.toFixed(2)}:1 (needs ${f.threshold.toFixed(1)}) ${f.what} on ${f.route} (${f.scheme}) — ${f.fg} on ${f.bg}`);
      }
      process.exit(1);
    }
    console.log(`\ncontrast: ${measured} element(s) measured, all at or above their own WCAG threshold`);
  }
  process.exit(anyBelowMin ? 1 : 0);
}

main();
