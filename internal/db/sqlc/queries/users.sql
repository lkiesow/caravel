-- name: CreateUser :one
INSERT INTO users (id, username, display_name, email, is_admin, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(username), sqlc.arg(display_name), sqlc.arg(email), sqlc.arg(is_admin), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

-- Used for exactly one decision, in two places: whether this is the first
-- account on the instance, which both makes it an admin and is the one case
-- where a closed registration still accepts a signup.
-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = sqlc.arg(username);

-- Substring match rather than prefix: people look for "user" in "Other User"
-- as readily as they type the start of a username, and on an instance this size
-- the difference in selectivity does not matter.
--
-- LOWER on both sides rather than ILIKE or a bare LIKE: sqlite's LIKE is
-- case-insensitive for ASCII while postgres' is case-sensitive, and ILIKE is
-- not valid sqlite. Normalising explicitly is the only way to get the same
-- behaviour from one statement in both dialects, which matters more than usual
-- here since nothing in this project ever runs the postgres one.
--
-- Two tooling constraints shaped this statement, both worth knowing before
-- editing it:
--
-- 1. The parentheses around each comparison are load-bearing for sqlc rather
--    than for the SQL. Without them its sqlite grammar rejects the two OR-ed
--    comparisons outright. Do not tidy them away.
--
-- 2. There is no ESCAPE clause, so LIKE's own % and _ pass through from
--    whatever the user typed. Deliberate rather than overlooked: sqlc's sqlite
--    grammar rejects an ESCAPE clause too, and escaping without one would work
--    in postgres (backslash is its default) and silently not work in sqlite
--    (which has no default). One documented behaviour beats two dialects
--    quietly disagreeing. Nothing is at risk either way: the query is
--    parameterised, so this is about search semantics only, and the widest
--    thing a lone % can do is return the same first rows any two-letter query
--    would.
--
-- Also, and this cost half an hour: do not put backticks in a comment in this
-- file. sqlc's sqlite lexer treats them as identifier quotes even inside a
-- '--' comment, swallows the line boundary, and reports a syntax error
-- pointing at the SQL below rather than at the comment.
-- name: SearchUsers :many
SELECT id, username, display_name FROM users
WHERE (LOWER(username) LIKE sqlc.arg(pattern)) OR (LOWER(display_name) LIKE sqlc.arg(pattern))
ORDER BY username
LIMIT sqlc.arg(max_results);

-- The admin user list. The trip count comes from a correlated subquery rather
-- than a LEFT JOIN with GROUP BY: it cannot duplicate a user row, and it counts
-- only trips they own -- trips shared with them belong to someone else and
-- would be misleading in a what-will-deleting-this-account-destroy column.
-- name: ListUsers :many
SELECT u.id, u.username, u.display_name, u.is_admin, u.created_at,
       (SELECT COUNT(*) FROM trips t WHERE t.owner_id = u.id) AS trip_count
FROM users u
ORDER BY u.username;

-- Compared against a bound parameter rather than against a literal: the column
-- is INTEGER in sqlite and BOOLEAN in postgres, so the comparison value has to
-- come from the store layer, which is the only place that knows which dialect
-- it is talking to.
-- name: CountAdmins :one
SELECT COUNT(*) FROM users WHERE is_admin = sqlc.arg(flag);

-- Username is deliberately not updatable. It is the handle people are added to
-- trips by, so renaming one silently breaks the mental model of everyone who
-- knows them by it, and there is no rename flow anywhere in the UI to make that
-- visible.
-- name: UpdateUser :one
UPDATE users
SET display_name = sqlc.arg(display_name),
    is_admin = sqlc.arg(is_admin),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = sqlc.arg(id);
