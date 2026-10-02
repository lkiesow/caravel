# Stage 43: tap a sort again to reverse it

## Context

Both list screens have a sort menu, and every order in them runs one fixed
way: locations "As added" is oldest first, "By name" is A→Z, "By date" is
earliest first; trips "By name" is A→Z and "Recently added" is newest first.
There is no way to see the last-added locations first, or a list Z→A.

Tapping the option that is already selected currently does nothing
(`components/menu.js`: `if (value === active) return;`), so that tap is free
to take on a job: **tap the checked order again to reverse it.**

### Decisions already taken

- **Tap again to flip.** The menu keeps one row per order, not one per
  direction. Reversible rows show the direction **in words**
  ("As added · oldest first" / "· newest first", "By name · A–Z" / "Z–A",
  "By date · earliest first" / "latest first"), not only as ↑/↓ arrows,
  because arrows are read both ways. The checked reversible row carries a
  small ↕ marker as the hint that tapping it again flips it.
- **Natural direction per order, reset on switch.** Choosing a different order
  always starts at its natural direction (today's direction), so nothing
  changes for anyone who never taps twice. Only the current order's direction
  is stored.
- **Trips' "Recently added" becomes "As added"**, flippable, with
  *newest first* as its natural direction on trips (today's behaviour).
- **"Upcoming first" is not reversible.** It is three blocks with their own
  internal directions; reversed, it means nothing. Its row has no marker and a
  second tap stays a no-op.
- **What flips:** only the primary key. Undated locations stay last in both
  date directions; title tie-breaks stay A→Z.
- **Tint:** the trigger tints whenever the order is not the default
  *including direction*, so a reversed "As added" tints.

## 1. The menu learns to reverse; the locations tab uses it

**`web/js/components/menu.js`** — new, opt-in item fields, so no other
caller changes:

- `items[].reversible: true` plus `items[].reversedLabel` (the label to show
  while reversed). The plain `label` is the natural-direction label.
- `activeReversed` option (default `false`), alongside `activeValue`.
- Clicking the **active reversible** row flips the internal `reversed` flag
  and calls `onSelect(value, { reversed })`. Clicking a **different** row sets
  `reversed = false` and calls `onSelect(value, { reversed: false })`. Clicking
  the active non-reversible row stays a no-op, as now.
- `syncLabel()` picks `reversedLabel` for the active row while reversed (in
  the row itself and in the trigger label). The tint condition becomes
  `active !== neutralValue || reversed`.
- Checked reversible row renders a trailing `icon("arrow-down-up",
  { className: "menu__reverse" })`. It is already in the sprite, so the icon
  sprite needs no regeneration. It is hidden with CSS on every row except
  `[aria-checked="true"]`, the same way `.menu__check` is.
- `setActive(value, reversed = false)` keeps working for the distance filter.
- Update the header comment block with the new option, matching its style.

**`web/css/base.css`** — `.menu__reverse` rules next to `.menu__check`: pushed
to the trailing edge (`margin-inline-start: auto`), muted, visible only on the
checked row.

**`web/js/pages/locations-tab.js`**

- `let reversed = saved.reversed ?? false;` next to `sort`. `toolbarState()`
  writes `state.reversed = true` only when reversed, so the neutral toolbar
  stays "no key" and the Back-press entry logic is unchanged.
- `sorted()`: `const dir = reversed ? -1 : 1;`
  - `added` → `out.reverse()` when reversed (the API order is the order).
  - `title` → `dir * collator.compare(...)`.
  - `date` → the undated-last branches unchanged, only the date comparison
    multiplied by `dir`.
- Menu items: `reversible: true`, `label: t("locations.sort.<key>.asc")`,
  `reversedLabel: t("locations.sort.<key>.desc")`, `activeReversed: reversed`;
  `onSelect(value, { reversed: r })` sets both and calls `applyFilters()`.
- Update the comment at the top (SORTS) to say the direction can now flip.

**`web/locales/en.json` + `de.json`** — replace `locations.sort.added|title|date`
with `.asc` / `.desc` pairs:

| key | en | de |
|---|---|---|
| added.asc / .desc | As added · oldest first / newest first | Wie hinzugefügt · älteste zuerst / neueste zuerst |
| title.asc / .desc | By name · A–Z / Z–A | Nach Name · A–Z / Z–A |
| date.asc / .desc | By date · earliest first / latest first | Nach Datum · früheste zuerst / späteste zuerst |

**Fit check at 324px:** the dropdown is right-anchored to the trigger
(`right: 0`) with `white-space: nowrap`. The German labels are the longest;
assert the open dropdown's bounding box stays inside the viewport. If it
doesn't, shorten the copy (e.g. drop the "Wie hinzugefügt" prefix wording)
rather than letting rows wrap.

**Tests — `tests/ui/locations.spec.js`**, extending the existing
"sorts by name and by date" test (its seed already makes the three orders
disagree):

- tap "By name · A–Z" again → Z→A order, label `By name · Z–A`, trigger tinted;
- "By date" reversed → latest first, the two undated **still last**;
- "As added" reversed → exactly `added.reverse()`, trigger **tinted**;
  tap again → original order, untinted;
- switching from a reversed order to another one starts at its natural
  direction;
- reversed state survives open-location → Back (history entry).
- Update the existing `choose("By name")` etc. labels to the new copy.

**`tests/ui/menu.spec.js`** — a small component-level case: the ↕ marker is
visible only on the checked reversible row, the accessible name
(`menuitemradio`) carries the direction words, and a non-reversible checked
row ignores a second tap.

**Done.** Landed as planned in `menu.js`, `base.css` and
`locations-tab.js`, with one deviation, in the copy. The planned
"As added · oldest first" style did not fit: measured at 324px the German
dropdown was 322px wide and ran 66px off the left edge, and the English one
ended 8px into the 16px gutter. The text budget for the longest row is about
158px (the menu opens leftwards from the trigger, whose right edge sits at
256px, and about 82px goes to padding, the check and the marker). The labels
were re-chosen with Lars at the checkpoint:
`Added (first) / Added (last)`, `Name (A–Z) / Name (Z–A)`,
`Date (first) / Date (last)`; German `Erstellt (zuerst) / Erstellt (zuletzt)`,
`Name (A–Z) / Name (Z–A)`, `Datum (zuerst) / Datum (zuletzt)`. The widest is
under 130px. Milestone 2's locale paragraph above was updated to match.

`menu.js` re-syncs every row label on each change, since a reversible row's
label changes with its direction, and `setActive` takes an optional
`reversed`. Verified: `make ci` green; full `make test-ui` green (321 passed).
New in `locations.spec.js`: reversing "as added" gives the exact reverse of
the fetch order and tints the trigger, while a second tap restores the order
and removes the tint; name Z–A; switching from a reversed order starts the
next one in its natural direction; date reversed keeps the two undated
locations last; the marker is visible on the checked row only; the direction
is in the `menuitemradio` accessible name; the dropdown stays inside the 16px
gutter at 324px; the reversed order survives opening a location and pressing
Back. New in `menu.spec.js`: the tab bar's More menu has no marker and a
second tap on its current row does not navigate (history length unchanged),
and the German sort labels fit at 324px. The manual MCP browser pass was not
possible because the MCP Firefox is not installed. The specs above run the
same checks in Firefox at 324×756.

## 2. The trips list: "As added" and a reversible name order

**`web/js/pages/trips-page.js`**

- `SORTS` stays `["upcoming", "title", "added"]`; `upcoming` not reversible.
- Storage: one key, value `"<sort>"` or `"<sort>:reversed"`. `storedSort()`
  parses it into `{ sort, reversed }` and falls back to the default for an
  unknown value, or for `upcoming:reversed`. Old stored values (`"title"`,
  `"added"`) still parse, as natural direction. The default is stored as the
  key's absence, as now.
- `sorted()`: `title` → `dir * tie(a, b)`; `added` →
  `dir * cmp(b.created_at, a.created_at) || tie(a, b)` (natural is newest
  first); `upcoming` untouched.
- Menu items as in Milestone 1.

**Locales** — replace `trips.sort.title|added` with `.asc`/`.desc` pairs in
the copy Milestone 1 settled on: `Name (A–Z) / Name (Z–A)` (same in German)
and `Added (first) / Added (last)` (`Erstellt (zuerst) / Erstellt (zuletzt)`).
`.asc` always means oldest/A first, so on trips the *natural* label for added
is `.desc` ("Added (last)") and the reversed one `.asc`. `trips.sort.upcoming`
stays.

**Tests — `tests/ui/trips.spec.js`**: update `SORT_LABELS`; add reversal
cases (name Z→A, added oldest-first = exact reverse of newest-first on this
seed, ignoring title ties, whose order doesn't flip); persistence of
`"title:reversed"` across a reload, and the default still clearing the key;
an old `"added"` value in localStorage loads as natural direction; tapping
"Upcoming first" again changes nothing.

**Docs — `docs/features/trips-and-locations.md`** lines ~13–15 and ~42–43:
rename "Recently added", and add one sentence that tapping the current order
again reverses it, with undated locations staying last. `make docs`.
Regenerating the screenshots is not needed: no screenshot shows an open sort
menu. Confirm with a grep of `scripts/gen_screenshots.mjs` for `sort`.

**Done.** Landed as planned. The order of the trips menu was confirmed with
Lars at the checkpoint: Upcoming first (still the list's default, not
reversible, wording unchanged), Name (A–Z ⇄ Z–A), and Added (last ⇄ first),
which starts newest first as "Recently added" did. Added therefore starts in
the opposite direction from the locations tab's, which is deliberate. The
items are listed explicitly in `trips-page.js` rather than mapped from
`SORTS`, because added's natural label is its `.desc` key. Stored values are
`<sort>` or `<sort>:reversed`. A suffix-less value from before this stage
loads in its natural direction, and `upcoming:reversed` or an unknown suffix
falls back to the default. `docs/features/trips-and-locations.md` describes
the reversal on both lists. The screenshot generator never opens a sort menu,
so the screenshots stand. The todo.md entry on in-browser trip sorting now
notes that a server-side `sort` would need a direction.

Verified: `make ci` green; `make docs` clean; full `make test-ui` green (323
passed). New in `trips.spec.js`: name Z–A is the exact reverse collation;
Added newest-first and oldest-first match `created_at` from `GET /api/trips`,
with ties A–Z in both; switching from a reversed order starts the next one
natural; the marker is on the checked reversible row only and absent from
Upcoming; a second tap on Upcoming changes neither the order nor the tint;
`title:reversed` is stored and survives a reload; an old `added` value loads
as "Added (last)"; `upcoming:reversed` falls back to the default; and
choosing the default still clears the key.

## Build order

1 → 2. Milestone 1 builds the component and proves it on the screen with the
most orders. Milestone 2 is then pure wiring plus the storage format.

## Workflow

Per milestone: implement → `make ci` green plus the Playwright specs
(`make test-ui GREP="sort"`, and the full `menu.spec.js`, since every menu in
the app goes through `menu.js`) plus a manual pass at 324×756 against
`make dev` → add a **Done.** paragraph here and update `plans/todo.md` (amend
the "trips list is filtered and sorted in the browser" entry: the orders are
now also reversible, which a server-side `sort` param would need to carry) →
one commit → `make dev` running → stop and wait.

## Verification

- `make ci` (i18n parity catches a missed `de.json` key).
- `make test-ui` in full at the end of each milestone, not only the sort specs.
  `menu.js` has many callers (tab bar, user menu, row actions, admin), and the
  opt-in design should leave them untouched. The full suite is what shows it.
- Manual at 324×756 in both locales: open each sort menu, check the marker
  appears only on the checked row, flip each order, confirm the dropdown fits
  on screen, confirm tint behaviour, and reload trips / Back on locations.
- Assertions over screenshots: titles in DOM order, `menu__trigger--active`
  class, `aria-checked`, accessible names, dropdown `getBoundingClientRect()`
  within `window.innerWidth`.
