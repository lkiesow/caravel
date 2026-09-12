-- Back to three categories.

-- Anything filed as an area becomes a site, which is where such a place would
-- have been filed before this migration existed. Lossy, and unavoidable: the
-- constraint being restored has no room for the value.
UPDATE items SET category = 'site' WHERE category = 'area';

PRAGMA foreign_keys = OFF;

CREATE TABLE items_old (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('site', 'stay', 'transport')),
    title TEXT NOT NULL,
    notes TEXT,
    image_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
    show_on_map INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO items_old (id, trip_id, category, title, notes, image_id, show_on_map, sort_order, created_at, updated_at)
SELECT id, trip_id, category, title, notes, image_id, show_on_map, sort_order, created_at, updated_at FROM items;

DROP TABLE items;

ALTER TABLE items_old RENAME TO items;

CREATE INDEX idx_items_trip_id_category ON items(trip_id, category);

PRAGMA foreign_keys = ON;
