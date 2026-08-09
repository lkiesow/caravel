-- name: CreateItineraryEntry :one
INSERT INTO itinerary_entries (id, itinerary_day_id, item_id, sort_order, note)
VALUES (sqlc.arg(id), sqlc.arg(itinerary_day_id), sqlc.arg(item_id), sqlc.arg(sort_order), sqlc.arg(note))
RETURNING *;

-- name: DeleteItineraryEntry :execrows
DELETE FROM itinerary_entries WHERE id = sqlc.arg(id) AND itinerary_day_id = sqlc.arg(itinerary_day_id);

-- name: ListItineraryEntriesByTrip :many
SELECT e.id, e.itinerary_day_id, e.item_id, e.sort_order, e.note,
       i.title AS item_title, i.category AS item_category, i.type AS item_type,
       i.image_id AS item_image_id
FROM itinerary_entries e
INNER JOIN itinerary_days d ON d.id = e.itinerary_day_id
INNER JOIN items i ON i.id = e.item_id
WHERE d.trip_id = sqlc.arg(trip_id)
ORDER BY e.sort_order;
