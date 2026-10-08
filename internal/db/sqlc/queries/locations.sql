-- name: CreateLocation :one
INSERT INTO locations (id, trip_id, category, title, notes, show_on_map, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(category), sqlc.arg(title), sqlc.arg(notes), sqlc.arg(show_on_map), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

-- name: GetLocationByID :one
SELECT * FROM locations WHERE id = sqlc.arg(id);

-- name: ListLocationsByTrip :many
-- The CAST around the optional category is required, not decoration. Without
-- it the generated Postgres query reads AND ($2 IS NULL OR category = $2) with
-- an untyped parameter, and the server refuses it at prepare time: could not
-- determine data type of parameter $2 (SQLSTATE 42P08). SQLite is happy either
-- way, which is why this shipped broken -- see internal/dbtest.
-- CAST rather than the :: form because both dialects have to parse this file.
SELECT * FROM locations
WHERE trip_id = sqlc.arg(trip_id)
  AND (CAST(sqlc.narg(category) AS text) IS NULL OR category = CAST(sqlc.narg(category) AS text))
-- Creation order, which is what the As added sort in the locations tab means.
-- Until migration 0012 this read ORDER BY sort_order, created_at, and the
-- sort_order column was the reason that sort was wrong. The id breaks a tie so
-- the order is total and a list does not shuffle between two reads.
ORDER BY created_at, id;

-- name: UpdateLocation :one
UPDATE locations
SET category = sqlc.arg(category),
    title = sqlc.arg(title),
    notes = sqlc.arg(notes),
    show_on_map = sqlc.arg(show_on_map),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- name: DeleteLocation :execrows
DELETE FROM locations WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);

-- name: SetLocationImage :one
UPDATE locations
SET image_id = sqlc.arg(image_id), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- ListLocationCoordinatesByTrip: every coordinate on the trip, keyed by
-- location.
--
-- Deliberately NOT ListMapLocationsByTrip below: that one also filters
-- show_on_map, which is about whether a place is drawn on the map and says
-- nothing about whether it has a position. The locations list filters by
-- distance, so it wants every location with coordinates regardless.
--
-- Rows with only an address and no coordinates are excluded here rather than
-- in Go: they are not "far away", they are unmeasurable, and the caller has
-- to be able to tell those apart.
-- name: ListLocationCoordinatesByTrip :many
SELECT g.location_id, g.lat, g.lng
FROM location_geo g
INNER JOIN locations loc ON loc.id = g.location_id
WHERE loc.trip_id = sqlc.arg(trip_id) AND g.lat IS NOT NULL AND g.lng IS NOT NULL;

-- ListMapLocationsByTrip: show_on_map is filtered in the store layer, not here,
-- since its Go type (int64 vs bool) diverges by dialect (plan Section 2.1).
--
-- The address is selected for the outbound Google Maps link, which names the
-- place rather than dropping a pin at a coordinate (Stage 29). A popup that
-- linked to a coordinate while the same location page linked to the named
-- place would be the inconsistency Milestone 1 just removed.
--
-- image_id is selected so the popup can show the same photo the location page
-- shows. It is the id, not a URL -- media assets are resolved through
-- resolveImageURL in the API layer, the way the itinerary list does it.
-- name: ListMapLocationsByTrip :many
SELECT loc.id, loc.category, loc.title, loc.show_on_map, loc.image_id, g.lat, g.lng, g.address
FROM locations loc
INNER JOIN location_geo g ON g.location_id = loc.id
WHERE loc.trip_id = sqlc.arg(trip_id) AND g.lat IS NOT NULL AND g.lng IS NOT NULL
-- Creation order, as in the locations list. Without an ORDER BY the markers
-- came out in whatever order the index scan gave, which on SQLite was sorted
-- by category name and on Postgres is not promised at all.
ORDER BY loc.created_at, loc.id;
