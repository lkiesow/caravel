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
