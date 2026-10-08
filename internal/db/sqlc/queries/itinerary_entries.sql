-- name: CreateItineraryEntry :one
INSERT INTO itinerary_entries (id, itinerary_day_id, location_id, sort_order, note)
VALUES (sqlc.arg(id), sqlc.arg(itinerary_day_id), sqlc.arg(location_id), sqlc.arg(sort_order), sqlc.arg(note))
RETURNING *;

-- name: DeleteItineraryEntry :execrows
DELETE FROM itinerary_entries WHERE id = sqlc.arg(id) AND itinerary_day_id = sqlc.arg(itinerary_day_id);

-- name: ListItineraryEntriesByTrip :many
SELECT e.id, e.itinerary_day_id, e.location_id, e.sort_order, e.note,
       loc.title AS location_title, loc.category AS location_category,
       loc.image_id AS location_image_id
FROM itinerary_entries e
INNER JOIN itinerary_days d ON d.id = e.itinerary_day_id
INNER JOIN locations loc ON loc.id = e.location_id
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

-- The days one location appears on, which is what a location date range is
-- made of since Stage 25. There is no separate table of dates on a location
-- any more: the itinerary is the record, and the ranges the location page shows
-- are these dates with contiguous runs collapsed in Go.
--
-- Nothing stops a location from being on one day twice -- there is no unique
-- constraint on the pair -- so duplicate dates come back as they are and the
-- caller reduces them to a set. The entry id and day id ride along because the
-- reconcile path needs to delete exact rows, not dates.
-- name: ListItineraryDatesByLocation :many
SELECT e.location_id, e.id AS entry_id, e.itinerary_day_id AS day_id, e.sort_order, d.date
FROM itinerary_entries e
INNER JOIN itinerary_days d ON d.id = e.itinerary_day_id
WHERE e.location_id = sqlc.arg(location_id)
ORDER BY d.date, e.sort_order;

-- Every dated location on a trip in one query, for the locations list.
--
-- The by-location version above answers one location, which is right for the
-- location page. Calling it once per card is a query per location, so the list
-- uses this and buckets the rows by location in Go -- the same shape as
-- ListLocationCoordinates and ListLocationTagsByTrip.
--
-- Joined through locations rather than through itinerary_days, because the
-- trip is reachable either way but only this direction also excludes an entry
-- whose location somehow belongs to another trip.
-- name: ListLocationDatesByTrip :many
SELECT e.location_id, e.id AS entry_id, e.itinerary_day_id AS day_id, e.sort_order, d.date
FROM itinerary_entries e
INNER JOIN itinerary_days d ON d.id = e.itinerary_day_id
INNER JOIN locations loc ON loc.id = e.location_id
WHERE loc.trip_id = sqlc.arg(trip_id)
ORDER BY e.location_id, d.date, e.sort_order;
