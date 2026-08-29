-- Recreates the table exactly as 0001_init left it, and EMPTY.
--
-- The rows cannot come back: the up migration discarded them, and they are not
-- derivable from the itinerary -- a range that was never on the itinerary left
-- no trace, and one that was is indistinguishable from a day added by hand. A
-- down migration that silently invented item_dates rows from itinerary_entries
-- would be worse than one that restores an empty table, because it would look
-- like the data survived.
DROP INDEX IF EXISTS idx_itinerary_entries_item_id;

CREATE TABLE item_dates (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    start_date TEXT,
    end_date TEXT,
    label TEXT,
    all_day INTEGER NOT NULL DEFAULT 1,
    start_time TEXT,
    end_time TEXT
);

CREATE INDEX idx_item_dates_item_id ON item_dates(item_id);
