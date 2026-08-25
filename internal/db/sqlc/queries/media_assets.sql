-- name: CreateMediaAsset :one
INSERT INTO media_assets (id, trip_id, kind, storage_path, external_url, content_type, width, height, source_url, credit, license, created_at)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(kind), sqlc.arg(storage_path), sqlc.arg(external_url), sqlc.arg(content_type), sqlc.arg(width), sqlc.arg(height), sqlc.arg(source_url), sqlc.arg(credit), sqlc.arg(license), sqlc.arg(created_at))
RETURNING *;

-- name: GetMediaAssetByID :one
SELECT * FROM media_assets WHERE id = sqlc.arg(id);
