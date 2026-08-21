-- name: CreateTrip :one
INSERT INTO trips (id, owner_id, title, start_date, end_date, subtitle, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(owner_id), sqlc.arg(title), sqlc.arg(start_date), sqlc.arg(end_date), sqlc.arg(subtitle), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

-- name: GetTripByID :one
SELECT * FROM trips WHERE id = sqlc.arg(id);

-- Every trip the user can reach, with their own role on each and the owner's
-- name for the ones they don't own.
--
-- The LEFT JOIN cannot duplicate a trip: trip_members' primary key is
-- (trip_id, user_id) and user_id is pinned to one value here, so it matches at
-- most one row per trip. That is also why the role can be selected inline
-- rather than needing a second query per trip.
-- name: ListTripsForUser :many
SELECT t.*,
       CAST(CASE WHEN t.owner_id = sqlc.arg(user_id) THEN 'owner' ELSE m.role END AS TEXT) AS role,
       u.username AS owner_username,
       u.display_name AS owner_display_name,
       (SELECT COUNT(*) FROM trip_members mm WHERE mm.trip_id = t.id) AS member_count
FROM trips t
JOIN users u ON u.id = t.owner_id
LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = sqlc.arg(user_id)
WHERE t.owner_id = sqlc.arg(user_id) OR m.user_id IS NOT NULL
ORDER BY t.created_at DESC;

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
