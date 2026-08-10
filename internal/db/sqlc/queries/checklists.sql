-- name: CreateChecklist :one
INSERT INTO checklists (id, trip_id, title, sort_order, created_at)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(title), sqlc.arg(sort_order), sqlc.arg(created_at))
RETURNING *;

-- name: GetChecklistByID :one
SELECT * FROM checklists WHERE id = sqlc.arg(id);

-- name: ListChecklistsByTrip :many
SELECT * FROM checklists WHERE trip_id = sqlc.arg(trip_id) ORDER BY sort_order;

-- name: DeleteChecklist :execrows
DELETE FROM checklists WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);

-- name: CreateChecklistItem :one
INSERT INTO checklist_items (id, checklist_id, text, checked, sort_order, created_at)
VALUES (sqlc.arg(id), sqlc.arg(checklist_id), sqlc.arg(text), sqlc.arg(checked), sqlc.arg(sort_order), sqlc.arg(created_at))
RETURNING *;

-- name: ListChecklistItemsByChecklist :many
SELECT * FROM checklist_items WHERE checklist_id = sqlc.arg(checklist_id) ORDER BY sort_order;

-- name: SetChecklistItemChecked :one
UPDATE checklist_items SET checked = sqlc.arg(checked) WHERE id = sqlc.arg(id) AND checklist_id = sqlc.arg(checklist_id)
RETURNING *;

-- name: DeleteChecklistItem :execrows
DELETE FROM checklist_items WHERE id = sqlc.arg(id) AND checklist_id = sqlc.arg(checklist_id);
