// Accessible names: every interactive control must be announced as something.
//
// Stage 07 hand-rolled this and found 157 controls across 10 routes. A control
// with no name is read out as just "button" or "edit text", which is useless —
// and icon-only buttons are the usual offenders, so the shadow-root walk matters
// here too.
//
// Run in both languages, and that is not decoration: `scripts/check_i18n.py`
// compares the *keys* across locale files and never looks at the values, and
// `t()` resolves with `??`, so an empty German string survives all the way to
// the DOM. Emptying `checklists.listActions` in de.json reproduces it: `make ci`
// stays green, the English sweep stays green, and the German one fails on the
// checklist card's menu trigger.
//
// The reach is narrower than it sounds, and worth knowing before trusting it:
// this only catches controls whose *only* name comes from the translated
// string, which in practice means the icon-only ones. Anything with visible
// text still resolves a name from the text, so emptying its label is invisible
// here.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute } from "./helpers/scenarios.js";
import { DEEP_DOM_SOURCE, ACCESSIBLE_NAME_SOURCE } from "./helpers/deep-dom.js";

for (const locale of ["en", "de"]) {
  test.describe(`accessible names (${locale})`, () => {
    test.use({ locale });

    test(`every form control and button resolves an accessible name (${locale})`, async ({
      page,
    }) => {
      await login(page);
      const routes = await buildRoutes(page);
      const failures = [];
      let totalChecked = 0;

      for (const [index, route] of routes.entries()) {
        await gotoRoute(page, route.path);
        // A German sweep that quietly rendered English would pass everything below.
        if (index === 0)
          await expect(page.locator("html")).toHaveAttribute("lang", locale);

        const result = await page.evaluate(
          ({ deepSource, nameSource }) => {
            eval(deepSource);
            eval(nameSource);
            const controls = deepQueryAll("input, select, textarea, button");
            const unnamed = [];
            let checked = 0;
            for (const el of controls) {
              const style = getComputedStyle(el);
              if (style.display === "none" || style.visibility === "hidden")
                continue;
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
          { deepSource: DEEP_DOM_SOURCE, nameSource: ACCESSIBLE_NAME_SOURCE },
        );

        totalChecked += result.checked;
        for (const u of result.unnamed) {
          failures.push(
            `${route.label}: <${u.tag}${u.type ? ` type="${u.type}"` : ""}> at ${u.desc} has no accessible name`,
          );
        }
      }

      // Print the count so a run that silently checks nothing is visible rather than
      // reading as a pass.
      console.log(
        `a11y-names (${locale}): checked ${totalChecked} controls across ${routes.length} routes`,
      );
      expect(
        totalChecked,
        "no controls were checked at all — the sweep found nothing, which is not a pass",
      ).toBeGreaterThan(50);
      expect(
        failures,
        `${failures.length} unnamed control(s):\n${failures.join("\n")}`,
      ).toEqual([]);
    });
  });
}
