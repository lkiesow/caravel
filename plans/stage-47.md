# Stage 47: Real links for location cards, trip cards and trip tabs

## Context

Opening a location in a background tab from a trip's location list does not
work. Middle-click, Ctrl/Cmd-click and the context menu's "Open link in new
tab" all need an `href`, and the list entries have none. Today you have to
open the location, copy the URL and paste it into a new tab.

The cause is that the cards are not links. `<item-card>`
(`web/js/components/location-card.js`) and `<trip-card>`
(`web/js/components/trip-card.js`) set `role="button"` and `tabindex="0"` on
themselves. On a click or an Enter/Space key press they dispatch
`item-open` / `trip-open`, and the page turns that into `navigate()`
(`locations-tab.js:612`, `trips-page.js:229`). The trip tab bar has the same
problem: each tab is a `<button data-tab>` with a click handler that calls
`pushState` itself (`trip-detail-page.js:70-101`).

Half the app already does this right. Itinerary entries, map popup links,
the location link on an expense, links inside notes and every back-link are
real `<a href>`. The router (`web/js/router.js:113-129`) takes over a plain
left-click on any `[data-link]` and leaves modified and middle clicks to the
browser. Their comments give the reason: "middle-click, open-in-new-tab,
copy link address … come free with a link". This stage brings the location
cards, the trip cards and the trip tab bar in line.

Out of scope:
- **The "More" tab menu.** Its items are menu-component buttons, and turning
  them into links would change a component shared with the filter and sort
  menus. It goes into `plans/todo.md`.
- **Action buttons** (Edit, New location/trip, user-menu entries). These
  start actions; they are not places you would want to open twice.

## 0. Land the plan

Commit this plan as `plans/stage-47.md`. `plans/todo.md` has no entry for
this yet.

## 1. Location cards and trip cards become links

**Approach: the page wraps each card in a real link, outside the card's
shadow DOM.**

```html
<a class="card-link" href="/trips/T/locations/I" data-link><item-card …></item-card></a>
<a class="card-link" href="/trips/T" data-link><trip-card …></trip-card></a>
```

A click inside the card's shadow root is reported to the document as coming
from the host element (`<item-card>`). From there the router's existing
`closest("[data-link]")` search finds the wrapper link, so its modifier-key
check applies with no new routing code. This avoids the workaround
`map-view.js:1517-1540` needed for a link *inside* a shadow root: its own
click listener that checks modifier keys and re-dispatches an event.

Changes:
- `location-card.js`, `trip-card.js`:
  - Drop `role="button"` and the `tabindex` default.
  - Drop the click and keydown listeners and the `open()` method; the link
    is now the focusable, activatable element.
  - Drop `cursor: pointer` and `:host(:focus-visible)` from `:host`; the link
    provides both.
  - Keep `.card:hover`. The host is still hovered, because hover works
    through the shadow boundary.
  - Update the header comments where they describe the open event.
- `locations-tab.js` (around line 313) and `trips-page.js` (around line
  209):
  - Create the `<a class="card-link" data-link>`, set its `href`, append the
    card inside it, and append the link to the list or grid.
  - Remove the `item-open` / `trip-open` listeners. Keep `trip-detail-page.js`'s
    `item-open` listener: the map popup still uses it.
  - Build the `href` with the same template the `navigate()` call uses today.
- `web/css/base.css`, new `.card-link` rule next to `.item-list` and
  `.trip-grid`:
  - `display: block; color: inherit; text-decoration: none;`. This is needed
    because the global `a { color: var(--color-accent) }` would otherwise
    reach the card text through inheritance, and the underline would carry
    into the shadow content.
  - `border-radius: 0.5rem` and a `:focus-visible` outline, copied from the
    old `:host(:focus-visible)` rule.
  - `.trip-grid > .card-link { height: 100% }`. This keeps the equal-height
    row that trip-card's `:host { height: 100% }` comment explains: the grid
    item is now the link, so the stretch has to pass through it.

Accepted side effects, to be noted in the Done paragraph:
- Screen readers announce "link" instead of "button".
- Space no longer opens a card; Enter still does, as for any link.
- The accessible name is the card's visible text (title, first date range,
  visible tags). No `aria-label`, so dates stay audible.
- Dragging over a card drags the link instead of selecting text.
- A location opened in a new tab does not inherit the list's filter state,
  which lives in the list's own history entry. Its back-link goes to
  `/trips/:id`. A new tab has no history state, and `leaveEditor` already
  handles that.

Tests (`tests/ui/locations.spec.js`, `tests/ui/trips.spec.js`):
- Each card's `closest("a")` has `href` equal to the expected path.
- A plain click on a card navigates client-side. Assert a `window` marker set
  before the click survives, plus `window.location.pathname`.
- Ctrl+click on a card opens a new page (`context.waitForEvent("page")`) with
  the right URL, and the current page's `pathname` is unchanged.
- Focusing the link and pressing Enter navigates.
- The accessible name: `getByRole("link", { name: /Hotel Ranga/ })` resolves.
- Trip grid: cards in one row still have equal heights. Compare the
  `getBoundingClientRect().height` of two cards, one with dates and one
  without.
- Existing specs that `.click()` a card keep working unchanged. Re-run
  `headings.spec.js`: the h2 is now inside a link, which is valid.

## 2. Trip tab bar becomes links

Changes:
- `trip-detail-page.js:70-74`:
  - Render each tab as `<a href="/trips/${trip.id}/${key}" data-tab="${key}" …>`
    instead of `<button>`.
  - Add `aria-current="page"` on the active tab, next to the `active` class.
  - No `data-link`: tabs keep their local re-render, so the router does not
    refetch the trip on a tab switch.
- `trip-detail-page.js:92-101`, the tab click handler:
  - Return early on `e.button !== 0` or any modifier key. Same guard as
    `router.js:121` and `map-view.js:1530`.
  - Otherwise `preventDefault()` and keep the existing
    `pushState` + `render()`.
- `web/css/base.css`, every `.trip-tabs > button` selector:
  - Change to `.trip-tabs > a`: lines ~1507, 1520, 1660, 1666, and the mobile
    block at ~3406-3502.
  - Add `text-decoration: none`. The color is already set by the rule, so the
    global link color does not apply. Confirm by computed style.
  - Update the comments at ~1546 and ~3447 and ~3500 that name the selector.
  - Leave the `.menu__trigger` rules alone.
- `tests/ui/menu.spec.js:344,349`: change `button[data-tab]` to `a[data-tab]`.
  Grep `tests/` and `scripts/gen_screenshots.mjs` for any other tab lookup by
  `button` role and update those too.

Tests (`tests/ui/menu.spec.js` or the trip-detail spec):
- Every visible tab has `href` `/trips/:id/<key>`, and exactly one has
  `aria-current="page"`.
- A plain click switches the tab with no `GET /api/trips/:id` request (the
  local re-render), and `pathname` updates.
- Ctrl+click opens a new page on that tab's URL and leaves the current tab
  unchanged.
- At 324px the row still shows four tabs plus More. The tab labels are still
  centered and tappable: the existing `menu.spec.js` layout tests cover this.
  Check that the tabs' computed `text-decoration-line` is `none`.

## Build order

0 → 1 → 2. Milestone 1 is the reported bug. Milestone 2 is independent of it
and only reuses the same idea.

## Workflow

For each milestone:
1. Implement.
2. `make ci` green, plus the Playwright assertions above. Also run
   `make test-ui` and do a manual check against `make dev`:
   - Middle-click a card and a tab; the page should open in a new tab.
   - Right-click shows "Open link in new tab".
   - Run at 324×756.
3. Add a **Done.** paragraph to this file and update `plans/todo.md`. After
   milestone 2, add "More menu items as links" (Stage 47).
4. Commit, one per milestone.
5. Make sure `make dev` is running, then stop and wait.

## Verification

- `make ci` and `make test-ui` green, including the new link and Ctrl-click
  assertions and the existing card, heading and tab-bar specs.
- `make check-contrast`: the focus ring has moved to a new element.
- `make screenshots`, then diff `docs/assets/screenshots/`. Only
  focus/hover-related differences are acceptable. Run `make docs` if any
  screenshot changes are committed.
- Manual, in a real browser:
  - Middle-click a location card, then a trip card, then the Map tab. Each
    should open in a background tab on the correct URL, while the current
    page stays where it is.
  - A plain click still navigates without a full reload.
  - Back from a location still restores the filtered and sorted list.
