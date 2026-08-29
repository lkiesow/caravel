-- The index goes with the table either way, but dropping it explicitly keeps
-- the down migration readable as the inverse of the up.
DROP INDEX IF EXISTS idx_item_tags_tag;
DROP TABLE IF EXISTS item_tags;
