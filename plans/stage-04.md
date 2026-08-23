# Stage 04 — Mobile responsiveness & a consistent button system

> **Status: complete.** Built one milestone at a time per the Workflow
> section below, each with its own commit and a manual-testing checkpoint.

## Context

`notes.md:2` asks for a full mobile sweep at 324 × 756 (the native resolution
of the user's phone). A one-off Playwright pass produced
`mobile-test-report.md` plus nine `mobile-fresh-*.png` screenshots. This stage
acts on that report — but the report is **only partly trustworthy**, so the
triage below records what actually reproduces.

The screenshots themselves are **not committed** (gitignored) — they are
throwaway dev artifacts, and at ~420K per test round they would outweigh the
rest of the repo's history within a few passes. The evidence they showed is
written up in the triage table below instead, which is what actually needs to
survive.

The report's framing is also wrong in one place: it reads as if this were a
server-rendered template app. It isn't. Caravel's frontend is a **vanilla-JS
SPA** — 25 ES modules, no framework, no bundler, no `node_modules` — that
builds HTML strings, assigns them to `innerHTML`, then runs `translatePage()`
over `data-i18n` attributes. "Template edits" are JS string edits. Three
components (`trip-card`, `location-card`, `leaflet-map`) render into **Shadow
DOM** with private `const styles` blocks that `web/css/base.css` cannot reach —
only CSS custom properties pierce them. All app CSS is one flat 911-line file.

### Report triage — verified against the screenshots and the CSS

**Real, reproduced in the screenshots:**

| Finding | Evidence |
| --- | --- |
| Trips-page header overflows | `mobile-fresh-trips-page.png`: "New trip" wraps to two lines and squeezes "Your trips" into two lines |
| Tab bar is cut off | Every trip screenshot: `Itinera…` clipped at the right edge, no scroll affordance |
| Checklists add-form overflows horizontally | `mobile-fresh-checklists-tab.png`: the "New checklist" button is genuinely pushed past the right edge |
| Itinerary add-row is cramped | `mobile-fresh-itinerary-tab.png`: "Add item" wraps to two lines in all ten day cards |
| Map legend eats the map | `mobile-fresh-map-tab.png`: the legend covers ~55% of the map's width |
| Locations filter row costs two rows | `mobile-fresh-trip-detail.png`: pills wrap, "New item" lands on its own row |

**False positives — do not spend time on these:**

- *"Trip editor date inputs and Save/Cancel overflow"* — they don't.
  `mobile-fresh-trip-editor.png` is clean at 324px.
- *"Location editor overflows in six places"* — it doesn't.
  `mobile-fresh-location-editor.png` is clean; `.location-form`/`.link-form`/
  `.date-form` already `flex-wrap: wrap` (`base.css:545-557`).
- *"'Add document' has no icon"* — it does
  (`web/js/components/document-list.js:19`).
- *"Map legend checkboxes are stacked horizontally"* — they're
  `flex-direction: column` (`web/js/components/leaflet-map.js:9-56`).
- *"Leaflet zoom controls overlap the legend"* — zoom is top-left, legend
  top-right. No collision.

Roughly half the report is hedged speculation ("may overflow") that its own
screenshots disprove. Its value is the inventory of *pressure points*, not its
verdicts.

**Real problems the report missed entirely** — these are the ones that
actually hurt on a touch device:

- **Tap targets.** `.btn` computes to ~34px tall; `.items-filter` pills ~28px;
  `.trip-tabs button` ~33px; `.icon-remove` has `padding: 0` around a `1rem`
  icon → a **~16 × 16px** target (`base.css:76-94`). All well under the 44px
  guideline.
- **No interaction states at all.** `.btn` has no `:hover`, no `:active`, no
  `:focus-visible`, no `:disabled`. Keyboard focus is invisible.
- **`.auth-form input` is broken in dark mode** — it omits `background`/`color`
  where every other input ruleset sets them (`base.css:133-138`).
- **Breakpoints are inconsistent**: 640 / 768 / 960, mixing one desktop-first
  `max-width` with two mobile-first `min-width` queries.

### The button strategy

Two rules, applied consistently:

1. **Every `.btn` carries an icon *and* a label.** Today six buttons are
   text-only (`trip-form` Save/Cancel, `location-form` Save/Cancel, login
   submit, image-field "Set"), which is why the desktop UI reads as
   inconsistent.
2. **On ≤640px, buttons in space-constrained rows drop the label** and become
   icon-only.

Rule 2 is implemented in **CSS, not JS**. Markup stays
`${icon("plus")} <span data-i18n="…"></span>` everywhere; a `.btn-collapse`
class hides the span below the breakpoint. Critically, hide it with a
**visually-hidden** rule, not `display: none` — that keeps the button's
accessible name intact for screen readers with no `aria-label` duplication,
and needs no re-render on resize. The tab bar (Milestone 3) uses the same
mechanism, so the whole app follows one rule.

The trip-detail header already proves the pattern: its icon-only pencil looks
correct at 324px while the trips-page's labelled "New trip" wraps.

**Decisions taken:** breakpoint **≤640px**, reusing the existing
`.user-menu__name` breakpoint so "mobile" means one thing app-wide; the tab bar
becomes a **non-scrolling 6-column grid of icon + micro-label**; a scripted
overflow-regression harness is **deferred** to the later Playwright-suite work
in `todo.md` — verification this stage is a manual Playwright pass per
milestone.

---

## 1. Foundations: tokens, `.sr-only`, tap targets, states

Groundwork everything else leans on. All in `web/css/base.css`, no JS.

- Extend the `:root` token block (`base.css:1-19`), which currently holds only
  six colors, with the values otherwise hard-coded ~15× each:
  `--radius: 0.375rem`, `--radius-lg: 0.5rem`, `--color-danger: #dc2626`
  (hard-coded at lines 71, 73, 88, 141, 638, 816), `--tap-min: 2.75rem`. No
  full spacing scale — out of scope.
- Add a `.sr-only` utility (`position:absolute; width:1px; height:1px;
  overflow:hidden; clip-path:inset(50%); white-space:nowrap`). Used by
  Milestones 2 and 3.
- Add the missing `.btn` states: `:hover`, `:active`, `:focus-visible`
  (a real `outline` — there is none today), `:disabled`.
- Tap targets under `@media (max-width: 640px)`: `min-height: var(--tap-min)`
  on `.btn`, `.items-filter button`, `.trip-tabs button`; give `.icon-remove`
  real padding so it reaches ~44px (currently ~16px, the worst offender).
- Fix `.auth-form input` to set `background: var(--color-bg)` and
  `color: var(--color-text)` like the other six input rulesets do.
- Defensive overflow hardening, cheap and broad:
  `.page__header { flex-wrap: wrap }` plus `min-width: 0` and
  `overflow-wrap: anywhere` on its `h1`; `min-width: 0` on the `flex: 1`
  children in `.checklist-*-form`, `.itinerary-*` and `.trip-form__dates`
  (`input[type=date]` and `<select>` have an intrinsic minimum width that
  `flex: 1` alone cannot shrink past — this is the actual mechanism behind the
  checklist overflow); `flex-wrap: wrap` on the three identical `*__actions`
  rows.

## 2. The button system

- **Add four sprite icons.** `web/icons/lucide-sprite.svg` has 15 symbols; add
  `check`, `log-in`, `info`, `list-checks`. Extend the `ICONS` list at
  `scripts/gen_icon_sprite.py:15` and re-run the script — it takes the icons
  directory as its one argument and rewrites the committed sprite. `npm` and
  node are available, and the script's docstring already prescribes a
  **throwaway prefix outside the repo** so nothing lands in the working tree
  (deliberate — `.gitignore` has no `node_modules` entry because one has never
  existed here):

  ```
  npm install lucide-static --prefix /tmp/lucide-scratch
  python3 scripts/gen_icon_sprite.py /tmp/lucide-scratch/node_modules/lucide-static/icons
  ```

  The script is not part of the build; only its regenerated output is
  committed. Confirm the four new names exist upstream before running
  (`list-checks` is the likeliest to have been renamed) — the script raises a
  plain `FileNotFoundError` on a bad name, so a typo fails loudly rather than
  silently dropping a symbol. Also check the 15 existing symbols come out
  byte-identical, so an upstream icon revision doesn't quietly restyle the
  current UI in the same commit.

  Keep `web/js/icon.js`'s external-file `<use href="…">` form exactly as is —
  the comment there is load-bearing, it's what makes icons work inside Shadow
  DOM.
- **Normalize every `.btn` to icon + `<span data-i18n>`.** Buttons that today
  put `data-i18n` directly on the `<button>` (`trip-form.js:30-31`,
  `location-form.js:35-36`) must move it onto a child `<span>` — the collapse
  rule targets that span. Add icons to the six text-only buttons:
  Save → `check`, Cancel → `x`, login submit → `log-in`, image "Set" → `check`.
- **Add `.btn-collapse`** and the ≤640px rule that applies the `.sr-only`
  treatment to its span and reduces padding to `.btn-icon`'s.
- **Apply `.btn-collapse` only to space-constrained rows:**

| File | Button | Icon |
| --- | --- | --- |
| `pages/trips-page.js:12` | New trip | `plus` |
| `pages/locations-tab.js:19` | New item — lets the filter pills and the button share one row | `plus` |
| `components/document-list.js:19` | Add document | `plus` |
| `components/checklist-list.js:19,39` | New checklist / Add item — **fixes the one real horizontal overflow** | `plus` |
| `pages/itinerary-tab.js:24,57` | Add a day / Add item — fixes the two-line wrap in every day card | `plus` |
| `pages/location-editor-page.js:70,79` | Add link / Add date | `plus` |
| `pages/location-view-page.js:37` | Edit → matches trip-detail's pencil | `pencil` |
| `pages/trip-detail-page.js:37` | Edit trip — *reverse* direction: drop `.btn-icon`, add a label span so it **expands** to "Edit" on desktop | `pencil` |

- **Never collapse**: Save, Cancel, Delete, "Save location", the login/
  register submit, the document-dialog Upload/Cancel pair. These have room in
  their own rows and must stay unambiguous.
- Every new label span reuses an **existing** i18n key, so no locale file
  edits were needed this milestone — `scripts/check_i18n.py` still gates CI
  for any future addition.

**Done.** One correction found during implementation: the plan's line
references for `location-editor-page.js` (70/79) actually named three
different buttons once re-checked against the file — "Save location" (70,
alone in its own row, not collapsed), "Add link" (79) and "Add date" (89,
collapsed). "Save location" also needed the icon-normalization pass (rule 1)
even though it wasn't in the plan's explicit six-button list — same treatment
as the other bare Save buttons. The document-dialog's Cancel button was
likewise text-only and got the same `x` icon as its `.trip-form`/
`.location-form` counterparts, for the same consistency reason.

## 3. Tab bar

`pages/trip-detail-page.js:11,39-41`.

- Map each tab to an icon: `overview → info`, `locations → map-pin`,
  `map → map`, `itinerary → calendar`, `documents → file-text`,
  `checklists → list-checks`.
- Render each tab as `icon` + `<span data-i18n="trip.tabs.…">`.
- ≤640px: `.trip-tabs` becomes `grid-template-columns: repeat(6, 1fr)` with
  `overflow-x: visible`; buttons stack icon over a `0.625rem` label with
  `min-height: var(--tap-min)`. Six columns at 324px is 54px each, so the
  labels need the small font size and possibly shortened i18n values — check
  the German strings too, they're longer.
- 641–767px: the current horizontal scroller, unchanged.
- ≥768px: the existing vertical 10rem sidebar (`base.css:367-395`) — now with
  icons, which improves it. Verify the icons don't break the `border-left`
  active indicator.
- `TABS` is duplicated between `trip-detail-page.js:11` and `app.js:18-25`.
  Adding icon metadata is a good moment to collapse that into one exported
  constant, so the two lists can't drift.

**Done.** Extracted to `web/js/trip-tabs.js` (`TRIP_TABS`, an array of
`{key, icon}`), imported by both `app.js` and `trip-detail-page.js`. Verified
at 324×756: all six tabs fit one row, 46px tall, zero overflow, in both
English and German (German's longest label, "Checklisten", stays on one line
at the 0.625rem size rather than wrapping or clipping). At 700px and 1200px
the icons carry over into the existing horizontal-scroller and vertical
sidebar layouts without any further CSS — confirmed the sidebar's active-tab
left-border indicator still renders correctly. Tab click → URL push → browser
back/forward all still work with the new icon+span markup, since the click
handler keys off `data-tab`, not text content.

## 4. Map tab

All inside the shadow style block, `components/leaflet-map.js:9-56`. `@media`
queries work normally inside Shadow DOM (they match the viewport), so no JS
resize plumbing is needed.

- ≤640px: `.legend` drops out of `position: absolute` to
  `position: static; flex-direction: row; flex-wrap: wrap` **below** the map —
  it currently covers over half the map's width, which is the actual defect.
- ≤640px: reduce `:host` from `60vh` / `min-height: 24rem` to roughly `50vh` /
  `min-height: 16rem`. The 384px floor is what forces page scroll on a short
  landscape phone.
- The legend's `var(--color-surface, #fff)` fallbacks are dead code — custom
  properties inherit through the shadow boundary. Harmless; leave them.

**Done.** Two issues surfaced during implementation, both fixed:

1. `.legend`'s new `width: 100%` (mobile only) combined with its existing
   padding/border pushed 10px past the viewport, because `base.css`'s global
   `* { box-sizing: border-box }` reset doesn't pierce the shadow root —
   inside it, the browser default is `content-box`, so `width: 100% + padding
   + border` exceeds the container. Fixed by adding `box-sizing: border-box`
   directly to `.legend`.
2. While writing the fix's explanatory comment, an unescaped backtick inside
   a JS template literal (quoting a CSS snippet the way Markdown would)
   silently closed the string early and reopened another one further down —
   the file remained *technically* valid JS by coincidence (so `node --check`
   passed) but `styles` no longer held the intended CSS, and Firefox's CSS
   parser then logged `SyntaxError: missing : after property id` for the
   garbled result. Fixed by rewording the comment without a backtick. Worth
   remembering for any future shadow-DOM `styles` template: never use a
   backtick inside it, even inside a comment.

Also caught mid-milestone: every route sweep run in Milestones 1–3 used
`/items/:id` for the location view/edit pages, which doesn't match any actual
route (the real pattern is `/trips/:tripId/locations/:itemId`) — the router
silently redirects unmatched paths to `/trips`, so those two pages were never
actually exercised by the automated checks, only by the one manual
`mobile-fresh-location-editor.png` screenshot back in the original report.
Re-ran the sweep with the corrected paths (now also asserting
`window.location.pathname` lands where expected, to catch silent redirects in
the future): zero overflow, no sub-44px targets on both.

Verified at 324×756: legend renders as a full-width row below the map with
`position: static`, zero overflow, zero console errors. Confirmed
`:host([lat])` (the location-view page's single-marker map, no legend) is
untouched by the mobile rule at any width — stays exactly 256px tall via a
live location-set/unset round trip through the API, not just by reading the
CSS.

## 5. Remaining density fixes and report update

- `.items-tab__header` / `.items-filter` (`base.css:466-495`): with "New item"
  collapsed by Milestone 2, confirm the four pills plus the button fit one row
  at 324px; add horizontal scroll with `scroll-snap` only if they still don't.
- Itinerary day cards: the `<select>` needs `min-width: 0` to shrink; with the
  add-button collapsed the row should be one line. The report's "ten day cards
  are a long scroll" is a **feature-level** concern (collapsing empty days
  behind `<details>`) — out of scope, goes to `todo.md` instead.
- Rewrite `mobile-test-report.md` as a Stage 04 *after* report: regenerate all
  nine screenshots at 324 × 756, drop the disproved findings, mark the
  confirmed ones resolved.
- Add to `todo.md`: the deferred overflow-regression harness (folded into the
  existing "No real Playwright UI test suite" bullet) and the itinerary
  day-collapsing idea.

**Done.** The itinerary `<select>` was already fixed by Milestone 1's
`min-width: 0` — confirmed via screenshot, no further change needed. The
filter row did *not* fit on one row (measured 14px short at 324px even with
the collapsed button), so applied the plan's documented fallback: `.items-
filter` shrinks (`flex: 1 1 0%; min-width: 0`) and scrolls horizontally
(`overflow-x: auto`, `scroll-snap-type: x proximity`) instead of wrapping,
letting the pills and the button share one row with "All" always the
leftmost, always-visible pill. Verified the filter still functions (clicked
"Site", list narrowed correctly; confirmed the desktop `flex-wrap: wrap`
behavior is untouched at 1200px). `mobile-test-report.md` rewritten as a
Stage 04 follow-up documenting resolved/still-open findings in prose (no
screenshots — see Context, they're gitignored, not a durable record).
`todo.md` updated with the deferred regression-harness note, a new note about
asserting the landed-on URL in any future scripted sweep (see below), and the
itinerary day-collapsing idea.

Also caught here: the plan's own verification method had a footgun worth
recording — earlier milestones' route sweeps used `/items/:id` for the two
location pages, a pattern matching no real route (it's `/trips/:tripId/
locations/:itemId`); the router silently redirects any unmatched path to
`/trips`, so those two pages had never actually been exercised by the
automated checks in Milestones 1–3, only by the one manual screenshot in the
original report. Re-ran the corrected sweep (now asserting
`window.location.pathname` too) across all 12 real routes: zero overflow, no
sub-44px targets anywhere, including both previously-untested pages.

## Build order

1. **Foundations** (Section 1) — supplies the `.sr-only` utility and tokens
   that Sections 2 and 3 consume, and lands the tap-target/focus-state fixes
   the report missed.
2. **Button system** (Section 2) — also frees the horizontal room Section 5
   depends on.
3. **Tab bar** (Section 3).
4. **Map tab** (Section 4) — independent of the others, could move.
5. **Density fixes + report/`todo.md` update** (Section 5) — last, since it
   verifies the cumulative result.

## Workflow: one milestone at a time, with a manual-testing checkpoint

Same loop as Stage 03:

1. Implement that milestone's changes.
2. Verify — `go build ./... && go vet ./...`, the CI script checks, and a
   Playwright pass at 324 × 756.
3. Update `todo.md`/`mobile-test-report.md` where that milestone resolves an
   entry.
4. Commit just that milestone's changes (one commit per milestone).
5. Start the dev server (`make dev`) and hand back control — **stop and wait**
   for manual testing rather than continuing automatically.
6. Resume only once told to.

## Verification

Per milestone, at **324 × 756** via the Playwright MCP tools against
`make dev` on :8080 (serves `web/` live from disk — no rebuild needed for
CSS/JS edits):

1. Resize to 324 × 756, then walk every route: login, register, trips list,
   trip editor, all six trip tabs, location view, location editor.
2. On each, assert no horizontal overflow:
   `document.documentElement.scrollWidth <= window.innerWidth`.
3. Spot-check tap targets — this should return an empty array:
   `[...document.querySelectorAll('.btn,.icon-remove,.trip-tabs button,.items-filter button')].filter(e => e.getBoundingClientRect().height < 44)`
4. Re-check at **768px and 1200px** that labels came back and nothing
   regressed on desktop — the collapse rule is the main risk of breaking the
   currently-working desktop layout.
5. Keyboard-tab through a page to confirm the new `:focus-visible` ring is
   visible, and check both light and dark (`prefers-color-scheme`) — Section 1
   touches the dark-mode auth input bug.
6. CI must stay green: `go build ./...`, `go vet ./...`, `node --check` over
   `web/js/`, and `python3 scripts/check_i18n.py`.
