-- Trip membership beyond the owner. The owner is NOT stored here: trips.owner_id
-- stays authoritative, so the two can never disagree and no code path can demote
-- an owner by touching a membership. The CHECK makes 'owner' unrepresentable.
CREATE TABLE trip_members (
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('editor', 'viewer')),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (trip_id, user_id)
);

-- The trips list asks "which trips is this user a member of?", so user_id needs
-- its own index; trip_id is already covered by the primary key's leading column.
CREATE INDEX idx_trip_members_user_id ON trip_members(user_id);
