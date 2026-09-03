-- A single free-text markdown document per trip.
--
-- Everything else a trip holds is structured: a location has coordinates, an
-- itinerary day has a date, a checklist item is done or it is not. The prose
-- that goes with planning a trip -- visa paperwork, why the northern route was
-- ruled out, what somebody said about the ferry -- has had nowhere to live, so
-- it ends up wedged into a location note that is not about that location, or
-- an itinerary day it does not belong to.
--
-- trip_id is the primary key, not a column beside an id of its own. There is
-- exactly one note per trip, so the table cannot represent a second one and
-- the API needs no note id: the resource is /trips/{tripId}/notes.
--
-- No visibility column, unlike checklists and files. Those are documents one
-- person owns and may or may not want to show; this is the trip notepad, and a
-- notepad only half the trip can read is a different feature from the one
-- being built here. Adding personal notes later means a new table, not a
-- column on this one.
--
-- The body is stored as the markdown the user typed. Rendering happens on read
-- through internal/markdown, the same call the item notes go through, so there
-- is no second column here that could drift from its source.
CREATE TABLE trip_notes (
    trip_id TEXT PRIMARY KEY REFERENCES trips(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    -- Who saved last. Nothing reads this yet -- the tab shows no byline -- but
    -- it is written from the first save, because a column added later can
    -- record only edits made after it existed.
    --
    -- Nullable, and ON DELETE SET NULL, for the same reason files.owner_user_id
    -- is: a note outlives the account of whoever last touched it.
    updated_by TEXT REFERENCES users(id) ON DELETE SET NULL
);
