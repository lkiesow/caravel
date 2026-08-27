-- Which location an expense was for.
--
-- Stage 17 shipped the ledger without this on purpose: the trip, the amount,
-- the payer and the share set were what a shared ledger needs, and a link to a
-- location was not needed to make the arithmetic right. What it leaves is a
-- reading problem. Looking at "Ferry, 45.00, 14 September" a month later and
-- asking "which ferry was that" is a memory test the app could answer, because
-- the location it belonged to already holds the picture, the address and the
-- notes.
--
-- Nullable, and it stays nullable: most expenses are not about one place --
-- groceries, fuel, a round of drinks -- and forcing every row to name a
-- location would mean inventing one.
--
-- ON DELETE SET NULL rather than CASCADE. Deleting a location must never delete
-- money from the ledger: the expense happened, the total is still right, and
-- all that is lost is the pointer. CASCADE here would make removing a
-- cancelled hotel quietly change what the trip cost.
ALTER TABLE expenses ADD COLUMN item_id TEXT REFERENCES items(id) ON DELETE SET NULL;

-- The expenses of one location, for the list endpoint. Without it that is a
-- scan of every expense on the instance, and the column is mostly NULL, so the
-- index is small.
CREATE INDEX idx_expenses_item_id ON expenses(item_id);
