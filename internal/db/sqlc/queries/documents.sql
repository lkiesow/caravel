-- name: CreateDocument :one
INSERT INTO documents (id, trip_id, item_id, filename, storage_path, content_type, size_bytes, uploaded_at, note)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(item_id), sqlc.arg(filename), sqlc.arg(storage_path), sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(uploaded_at), sqlc.arg(note))
RETURNING *;

-- name: GetDocumentByID :one
SELECT * FROM documents WHERE id = sqlc.arg(id);

-- Every file on the trip, including those attached to a location: each row
-- carries the trip's id regardless of item_id (see uploadDocument), so no join
-- is needed to find them - only to name the location for display. LEFT, not
-- INNER: a trip-level row has a NULL item_id and must survive the join.
-- name: ListTripDocuments :many
SELECT d.id, d.trip_id, d.item_id, d.filename, d.storage_path, d.content_type,
       d.size_bytes, d.uploaded_at, d.note,
       i.title AS item_title
FROM documents d
LEFT JOIN items i ON i.id = d.item_id
WHERE d.trip_id = sqlc.arg(trip_id)
ORDER BY d.uploaded_at DESC;

-- name: ListItemDocuments :many
SELECT * FROM documents WHERE item_id = sqlc.arg(item_id) ORDER BY uploaded_at DESC;

-- name: DeleteDocument :execrows
DELETE FROM documents WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);
