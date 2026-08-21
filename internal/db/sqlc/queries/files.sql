-- name: CreateFile :one
INSERT INTO files (id, trip_id, item_id, filename, storage_path, content_type, size_bytes, uploaded_at, note, visibility, owner_user_id)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(item_id), sqlc.arg(filename), sqlc.arg(storage_path), sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(uploaded_at), sqlc.arg(note), sqlc.arg(visibility), sqlc.arg(owner_user_id))
RETURNING *;

-- name: GetFileByID :one
SELECT * FROM files WHERE id = sqlc.arg(id);

-- Every file on the trip, including those attached to a location: each row
-- carries the trip's id regardless of item_id (see uploadFile), so no join
-- is needed to find them - only to name the location for display. LEFT, not
-- INNER: a trip-level row has a NULL item_id and must survive the join.
--
-- The visibility predicate is the same one on ListItemFiles and on the Go-side
-- check in loadFile: a personal file is visible only to whoever uploaded it. It
-- lives in the SQL as well as in Go on purpose - a list endpoint that forgot it
-- would leak silently, where a single-file endpoint that forgot it at least
-- needs an id to be guessed first.
--
-- NULL owner_user_id matches nobody, which is the intended failure: see
-- migration 0009 on why that column is nullable.
-- name: ListTripFiles :many
SELECT f.id, f.trip_id, f.item_id, f.filename, f.storage_path, f.content_type,
       f.size_bytes, f.uploaded_at, f.note, f.visibility, f.owner_user_id,
       i.title AS item_title
FROM files f
LEFT JOIN items i ON i.id = f.item_id
WHERE f.trip_id = sqlc.arg(trip_id)
  AND (f.visibility = 'trip' OR f.owner_user_id = sqlc.arg(user_id))
ORDER BY f.uploaded_at DESC;

-- name: ListItemFiles :many
SELECT * FROM files
WHERE item_id = sqlc.arg(item_id)
  AND (visibility = 'trip' OR owner_user_id = sqlc.arg(user_id))
ORDER BY uploaded_at DESC;

-- A note is the only thing about a file that can change after upload: it is
-- the readable name a file gets when its own filename is a storage blob, so
-- write-once was the wrong lifetime for it. Scoped by (id, trip_id) exactly
-- like DeleteFile, so a trip-role check is the whole authorization story.
-- Passing NULL clears it.
-- name: UpdateFileNote :one
UPDATE files SET note = sqlc.narg(note) WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- Separate from UpdateFileNote rather than folded into it: changing a note is
-- an editor-level edit of shared content, while changing visibility is a
-- decision only the file's own uploader may make. Two different authorization
-- rules should not share one statement.
-- name: SetFileVisibility :one
UPDATE files SET visibility = sqlc.arg(visibility)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- name: DeleteFile :execrows
DELETE FROM files WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);

-- Every personal file belonging to one user on one trip, for the moment they
-- stop being a member. The rows are found first so their blobs can be deleted
-- too: a row removed without its blob leaks bytes nobody can reach.
-- name: ListPersonalFilesForUser :many
SELECT * FROM files
WHERE trip_id = sqlc.arg(trip_id) AND owner_user_id = sqlc.arg(user_id) AND visibility = 'personal';
