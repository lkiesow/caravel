-- "Files" is what the UI has called these since Stage 09; the schema was the
-- last place still saying "documents" (Stage 11 Milestone 1).
--
-- Postgres carries indexes across a table rename and can rename them in place,
-- so unlike the SQLite twin nothing is dropped here. Nothing moves in blob
-- storage either: storage_path is recorded per row, so files uploaded before
-- this keep resolving from the key they were written under.
ALTER TABLE documents RENAME TO files;

ALTER INDEX idx_documents_trip_id RENAME TO idx_files_trip_id;
ALTER INDEX idx_documents_item_id RENAME TO idx_files_item_id;
