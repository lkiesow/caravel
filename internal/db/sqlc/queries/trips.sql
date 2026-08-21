-- name: CreateTrip :one
INSERT INTO trips (id, owner_id, title, start_date, end_date, subtitle, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(owner_id), sqlc.arg(title), sqlc.arg(start_date), sqlc.arg(end_date), sqlc.arg(subtitle), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

-- name: GetTripByID :one
SELECT * FROM trips WHERE id = sqlc.arg(id);

-- name: ListTripsByOwner :many
SELECT * FROM trips WHERE owner_id = sqlc.arg(owner_id) ORDER BY created_at DESC;

-- name: UpdateTrip :one
UPDATE trips
SET title = sqlc.arg(title),
    start_date = sqlc.arg(start_date),
    end_date = sqlc.arg(end_date),
    subtitle = sqlc.arg(subtitle),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- Keeps its owner_id predicate where UpdateTrip and SetTripPreviewImage lost
-- theirs: the role required to delete a trip is exactly 'owner', so the second
-- belt costs nothing and guards the most destructive call in the app.
-- name: DeleteTrip :execrows
DELETE FROM trips WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id);

-- name: SetTripPreviewImage :one
UPDATE trips
SET preview_image_id = sqlc.arg(preview_image_id), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING *;
