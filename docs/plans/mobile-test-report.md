# Mobile UI/UX Test Report — Stage 04 follow-up

**Original test:** 324 × 756 (portrait), Playwright/Chromium, 2026-08-10.
**This follow-up:** 324 × 756, Playwright/Firefox, 2026-08-11, verified
against each of Stage 04's five milestones (`stage-04.md`).

The original report (superseded below) was produced under a mistaken premise
— it read as if Caravel were a server-rendered template app, when the
frontend is actually a vanilla-JS SPA with three Shadow-DOM components — and
roughly half its findings were hedged speculation ("may overflow") that its
own screenshots disproved. Its value was the inventory of pressure points,
not its verdicts; `stage-04.md`'s "Report triage" section has the full
finding-by-finding breakdown of what reproduced and what didn't. This
document instead records the *outcome*: what was fixed, how it was verified,
and what remains open. Screenshots from both the original test and this
follow-up are gitignored (`docs/plans/mobile-fresh-*.png`) — they're
throwaway dev artifacts regenerated per test run, not durable evidence, so
findings are written up as prose here instead.

## Resolved

| Finding | Fix | Milestone |
| --- | --- | --- |
| Trips-page header overflow ("New trip" wrapping) | `.btn-collapse` drops the label to icon-only ≤640px | 2 |
| Tab bar cut off mid-word, no scroll affordance | 6-tab bar becomes a non-scrolling icon+micro-label grid ≤640px | 3 |
| Checklists add-form genuinely overflowing 324px | `min-width: 0` on the flex input + `.btn-collapse` on the button | 1, 2 |
| Itinerary add-row wrapping to two lines in every day card | Same `min-width: 0` + collapse combination | 1, 2 |
| Map legend covering ~55% of the map | Legend moves to a static full-width row below the map ≤640px; map height reduced from 60vh/24rem to 50vh/16rem | 4 |
| Locations filter row forcing "New item" onto its own row | Filter pills shrink and scroll internally instead of wrapping, freeing room for the (now collapsed) button on the same row | 5 |

## Also fixed, not in the original report

The original report's manual pass didn't check touch-target sizing or
keyboard focus at all — these turned out to be the more consequential gaps:

- Every interactive control was under the ~44px touch-target guideline
  (`.btn` ~34px, filter pills ~28px, tabs ~33px, `.icon-remove` ~16px — the
  worst offender in the app). All raised to 44px minimum ≤640px.
- `.btn` had no `:hover`, `:active`, `:focus-visible`, or `:disabled`
  styling at all — keyboard focus was invisible anywhere in the app.
- `.auth-form input` was missing `background`/`color`, making the
  login/register form unreadable in dark mode.
- Inconsistent button styling: six buttons across the app were text-only
  with no icon, unlike every other `.btn`. All six normalized to icon+label.

## False positives from the original report (confirmed, not fixed — nothing to fix)

- Trip editor and location editor were already clean at 324px; both
  `.location-form`/`.link-form`/`.date-form` already wrapped correctly.
- "Add document" already had an icon; the map legend checkboxes were
  already a vertical column, not horizontal; Leaflet's zoom control and the
  legend don't overlap (opposite corners).

## Verification method

Each milestone was checked with the Playwright MCP tools against `make dev`,
resized to 324×756:

- `document.documentElement.scrollWidth <= window.innerWidth` on every route
  (asserting the landed-on URL too, after discovering mid-stage that an
  earlier pass used a URL pattern matching no real route and was silently
  redirected to `/trips` — see `todo.md`'s testing-gaps section).
- No interactive control under 44px tall.
- Re-checked at 768px and 1200px that collapsed labels re-expand and nothing
  regressed on desktop.
- Console checked for JS/CSS errors on every navigation (this caught a real
  bug in Milestone 4 — see that milestone's notes in `stage-04.md`).
- Existing CI (`make ci`): build, vet, JS syntax, i18n key parity.

## Still open

- The itinerary tab's 10-day-trip scroll length (all days render open,
  unconditionally) is a feature-level fix, not a layout one — tracked in
  `todo.md` under "Collapse empty/past itinerary days behind a `<details>`
  disclosure."
- No scripted regression check exists yet for any of the above — this
  report and `stage-04.md` are the durable record until the Playwright test
  suite (`todo.md`) exists to encode it as an assertion.
