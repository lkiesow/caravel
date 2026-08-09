-- name: CreateItemDate :one
INSERT INTO item_dates (id, item_id, start_date, end_date, label, all_day, start_time, end_time)
VALUES (sqlc.arg(id), sqlc.arg(item_id), sqlc.arg(start_date), sqlc.arg(end_date), sqlc.arg(label), sqlc.arg(all_day), sqlc.arg(start_time), sqlc.arg(end_time))
RETURNING *;

-- name: ListItemDatesByItem :many
SELECT * FROM item_dates WHERE item_id = sqlc.arg(item_id) ORDER BY start_date;

-- name: DeleteItemDate :execrows
DELETE FROM item_dates WHERE id = sqlc.arg(id) AND item_id = sqlc.arg(item_id);
