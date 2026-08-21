-- name: CreateUser :one
INSERT INTO users (id, username, display_name, email, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(username), sqlc.arg(display_name), sqlc.arg(email), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING *;

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
