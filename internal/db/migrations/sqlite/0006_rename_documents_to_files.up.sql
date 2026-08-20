-- "Files" is what the UI has called these since Stage 09; the schema was the
-- last place still saying "documents" (Stage 11 Milestone 1).
--
-- SQLite has ALTER TABLE ... RENAME TO but no ALTER INDEX, so the two indexes
-- are dropped and recreated rather than renamed - the only place this
-- migration differs from its Postgres twin. Nothing moves in blob storage:
-- storage_path is recorded per row, so files uploaded before this keep
-- resolving from the key they were written under.
ALTER TABLE documents RENAME TO files;

DROP INDEX idx_documents_trip_id;
DROP INDEX idx_documents_item_id;

CREATE INDEX idx_files_trip_id ON files(trip_id);
CREATE INDEX idx_files_item_id ON files(item_id);
