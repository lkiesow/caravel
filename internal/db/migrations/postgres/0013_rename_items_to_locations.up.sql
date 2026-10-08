-- "item" becomes "location" in the schema, as it long since has on screen.

-- See the SQLite side for the account, and for why item_locations becomes
-- location_geo rather than following the pattern. Postgres repoints foreign
-- keys at a renamed table by itself; what it does not do is rename anything it
-- named after the old table, so the indexes and the constraints are renamed
-- here too. Most of those names are cosmetic, but items_category_check is not:
-- 0010 and 0011 drop it by name, and the next migration to touch the category
-- list will do the same with the new one.

ALTER TABLE items RENAME TO locations;
ALTER TABLE item_locations RENAME TO location_geo;
ALTER TABLE item_links RENAME TO location_links;
ALTER TABLE item_tags RENAME TO location_tags;

ALTER TABLE location_geo RENAME COLUMN item_id TO location_id;
ALTER TABLE location_links RENAME COLUMN item_id TO location_id;
ALTER TABLE location_tags RENAME COLUMN item_id TO location_id;
ALTER TABLE files RENAME COLUMN item_id TO location_id;
ALTER TABLE itinerary_entries RENAME COLUMN item_id TO location_id;
ALTER TABLE expenses RENAME COLUMN item_id TO location_id;

ALTER INDEX idx_items_trip_id_category RENAME TO idx_locations_trip_id_category;
ALTER INDEX idx_item_links_item_id RENAME TO idx_location_links_location_id;
ALTER INDEX idx_item_tags_tag RENAME TO idx_location_tags_tag;
ALTER INDEX idx_files_item_id RENAME TO idx_files_location_id;
ALTER INDEX idx_itinerary_entries_item_id RENAME TO idx_itinerary_entries_location_id;
ALTER INDEX idx_expenses_item_id RENAME TO idx_expenses_location_id;

-- Renaming a primary key or unique constraint renames its index with it.
ALTER TABLE locations RENAME CONSTRAINT items_pkey TO locations_pkey;
ALTER TABLE locations RENAME CONSTRAINT items_category_check TO locations_category_check;
ALTER TABLE locations RENAME CONSTRAINT items_trip_id_fkey TO locations_trip_id_fkey;
ALTER TABLE locations RENAME CONSTRAINT items_image_id_fkey TO locations_image_id_fkey;
ALTER TABLE location_geo RENAME CONSTRAINT item_locations_pkey TO location_geo_pkey;
ALTER TABLE location_geo RENAME CONSTRAINT item_locations_item_id_key TO location_geo_location_id_key;
ALTER TABLE location_geo RENAME CONSTRAINT item_locations_item_id_fkey TO location_geo_location_id_fkey;
ALTER TABLE location_links RENAME CONSTRAINT item_links_pkey TO location_links_pkey;
ALTER TABLE location_links RENAME CONSTRAINT item_links_item_id_fkey TO location_links_location_id_fkey;
ALTER TABLE location_tags RENAME CONSTRAINT item_tags_pkey TO location_tags_pkey;
ALTER TABLE location_tags RENAME CONSTRAINT item_tags_item_id_fkey TO location_tags_location_id_fkey;
ALTER TABLE files RENAME CONSTRAINT files_item_id_fkey TO files_location_id_fkey;
ALTER TABLE itinerary_entries RENAME CONSTRAINT itinerary_entries_item_id_fkey TO itinerary_entries_location_id_fkey;
ALTER TABLE expenses RENAME CONSTRAINT expenses_item_id_fkey TO expenses_location_id_fkey;
