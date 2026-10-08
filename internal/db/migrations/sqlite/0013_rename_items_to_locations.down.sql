-- Back to the "item" names. The exact inverse of the up migration; see there.

DROP INDEX idx_expenses_location_id;
CREATE INDEX idx_expenses_item_id ON expenses(location_id);
DROP INDEX idx_itinerary_entries_location_id;
CREATE INDEX idx_itinerary_entries_item_id ON itinerary_entries(location_id);
DROP INDEX idx_files_location_id;
CREATE INDEX idx_files_item_id ON files(location_id);
DROP INDEX idx_location_tags_tag;
CREATE INDEX idx_item_tags_tag ON location_tags(tag);
DROP INDEX idx_location_links_location_id;
CREATE INDEX idx_item_links_item_id ON location_links(location_id);
DROP INDEX idx_locations_trip_id_category;
CREATE INDEX idx_items_trip_id_category ON locations(trip_id, category);

ALTER TABLE expenses RENAME COLUMN location_id TO item_id;
ALTER TABLE itinerary_entries RENAME COLUMN location_id TO item_id;
ALTER TABLE files RENAME COLUMN location_id TO item_id;
ALTER TABLE location_tags RENAME COLUMN location_id TO item_id;
ALTER TABLE location_links RENAME COLUMN location_id TO item_id;
ALTER TABLE location_geo RENAME COLUMN location_id TO item_id;

ALTER TABLE location_tags RENAME TO item_tags;
ALTER TABLE location_links RENAME TO item_links;
ALTER TABLE location_geo RENAME TO item_locations;
ALTER TABLE locations RENAME TO items;
