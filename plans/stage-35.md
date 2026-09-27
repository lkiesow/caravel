# Stage 35 — The trips list opens on the trip you are about to take

## Context

Two problems with the trips list's sort menu, both in
`web/js/pages/trips-page.js`.

**"Newest first" does not say what it sorts by.** It is
`ORDER BY t.created_at DESC` from `internal/db/sqlc/queries/trips.sql` —
creation date — but the label leaves the reader to guess between creation
and last modification. The locations tab already calls the same idea
"added", so the app has the better word and the trips list is not using
it.

**"By start date" is ascending, oldest first.** The trip least likely to
be wanted is the one at the top. For a trip planner the interesting trip
is the next one, and the ordering that puts it first is *not* plain
descending either: descending puts a vague idea pencilled in for 2029
above next week's flight. What is wanted is upcoming-first — future and
in-progress trips soonest-first, then past trips most-recent-first — and
that should be the list's default order, not an option you have to go and
choose.

Sorting is also forgotten on every navigation: `sort` is a closure
variable re-initialised on each render, so opening a trip and pressing
Back drops you back to the default. Since Stage 26 the locations tab
carries its toolbar state one history entry deep; the trips list has
nothing. This stage gives it the simpler half — a remembered preference
per browser — and leaves the URL half where `plans/todo.md` already parks
it, because that wants deciding for both lists at once.

No backend change. Sorting stays in memory over the one `GET /trips` the
page makes, for the reasons the file header already records; the SQL's
`created_at DESC` stays as the fetch order.

## 1. Upcoming first, and "Recently added" says what it means

All in `web/js/pages/trips-page.js` plus the two locale files.

**The option set** becomes `["upcoming", "title", "added"]` with
`DEFAULT_SORT = "upcoming"`. `"start"` and `"newest"` disappear as values
— `"upcoming"` replaces the former outright (a separate ascending
"Earliest first" was offered and declined), and `"added"` is the renamed
latter. The menu stays three items; the toolbar stays three controls.

**The comparator.** Replace the `sort === "start"` branch with a bucket
rank plus a within-bucket order, against today in the *local* timezone:

- bucket 0 — **current or future**: `end_date ?? start_date >= today`.
  A trip that has started but not ended lands here, and because the
  bucket sorts by `start_date` ascending it sits above a trip that starts
  next week. That is the right answer: you are on it now.
- bucket 1 — **past**: `end_date ?? start_date < today`, sorted by
  `start_date` **descending**, so the trip you just got back from is the
  first past one.
- bucket 2 — **undated**: no `start_date`, sorted `created_at`
  descending. Unscheduled is not imminent — same rule the list has today,
  and the same one `docs/features/trips-and-locations.md` states for
  locations.

Break ties within a bucket with the title collator so the order is
deterministic rather than dependent on the fetch order.

**`"added"` becomes an explicit sort** on `created_at` descending
(already in the response — `internal/httpapi/trips.go`) rather than "do
nothing and let the server's order stand". The existing comment claiming
the fetched order *is* the answer stops being true the moment the default
is something else, and a sort that is real code is cheaper to reason
about than one that is an absence of code.

**Reuse rather than re-derive:**

- `todayISO()` currently lives private in `web/js/pages/itinerary-tab.js`,
  with a comment explaining why `toISOString()` is wrong here (UTC
  conversion makes an evening east of Greenwich land on tomorrow). Move
  it into `web/js/format.js` as an export and import it in both places —
  a second copy of that reasoning is exactly what the todo.md "palette
  lives in four files" entry is about.
- Use `byTitle()` from `web/js/sort.js` for the `"title"` branch and for
  the tiebreak, instead of the inline `Intl.Collator` — it is the same
  collator with the same settings, already exported for this.

**i18n.** In both `web/locales/en.json` and `de.json`: drop
`trips.sort.newest` and `trips.sort.start`, add `trips.sort.upcoming`
("Upcoming first" / "Bevorstehende zuerst") and `trips.sort.added`
("Recently added" / "Zuletzt hinzugefügt"); `trips.sort.title` and
`trips.sort.label` are unchanged. `scripts/check_i18n.py` in `make ci`
catches a one-sided edit.

**Docs.** `docs/features/trips-and-locations.md` describes the list as
searchable and sortable; give it a sentence on what the default order now
is and why, next to the existing unscheduled-sorts-last rule.

**Tests** — `tests/ui/trips.spec.js`:

- `SORT_LABELS` takes the new keys and strings in both locales.
- The toolbar test's default-label assertion expects "Upcoming first" /
  "Bevorstehende zuerst".
- Rework the sort test to assert the bucket property against a `today`
  computed in the page, not against literal titles — the file's header
  explains why, and the seed makes all three buckets reachable:
  `today+7` and the year-end trip are future, the `2026-06-15`-based ones
  are past, and "Demo: No Dates Yet" is undated. Assert: every
  future-bucket card precedes every past one, which precedes every
  undated one; future starts non-decreasing; past starts non-increasing;
  and "Recently added" still restores a different order without dropping
  or duplicating a card.

**Done.** Landed as planned. `SORTS` is `["upcoming", "title", "added"]` with
`upcoming` the default; `upcomingBucket()` ranks a trip 0 (current or future,
by `end_date ?? start_date >= today`), 1 (past) or 2 (undated), and the
comparator orders bucket 0 by `start_date` ascending, bucket 1 descending and
bucket 2 by `created_at` descending, each tie-broken with `byTitle()` from
`web/js/sort.js` -- which also replaced the inline `Intl.Collator` the page
built for its own name sort. `"added"` became a real `created_at DESC` sort
rather than "leave the fetch order alone". `todayISO()` moved from private in
`itinerary-tab.js` to an export in `web/js/format.js`, with its
why-not-toISOString comment, and both callers import it. Both locale files
swapped `trips.sort.newest`/`trips.sort.start` for
`trips.sort.upcoming`/`trips.sort.added`.

Verified: `make ci` green, including `check_i18n.py` at 463 keys in sync.
`make test-ui GREP="trips"` green, 9 tests -- the old single sort test split
into one that asserts the default order is bucket-ordered (bucket numbers never
decrease down the list; future starts non-decreasing, past starts
non-increasing; all three buckets present in the seed) computed against a
`today` derived in the page, and one that asserts by-name and by-added neither
drop nor duplicate a card and that returning to Upcoming first restores the
opening order. Manual pass against `make dev` + `make dev-seed` at 324x756 on
2026-09-27 gave exactly the intended list: Iceland Ring Road (2026-10-04) and
New Year Crossing (2026-12-29) first, then the five past trips from
Delete Me (Cascade) (2026-09-13) down to Single Pin (2026-06-15), then
No Dates Yet -- trigger reading "Upcoming first", untinted, tinting on picking
"By name", and zero horizontal overflow.

Also written into `plans/todo.md`: the trips list's in-memory search and sort,
which `trips-page.js` has described as "a todo.md entry" since Stage 15 without
one ever existing there.

## 2. The chosen sort survives leaving the page

A `caravel.trips.sort` key in `localStorage`, following the pattern
`web/js/theme.js` already sets:

- every access wrapped in `try`/`catch` — private windows and embedded
  webviews throw, and a sort preference is not worth breaking the page
  over;
- the stored value validated against `SORTS` on read, so the now-removed
  `"newest"` and `"start"` from a browser that used the old build fall
  back to the default instead of selecting nothing;
- the default stored as the *absence* of the key, as theme.js does, so
  "never chose" and "chose the default" are one state and a future change
  of default is not silently sticky.

`renderMenu`'s `activeValue` reads from the stored value rather than from
`DEFAULT_SORT`, so the trigger opens showing the order actually in force.
Keep `neutralValue` on `DEFAULT_SORT` specifically — the accent tint
means "not the normal order", and that stays true of a remembered
non-default choice.

Add a Playwright case: choose "By name", reload, assert the trigger label
and the card order both came back.

Then add a todo.md note that the trips list now remembers its sort per
browser while the locations tab remembers its toolbar per history entry —
two different mechanisms for the same idea, which the existing "List view
state is not in the URL" entry should absorb rather than duplicate.

## Build order

1. Milestone 1 — the ordering, the rename, docs, tests.
2. Milestone 2 — persistence.

Milestone 1 stands alone: it is the behaviour change worth looking at,
and it is reviewable without the storage code. Milestone 2 is small and
purely additive.

## Workflow

The loop from `CLAUDE.md`: implement, verify with `make ci` plus a real
browser pass, add a **Done.** paragraph to the milestone's section here,
update `plans/todo.md` in both directions, one commit per milestone,
leave `make dev` running, hand back and wait.

## Verification

- `make ci` green — the build, and `scripts/check_i18n.py` proving both
  locales moved together.
- `npx playwright test tests/ui/trips.spec.js` green in both locales,
  including the new bucket-order and persistence assertions.
- Manual pass against `make dev` + `make dev-seed` at 324×756: the list
  opens on the `today+7` trip with the past trips below it and "Demo: No
  Dates Yet" last; the trigger reads "Upcoming first" and is untinted;
  picking "By name" tints it; a reload keeps the choice.
- Toolbar geometry unchanged — still one non-wrapping row at 324px,
  which the existing toolbar test covers for the longer German strings.
