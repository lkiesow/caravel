-- Who an expense was for, as opposed to who paid for it. Stage 17.
--
-- No amount column, deliberately. The split is equal among the rows present and
-- is computed when read. Storing a share list AND a per-share amount is two
-- sources of truth for one number, and they drift the first time somebody is
-- added to an expense -- at which point the stored amounts are silently wrong
-- and nothing says so.
--
-- No rows at all for an expense means everyone on the trip, resolved at read
-- time rather than written out at create time. That keeps the common case free
-- of writes, and means adding a member does not require rewriting history. The
-- consequence is worth stating plainly: a new member's share of past expenses
-- appears retroactively. An explicit set of rows is how you pin an expense to a
-- subset of the trip.
--
-- The primary key is the pair, so a client that names the same person twice
-- cannot double their weight in the split.
CREATE TABLE expense_shares (
    expense_id TEXT NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (expense_id, user_id)
);

-- The expense_id half of the key already serves lookups by expense. This is
-- for the other direction, which is what happens when somebody leaves a trip.
CREATE INDEX idx_expense_shares_user_id ON expense_shares(user_id);
