-- name: InsertItemLocation :one
INSERT INTO item_locations (id, item_id, lat, lng, address)
VALUES (sqlc.arg(id), sqlc.arg(item_id), sqlc.arg(lat), sqlc.arg(lng), sqlc.arg(address))
RETURNING *;

-- name: UpdateItemLocation :execrows
UPDATE item_locations
SET lat = sqlc.arg(lat), lng = sqlc.arg(lng), address = sqlc.arg(address)
WHERE item_id = sqlc.arg(item_id);

-- name: GetItemLocationByItemID :one
SELECT * FROM item_locations WHERE item_id = sqlc.arg(item_id);
