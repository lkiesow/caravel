-- Instance-wide settings an admin changes at runtime. See migration 0008 for
-- why this is a key/value table rather than one column per setting, and why the
-- column is called "name" rather than "key".
--
-- Note the quotes above are not stylistic: backticks anywhere in a comment in a
-- query file break sqlc's sqlite lexer, which reads them as identifier quotes,
-- swallows the line boundary, and then reports a syntax error pointing at the
-- SQL below. See CLAUDE.md's gotchas.

-- name: GetAppSetting :one
SELECT value FROM app_settings WHERE name = sqlc.arg(name);

-- Upsert, because a caller setting a value does not care whether the row
-- already existed, and a migration seeding a default must not make the first
-- write a special case.
-- name: SetAppSetting :exec
INSERT INTO app_settings (name, value) VALUES (sqlc.arg(name), sqlc.arg(value))
ON CONFLICT (name) DO UPDATE SET value = excluded.value;
