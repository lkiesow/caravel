-- Drops the OpenStreetMap identity columns.
--
-- Lossy, and honestly so: the identities came from Nominatim at save time and
-- re-deriving them means searching for each address again. Unlike 0004 and
-- 0006 there is nothing to preserve them into -- an OSM element id has no
-- other home in this schema.
--
-- Both dialects support dropping a column outright: SQLite has done so since
-- 3.35, which predates the minimum this project builds against.
ALTER TABLE item_locations DROP COLUMN osm_id;
ALTER TABLE item_locations DROP COLUMN osm_type;
