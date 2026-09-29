-- Restores the column, but not what was in it: every value is the default.
ALTER TABLE items ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
