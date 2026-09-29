-- Restores the column, but not what was in it: every value is the default.

-- Nothing read it, so nothing regresses -- with one exception worth naming. A
-- batch of locations created while this migration was applied has no
-- sort_order to come back to, so rolling back leaves those rows tied at 0 and
-- ordered by created_at, exactly as the version that dropped the column did.
ALTER TABLE items ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
