-- name: CreateFile :one
INSERT INTO files (id, trip_id, item_id, filename, storage_path, content_type, size_bytes, uploaded_at, note)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(item_id), sqlc.arg(filename), sqlc.arg(storage_path), sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(uploaded_at), sqlc.arg(note))
RETURNING *;

-- name: GetFileByID :one
SELECT * FROM files WHERE id = sqlc.arg(id);

-- Every file on the trip, including those attached to a location: each row
-- carries the trip's id regardless of item_id (see uploadFile), so no join
-- is needed to find them - only to name the location for display. LEFT, not
-- INNER: a trip-level row has a NULL item_id and must survive the join.
-- name: ListTripFiles :many
SELECT f.id, f.trip_id, f.item_id, f.filename, f.storage_path, f.content_type,
       f.size_bytes, f.uploaded_at, f.note,
       i.title AS item_title
FROM files f
LEFT JOIN items i ON i.id = f.item_id
WHERE f.trip_id = sqlc.arg(trip_id)
ORDER BY f.uploaded_at DESC;

-- name: ListItemFiles :many
SELECT * FROM files WHERE item_id = sqlc.arg(item_id) ORDER BY uploaded_at DESC;

-- name: DeleteFile :execrows
DELETE FROM files WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);
