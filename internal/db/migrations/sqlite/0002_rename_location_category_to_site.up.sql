-- Renames the items.category value "location" to "site" (Stage 02 review:
-- tab "Items" became "Locations", so the "location" category was renamed
-- to "site" to avoid the name collision). SQLite can't ALTER a CHECK
-- constraint, so the table is recreated. foreign_keys is toggled off for
-- the swap: with it on, "DROP TABLE items" cascade-deletes every row in
-- item_locations/item_links/item_dates/documents/itinerary_entries first
-- (SQLite applies ON DELETE CASCADE as if each items row were deleted
-- before the table itself is dropped). This pragma is a no-op inside an
-- open transaction, so the sqlite migrator is configured with NoTxWrap
-- (see internal/db/db.go) to run this file statement-by-statement outside
-- a transaction. Foreign key integrity is unaffected by the toggle since
-- the same id values are preserved in the new table.
PRAGMA foreign_keys=OFF;

CREATE TABLE items_new (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('site', 'stay', 'transport')),
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
    CASE WHEN category = 'location' THEN 'site' ELSE category END,
    type, title, notes, image_id, show_on_map, sort_order, created_at, updated_at
FROM items;

DROP TABLE items;
ALTER TABLE items_new RENAME TO items;
CREATE INDEX idx_items_trip_id_category ON items(trip_id, category);

PRAGMA foreign_keys=ON;
