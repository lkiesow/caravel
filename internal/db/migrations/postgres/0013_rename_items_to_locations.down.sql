-- Back to the "item" names. The exact inverse of the up migration; see there.

ALTER TABLE expenses RENAME CONSTRAINT expenses_location_id_fkey TO expenses_item_id_fkey;
ALTER TABLE itinerary_entries RENAME CONSTRAINT itinerary_entries_location_id_fkey TO itinerary_entries_item_id_fkey;
ALTER TABLE files RENAME CONSTRAINT files_location_id_fkey TO files_item_id_fkey;
ALTER TABLE location_tags RENAME CONSTRAINT location_tags_location_id_fkey TO item_tags_item_id_fkey;
ALTER TABLE location_tags RENAME CONSTRAINT location_tags_pkey TO item_tags_pkey;
ALTER TABLE location_links RENAME CONSTRAINT location_links_location_id_fkey TO item_links_item_id_fkey;
ALTER TABLE location_links RENAME CONSTRAINT location_links_pkey TO item_links_pkey;
ALTER TABLE location_geo RENAME CONSTRAINT location_geo_location_id_fkey TO item_locations_item_id_fkey;
ALTER TABLE location_geo RENAME CONSTRAINT location_geo_location_id_key TO item_locations_item_id_key;
ALTER TABLE location_geo RENAME CONSTRAINT location_geo_pkey TO item_locations_pkey;
ALTER TABLE locations RENAME CONSTRAINT locations_image_id_fkey TO items_image_id_fkey;
ALTER TABLE locations RENAME CONSTRAINT locations_trip_id_fkey TO items_trip_id_fkey;
ALTER TABLE locations RENAME CONSTRAINT locations_category_check TO items_category_check;
ALTER TABLE locations RENAME CONSTRAINT locations_pkey TO items_pkey;

ALTER INDEX idx_expenses_location_id RENAME TO idx_expenses_item_id;
ALTER INDEX idx_itinerary_entries_location_id RENAME TO idx_itinerary_entries_item_id;
ALTER INDEX idx_files_location_id RENAME TO idx_files_item_id;
ALTER INDEX idx_location_tags_tag RENAME TO idx_item_tags_tag;
ALTER INDEX idx_location_links_location_id RENAME TO idx_item_links_item_id;
ALTER INDEX idx_locations_trip_id_category RENAME TO idx_items_trip_id_category;

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
