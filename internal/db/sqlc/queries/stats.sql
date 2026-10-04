-- Instance-wide totals for the metrics endpoint, one row per scrape.
-- Every column is cast so both dialects generate a plain int64.

-- name: InstanceCounts :one
SELECT
    CAST((SELECT COUNT(*) FROM users) AS BIGINT) AS users,
    CAST((SELECT COUNT(*) FROM trips) AS BIGINT) AS trips,
    CAST((SELECT COUNT(*) FROM files) AS BIGINT) AS files,
    CAST((SELECT COALESCE(SUM(size_bytes), 0) FROM files) AS BIGINT) AS file_bytes,
    CAST((SELECT COUNT(*) FROM expenses) AS BIGINT) AS expenses;

-- name: CountLocationsByCategory :many
SELECT category, CAST(COUNT(*) AS BIGINT) AS location_count
FROM items
GROUP BY category
ORDER BY category;
