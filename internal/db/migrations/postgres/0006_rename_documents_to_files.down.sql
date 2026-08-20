ALTER INDEX idx_files_trip_id RENAME TO idx_documents_trip_id;
ALTER INDEX idx_files_item_id RENAME TO idx_documents_item_id;

ALTER TABLE files RENAME TO documents;
