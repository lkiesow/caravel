-- name: CreateSession :one
INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen_at, user_agent, ip)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(created_at), sqlc.arg(expires_at), sqlc.arg(last_seen_at), sqlc.arg(user_agent), sqlc.arg(ip))
RETURNING *;

-- name: GetSessionByID :one
SELECT * FROM sessions WHERE id = sqlc.arg(id);

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = sqlc.arg(last_seen_at), expires_at = sqlc.arg(expires_at) WHERE id = sqlc.arg(id);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = sqlc.arg(id);

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < sqlc.arg(now);

-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = sqlc.arg(user_id);
