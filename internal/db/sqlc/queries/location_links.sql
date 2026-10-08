-- name: CreateLocationLink :one
INSERT INTO location_links (id, location_id, url, label, sort_order)
VALUES (sqlc.arg(id), sqlc.arg(location_id), sqlc.arg(url), sqlc.arg(label), sqlc.arg(sort_order))
RETURNING *;

-- name: ListLocationLinksByLocation :many
SELECT * FROM location_links WHERE location_id = sqlc.arg(location_id) ORDER BY sort_order;

-- name: DeleteLocationLink :execrows
DELETE FROM location_links WHERE id = sqlc.arg(id) AND location_id = sqlc.arg(location_id);
