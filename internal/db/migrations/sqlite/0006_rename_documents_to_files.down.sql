DROP INDEX idx_files_trip_id;
DROP INDEX idx_files_item_id;

ALTER TABLE files RENAME TO documents;

CREATE INDEX idx_documents_trip_id ON documents(trip_id);
CREATE INDEX idx_documents_item_id ON documents(item_id);
