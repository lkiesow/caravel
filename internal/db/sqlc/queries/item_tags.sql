-- name: CreateItemTag :exec
INSERT INTO item_tags (item_id, tag)
VALUES (sqlc.arg(item_id), sqlc.arg(tag));

-- name: ListItemTagsByItem :many
SELECT tag FROM item_tags WHERE item_id = sqlc.arg(item_id) ORDER BY tag;

-- Every tag on a trip in one query, carrying the location each one belongs to.
-- The locations list needs the tags of each of its rows, and asking per
-- location is a query per row. The same rows, deduplicated, are the distinct
-- tag list the editor offers as suggestions.
-- name: ListItemTagsByTrip :many
SELECT t.item_id, t.tag FROM item_tags t
JOIN items i ON i.id = t.item_id
WHERE i.trip_id = sqlc.arg(trip_id)
ORDER BY t.item_id, t.tag;

-- The tag set is replaced as a whole rather than patched tag by tag, so a write
-- deletes and reinserts inside one transaction. Two people editing the same
-- location then produce one set or the other, never a mixture.
-- name: DeleteItemTagsByItem :exec
DELETE FROM item_tags WHERE item_id = sqlc.arg(item_id);
