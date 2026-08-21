-- name: CreateItem :one
INSERT INTO items (id, trip_id, category, type, title, notes, show_on_map, sort_order, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(category), sqlc.arg(type), sqlc.arg(title), sqlc.arg(notes), sqlc.arg(show_on_map), sqlc.arg(sort_order), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

-- name: GetItemByID :one
SELECT * FROM items WHERE id = sqlc.arg(id);

-- name: ListItemsByTrip :many
SELECT * FROM items
WHERE trip_id = sqlc.arg(trip_id)
  AND (sqlc.narg(category) IS NULL OR category = sqlc.narg(category))
ORDER BY sort_order, created_at;

-- name: UpdateItem :one
UPDATE items
SET category = sqlc.arg(category),
    type = sqlc.arg(type),
    title = sqlc.arg(title),
    notes = sqlc.arg(notes),
    show_on_map = sqlc.arg(show_on_map),
    sort_order = sqlc.arg(sort_order),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- name: DeleteItem :execrows
DELETE FROM items WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);

-- name: SetItemImage :one
UPDATE items
SET image_id = sqlc.arg(image_id), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- ListItemLocationsByTrip: every coordinate on the trip, keyed by item.
--
-- Deliberately NOT ListMapItemsByTrip below: that one also filters
-- show_on_map, which is about whether a place is drawn on the map and says
-- nothing about whether it has a position. The locations list filters by
-- distance, so it wants every located item regardless.
--
-- Rows with only an address and no coordinates are excluded here rather than
-- in Go: they are not "far away", they are unmeasurable, and the caller has
-- to be able to tell those apart.
-- name: ListItemLocationsByTrip :many
SELECT l.item_id, l.lat, l.lng
FROM item_locations l
INNER JOIN items i ON i.id = l.item_id
WHERE i.trip_id = sqlc.arg(trip_id) AND l.lat IS NOT NULL AND l.lng IS NOT NULL;

-- ListMapItemsByTrip: show_on_map is filtered in the store layer, not here,
-- since its Go type (int64 vs bool) diverges by dialect (plan Section 2.1).
-- name: ListMapItemsByTrip :many
SELECT i.id, i.category, i.title, i.show_on_map, l.lat, l.lng
FROM items i
INNER JOIN item_locations l ON l.item_id = i.id
WHERE i.trip_id = sqlc.arg(trip_id) AND l.lat IS NOT NULL AND l.lng IS NOT NULL;
