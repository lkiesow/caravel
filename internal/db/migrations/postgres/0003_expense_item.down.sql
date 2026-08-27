-- Postgres drops an index with its column, but dropping it explicitly keeps the
-- down migration readable as the inverse of the up.
DROP INDEX IF EXISTS idx_expenses_item_id;
ALTER TABLE expenses DROP COLUMN item_id;
