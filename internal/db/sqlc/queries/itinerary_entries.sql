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

-- Entries of one day, in their stored order. Used to number a new entry and to
-- validate a reorder against the set of entries the day actually has.
-- name: ListItineraryEntriesByDay :many
SELECT * FROM itinerary_entries
WHERE itinerary_day_id = sqlc.arg(itinerary_day_id)
ORDER BY sort_order;

-- The day id is part of the predicate, not just the id: it keeps a reorder from
-- renumbering an entry that belongs to a different day.
-- name: SetItineraryEntrySortOrder :execrows
UPDATE itinerary_entries
SET sort_order = sqlc.arg(sort_order)
WHERE id = sqlc.arg(id) AND itinerary_day_id = sqlc.arg(itinerary_day_id);

-- Moving an entry to another day. Both columns change together: an entry that
-- arrives on a new day needs a place in that day order, and leaving the old
-- number behind would put it in the middle of the target day rather than at
-- the end. The caller renumbers both days afterwards.
--
-- The predicate names the day the entry is expected to be on -- the same belt
-- as SetItineraryEntrySortOrder above. Zero rows means the entry moved under
-- the caller, which a move must treat as a conflict rather than as success.
-- name: SetItineraryEntryDay :execrows
UPDATE itinerary_entries
SET itinerary_day_id = sqlc.arg(to_itinerary_day_id),
    sort_order = sqlc.arg(sort_order)
WHERE id = sqlc.arg(id) AND itinerary_day_id = sqlc.arg(from_itinerary_day_id);
