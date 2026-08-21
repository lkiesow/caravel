DROP INDEX idx_files_owner_user_id;
ALTER TABLE files DROP COLUMN owner_user_id;
ALTER TABLE files DROP COLUMN visibility;
