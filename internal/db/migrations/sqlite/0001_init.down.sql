-- The reverse of 0001_init.up.sql: every table, in an order that respects the
-- foreign keys (children before parents), so this works with foreign_keys ON.
--
-- Indexes are not dropped separately: SQLite drops a table's indexes with the
-- table.
DROP TABLE IF EXISTS expense_shares;
DROP TABLE IF EXISTS expenses;
DROP TABLE IF EXISTS app_settings;
DROP TABLE IF EXISTS trip_members;
DROP TABLE IF EXISTS checklist_items;
DROP TABLE IF EXISTS checklists;
DROP TABLE IF EXISTS itinerary_entries;
DROP TABLE IF EXISTS itinerary_days;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS item_dates;
DROP TABLE IF EXISTS item_links;
DROP TABLE IF EXISTS item_locations;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS media_assets;
DROP TABLE IF EXISTS trips;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS auth_identities;
DROP TABLE IF EXISTS users;
