-- Account administration. Note what is_admin is NOT: it governs users, not
-- data. An admin gets no access to anyone else's trips — Server.tripRole never
-- consults it — because a "personal" file the instance operator can read is not
-- a personal file.
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- Instance-wide settings that an admin can change at runtime, which an
-- environment variable cannot be. Only one key so far, but a key/value table
-- rather than a column-per-setting table with a single row: the next setting
-- should not need a migration.
-- The column is `name`, not `key`: sqlc's sqlite parser mis-lexes `key` in an
-- INSERT ... VALUES clause (and it is a reserved word in other dialects
-- besides), so the generator cannot produce a setter for it at all.
CREATE TABLE app_settings (
    name TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Registration is closed by default. The first account is still possible —
-- handleRegister also allows a signup when no users exist at all, which is how
-- a fresh instance is bootstrapped — so a closed door here does not mean an
-- unusable install.
INSERT INTO app_settings (name, value) VALUES ('open_signup', 'false');
