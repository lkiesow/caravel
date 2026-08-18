// Accessible names: every interactive control must be announced as something.
//
// Stage 07 hand-rolled this and found 157 controls across 10 routes. A control
// with no name is read out as just "button" or "edit text", which is useless —
// and icon-only buttons are the usual offenders, so the shadow-root walk matters
// here too.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute } from "./helpers/scenarios.js";
import { DEEP_DOM_SOURCE, ACCESSIBLE_NAME_SOURCE } from "./helpers/deep-dom.js";

test("every form control and button resolves an accessible name", async ({ page }) => {
  await login(page);
  const routes = await buildRoutes(page);
  const failures = [];
  let totalChecked = 0;

  for (const route of routes) {
    await gotoRoute(page, route.path);

    const result = await page.evaluate(
      ({ deepSource, nameSource }) => {
        eval(deepSource);
        eval(nameSource);
        const controls = deepQueryAll("input, select, textarea, button");
        const unnamed = [];
        let checked = 0;
        for (const el of controls) {
          const style = getComputedStyle(el);
          if (style.display === "none" || style.visibility === "hidden") continue;
          if (el.type === "hidden") continue;
          // Genuinely hidden from the a11y tree is fine — it isn't announced at
          // all, so it needs no name.
          if (el.closest && el.closest("[aria-hidden='true']")) continue;
          checked++;
          const resolved = accessibleName(el);
          if (resolved.hidden) continue;
          if (!resolved.name) {
            unnamed.push({
              desc: describeElement(el),
              tag: el.localName,
              type: el.type || "",
            });
          }
        }
        return { unnamed, checked };
      },
      { deepSource: DEEP_DOM_SOURCE, nameSource: ACCESSIBLE_NAME_SOURCE }
    );

    totalChecked += result.checked;
    for (const u of result.unnamed) {
      failures.push(`${route.label}: <${u.tag}${u.type ? ` type="${u.type}"` : ""}> at ${u.desc} has no accessible name`);
    }
  }

  // Print the count so a run that silently checks nothing is visible rather than
  // reading as a pass.
  console.log(`a11y-names: checked ${totalChecked} controls across ${routes.length} routes`);
  expect(totalChecked, "no controls were checked at all — the sweep found nothing, which is not a pass").toBeGreaterThan(50);
  expect(failures, `${failures.length} unnamed control(s):\n${failures.join("\n")}`).toEqual([]);
});
