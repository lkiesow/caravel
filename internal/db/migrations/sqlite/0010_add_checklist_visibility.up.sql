-- Per-checklist visibility. Three states, where files have two, because a
-- checklist can be ticked and a file cannot:
--
--   personal  only its author sees it at all
--   trip      everyone on the trip sees it, only its author changes it
--   shared    everyone on the trip sees it and ticks it
--
-- The middle one is the reason this is not the files model with a third name
-- bolted on: seeing a list and being able to tick it are genuinely different
-- permissions once more than one person is looking at it.
--
-- Default is shared, and note that it differs from the files default for a
-- reason. A file is one person's document, so trip-visible-but-not-editable is
-- its natural resting state. A checklist is a job to be done together, and the
-- packing list everyone ticks is the case that made anyone want checklists at
-- all. Existing rows become shared, which is what they effectively were.
ALTER TABLE checklists ADD COLUMN visibility TEXT NOT NULL DEFAULT 'shared'
    CHECK (visibility IN ('personal', 'trip', 'shared'));

-- Whose list it is. Nullable for the same reason files.owner_user_id is: sqlite
-- cannot add a NOT NULL column without a constant default and the right value is
-- per-row. The backfill fills every existing row, the application always writes
-- it, and NULL fails closed everywhere it is compared.
ALTER TABLE checklists ADD COLUMN owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;

-- Every existing checklist was necessarily created by the trip owner: until
-- Stage 14 a trip had exactly one person who could reach it.
UPDATE checklists SET owner_user_id = (SELECT owner_id FROM trips WHERE trips.id = checklists.trip_id);

CREATE INDEX idx_checklists_owner_user_id ON checklists(owner_user_id);
