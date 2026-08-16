-- name: InsertItineraryDay :one
INSERT INTO itinerary_days (id, trip_id, date, notes)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(date), sqlc.arg(notes))
RETURNING *;

-- name: UpdateItineraryDayNotes :execrows
UPDATE itinerary_days
SET notes = sqlc.arg(notes)
WHERE trip_id = sqlc.arg(trip_id) AND date = sqlc.arg(date);

-- name: GetItineraryDayByTripAndDate :one
SELECT * FROM itinerary_days WHERE trip_id = sqlc.arg(trip_id) AND date = sqlc.arg(date);

-- name: GetItineraryDayByID :one
SELECT * FROM itinerary_days WHERE id = sqlc.arg(id);

-- name: ListItineraryDaysByTrip :many
SELECT * FROM itinerary_days WHERE trip_id = sqlc.arg(trip_id) ORDER BY date;

-- name: DeleteItineraryDay :execrows
-- Scoped by trip_id as well as id, mirroring DeleteItineraryEntry: the
-- handler has already checked ownership, and this keeps a day from being
-- deleted through the wrong trip even if that check is ever bypassed.
-- Entries on the day go with it via itinerary_entries' ON DELETE CASCADE.
DELETE FROM itinerary_days WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);
