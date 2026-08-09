PRAGMA foreign_keys=OFF;

CREATE TABLE items_new (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('location', 'stay', 'transport')),
    type TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    notes TEXT,
    image_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
    show_on_map INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO items_new SELECT
    id, trip_id,
    CASE WHEN category = 'site' THEN 'location' ELSE category END,
    type, title, notes, image_id, show_on_map, sort_order, created_at, updated_at
FROM items;

DROP TABLE items;
ALTER TABLE items_new RENAME TO items;
CREATE INDEX idx_items_trip_id_category ON items(trip_id, category);

PRAGMA foreign_keys=ON;
