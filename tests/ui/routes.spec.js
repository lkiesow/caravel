// The sweep matrix: every route × {desktop, mobile} × {light, dark}.
//
// Stage 07 hand-rolled this every round and it took seconds to run; the point of
// writing it down is that the fiddly parts (dark mode without touching the OS
// theme, asserting the landed URL, reaching controls inside shadow roots) stop
// getting skipped.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute, VIEWPORTS, COLOR_SCHEMES } from "./helpers/scenarios.js";
import { DEEP_DOM_SOURCE } from "./helpers/deep-dom.js";

// Tap-target floor at phone width, in CSS px. This is THE ACCESSIBILITY
// GUIDELINE, not a measured floor.
//
// It used to be 40 with a long comment explaining that nothing in the app
// reached 44 — buttons bottomed out at 40px, block links at 30px, the icon+text
// "Back"/"Home" links at 22px, checkbox rows at 20px — so the constant locked in
// the current state instead. Stage 09 Milestone 6 closed that gap in CSS
// (base.css's max-width: 640px block, plus the map legend inside
// leaflet-map.js's own shadow styles), so the constant now says what it should:
// 2.75rem, the same value --tap-min carries.
const MIN_TAP_TARGET_PX = 44;

for (const scheme of COLOR_SCHEMES) {
  for (const viewport of VIEWPORTS) {
    test.describe(`${viewport.name} ${scheme}`, () => {
      test.use({
        viewport: { width: viewport.width, height: viewport.height },
        // Dark mode via emulation, which removes any need to change the OS or
        // browser theme by hand — the thing that made this awkward to check.
        colorScheme: scheme,
      });

      test(`no horizontal overflow on any route (${viewport.name}, ${scheme})`, async ({ page }) => {
        await login(page);
        const routes = await buildRoutes(page);
        const failures = [];

        for (const route of routes) {
          await gotoRoute(page, route.path);

          const result = await page.evaluate(() => {
            const doc = document.documentElement;
            const overflow = doc.scrollWidth - window.innerWidth;
            let widest = null;
            if (overflow > 0) {
              // Name the widest offender, or the failure is unactionable.
              for (const el of document.querySelectorAll("*")) {
                const r = el.getBoundingClientRect();
                if (r.right > window.innerWidth + 1 && (!widest || r.right > widest.right)) {
                  widest = {
                    right: r.right,
                    tag: el.localName,
                    cls: el.className && String(el.className).slice(0, 60),
                  };
                }
              }
            }
            return { overflow, widest, scrollWidth: doc.scrollWidth, innerWidth: window.innerWidth };
          });

          if (result.overflow > 0) {
            failures.push(
              `${route.label} (${route.path}): scrollWidth ${result.scrollWidth} > viewport ${result.innerWidth}` +
                (result.widest ? ` — widest: <${result.widest.tag} class="${result.widest.cls}"> right=${Math.round(result.widest.right)}` : "")
            );
          }
        }

        expect(failures, `horizontal overflow on ${failures.length} route(s)`).toEqual([]);
      });

      // Tap targets only matter at phone width; skip the duplicate desktop run.
      if (viewport.name === "mobile") {
        test(`interactive controls meet the ${MIN_TAP_TARGET_PX}px tap target (${scheme})`, async ({ page }) => {
          await login(page);
          const routes = await buildRoutes(page);
          const failures = [];
          let totalChecked = 0;

          for (const route of routes) {
            await gotoRoute(page, route.path);

            const result = await page.evaluate(
              ({ deepSource, min }) => {
                eval(deepSource);

                // Every control the user aims a finger at, not just the ones
                // that look like buttons — which is what this checked before
                // Stage 09 Milestone 6, when links and inputs were exempt
                // because too many of them were too small to assert on.
                //
                // Three exclusions, each for a reason, not for convenience:
                //
                // 1. Prose links. A link inside a paragraph has to be allowed
                //    to be inline-sized; making body copy 44px tall per line
                //    is not the guideline's intent.
                // 2. Leaflet's own controls and its OpenStreetMap attribution
                //    (anything under a `leaflet-*` class). That markup and its
                //    CSS come from the vendored library inside the map's shadow
                //    root — the zoom buttons measure 30px and the attribution
                //    link 14px. Restyling a dependency's internals to satisfy
                //    our own sweep would be the tail wagging the dog, and the
                //    attribution is conventionally small on purpose.
                // 3. Checkbox and radio inputs *that a <label> wraps*. A native
                //    checkbox is ~14px; the tap target is the label around it,
                //    which toggles it, and that label is measured here instead
                //    (base.css gives those labels the min-height for the same
                //    reason). A checkbox with no wrapping label has no larger
                //    target and is still measured.
                const scope = (el) => {
                  if (el.closest("p")) return false;
                  if (el.closest('[class*="leaflet-"]')) return false;
                  if (el.localName === "label") return Boolean(el.querySelector("input, select, textarea"));
                  if (el.localName === "input" && (el.type === "checkbox" || el.type === "radio")) {
                    return !el.closest("label");
                  }
                  return true;
                };

                const controls = deepQueryAll("button, a, input, select, textarea, label");
                const out = [];
                let checked = 0;
                for (const el of controls) {
                  const style = getComputedStyle(el);
                  if (style.display === "none" || style.visibility === "hidden") continue;
                  if (el.localName === "input" && el.type === "hidden") continue;
                  const rect = el.getBoundingClientRect();
                  if (rect.width === 0 || rect.height === 0) continue;
                  if (!scope(el)) continue;
                  checked++;
                  if (rect.height < min) {
                    out.push({
                      desc: describeElement(el),
                      height: Math.round(rect.height * 10) / 10,
                      text: (el.textContent || el.value || "").replace(/\s+/g, " ").trim().slice(0, 30),
                    });
                  }
                }
                return { small: out, checked };
              },
              { deepSource: DEEP_DOM_SOURCE, min: MIN_TAP_TARGET_PX }
            );

            totalChecked += result.checked;
            for (const s of result.small) {
              failures.push(`${route.label}: ${s.desc} is ${s.height}px tall ("${s.text}")`);
            }
          }

          // A sweep that finds nothing is not a pass. The floor is well under
          // the ~200 controls the current routes actually yield, so it catches
          // "the selector stopped matching" without breaking on every UI edit.
          expect(
            totalChecked,
            "no controls were measured at all — the sweep found nothing, so this check proves nothing"
          ).toBeGreaterThan(100);
          expect(failures, `${failures.length} control(s) below ${MIN_TAP_TARGET_PX}px`).toEqual([]);
        });
      }
    });
  }
}
