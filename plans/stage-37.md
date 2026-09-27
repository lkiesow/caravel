# Stage 37 — Three more location categories

A one-milestone interstage change rather than a full stage: no new
surfaces, no new behaviour, just a wider closed vocabulary.

## Context

A location carries exactly one category, and there were four: `site`,
`stay`, `transport`, `area`. Three common kinds of place had nowhere to
go and all landed in `site`:

- a **restaurant, bar or cafe** — somewhere you go *for* something;
- an **event** — a concert, festival, show or market, a thing you go to
  for one evening rather than a place that is always there, which you
  still want pinned on the map;
- a **shop or market**.

Each is something a reader looks for as a group, which is what the
category axis is for. Tags stay the free-text axis for anything finer.

Names were chosen with the user: `food` labelled "Food & Drink" /
"Essen & Trinken", `event` labelled "Event" in both languages, `shop`
labelled "Shopping" / "Einkaufen". `shop` was added on top of the
original two on the user's call.

## 1. The three categories

Migration `0011_food_event_shop_categories`, both dialects: Postgres
drops and re-adds `items_category_check`; SQLite rebuilds the whole
items table by the same twelve-step procedure 0010 used, because a CHECK
cannot be altered there. Both downs remap the three values to `site`
first, since the restored constraint has no room for them.

Then the same list everywhere it is deliberately duplicated:
`internal/httpapi/items.go` (the map and its hand-written error string),
`internal/assist/agent.go`, the JSON schema enum and description in
`internal/assist/schema.go`, the prose sentence in
`internal/assist/prompt.go`, `CATEGORIES` in `location-form.js`,
`locations-tab.js` and `suggest-page.js`, `CATEGORY_COLORS` in
`map-view.js`, `location-card.js`, `location-view-page.js` and
`itinerary-tab.js`, the light and dark `--marker-*` blocks in
`map-view.js`, and `item.category.*` in both locale files.

**Done.** All of the above landed as described, plus four things the
plan did not anticipate:

- **The palette was chosen by measurement, not by eye.** The first pass
  used an obvious amber `#ca8a04` for food; measured against the liberty
  light style it is 2.7:1, below the 3:1 floor the palette comment sets.
  Every candidate was then scored on contrast against both papers *and*
  on CIELab distance to every other marker colour, the pick amber and
  the here cyan. The winners: `food` `#a16207` / `#facc15`, `event`
  `#a21caf` / `#d946ef`, `shop` `#3f6212` / `#a3e635`, all 4.5:1 to
  6.5:1 on liberty — better than the green `site` has ever had. The dark
  fuchsia is the 5.7:1 `#d946ef` rather than the brighter `#e879f9`
  because the brighter one sits 27 dE from the violet `stay`; nothing
  new is now closer than 39 dE to anything else. The reasoning is in the
  palette comment in `map-view.js`. Seven categories is about what this
  encoding holds — an eighth needs a pin shape or an icon.
- **`TestValidCategoriesMatchTheSchema` did not do what its name says.**
  It compared `validCategories` against a literal only, so adding a
  category and forgetting the enum next door in `schema.go` would have
  passed — and a model handed an enum without `food` in it never
  proposes food. It now parses `proposalSchema` and also asserts that
  every value is mentioned in the schema description, which is what
  actually tells the model when to pick one. Verified by injecting a
  half-update: the strengthened test fails, the old one would not have.
- **`migration_0011_test.go`** is modelled on the 0010 test and adds the
  itinerary entry the 0010 test describes but never inserted.
- **The seed was left alone.** The plan wanted one item per new category
  in the demo trip; the full scenario's item count is asserted in the UI
  suite (`map.spec.js:2471` pins it at three cards, among others), so
  three extra items would have churned a dozen specs for demo data.
  Deferred to `plans/todo.md` instead.

Stale comments that said "three"/"four" categories were corrected in
`agent.go`, `agent_test.go`, `domain.go`, `types.go` and `map-view.js`.

**Verified.** `make ci` green, and `make test-postgres` green — required
here, since the two dialects change the constraint in completely
different ways. `TestMigration0011AddsThreeCategoriesAndKeepsEverything`
pins the SQLite rebuild in both directions: locations, links, tags and
itinerary entries survive, the index comes back, the three values are
accepted, nonsense is still refused, and the down folds the three into
sites while leaving `area` alone. Against a running `make dev`: the API
creates all three and refuses `nonsense` with the new message, and
`?category=food|event|shop` each return their one item. In the browser
at 324x756 the filter menu lists eight rows (All plus seven) in English
and German, filtering by Food & Drink narrows the list to its one card,
and the editor select offers all seven in both languages. On the map,
seven pins render in seven distinct colours, each pin's colour equals
its legend dot's in both light and dark scheme, and unchecking Food &
Drink removes exactly the `#facc15` pin and restores it. The `shop`
lime against the `site` green was eyeballed on the real light map as
planned: dark olive against grass green, distinct. `menu.spec.js`,
`locations.spec.js`, `map.spec.js`, `map-theme.spec.js`,
`settings.spec.js`, `assist.spec.js` and `assist-suggest.spec.js` all
pass. The four items created by hand for the browser pass were deleted
afterwards, so the dev database is back to its seeded state.
