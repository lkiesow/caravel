-- What a trip cost. Stage 17.
--
-- One currency per trip, not per expense. A trip is normally spent in one
-- currency, and making it per-expense makes every total and every balance
-- per-currency too -- so the common case pays for the rare one. A purchase in
-- another currency is entered as the converted amount.
--
-- A constant default so existing rows are valid.
-- The code validates against an allowlist (see db.Currencies); no CHECK here,
-- because the list of currencies people want is going to grow and a CHECK
-- constraint would need a migration each time.
ALTER TABLE trips ADD COLUMN currency TEXT NOT NULL DEFAULT 'EUR';

CREATE TABLE expenses (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    title TEXT NOT NULL,

    -- An integer in the minor unit of the trip currency -- cents for EUR, yen
    -- for JPY, which has no minor unit at all. Never a REAL: a ledger that is
    -- out by a cent is worse than one that refuses to exist, and floating
    -- point money is how you get one. The exponent is a display concern and
    -- lives in the client, which reads it from Intl rather than assuming two.
    --
    -- Positive only. Refunds and negative amounts are deliberately out of
    -- scope for now, and a zero-cost row is a note rather than an expense.
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),

    -- A calendar day, matching trips.start_date and itinerary_days.date --
    -- DATE here, TEXT in the sqlite dialect, which the store layer normalises
    -- to YYYY-MM-DD either way. It is the day you spent the money, not an
    -- instant, so storing it as a timestamp would invent a time zone nobody
    -- chose.
    spent_on DATE NOT NULL,

    -- Who paid. Nullable with ON DELETE SET NULL, exactly as files.owner_user_id
    -- and checklists.owner_user_id are: deleting an account must not delete the
    -- trip ledger.
    --
    -- Unlike those two, NULL here does not fail closed. The expense stays
    -- visible to everyone on the trip and still counts toward the total; it is
    -- shown as unattributed, and the balances view reports it rather than
    -- splitting it, since a payer nobody can name cannot be credited.
    payer_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_expenses_trip_id ON expenses(trip_id);
CREATE INDEX idx_expenses_payer_user_id ON expenses(payer_user_id);
