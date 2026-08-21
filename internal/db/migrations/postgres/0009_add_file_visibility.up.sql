-- Per-file visibility, so a boarding pass or an identity card can go on a
-- shared trip without everyone on it seeing the file.
--
-- Two values, not three. Checklists get personal/trip/shared because a
-- checklist can be *ticked* and "everyone sees it" and "everyone may change it"
-- are different questions. A file has no equivalent second axis: who may edit
-- its note or delete it already follows the trip role.
--
-- Default is trip, deliberately, and it is the decision in this stage most
-- worth revisiting once it has been used. An invisible privacy default produces
-- "where did my upload go?" rather than safety, and every file on a solo trip
-- would be born private for no reason. The risk it would guard against is
-- handled instead by making the choice visible at upload time and marking
-- personal rows with a lock.
ALTER TABLE files ADD COLUMN visibility TEXT NOT NULL DEFAULT 'trip'
    CHECK (visibility IN ('personal', 'trip'));

-- Who uploaded it, which is who a personal file belongs to.
--
-- Nullable rather than NOT NULL: sqlite cannot add a NOT NULL column without a
-- constant default, and the correct value here is per-row (each file's trip
-- owner). The backfill below fills every existing row and the application
-- always sets it, so NULL means only "a bug wrote this row". That case fails
-- closed: the visibility predicate compares owner_user_id against the reading
-- user, and NULL matches nobody, so a personal file with no owner is visible to
-- no one rather than to everyone. Worth making NOT NULL when the migrations are
-- squashed before the first release.
ALTER TABLE files ADD COLUMN owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;

-- Existing files predate any notion of an uploader, and every one of them was
-- necessarily uploaded by the trip owner: until this stage a trip had exactly
-- one person who could reach it.
UPDATE files SET owner_user_id = (SELECT owner_id FROM trips WHERE trips.id = files.trip_id);

CREATE INDEX idx_files_owner_user_id ON files(owner_user_id);
