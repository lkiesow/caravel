-- name: GetTripNote :one
SELECT * FROM trip_notes WHERE trip_id = sqlc.arg(trip_id);

-- name: UpdateTripNote :execrows
UPDATE trip_notes
SET body = sqlc.arg(body),
    updated_at = sqlc.arg(updated_at),
    updated_by = sqlc.arg(updated_by)
WHERE trip_id = sqlc.arg(trip_id);

-- name: InsertTripNote :one
INSERT INTO trip_notes (trip_id, body, updated_at, updated_by)
VALUES (sqlc.arg(trip_id), sqlc.arg(body), sqlc.arg(updated_at), sqlc.arg(updated_by))
RETURNING *;

-- name: DeleteTripNote :execrows
-- Clearing a note removes the row rather than storing an empty string, so
-- there is one representation of a trip with nothing written down. The tab
-- reads that as fresh and opens in the editor.
DELETE FROM trip_notes WHERE trip_id = sqlc.arg(trip_id);
