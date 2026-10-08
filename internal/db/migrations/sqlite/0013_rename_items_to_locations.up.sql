-- "item" becomes "location" in the schema, as it long since has on screen.

-- Nothing about the data changes: four tables and six item_id columns are
-- renamed, and the indexes on them follow. The UI has called these locations
-- since Stage 02; the schema kept the old word, and every layer above it took
-- the old word from here (Stage 49).

-- item_locations is the one table that does not become locations_something by
-- rote. It holds where a location is -- coordinates, address, OSM identity --
-- and "the location of a location" says nothing, so it is location_geo.

-- No table rebuild. SQLite has had RENAME COLUMN since 3.25, and RENAME TO
-- rewrites the REFERENCES clauses of every other table that points at the
-- renamed one. The one setting where it does not is legacy_alter_table on with
-- foreign_keys off; here the first is off by default and the second is on for
-- every connection (see Open). migration_0013_test.go deletes a location after
-- this runs and checks the cascade, so that is tested rather than assumed.

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

-- SQLite cannot rename an index. The renames above already point each index at
-- its new table and column, so this only changes the names.
DROP INDEX idx_items_trip_id_category;
CREATE INDEX idx_locations_trip_id_category ON locations(trip_id, category);
DROP INDEX idx_item_links_item_id;
CREATE INDEX idx_location_links_location_id ON location_links(location_id);
DROP INDEX idx_item_tags_tag;
CREATE INDEX idx_location_tags_tag ON location_tags(tag);
DROP INDEX idx_files_item_id;
CREATE INDEX idx_files_location_id ON files(location_id);
DROP INDEX idx_itinerary_entries_item_id;
CREATE INDEX idx_itinerary_entries_location_id ON itinerary_entries(location_id);
DROP INDEX idx_expenses_item_id;
CREATE INDEX idx_expenses_location_id ON expenses(location_id);
