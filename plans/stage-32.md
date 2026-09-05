# Stage 32 — More than one currency: rates, and what a total means

## Context

A trip has exactly one currency today, and the schema says so on purpose
— [0001_init.up.sql:70-78](internal/db/migrations/sqlite/0001_init.up.sql#L70-L78):

> One currency per trip, not per expense. A trip is normally spent in one
> currency, and making it per-expense makes every total and every balance
> per-currency too -- so the common case would pay for the rare one. A
> purchase in another currency is entered as the converted amount.

That reasoning still holds for *totals*, and this stage does not overturn
it. What it overturns is the last sentence. Converting by hand before
typing is the part that does not survive contact with a real trip: the
receipt says ¥1,200, the ledger says €7.60, and a month later nobody can
reconcile the two. A trip through Japan on a Euro budget wants both
numbers on the row.

So: the trip keeps **one main currency**, and every total, every share
and every balance stays denominated in it. A trip may additionally
configure **extra currencies, each with one exchange rate into the main
currency**. An expense may be recorded in any of them; it is *stored* in
what was paid, and *counted* in the main currency.

Decisions taken up front:

- **One live rate per currency, not a snapshot per expense.** Editing a
  rate re-converts every expense in that currency. One source of truth,
  no per-expense rate column, no historical-rate UI. A trip's rate is
  "the rate we're using", not a market record.
- **The rate is a minor-unit → minor-unit factor, stored as an integer
  in parts per billion.** The server deliberately knows nothing about
  decimal exponents — `formatMoney` asks `Intl`, and
  [format.js:52-54](web/js/format.js#L52-L54) says why. A human-readable
  "1 JPY = 0.0058 EUR" would force an exponent table into Go, a third
  hand-maintained copy of what the platform already knows. Instead the
  browser folds the exponents in: 1 yen (exponent 0) → 0.58 cents
  (exponent 2) → `rate_ppb = 580_000_000`. The settings form converts
  back the same way for display. The server multiplies integers and
  never asks what a decimal place is.
- **A currency in use cannot be removed.** Refused with a message naming
  how many expenses hold it, rather than silently orphaning amounts or
  freezing a last-known rate somewhere.
- **The rate editor lives in the trip settings tab only**, not in the
  create form — rates are rarely known before the trip exists.
- **Rows read original-first**: `¥1,200 (≈ €7.60)`. What was paid stays
  primary; the converted figure is visibly an approximation.
- **Everything else is out of scope**, deliberately: no rate lookup
  service, no negative amounts, no per-currency subtotals, no export
  changes, no touching the browser-locale `Intl` question already parked
  in [todo.md:315-337](plans/todo.md#L315-L337).

---

## Milestone 1 — The schema and the store

**Migration.** `internal/db/migrations/{sqlite,postgres}/0009_trip_currencies.{up,down}.sql`
(0008 is the current head). Two changes:

```sql
CREATE TABLE trip_currencies (
    trip_id  TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    code     TEXT NOT NULL,
    rate_ppb INTEGER NOT NULL CHECK (rate_ppb > 0),
    created_at TEXT NOT NULL,
    PRIMARY KEY (trip_id, code)
);

ALTER TABLE expenses ADD COLUMN currency TEXT;
```

`expenses.currency` is **nullable, and NULL means the trip's main
currency** — which is exactly today's semantics, so there is no backfill
and no behaviour change for any existing row. A non-NULL value is always
an additional currency configured on that trip.

`rate_ppb` is documented in the migration in the terms above: minor unit
of `code` → minor unit of the trip's main currency, ×10⁹. Parts per
*billion* rather than million so a weak currency added to
`db.Currencies` later still has resolution to spare.

Postgres twin: `BIGINT` for `rate_ppb`, `TIMESTAMPTZ` for `created_at`,
matching `0001_init`'s dialect conventions rather than copying the SQLite
text. `scripts/check_migrations.py` enforces the pairing in `make ci`.
No `CHECK` on `code`: same reasoning as `trips.currency` — the allowlist
lives in `db.Currencies`.

**Queries.** New `internal/db/sqlc/queries/trip_currencies.sql`:
`ListTripCurrencies`, `UpsertTripCurrency` (`ON CONFLICT ... DO UPDATE SET
rate_ppb = excluded.rate_ppb` — named args are *not* substituted inside
`DO UPDATE`, per `CLAUDE.md`), `DeleteTripCurrenciesNotIn`, and
`CountExpensesByCurrency` (`GROUP BY currency` over one trip, for the
in-use guard). `expenses.sql` gains `currency` in `CreateExpense` and
`UpdateExpense`; the `SELECT *` queries pick it up for free.

Keep all comment prose in these `.sql` files plain — no backticks, no
double quotes, no apostrophes. That trap has cost this project time
twice. Run `sqlc generate` by hand from `internal/db/sqlc/` for **both**
dialects and read the generated files, not just the diff.

**Domain and store.** `internal/db/domain.go` gains
`type TripCurrency struct { TripID, Code string; RatePPB int64; CreatedAt time.Time }`
and `Expense.Currency *string`, both with doc comments carrying the
reasoning above. `internal/db/store.go` gains the four store methods,
implemented in `sqlite_store.go` and `postgres_store.go`;
`Create/UpdateExpenseParams` gain `Currency *string`.

**Verify.** New `internal/httpapi/trip_currency_store_test.go` in the
existing store-test style: round-trip, upsert overwrites the rate,
cascade on trip delete, expense currency round-trips as NULL and as a
code. Plus `make test-postgres` — this milestone is squarely in
`internal/db`, which is the case CLAUDE.md says to run it for.

**Done.** Landed as planned, with one deliberate deviation in the
queries. The plan called for `UpsertTripCurrency` plus
`DeleteTripCurrenciesNotIn`; what landed is `CreateTripCurrency` plus
`DeleteTripCurrenciesByTrip` — delete-all-then-reinsert, the same
wholesale-replace shape `DeleteExpenseSharesByExpense` already uses two
queries further up the same file. The reason is `sqlc.slice()`, which a
`NOT IN` would have needed and whose SQLite support is not something to
bet a dialect on. The replace is a transaction either way, so the
concurrency property the plan wanted is unchanged.

Otherwise as written: `0009_trip_currencies.{up,down}.sql` in both
dialects (`BIGINT`/`TIMESTAMPTZ` in the Postgres twin), the new
`trip_currencies` table, `expenses.currency` nullable with no backfill,
`db.TripCurrency` + `db.RateOne` + `Expense.Currency` in
[domain.go](internal/db/domain.go), four store methods on the interface
and both implementations, and `Currency *string` on the two expense
param structs.

Verified: `make ci` green (`check_migrations.py` reports 9 per dialect,
both agree, chain intact) and **`make test-postgres` green** — which is
the run that mattered here, because `sqlc.narg(currency)` is exactly the
construct Stage 18 Milestone 3 found Postgres refusing without a `CAST`.
It does not need one in `INSERT`/`SET` position, where the column type
is known, and the 8 new tests passing on that dialect is the evidence.
The generated files were read rather than diffed: all params substituted
(`?1`/`$1`), `sql.NullString` for the nullable column, no `interface{}`
from the `COUNT` thanks to its `CAST`.

New: `internal/httpapi/trip_currency_store_test.go`, 8 tests —
round-trip naming every field, wholesale replace and the `ORDER BY code`
promise, cascade on trip delete, expense currency round-tripping as
**both** NULL and a code through create/get/list, an update moving a row
into a currency and back out, and `CountExpensesByCurrency` proving the
NULL rows are excluded.

One environment note for whoever runs this next: `sqlc` was not
installed on this machine, and `GOPATH` here is `/home/lars/dev/go`, so
the binary lands at `/home/lars/dev/go/bin/sqlc` rather than
`~/go/bin/sqlc`. Installed v1.31.1 to match the version stamped in the
generated files — regenerating with a different one would rewrite every
file in both `gen/` directories.

---

## Milestone 2 — Configuring currencies over the API

**Read.** `tripResponse` in [trips.go](internal/httpapi/trips.go) gains

```json
"currencies": [{ "code": "JPY", "rate_ppb": 580000000 }]
```

Always an array, empty for a single-currency trip. On the trip rather
than behind its own GET: both the settings tab and the expenses tab
already load the trip, and neither should need a second request to know
whether to show a picker at all.

**Write.** A new route beside the existing trip routes in
[router.go:371](internal/httpapi/router.go#L371):
`PUT /trips/{tripId}/currencies`, body `{"currencies": [...]}`,
**replace-all** semantics — it mirrors a form with repeatable rows and
one Save button, and makes "remove this row" need no second verb. Same
role requirement as `PATCH /trips/{tripId}` (read it off the router
rather than assuming); added to the `roles_test.go` and
`ownership_test.go` tables, which every new route must appear in.

Validation, each with its own message:

- every `code` passes `db.ValidCurrency`;
- no `code` equals the trip's main currency;
- no duplicate codes in the body;
- `rate_ppb > 0`;
- **any code being removed that expenses still hold** → `409`, message
  naming the code and the count, from `CountExpensesByCurrency`.

**Main-currency collision.** `handleUpdateTrip` must also refuse a
`PATCH` that sets the main currency to one already configured as an
additional currency — otherwise a trip ends up with a currency that
converts to itself at a rate nobody chose.

**Verify.** `internal/httpapi/trip_currencies_test.go`: the round-trip,
each validation message, the in-use refusal (and that it *succeeds* once
the expense is deleted), the main-currency collision, and cascade.

**Done.** Landed as planned, plus one route the plan did not name.
Alongside `PUT /trips/{tripId}/currencies` there is now a
`GET /trips/{tripId}/currencies` (viewer). The plan reasoned that the
trip response makes a separate GET unnecessary, and for the client that
is still true — nothing fetches it. It exists because the PUT needed a
read-back path in its own tests that did not go through the whole trip
response, and a write-only sub-resource is a strange thing to leave in a
router. Cheap, symmetric, and in both authz tables.

The trip response field is `currencies`, **`omitempty`** rather than
always-present. A trip with no additional currencies omits it entirely,
which is what tells the client not to offer a picker — and it is also
what keeps the trips *list* honest, since that endpoint builds its rows
inline and never loads rates. Absent and empty mean the same thing to a
reader, so nothing is lost. The cost is one extra `ListTripCurrencies`
per single-trip response; it is shrugged off on error like the member
count beside it, which is safe here only because no amount is converted
from this field — the expenses endpoint does its own load in Milestone 3
and fails loudly instead.

Validation landed as five refusals, each with a message naming the
actual problem, since they are rendered next to the row that caused
them: unsupported code, duplicate code, non-positive rate, implausible
rate (a new `maxRatePPB`, a factor of one million — far past any real
pair, there so a typo cannot store a number that makes every total
meaningless), and the trip's own main currency. The in-use guard answers
`409` naming the code and the count. `PATCH /trips/{id}` adopting a
configured currency as its main one is also `409`, and costs no query
unless the request actually names a different currency.

Verified: `make ci` green. New `trip_currencies_test.go`, 5 tests
(7 subtests) — the round trip including the `ORDER BY code` promise and
the omitted-when-empty contract, wholesale replace including clearing to
empty, every refusal *and* that a refused PUT wrote nothing, the in-use
guard in all three directions (refused, re-rating still allowed, allowed
once the expense is gone), and the main-currency collision including
that the field stays writable to an unconfigured code. Both authz tables
gained the two routes.

Then end to end against the running dev server on seeded data, which is
what actually proves the wiring: configured JPY+USD and watched them
come back ordered by code on the trip itself; drew all five refusals
with their real messages; inserted a JPY expense directly into
`data/caravel.db` and watched the removal turn into
`JPY cannot be removed: 1 expense(s) are recorded in it` while
re-rating the same currency still returned 200; deleted the row and
watched the removal succeed. `make dev-restart MARKER=handleSetTripCurrencies`
first, so this was demonstrably the new binary and not a stale one.

`make test-postgres` deliberately **not** re-run here: this milestone
adds no SQL, only handler logic over the queries Milestone 1 already
proved on both dialects. Milestone 3 changes the arithmetic and gets it
again.

The one thing the plan assumed and got wrong: the in-use guard's test
could not create its foreign expense through the API, because
`expenseRequest` did not accept `currency` until Milestone 3 and
`readJSON` refuses unknown fields. It wrote the row through the store
instead. *(Milestone 3 gave the API that field and the test now goes
through it, as it should.)*

---

## Milestone 3 — Expenses in a foreign currency, and what the totals mean

This is the milestone that changes arithmetic, and it is the one to
review most carefully.

**Request.** `expenseRequest` gains `Currency *string`. Absent or `null`
means the main currency and stores NULL. A value must be one of the
trip's configured additional currencies — the main currency sent
explicitly is accepted and normalised to NULL, so a client that always
sends the field is not punished for it.

**Conversion.** One new file, `internal/httpapi/expense_convert.go`,
holding one function:

```go
func convertMinor(amountMinor int64, ratePPB int64) int64
```

`amountMinor × ratePPB / 1e9`, rounded half away from zero, computed
through `math/big` rather than `int64` — `1e15 × 1e9` overflows an
`int64` and a ledger is not the place to find that out. Clamped to a
minimum of 1, so a real expense never converts to nothing. Rate 1e9 (the
main currency) returns the input unchanged, by construction.

**Where it applies.** Conversion happens **once, per expense, before
anything else** — the converted integer is then what `splitAmount`,
`payerTotals` and `computeBalances` all consume. That ordering is not
incidental: it is what preserves the property
`expense_split_test.go` exists to protect, that shares sum to exactly
the amount. Converting after splitting would break it.

**The total moves from SQL into Go.** `SumExpensesByTrip` cannot sum
mixed currencies, and pushing the multiply into SQL would put a second
rounding rule in a second language. The handler sums the converted rows
it has already built. The envelope comment at
[expenses.go:84-88](internal/httpapi/expenses.go#L84-L88) justifies the
DB sum by "a client showing part of the list" — that still holds, since
`ListExpensesByTrip` is unpaginated and the server sums the full set;
the comment gets rewritten to say so. The now-unused query and its
generated code are deleted **by hand in both dialect packages** —
`sqlc generate` never deletes, and CLAUDE.md records that exact trap.

**Response.** `expenseResponse` gains `currency` (always populated with
the *effective* code — the main currency where the column is NULL, so
the client never re-implements the rule) and `converted_minor` (equal to
`amount_minor` for main-currency rows). `share_minor` is already in the
main currency and stays as it is. `total_minor`, `payers[].paid_minor`
and every figure under `balances` are main-currency throughout, as they
are today — the point of the whole design is that this is not a new
question the client has to ask.

**Verify.** New cases in `expenses_test.go`: a JPY expense on a EUR trip
comes back with both amounts and the right conversion; the total of one
EUR and one JPY expense is the converted sum; a balance between two
people with expenses in different currencies settles in EUR; an unknown
or unconfigured currency is a 400; editing the rate changes the total on
the next read. Unit tests for `convertMinor` in
`expense_convert_test.go`: rounding at the half, the identity rate, the
clamp, and an amount large enough that `int64` arithmetic would have
overflowed.

**Done.** Landed as planned. The shape that carries it is one the plan
did not spell out: `convertedExpenses` returns a **copy** of the ledger
with `AmountMinor` restated in the main currency, and `payerTotals` and
`computeBalances` are then handed that copy unchanged. Neither of them
learned what a currency is, and there is exactly one place in the
codebase where a rate is applied. Conversion happens once per expense
before anything is split, totalled or balanced — which is what preserves
`expense_split_test.go`'s property, and there is now a test asserting
that directly across a grid of amounts and rates.

`convertMinor` goes through `math/big` as planned: 1e15 minor units
against a rate near 1e9 is 1e24 and `int64` stops at 9.2e18. Rounds half
away from zero **in both directions** — amounts are positive today, but a
rounding helper that is only right for positive input is a trap for
whoever lifts that `CHECK` — and never returns less than 1, so a recorded
expense cannot round away to nothing. The identity rate short-circuits
and returns its input untouched, which is what keeps every
single-currency trip bit-for-bit what it was.

`SumExpensesByTrip` is gone from the queries, both `gen/` packages, the
store interface and both implementations. The hand-deletion trap in
`CLAUDE.md` did **not** apply: it bites when a whole `queries/*.sql` file
is removed, and `expenses.sql` still exists, so regeneration rewrote it
minus the query. Verified by grep rather than assumed. Its store-level
test lost the sum half and was renamed `TestListExpensesByTrip`, with a
comment saying where that coverage went; the ordering half stays, since
that is still the query's own promise.

An expense in a currency the trip has no rate for is a hard error, not a
1:1 fallback — repricing it silently would report a confidently wrong
total. Unreachable through the API (Milestone 2's guard), reachable by
hand-editing the database, which is exactly when you want it loud.

Verified: `make ci` green and **`make test-postgres` green**, the second
run the plan asked for. 17 new tests. `expense_convert_test.go` covers
the arithmetic — the worked JPY example, the identity rate, four
rounding cases at and around the half, the clamp, symmetry across zero,
an amount whose product overflows `int64`, the shares-sum-to-the-whole
property, and the unrated-currency refusal. `expenses_test.go` covers the
wiring: a foreign expense reporting both figures, the main currency named
explicitly and normalised away, totals *and* per-payer rows *and* nets
built from converted amounts (including that the nets still sum to zero,
the property conversion could most plausibly have broken), a live
re-rating moving the total while what was paid stays put, three refusals,
editing an expense between currencies in both directions, and a
single-currency trip proven unchanged — including the empty-trip zero
that used to be the `COALESCE`.

Then end to end against the dev server, after
`make dev-restart MARKER=convertedExpenses`: ¥12,000 at 0.0058 came back
as 6960 EUR minor — the plan's own worked example, €69.60. Alongside a
€45.00 expense the total read 11460; re-rating the yen to 0.0070 moved
that row to 8400 and the total to 12900 while the euro row did not budge;
`USD` was refused with *"USD is not one of this trip's currencies"*. The
probe rows were then deleted and the currencies cleared, so the seeded
scenarios `make test-ui` depends on are back at baseline.

Surfaced and deferred to `plans/todo.md`: changing a trip's *main*
currency silently reinterprets every configured rate, because a rate does
not record which main currency it was entered against. The Milestone 2
collision guard and the Stage 17 hint copy each soften it; neither covers
it. It needs a decision — warn, or refuse the switch while currencies are
configured — and this milestone was already changing how every total is
computed.

---

## Milestone 4 — The rate editor in trip settings

A section below the existing trip form in
[settings-tab.js](web/js/pages/settings-tab.js), not inside
[trip-form.js](web/js/components/trip-form.js) — the trip form is shared
with the create page, and rates are not a create-time concern.

Repeatable rows, each a currency `<select>` (the `CURRENCIES` list from
`format.js`, minus the main currency and minus codes already chosen) and
a rate field, an "add currency" button, a remove button per row, one
Save. Read as `1 <code> = [____] <main>`, which is the direction the
number is looked up in.

**The exponent fold, in `format.js`**, beside the existing money
helpers, as two mirrored functions:

- `parseRate(text, foreign, main)` → `rate_ppb`. Parses the typed
  decimal into an integer and a decimal-place count **on the string**,
  exactly as `parseMoney` does and for the same stated reason, then
  scales by `10 ** (9 - places + exponent(main) - exponent(foreign))`.
  `"0.0058"`, JPY→EUR: 58, 4 places → `58 × 10^(9-4+2-0)` =
  `580_000_000`. Integer throughout; `null` on anything unparseable or
  on a result that is not a safe integer.
- `formatRate(ratePPB, foreign, main)` → the string to put back in the
  field, the same arithmetic inverted.

Round-tripping these two is the thing to test hardest: every currency
pair in `CURRENCIES` × a handful of rates, typed → stored → redisplayed
→ identical.

**i18n.** New keys under `trip.currencies.*` (the namespace matching
`trip.form.currency`, since this is a trip property) in **both**
`en.json` and `de.json` — heading, hint, add/remove labels, the
`1 {code} = ... {main}` row label, and the error strings including the
in-use refusal. `scripts/check_i18n.py` gates parity in `make ci`.

**Verify.** `make ci`, plus Playwright against `make dev` at 324×756:
configure JPY on the seeded trip, reload, assert the stored rate renders
back as typed; assert removing a currency in use surfaces the server's
message; assert the German locale's copy is present.

---

## Milestone 5 — The picker, the dual row, and the docs

All in [expenses-tab.js](web/js/pages/expenses-tab.js).

**The form.** A currency `<select>` beside the amount field, populated
from `trip.currencies` plus the main currency, defaulting to the main
currency — and **absent entirely when the trip has no additional
currencies**, which is the common case and must look exactly as it does
today. Switching it re-derives the amount field's placeholder and the
`parseMoney` exponent (`moneyPlaceholder`, `moneyExample` already take a
currency; they just need the selected one rather than `currency()`). The
`expenses.form.amount` label's `{currency}` interpolation follows the
selection. A live "≈ €7.60" preview under the field as the amount is
typed, using a small `convertMinor` mirror in `format.js` — the server
remains the authority, this only spares a round trip for the number the
user most wants to see confirmed.

**The rows.** A row whose `currency` differs from the trip's renders
`formatMoney(amount_minor, row.currency)` followed by
`≈ formatMoney(converted_minor, main)` in a muted span. Main-currency
rows are untouched. The total, the payer rows and the balances block all
already read main-currency integers and need no change — the point of
Milestone 3.

**Docs.** `docs/features/sharing-and-expenses.md` gains the multi-currency
section: how to configure a rate, that rates are live rather than
historical, and that totals and balances are always in the main
currency.

**Verify.** `make ci`; `tests/ui/expenses.spec.js` gains a case that
adds a JPY expense to a EUR trip and asserts both figures on the row and
the converted total — assertions on text content, not screenshots. Plus
a case asserting the picker is **absent** on a single-currency trip,
which is the regression that would otherwise go unnoticed.

---

## Build order

1. **Schema and store** first: everything else needs the column and the
   table to exist, and it is the only milestone that must be checked on
   both dialects.
2. **Configuration API** before the conversion, so there is a way to
   create the rate a conversion test needs.
3. **Conversion** before any UI, so the arithmetic is settled and tested
   in Go while it is the only thing in the diff.
4. **Settings UI** before the expense UI, because the expense picker has
   nothing to offer until a rate can be entered through the app.
5. **Expense UI and docs** last.

## Workflow

Per `CLAUDE.md`, for each milestone in order: implement → verify
(`make ci` green plus a real behavioural check, assertions preferred
over screenshots) → add a **Done.** paragraph to `plans/stage-32.md`
recording what actually landed and any deviation → reconcile
`plans/todo.md` in both directions → one commit describing what, why and
exactly how it was verified → make sure `make dev` is up → **stop and
hand back control**, and wait before starting the next milestone.

## Verification (whole stage)

- `make ci` green at every milestone.
- `make test-postgres` after Milestone 1, and again after Milestone 3 —
  the two that touch `internal/db` and its queries.
- End to end against `make dev-seed` + `make dev`: set the demo trip to
  EUR, add JPY at `1 JPY = 0.0058 EUR`, record `¥12,000`, and confirm
  the row reads `¥12,000 (≈ €69.60)`, the total includes €69.60, and the
  balances settle in Euro. Then edit the rate and confirm the row and
  the total both move.
- Confirm a single-currency trip is byte-for-byte the experience it is
  today: no picker, no second figure, no extra request.

## How complex is this, really

The risk is concentrated in Milestone 3 and in one place inside it: the
order of conversion and splitting. Everything else is a table, a form
and a select. The exponent fold in Milestone 4 looks like the tricky
part and is not — it is integer arithmetic with a round-trip test — but
it *is* the part that will read as magic in six months, so its comment
matters more than its code.
