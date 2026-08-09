-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (id, user_id, provider, provider_user_id, password_hash, created_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(provider), sqlc.arg(provider_user_id), sqlc.arg(password_hash), sqlc.arg(created_at))
RETURNING *;

-- name: GetAuthIdentityByProvider :one
SELECT * FROM auth_identities WHERE provider = sqlc.arg(provider) AND provider_user_id = sqlc.arg(provider_user_id);
