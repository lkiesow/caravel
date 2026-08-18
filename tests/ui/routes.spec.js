// The sweep matrix: every route × {desktop, mobile} × {light, dark}.
//
// Stage 07 hand-rolled this every round and it took seconds to run; the point of
// writing it down is that the fiddly parts (dark mode without touching the OS
// theme, asserting the landed URL, reaching controls inside shadow roots) stop
// getting skipped.
import { test, expect } from "@playwright/test";
import { login, buildRoutes, gotoRoute, VIEWPORTS, COLOR_SCHEMES } from "./helpers/scenarios.js";
import { DEEP_DOM_SOURCE } from "./helpers/deep-dom.js";

// Tap-target floor for buttons, in CSS px at phone width.
//
// This is a REGRESSION GUARD SET TO THE APP'S MEASURED CURRENT FLOOR, not the
// accessibility guideline. The guideline is 44px; measured across seven routes at
// 324px wide, nothing in Caravel reaches it — buttons bottom out at 40px, block
// links at 30px, inline-flex links (the "Back"/"Home" links) at 22px, and
// checkbox inputs at 14px (20px counting their label). Stage 04's note that "the
// tap targets themselves are fine (≥44px)" was about the trip tab bar
// specifically, not the whole app.
//
// So asserting 44px here would just be red on every route, which is a finding to
// record rather than a test to run. 40px locks in the current state so a future
// change can't quietly shrink a button; closing the remaining 4px is an app
// change, recorded in todo.md.
const MIN_TAP_TARGET_PX = 40;

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
                // Buttons that are *styled as buttons*. Links are excluded
                // because prose links must be allowed to be inline-sized, and
                // Caravel's icon+text "Back" links are a design question rather
                // than a regression risk; input sizing likewise.
                //
                // The style filter is not a convenience. `.itinerary-entry__link`
                // is a <button> with no background, no border, no padding and
                // `font: inherit` — a text link in disguise, sized by its text
                // (22px). Judging it by a button's standard would be judging the
                // wrong thing. Everything excluded here is measured and recorded
                // in todo.md rather than dropped.
                const looksLikeAButton = (el, style) => {
                  if (/(^|\s)btn(\s|$|-)/.test(el.className || "")) return true;
                  const bg = style.backgroundColor;
                  const hasBg = bg && bg !== "transparent" && !/rgba\(\s*0,\s*0,\s*0,\s*0\s*\)/.test(bg);
                  const hasBorder = parseFloat(style.borderTopWidth) > 0 || parseFloat(style.borderBottomWidth) > 0;
                  return hasBg || hasBorder;
                };

                const controls = deepQueryAll("button");
                const out = [];
                let checked = 0;
                for (const el of controls) {
                  const style = getComputedStyle(el);
                  if (style.display === "none" || style.visibility === "hidden") continue;
                  const rect = el.getBoundingClientRect();
                  if (rect.width === 0 || rect.height === 0) continue;
                  if (!looksLikeAButton(el, style)) continue;
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

          // A sweep that finds nothing is not a pass.
          expect(
            totalChecked,
            "no buttons were measured at all — the sweep found nothing, so this check proves nothing"
          ).toBeGreaterThan(20);
          expect(failures, `${failures.length} button(s) below ${MIN_TAP_TARGET_PX}px`).toEqual([]);
        });
      }
    });
  }
}
