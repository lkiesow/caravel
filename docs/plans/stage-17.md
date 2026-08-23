# Stage 17 — Expenses and cost-splitting

## Context

A trip costs money, and Caravel records none of it. There is no `currency`
anywhere in the schema, no money column, and nothing in `web/js` that formats
an amount — this is greenfield, which is unusual for a stage this late.

`docs/plans/todo.md` has carried the entry since Stage 01 ("Expenses /
cost-splitting", tagged **(soon)** in the Stage 15 backlog review) with a
one-line design: a new `expenses` table referencing `trip_id`, no changes to
existing tables, and a note that the *splitting* half only means anything once
several people share a trip. Stage 14 shipped exactly that — `trip_members`,
roles, and a role minimum on every trip-scoped handler through one seam — so
the second half is now buildable, and this stage builds both.

**The feature.** An Expenses tab on a trip: record what something cost, who
paid, and who it was for. Totals per trip and per person, and a balances view
answering "who owes whom". The trip carries a single currency, and amounts are
stored as integer minor units so no float ever touches money.

### Scope decisions taken before this plan was written

- **Tracking first, then balances.** Milestones 1–4 record and total expenses;
  5–6 add shares and balances. **Settlement** — "mark as paid", a payment
  between two members that drives a balance back to zero — is out of scope and
  goes to the backlog. It is the part most likely to want redesign after
  somebody has actually used the rest.
- **Per-trip currency, integer minor units.** One `currency` column on `trips`,
  and every amount an integer in that currency's minor unit. A
  foreign-currency purchase is entered as the converted amount. Per-expense
  currency, and per-expense stored conversion rates, are both out: they make
  totals and balances per-currency, which is a much busier UI for a feature
  nobody has used yet.
- **No visibility axis.** Every expense on a trip is visible to everyone on it.
  This is deliberately *unlike* files and checklists, and the reason is the
  totals: hidden rows in a shared ledger make an incorrect total look correct.
  An expense you do not want shared is one you do not record on the trip.
- **Minimal fields** — amount, title, date, payer. No category vocabulary and
  no link to a location. Both are real wants and both go to the backlog; the
  depth in this stage goes into splitting instead.
- **Postgres CI coverage is not part of this stage.** Migrations are written
  for both dialects as always, and nothing new runs against Postgres — that
  backlog entry stays open and untouched.

### What exploring the code changed about the plan

- **The backlog's "no changes to existing tables" does not survive the
  per-trip-currency decision.** `trips` gains a `currency` column, so
  `CreateTrip` and `UpdateTrip` gain a parameter and both hand-written store
  adapters change. That is the only place in this stage where an existing code
  path is modified rather than extended.
- **Checklists are the precedent to copy, minus visibility.** `checklists` is
  the most recent trip-scoped table with its own tab, CRUD routes split between
  `/trips/{tripId}/...` and `/{resource}/{id}`, and a `load*` helper in
  `internal/httpapi/authz.go`. Expenses copy that shape. What they do *not*
  copy is the visibility predicate written twice on purpose — once in
  `ListChecklistsByTrip`, once in `loadChecklist`. There is no personal
  expense, so that whole class of check is absent, and its absence is the one
  structural difference to keep in mind while reading checklists as a model.
- **The tab has to be an overflow tab, and it needs a new icon.** `TRIP_TABS`
  in `web/js/trip-tabs.js` holds seven entries, four of them primary; an eighth
  cannot go in a phone row at 324px. That file also states an invariant to
  respect — every overflow tab comes after every primary one, so the desktop
  order and the phone order agree by construction — so Expenses is inserted
  inside the overflow group, not wherever it reads best. No wallet, receipt or
  coins icon is in `scripts/gen_icon_sprite.py`'s `ICONS` list, so Milestone 3
  includes the sprite regeneration steps from CLAUDE.md.
- **`internal/db` has no test files at all.** Store behaviour is tested through
  `internal/httpapi`'s harness (`testing_test.go`), which builds a real Server
  over a real, migrated, per-test SQLite database. New tests follow that rather
  than introducing a test package under `internal/db`.
- **The currency exponent comes from `Intl`, not from a table.** JPY has no
  minor unit, so a hardcoded divide-by-100 is wrong for it.
  `Intl.NumberFormat(undefined, {style: "currency", currency})
  .resolvedOptions().minimumFractionDigits` gives the right exponent per
  currency, and the same formatter renders the amount — so one place in
  `format.js` owns both directions.
- **The trip PATCH endpoint already exists and is editor-gated**, so currency
  needs no endpoint of its own: `tripRequest` and `tripResponse` in
  `internal/httpapi/trips.go` gain a field and `handleUpdateTrip` passes it
  through.

---

## 1. Money in the schema

Migration pair `0011_add_expenses`, in **both** dialects
(`internal/db/migrations/sqlite/` and `.../postgres/`):

- `ALTER TABLE trips ADD COLUMN currency TEXT NOT NULL DEFAULT 'EUR'` — a
  constant default, so every existing row is valid and the sqlite ALTER is
  legal.
- ```sql
  CREATE TABLE expenses (
      id            TEXT PRIMARY KEY,
      trip_id       TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
      title         TEXT NOT NULL,
      amount_minor  INTEGER NOT NULL CHECK (amount_minor > 0),
      spent_on      TEXT NOT NULL,
      payer_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      created_at    TEXT NOT NULL
  );
  ```
  plus indexes on `trip_id` and on `payer_user_id`.

Three things the migration comment should carry, because each is a decision
rather than a detail:

- **`amount_minor` is an integer in the trip currency's minor unit.** Never a
  REAL. A ledger that is out by a cent is worse than one that refuses to
  exist, and floating-point money is how you get one. `CHECK (amount_minor >
  0)`: refunds and negative amounts are out of scope, and a zero-cost row is a
  note rather than an expense.
- **`spent_on` is a `YYYY-MM-DD` string**, matching `trips.start_date` and
  `itinerary_days.date`. Same reasoning as those — it is a calendar day, not
  an instant, and storing it as one avoids a timezone question nobody asked.
- **`payer_user_id` is nullable with `ON DELETE SET NULL`**, exactly as
  `files.owner_user_id` and `checklists.owner_user_id` are: deleting an account
  must not delete the trip's ledger. Unlike those two, NULL here does **not**
  fail closed. The expense stays visible and still counts toward the trip
  total, shown as unattributed. Milestone 6 says what balances do with it.

Then, in order:

1. `internal/db/sqlc/queries/expenses.sql` — `CreateExpense`,
   `GetExpenseByID`, `ListExpensesByTrip` (`ORDER BY spent_on DESC, created_at
   DESC`), `UpdateExpense` (id + trip_id), `DeleteExpense :execrows` (id +
   trip_id). Add `currency` to `CreateTrip` and `UpdateTrip` in
   `queries/trips.sql`.
2. `sqlc generate`, by hand, from `internal/db/sqlc/`. Keep every comment in
   the new file plain prose — no backticks, no double quotes, avoid
   apostrophes — per the three sqlc traps in CLAUDE.md, and **read** the
   generated file rather than only diffing it for churn.
3. `db.Expense` in `internal/db/domain.go`, the five new methods on the `Store`
   interface in `internal/db/store.go`, and adapters in both
   `sqlite_store.go` and `postgres_store.go`.

**Verification.** `make ci`, plus a Go test that creates an expense and reads
it back asserting **every field**. Stage 14 twice had a hand-written adapter
silently drop a field on read, in both dialects, which a compile cannot see and
only a test that names each field will catch. Also assert the trip-delete
cascade removes expenses, and that `UpdateTrip` round-trips `currency`.

**Done.** Landed as planned, with three things worth recording.

*`sqlc` needed a CAST to type the total.* `SELECT COALESCE(SUM(amount_minor),
0)` generated `SumExpensesByTrip` returning **`interface{}`** in *both*
dialects — it compiles, and would have needed a type assertion at every call
site whose correct form differs per dialect. Wrapping it as `CAST(... AS
BIGINT)` yields `int64` in both. This is exactly the failure CLAUDE.md's "read
the generated file rather than only diffing it" rule exists for: nothing about
the query looked wrong, and the build was green either way. The query comment
now says why the CAST is there, so it does not get tidied away.

*`amount_minor` is `BIGINT` in the postgres dialect, `INTEGER` in sqlite.*
sqlite's INTEGER is already 64-bit, but postgres INTEGER is 32-bit and sqlc
maps it to `int32`, which would have made the domain type's `int64` a
per-dialect conversion for no reason.

*Every `CreateTrip`/`UpdateTrip` call site had to be found by hand.* `Currency`
is a plain `string`, so an omitting caller compiles and silently writes `""`
into a NOT NULL column. Three sites needed it: `handleCreateTrip` (takes
`db.DefaultCurrency` — the create form has no control for it yet),
`handleUpdateTrip` (carries over `trip.Currency`, since `UpdateTrip` writes
every column it names and would otherwise blank it on every rename), and
`cmd/seed`.

**Verified.** `make ci` green. New `internal/httpapi/expense_store_test.go`
(store-level, in that package because `internal/db` has no test files and the
harness there already builds a real migrated database): a field-by-field round
trip through `GetExpenseByID` rather than the insert's own `RETURNING`; a nil
payer surviving as nil; `DeleteUser` on the payer leaving the row intact with a
nil payer; list ordering and `SumExpensesByTrip` (including 0 for an empty
trip); `UpdateExpense`/`DeleteExpense` refusing the right id under the wrong
trip; the trip-delete cascade; `currency` round-tripping through create, get,
**`ListTripsForUser`** (which builds its `Trip` inline, so it is a second place
the field can be dropped) and update; and `ValidCurrency`. Beyond the tests,
the migration was applied to the real populated dev database via `make
dev-restart` — schema version 11, clean, all seven existing trips backfilled to
EUR — and the down migration was run against a `.backup` copy, dropping the
table and the column with the seven trips intact.

## 2. The expenses API

Routes, mirroring the checklists split in `internal/httpapi/router.go`:

| Route | Min role |
|---|---|
| `GET /api/trips/{tripId}/expenses` | viewer |
| `POST /api/trips/{tripId}/expenses` | editor |
| `PATCH /api/expenses/{expenseId}` | editor |
| `DELETE /api/expenses/{expenseId}` | editor |

- `loadExpense` joins the `load*` family in `authz.go`, resolving
  `{expenseId}` and authorizing against its trip. Nothing follows the
  authorization, which is the whole difference from `loadChecklist`.
- The list response carries `currency` and `total_minor` alongside the rows, so
  the client never sums a list it may be showing only part of and never has to
  guess the currency.
- **Amounts cross the wire as minor units**, never as a formatted string.
  Formatting is the client's job — the `format.js` precedent.
- **Currency is validated against an allowlist**: a `db.Currencies` slice (EUR,
  USD, GBP, CHF, SEK, NOK, DKK, PLN, CZK, ISK, JPY, CAD, AUD), the same shape
  as `config.SearchProviders`. An unknown code is a 400 at the trip PATCH
  rather than a rendering surprise three screens later.
- **The payer must hold a role on the trip.** The id arrives in a request body
  with no route param to authorize it, which is precisely the situation
  `requireSameTrip`'s comment describes; this gets a sibling helper that
  resolves the named user's role through `tripRole` and answers 400 otherwise.
  An absent payer defaults to the caller.
- `currency` joins `tripRequest`/`tripResponse` and `handleUpdateTrip`.

**Verification.** `internal/httpapi/expenses_test.go`: the role matrix (a
viewer gets 403 on each write, a stranger 404 on all four), a PATCH against an
expense in another trip → 404, amount validation (zero, negative, missing), an
unknown currency → 400, a payer who is not on the trip → 400, an absent payer
defaulting to the caller, and `total_minor` matching the rows it was computed
from.

**Done.** Landed as planned, with one design reversal and two additions.

*The planned "absent payer defaults to the caller" is gone; the server never
fills the payer in.* A test written straight from the plan failed and was right
to: Go cannot distinguish an absent `*string` from an explicit `null`, so
"absent means me" also means "`null` means me" — and there would then be no way
to record an expense somebody outside the trip paid for. The default was also
outright wrong on update, where a PATCH replaces every field it names: editing
a title would have reassigned somebody else's expense to whoever edited it. A
rule that has to differ per verb to stay safe is the wrong rule, so defaulting
to yourself moves to the client, where the form has a visible default. Absent
or null now means unattributed in both verbs, with no asymmetry to document.

*The response carries `payer_display_name`.* Planned as `payer_user_id` only,
on the assumption the client would resolve names from the members list. It
cannot: somebody who paid and then left the trip is no longer a member, so a
client-side lookup renders a blank name for a payer who is perfectly well
recorded. Resolved server-side through a small per-response cache, so a list of
twenty expenses paid by two people costs two lookups. There is a test for the
paid-then-left case specifically.

*The authorization matrix was extended, not duplicated.* `roles_test.go`
already tables every trip-scoped route against all four kinds of caller, so the
four expense routes became four rows there (plus an expense in its fixture and
its title in the leak list), and the requires-a-session list in
`ownership_test.go` gained the list route. Writing a private matrix in
`expenses_test.go` would have been a second, weaker copy of the same policy.

Also landed: `currency` on `tripRequest`/`tripResponse`, optional in both verbs,
where absent means the shipped default on create and *the trip's current value*
on update — not the default, or renaming a trip priced in yen would reset it to
euros. `handleListTrips` builds its response inline and needed the field
separately; that is the second inline site Milestone 1's test flagged, and it
would otherwise have shipped an empty currency in the trips list.

**Verified.** `make ci` green. `expenses_test.go` covers the seven validation
messages (each asserted by message, since a form that says "amount is required"
when the date is missing sends the user to the wrong field), nothing being
written by a refusal, the payer being exactly what the request said, the
membership check refusing a stranger and then accepting the same body once they
are a member, an explicit null payer still counting toward the total, the total
agreeing with the rows it was computed from, list order, a cross-trip PATCH and
DELETE answering 404 with the row left intact, a viewer reading the whole
ledger by content rather than by status, and the currency lifecycle including
`XYZ`/`eur`/`""` refused. Beyond the tests: every endpoint was exercised
against the running dev server on a throwaway JPY trip — the zero-exponent case
the client will have to format — confirming 201/200/400 bodies by hand, then the
trip was deleted and the seeded scenarios left at their original seven. The
Playwright suite was run as the regression net for the widened trip response
and the new routes: 91 passed, 3 skipped (assist, unconfigured locally).

## 3. The Expenses tab

- `{ key: "expenses", icon: <new>, overflow: true }` added to `TRIP_TABS`
  inside the overflow group, the route added to `app.js`'s table, and
  `trip.tabs.expenses` added to **both** locale files.
- The new icon: extend `ICONS` in `scripts/gen_icon_sprite.py`, run the two
  commands from CLAUDE.md, and **diff the sprite** — every existing symbol must
  come out byte-identical, or an upstream Lucide revision has quietly
  restyled icons already in use.
- `web/js/pages/expenses-tab.js`: the trip total at the top, the rows grouped
  by date, then an add form. Edit and delete per row through the action-menu
  idiom in `components/checklist-list.js`; delete behind `confirmDialog`.
- `formatMoney(amountMinor, currency)` in `web/js/format.js`, plus the inverse
  for parsing typed input, both driven by the `Intl` exponent above. That is
  the only place money becomes, or stops being, a string.
- Read-only for viewers: no add form and no row actions, through the
  `trip-role.js` + `canEdit` pattern `settings-tab.js` already uses.
- The currency `<select>` goes into `components/trip-form.js`, so it appears
  both when creating a trip and in the Settings tab that embeds the same form.

**Verification.** A manual pass at 324×756 against `make dev` per the repo's
mobile convention, with assertions rather than screenshots: the tab appears in
the More menu at that width, a created expense renders with the correctly
formatted amount, the total matches, and a viewer session sees no controls.
`make ci` covers i18n parity.

**Done.** Landed as planned. Four things worth recording.

*The amount example in the error message had to become currency-aware.* The
first version read "Enter an amount, for example 12.50." — which a JPY trip
showed after refusing exactly that, since yen has no minor unit. Found by
typing it. The message now takes an `{example}` derived from the currency
(`moneyExample` in `format.js`), so a yen trip is told "for example 1200".

*Parsing is done on the string, not through `parseFloat`.* `parseFloat("12.55")
* 100` is `1254.9999999999998`; `Math.round` covers that one case, but the class
of bug has no business near money. `parseMoney` pads the fraction and
concatenates, which is exact by construction, and refuses more decimals than
the currency has rather than silently rounding — "12.567" EUR is not 12.57 with
any confidence. It accepts a comma as well as a dot, because a German-speaking
user types the separator their keyboard gives them.

*The currency `<select>` joined the existing input rule in `base.css` rather
than adding a fourth copy.* The backlog's "three near-identical input rules"
entry stays open, but this milestone did not widen it — `.trip-form select` was
added to the rule that already styles `.members-add select`.

*`menu.spec.js` failed, correctly, and needed updating.* Both its `TAB_ORDER`
and `OVERFLOW_LABELS` spell the tab labels out rather than importing them from
`trip-tabs.js` — deliberately, per that file's own comment, since an imported
list cannot disagree with the source. An eighth tab is exactly the change those
lists exist to catch, so both gained "Expenses"/"Ausgaben" in the overflow
group. Its "checklists before files" assertion still holds.

Deferred, and now in the backlog: `Intl` is called with an undefined locale
throughout `format.js`, so with the app in German a total still renders as
"€97.55" rather than "97,55 €". That is pre-existing and deliberate (dates do
the same), but money makes it louder.

**Verified.** `make ci` green; `make test-ui` green after the `menu.spec.js`
update. Driven by hand in Firefox against `make dev` at **324×756** and at
1280×800, in both locales, with assertions rather than screenshots:

- At 324px the row still holds the four primary tabs, the page does not scroll
  horizontally, and the More menu lists Files → **Expenses** → Members →
  Settings. At 1280px all eight show in the row in the same relative order, and
  the More slot computes to `display: none`.
- On a **JPY** trip: total renders `JP¥0`, the amount placeholder is `0` not
  `0.00`, the label reads "Amount (JPY)", and `12.50` is refused with the
  currency-appropriate example. On a **EUR** trip: `€85.05` for
  45.00 + 32.55 + 7.50, grouped newest-day-first with two rows sharing a day.
- A created expense really carries a payer (`payer_display_name: "Demo User"`)
  even though the server has no default — the client's explicit default works.
  Editing preserves the existing payer rather than reassigning it to whoever
  edited, and the JPY amount round-trips into the form as `1200`, not `12.00`.
- Edit mode: heading switches, fields prefill, Cancel appears, and the row
  being edited takes the accent border (`rgb(37, 99, 235)`). Saving returns the
  form to add mode and the total updates.
- Delete goes through `confirmDialog` and drops the total by exactly the row's
  amount.
- German: `12,50` parses to `1250`, and a 58-character German title truncates
  with an ellipsis while the amount stays fully visible — the intended priority,
  since an expense with an unreadable number records nothing.
- **Read-only from a real viewer session** (the seeded `other` account added as
  a viewer, not by trusting the flag): sees all five rows and the total, has no
  form and no row menus, and a `POST` attempted from the console is refused
  **403** — so hiding the controls is a courtesy, not the boundary.
- Zero console errors or warnings throughout. Both scratch trips were deleted
  afterwards; the seeded scenarios are back at their original seven.

## 4. Who paid, and per-person totals

- A "paid by" select on the add and edit forms, listing the trip's members
  (`GET /trips/{tripId}/members` exists and needs only viewer), defaulting to
  the current user.
- The payer shown per row, and a per-person "paid" summary under the trip
  total. Both collapse to nothing worth showing on a solo trip, so the summary
  renders only when the trip has more than one member rather than growing a
  table with one row in it.
- An unattributed row — NULL payer, from a deleted account — renders with an
  explicit label rather than a blank cell.

**Verification.** A Go test asserting the payer round-trips and that changing it
re-checks membership, plus a manual pass on a two-member trip driving the
seeded `other` account.

**Done.** Landed with one deliberate departure from the plan.

*The per-person totals are computed server-side, not in the client.* The plan
implied a client-side summary; this is plain integer addition, so summing in JS
would have been exact and easy. It is on the server anyway because **Milestone
6's balances are this same grouping with a division on top** — two places
deciding what somebody paid is precisely how a ledger and a balance come to
disagree. `GET .../expenses` now carries a `payers` array
(`payerTotals` in `internal/httpapi/expenses.go`), reusing the payer-name cache
the rows already populated, so the extra rows cost no extra lookups.

Two details in that grouping worth keeping:

- **The order is deterministic** — most paid first, then by name, unattributed
  last. Without the tie-break the summary reshuffles between reloads whenever
  two people have paid the same amount, and `TestPerPersonTotalsAreStableOnATie`
  pins it with names whose insertion order and alphabetical order disagree, so
  it cannot pass on the wrong rule.
- **Somebody who has paid nothing is absent.** The section answers "who paid",
  and a row of zero answers nothing. They become interesting in Milestone 6,
  where paying nothing is exactly what puts them in debt.

*All three payer affordances are gated on `shared`, not just the select.* On a
solo trip the select would hold one option, the line under every row would
repeat the same name, and the summary would be a one-row table restating the
total. `trip-detail-page.js` passes `isShared(trip)` — the same flag files and
checklists use for visibility, here meaning "is who paid a real question".
Importantly the *data* is unaffected: a solo trip still records you as the
payer, so a trip that later gains a member has correct history rather than a
pile of unattributed rows.

The row shows the payer as a second line under the title rather than a fourth
column: at 324px the amount and the ⋮ already take fixed width, and a name is
the piece that would have had to truncate to nothing.

**Verified.** `make ci` and `make test-ui` green. Two new Go tests cover the
grouping — two expenses for one person summing rather than listing twice, two
unattributed ones collapsing into a single row, the rows summing to
`total_minor` (a grouping that drops one is a summary that quietly disagrees
with the ledger above it), a paid-nothing member being absent, and the tie
ordering. By hand in Firefox at 324×756, on a two-member trip driving the
seeded `other` account:

- The summary reads Other User €60.00 / Demo User €50.00 / *Nobody on the trip*
  €3.00 against a €113.00 total, the unattributed row rendering in italic
  because it is a fact rather than a person. No horizontal overflow, and every
  row stays inside the viewport with the title stacked over the payer line.
- The select defaults to "Demo User (you)", and **resets to you after an add**
  rather than keeping the last person picked. Creating with Other User selected
  attributes it to them and regroups the summary (Other User → €120.40).
- Editing an expense paid by somebody else **preselects them, not the editor**,
  and changing only the title leaves the payer and amount untouched. This is the
  case Milestone 2's payer rule exists to protect.
- On a solo trip: no summary, no payer lines, no select — but a newly added
  expense is still attributed to Demo User.
- As a **read-only viewer** (`other`, demoted for the check): sees the German
  summary ("Wer bezahlt hat", "Niemand auf der Reise") and per-row "Bezahlt von
  …", with no form and no row menus.
- `60,40` typed with a comma parsed to `6040`. Zero console errors or warnings.
  Both scratch trips deleted; the seeded scenarios are back at seven.

## 5. Shares: who an expense was for

Migration pair `0012_add_expense_shares`, both dialects:

```sql
CREATE TABLE expense_shares (
    expense_id TEXT NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (expense_id, user_id)
);
```

- **No per-row amount.** The split is equal among the rows present and computed
  at read time. Storing a share list *and* per-share amounts is two sources of
  truth for one number, and they drift the first time somebody is added.
- **The remainder is distributed deterministically.** 1000 across three people
  is 334/333/333, the extra units going to the lowest `user_id`s, so the shares
  always sum to exactly the expense. A split that loses a unit is the bug the
  whole integer-minor-unit decision exists to prevent, and it is not allowed
  back in through the division.
- **No shares means everyone on the trip**, resolved at read time. That keeps
  the common case free of writes and means adding a member does not require
  rewriting history — with the consequence, which the comment should say out
  loud, that a new member's share of past expenses appears retroactively. An
  explicit share list is how you pin an expense to a subset.
- Shares are set as a whole list on the expense — a `share_user_ids` field on
  the create and update bodies, replacing the set inside `WithTx` — rather than
  through per-share endpoints.
- UI: a member checkbox group on the expense form, hidden entirely on a
  single-member trip.

**Verification.** Go tests for the remainder distribution (the 1000-across-3
case, and a zero-exponent currency), a share naming a non-member → 400, the
empty-list-means-everyone rule, and the cascade on expense delete.

## 6. Balances

- A balances section in the tab: net per person — what they paid minus what
  they owe — then a suggested set of transfers, greedy largest-creditor against
  largest-debtor, with deterministic ordering so the same ledger always
  produces the same advice.
- **Computed server-side**, in the `GET .../expenses` response or a sibling
  `GET .../expenses/balances`, so exactly one implementation answers it. A
  balance recomputed in JS is a second implementation of the rounding rule, and
  the two will disagree eventually.
- **Unattributed rows are called out, not absorbed.** An expense with a NULL
  payer cannot be attributed to anyone, so it is excluded from the balance and
  the section says so plainly. Splitting it silently would produce a
  confidently wrong number, which is worse than an incomplete one.
- Renders only when the trip has more than one member.

**Verification.** Go tests: a symmetric two-person case, a three-person case
with a remainder, a case where a subset share list excludes the payer, and one
containing an unattributed expense — asserting it is reported rather than
folded in.

## 7. Coverage, seed and documentation

- **Expenses in a seed scenario.** Added to an existing multi-member scenario
  in `cmd/seed/main.go`. Safe to extend: the seeded-data hazard the backlog
  records is `map.spec.js` asserting an exact *location* count, and expenses
  add no locations.
- **`tests/ui/expenses.spec.js`**, following the `files.spec.js` /
  `assist.spec.js` shape — its own trip created in `beforeEach`, mutated, and
  deleted in `afterEach` — covering: add an expense, the total updating, edit,
  delete, and a viewer seeing no controls. This is one of the mutating flows
  the backlog wants covered, so it counts against that entry too.
- **README** gains a short Expenses section. No new environment variables.
- **`docs/plans/todo.md`**: delete the expenses entry, and add what this stage
  deferred — settlement payments, expense categories, linking an expense to a
  location, per-expense currency, refunds and negative amounts, and a
  trip-level total on the trip card.

---

## Build order

1 → 2 → 3 → 4 → 5 → 6 → 7, strictly: the schema precedes its API, which
precedes its UI. Milestones 1–4 are a complete and useful expense tracker on
their own, so 4 is the natural stopping point if the stage runs long.

## Workflow

Per CLAUDE.md, for each milestone in order: **implement**; **verify** (`make
ci` green, plus a real behaviour check — assertions over screenshots); **update
this file** with a "**Done.**" paragraph for that milestone, and
`docs/plans/todo.md` in both directions; **commit** (one per milestone, a
follow-up fix getting its own "... follow-up: ..." commit); leave `make dev`
running; then **stop and hand back control** and wait before starting the next
milestone.

## Verification (whole stage)

- `make ci` green at every commit — build, vet, JS syntax, i18n parity, `go
  test`.
- `make test-ui` green, including the new `expenses.spec.js`.
- A manual pass against `make dev` at 324×756 and at desktop width, in **both**
  locales. German is the longer copy and the Expenses tab adds a number
  column, which makes it the layout most likely to overflow.
- A two-member trip driven through the seeded `demo` and `other` accounts: both
  see the same total, the balance is symmetric, and a viewer can read the
  ledger without being able to change it.
