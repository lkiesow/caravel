DROP INDEX idx_checklists_owner_user_id;
ALTER TABLE checklists DROP COLUMN owner_user_id;
ALTER TABLE checklists DROP COLUMN visibility;
