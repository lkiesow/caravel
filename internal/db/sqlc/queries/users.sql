-- name: CreateUser :one
INSERT INTO users (id, username, display_name, email, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(username), sqlc.arg(display_name), sqlc.arg(email), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = sqlc.arg(username);
