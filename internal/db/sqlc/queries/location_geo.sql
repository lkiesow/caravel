-- name: InsertLocationGeo :one
INSERT INTO location_geo (id, location_id, lat, lng, address, osm_type, osm_id)
VALUES (sqlc.arg(id), sqlc.arg(location_id), sqlc.arg(lat), sqlc.arg(lng), sqlc.arg(address), sqlc.arg(osm_type), sqlc.arg(osm_id))
RETURNING *;

-- name: UpdateLocationGeo :execrows
UPDATE location_geo
SET lat = sqlc.arg(lat), lng = sqlc.arg(lng), address = sqlc.arg(address),
    osm_type = sqlc.arg(osm_type), osm_id = sqlc.arg(osm_id)
WHERE location_id = sqlc.arg(location_id);

-- name: GetLocationGeoByLocationID :one
SELECT * FROM location_geo WHERE location_id = sqlc.arg(location_id);
