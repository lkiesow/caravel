-- The owner is deliberately absent from trip_members (see migration 0007), so
-- every query here answers only "what does this *non-owner* have on this trip?"
-- The owner's role is decided from trips.owner_id, by the caller.

-- name: GetTripMember :one
SELECT * FROM trip_members
WHERE trip_id = sqlc.arg(trip_id) AND user_id = sqlc.arg(user_id);

-- Joined to users because the members list is a list of people, not of ids,
-- and rendering it otherwise would be an N+1 lookup per row.
-- name: ListTripMembers :many
SELECT m.trip_id, m.user_id, m.role, m.created_at,
       u.username, u.display_name
FROM trip_members m
JOIN users u ON u.id = m.user_id
WHERE m.trip_id = sqlc.arg(trip_id)
ORDER BY u.display_name, u.username;

-- Upsert rather than insert: changing someone's role and adding them are the
-- same intent expressed twice, and a conflict here is never an error worth
-- reporting - the caller asked for a state, not for an event.
-- name: UpsertTripMember :one
INSERT INTO trip_members (trip_id, user_id, role, created_at)
VALUES (sqlc.arg(trip_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(created_at))
ON CONFLICT (trip_id, user_id) DO UPDATE SET role = excluded.role
RETURNING *;

-- name: DeleteTripMember :execrows
DELETE FROM trip_members
WHERE trip_id = sqlc.arg(trip_id) AND user_id = sqlc.arg(user_id);

-- name: CountTripMembers :one
SELECT COUNT(*) FROM trip_members WHERE trip_id = sqlc.arg(trip_id);
