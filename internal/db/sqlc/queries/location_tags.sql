-- name: CreateLocationTag :exec
INSERT INTO location_tags (location_id, tag)
VALUES (sqlc.arg(location_id), sqlc.arg(tag));

-- name: ListLocationTagsByLocation :many
SELECT tag FROM location_tags WHERE location_id = sqlc.arg(location_id) ORDER BY tag;

-- Every tag on a trip in one query, carrying the location each one belongs to.
-- The locations list needs the tags of each of its rows, and asking per
-- location is a query per row. The same rows, deduplicated, are the distinct
-- tag list the editor offers as suggestions.
-- name: ListLocationTagsByTrip :many
SELECT t.location_id, t.tag FROM location_tags t
JOIN locations loc ON loc.id = t.location_id
WHERE loc.trip_id = sqlc.arg(trip_id)
ORDER BY t.location_id, t.tag;

-- The tag set is replaced as a whole rather than patched tag by tag, so a write
-- deletes and reinserts inside one transaction. Two people editing the same
-- location then produce one set or the other, never a mixture.
-- name: DeleteLocationTagsByLocation :exec
DELETE FROM location_tags WHERE location_id = sqlc.arg(location_id);
