-- More than one currency on a trip. Stage 32.
--
-- Until now a trip had exactly one currency and 0001_init argued for it: making
-- the currency per-expense makes every total and every balance per-currency
-- too, so the common case would pay for the rare one. That reasoning still
-- holds for totals, and this migration does not overturn it. What it overturns
-- is the sentence that followed -- that a purchase in another currency is
-- entered as the converted amount. Converting by hand before typing is the part
-- that does not survive a real trip: the receipt says 1200 yen, the ledger says
-- 7.60 euro, and a month later nobody can reconcile the two.
--
-- So the trip keeps one main currency in trips.currency, and every total, share
-- and balance stays denominated in it. A trip may additionally configure extra
-- currencies here, each with one rate into the main currency.
CREATE TABLE trip_currencies (
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,

    -- An ISO 4217 code, never equal to trips.currency. No CHECK, for the same
    -- reason trips.currency carries none: the allowlist lives in db.Currencies,
    -- and growing it should not need a migration.
    code TEXT NOT NULL,

    -- The exchange rate, as an integer in parts per billion, converting the
    -- MINOR UNIT of code into the MINOR UNIT of the trip main currency.
    --
    -- Minor unit to minor unit, rather than the human readable rate, because
    -- the server deliberately does not know how many decimal places a currency
    -- has. That knowledge lives in the client, which asks Intl for it -- see
    -- amount_minor in 0001_init and web/js/format.js. A stored rate of
    -- 1 JPY = 0.0058 EUR would force a second exponent table into Go, a third
    -- hand maintained copy of what the platform already knows. Instead the
    -- browser folds both exponents in: one yen, which has no minor unit, is
    -- 0.58 cents, so the stored value is 580000000. The settings form inverts
    -- the same arithmetic to show the number back. The server only ever
    -- multiplies integers.
    --
    -- Parts per billion rather than per million so that a weak currency added
    -- to db.Currencies later still has resolution to spare.
    --
    -- One live rate per currency, not a snapshot per expense: editing a rate
    -- reconverts every expense recorded in that currency. A trip rate is the
    -- rate we are using, not a market record, so there is one source of truth
    -- and no historical rate column.
    rate_ppb BIGINT NOT NULL CHECK (rate_ppb > 0),

    created_at TIMESTAMPTZ NOT NULL,

    -- The pair, so one trip cannot hold two rates for the same currency.
    PRIMARY KEY (trip_id, code)
);

-- What this expense was actually paid in.
--
-- NULL means the trip main currency, which is exactly what every existing row
-- means today -- so there is no backfill and no behaviour change for any
-- expense already recorded. A non NULL value is always one of the codes
-- configured for that trip in trip_currencies above.
--
-- amount_minor stays what was paid, in the minor unit of THIS column rather
-- than of the trip. The converted figure is computed on read and never stored:
-- storing it would be a second source of truth that goes stale the moment the
-- rate is edited.
ALTER TABLE expenses ADD COLUMN currency TEXT;
