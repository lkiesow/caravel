-- Three more location categories: food, event and shop.

-- Four categories left three common kinds of place with nowhere to go, so all
-- three were filed as sites: a restaurant or bar, an event you go to for one
-- evening rather than a place that is always open, and a shop or market. Each
-- is something you look for on the map as a group, which is what a category is
-- for -- tags stay the axis for anything finer.

-- SQLite cannot alter a CHECK constraint, so the table is rebuilt by the
-- documented twelve-step procedure, exactly as 0010 did. The column list is
-- the table as it stands after 0006 dropped items.type.

-- Foreign keys are off across the rebuild so that dropping the old table does
-- not cascade every location row out of item_locations, item_links, item_tags
-- and itinerary_entries. golang-migrate runs SQLite migrations without an
-- enclosing transaction (NoTxWrap in internal/db/db.go) precisely so this
-- pragma takes effect; inside a transaction it is a silent no-op.
PRAGMA foreign_keys = OFF;

CREATE TABLE items_new (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    -- "site" rather than "location": the UI calls a whole item a location, so
    -- the category needed a different word (Stage 02).
    category TEXT NOT NULL CHECK (category IN ('site', 'stay', 'transport', 'area', 'food', 'event', 'shop')),
    title TEXT NOT NULL,
    notes TEXT,
    image_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
    show_on_map INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO items_new (id, trip_id, category, title, notes, image_id, show_on_map, sort_order, created_at, updated_at)
SELECT id, trip_id, category, title, notes, image_id, show_on_map, sort_order, created_at, updated_at FROM items;

DROP TABLE items;

-- Nothing references items_new, so this rename rewrites no other table's
-- foreign keys -- which is the whole reason for the temporary name.
ALTER TABLE items_new RENAME TO items;

CREATE INDEX idx_items_trip_id_category ON items(trip_id, category);

PRAGMA foreign_keys = ON;
