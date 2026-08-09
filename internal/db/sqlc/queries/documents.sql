-- name: CreateDocument :one
INSERT INTO documents (id, trip_id, item_id, filename, storage_path, content_type, size_bytes, uploaded_at)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(item_id), sqlc.arg(filename), sqlc.arg(storage_path), sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(uploaded_at))
RETURNING *;

-- name: GetDocumentByID :one
SELECT * FROM documents WHERE id = sqlc.arg(id);

-- name: ListTripDocuments :many
SELECT * FROM documents WHERE trip_id = sqlc.arg(trip_id) AND item_id IS NULL ORDER BY uploaded_at DESC;

-- name: ListItemDocuments :many
SELECT * FROM documents WHERE item_id = sqlc.arg(item_id) ORDER BY uploaded_at DESC;

-- name: DeleteDocument :execrows
DELETE FROM documents WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);
