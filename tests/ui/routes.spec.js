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
        // Reported separately from document-level overflow: the two say
        // different things, and one shouldn't hide the other in the output.
        const clippedFailures = [];

        for (const route of routes) {
          await gotoRoute(page, route.path);

          const result = await page.evaluate((deepSource) => {
            eval(deepSource);
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

            // Second, independent sweep: content wider than the box holding it.
            //
            // The document-level measurement above misses this entirely, which
            // is how six overlapping tab labels passed in Stage 09 Milestone 6 —
            // the bar fit the viewport and the tabs were tall enough, while the
            // text ran into its neighbours. Note this runs unconditionally, not
            // only when the page already overflows, and it pierces shadow roots
            // (the trip and location cards live in them, and a clipped card
            // title is exactly this bug).
            //
            // Exclusions, all deliberate:
            //  - overflow-x auto/scroll: scrolling is the element's job.
            //  - text-overflow: ellipsis: truncation on purpose, and it is
            //    *implemented* as content wider than its box.
            //  - form controls: an <input>/<textarea>/<select> whose value is
            //    longer than its box is normal, not a layout bug.
            //  - Leaflet's internals, whose panes are far wider than the map on
            //    purpose (same reason the tap-target sweep skips them).
            const clipped = [];
            for (const el of deepQueryAll("*")) {
              const style = getComputedStyle(el);
              if (style.display === "none" || style.visibility === "hidden") continue;
              if (style.overflowX === "auto" || style.overflowX === "scroll") continue;
              if (style.textOverflow === "ellipsis") continue;
              if (["input", "textarea", "select", "svg", "img"].includes(el.localName)) continue;
              if (el.closest?.('[class*="leaflet-"]')) continue;
              // ...and the box immediately *around* a Leaflet map, for the
              // same reason one level up. Leaflet parks its panes at enormous
              // offsets on purpose (measured at right=1825757), and
              // .map-wrap's overflow:hidden exists precisely to stop that
              // reaching the document - so its content width reports the
              // library's internals, not a layout bug of ours. Matched by
              // "contains a .leaflet-container" rather than by our own class
              // name, so it cannot quietly start excluding something else.
              // The legend inside it is our markup and is still swept.
              if (el.querySelector?.(".leaflet-container")) continue;
              // Visually-hidden labels, which are *defined* as content much
              // wider than a 1px box: .sr-only and the .btn-collapse span rule
              // (base.css) clip a real label down to 1x1 so the button keeps its
              // accessible name while showing only its icon. Every one of them
              // reported here on the first run - 10 across the mobile routes -
              // and all of them are correct as they are.
              if (style.clipPath !== "none") continue;
              // clientWidth is 0 for inline elements, where scrollWidth is
              // meaningless - measuring those would report every <span> - and a
              // box a few px wide isn't laying out content in any sense worth
              // asserting on.
              if (el.clientWidth <= 4) continue;
              const over = el.scrollWidth - el.clientWidth;
              if (over > 1) {
                clipped.push({
                  desc: describeElement(el),
                  over,
                  scrollWidth: el.scrollWidth,
                  clientWidth: el.clientWidth,
                  text: (el.textContent || "").replace(/\s+/g, " ").trim().slice(0, 40),
                });
              }
            }
            return { overflow, widest, clipped, scrollWidth: doc.scrollWidth, innerWidth: window.innerWidth };
          }, DEEP_DOM_SOURCE);

          if (result.overflow > 0) {
            failures.push(
              `${route.label} (${route.path}): scrollWidth ${result.scrollWidth} > viewport ${result.innerWidth}` +
                (result.widest ? ` — widest: <${result.widest.tag} class="${result.widest.cls}"> right=${Math.round(result.widest.right)}` : "")
            );
          }
          for (const c of result.clipped) {
            clippedFailures.push(
              `${route.label}: ${c.desc} content is ${c.over}px wider than its box ` +
                `(${c.scrollWidth} vs ${c.clientWidth}${c.text ? `, "${c.text}"` : ""})`
            );
          }
        }

        expect(failures, `horizontal overflow on ${failures.length} route(s)`).toEqual([]);
        expect(
          clippedFailures,
          `${clippedFailures.length} element(s) whose content is wider than the box holding it`
        ).toEqual([]);
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

                // <summary> is in the list because it is a real control: it
                // opens and closes the itinerary's day cards (Stage 10
                // Milestone 4), and without it here the 44px claim on those
                // rows would rest on one manual measurement.
                const controls = deepQueryAll("button, a, input, select, textarea, label, summary");
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
                  // Width as well as height. A target is only 44px if it is
                  // 44px in both directions: the tab bar's "More" trigger sized
                  // to 45px inside a 58px cell in Stage 09 Milestone 6 and
                  // nothing noticed, because only height was ever measured.
                  if (rect.height < min || rect.width < min) {
                    out.push({
                      desc: describeElement(el),
                      height: Math.round(rect.height * 10) / 10,
                      width: Math.round(rect.width * 10) / 10,
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
              failures.push(`${route.label}: ${s.desc} is ${s.width}x${s.height}px ("${s.text}")`);
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
