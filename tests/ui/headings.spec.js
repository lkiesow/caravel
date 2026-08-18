// Heading outline: exactly one h1, first, and no skipped levels.
//
// The shadow-root walk is the whole reason this is worth writing down. The
// trip-card and location-card headings that Stage 07 found wrong live inside
// shadow DOM, where a plain document.querySelectorAll("h1,h2,...") sees nothing
// at all — so the naive version of this check passes on precisely the markup
// that's broken.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute } from "./helpers/scenarios.js";
import { DEEP_DOM_SOURCE } from "./helpers/deep-dom.js";

test("every route has a valid heading outline (light DOM and shadow roots)", async ({ page }) => {
  await login(page);
  const routes = await buildRoutes(page);
  const failures = [];

  for (const route of routes) {
    await gotoRoute(page, route.path);

    const outline = await page.evaluate((deepSource) => {
      eval(deepSource);
      return deepQueryAll("h1, h2, h3, h4, h5, h6")
        .filter((el) => {
          const style = getComputedStyle(el);
          return style.display !== "none" && style.visibility !== "hidden";
        })
        .map((el) => ({
          level: Number(el.localName.slice(1)),
          text: (el.textContent || "").replace(/\s+/g, " ").trim().slice(0, 50),
          where: describeElement(el),
          inShadow: el.getRootNode() !== document,
        }));
    }, DEEP_DOM_SOURCE);

    if (outline.length === 0) {
      failures.push(`${route.label} (${route.path}): no headings at all`);
      continue;
    }

    const h1s = outline.filter((h) => h.level === 1);
    if (h1s.length !== 1) {
      failures.push(
        `${route.label}: expected exactly 1 h1, found ${h1s.length}` +
          (h1s.length ? ` — ${h1s.map((h) => `"${h.text}"`).join(", ")}` : "")
      );
    }
    if (outline[0].level !== 1) {
      failures.push(
        `${route.label}: first heading in document order is h${outline[0].level} ("${outline[0].text}" at ${outline[0].where}), not h1`
      );
    }

    for (let i = 1; i < outline.length; i++) {
      const jump = outline[i].level - outline[i - 1].level;
      if (jump > 1) {
        failures.push(
          `${route.label}: h${outline[i - 1].level} -> h${outline[i].level} skips a level ` +
            `("${outline[i].text}" at ${outline[i].where}${outline[i].inShadow ? ", in shadow DOM" : ""})`
        );
      }
    }
  }

  expect(failures, `heading outline problems:\n${failures.join("\n")}`).toEqual([]);
});

// Guards the guard: if this suite ever stops seeing into shadow roots, the
// outline check silently weakens to near-nothing. Caravel renders trip cards as
// custom elements with shadow roots on the trips list, so there is always
// something to find there.
test("the shadow-DOM walk actually reaches into shadow roots", async ({ page }) => {
  await login(page);
  await gotoRoute(page, "/trips");

  const counts = await page.evaluate((deepSource) => {
    eval(deepSource);
    const sel = "h1, h2, h3, h4, h5, h6";
    return {
      lightOnly: document.querySelectorAll(sel).length,
      deep: deepQueryAll(sel).length,
      shadowHosts: deepWalk(document.documentElement).filter((el) => el.shadowRoot).length,
    };
  }, DEEP_DOM_SOURCE);

  expect(counts.shadowHosts, "expected shadow-root hosts on /trips (the seeded trip cards)").toBeGreaterThan(0);
  expect(
    counts.deep,
    `the deep walk found ${counts.deep} headings vs ${counts.lightOnly} in the light DOM — if these are equal, the shadow walk is not working and this suite's heading check is toothless`
  ).toBeGreaterThan(counts.lightOnly);
});
