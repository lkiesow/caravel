-- name: CreateItemLink :one
INSERT INTO item_links (id, item_id, url, label, sort_order)
VALUES (sqlc.arg(id), sqlc.arg(item_id), sqlc.arg(url), sqlc.arg(label), sqlc.arg(sort_order))
RETURNING *;

-- name: ListItemLinksByItem :many
SELECT * FROM item_links WHERE item_id = sqlc.arg(item_id) ORDER BY sort_order;

-- name: DeleteItemLink :execrows
DELETE FROM item_links WHERE id = sqlc.arg(id) AND item_id = sqlc.arg(item_id);
