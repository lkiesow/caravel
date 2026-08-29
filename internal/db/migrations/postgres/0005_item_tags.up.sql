-- Keywords on a location, whose meaning the user chooses.
--
-- The locations tab can already filter by category and by distance. Neither
-- helps on a long trip, because the questions people actually have are about
-- their own structure -- what is in Reykjavik, what is a waterfall, what did
-- the kids ask for -- and no fixed vocabulary the app ships could answer them.
-- So the value is free text and the app never interprets it.
--
-- Text rows, not a tags table with ids. A tag has no properties beyond its own
-- name: nothing hangs off it, nothing else references it, and a tags table
-- would buy one thing, cheap renaming, at the cost of an orphan-row lifecycle
-- to own -- deciding when the last location drops a tag and the row should go.
-- Renaming a tag across a trip is one UPDATE over this column, which is a
-- feature nobody has asked for yet and would not be hard when they do.
--
-- The primary key is the pair, so a client that sends the same tag twice for
-- one location cannot store it twice. That is the same reason expense_shares
-- keys on its pair.
--
-- Tags are stored as typed, trimmed. Deduplication is case-insensitive within
-- one location, but two locations may carry Museum and museum: the editor
-- suggests the tags already used on the trip, so spellings converge by use
-- rather than by a rule that would have to pick a winner and rewrite what
-- somebody typed.
CREATE TABLE item_tags (
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    PRIMARY KEY (item_id, tag)
);

-- The item_id half of the key already serves the tags of one location. This is
-- for the other direction: every location on a trip carrying a given tag, and
-- the distinct tag list the filter menu and the editor suggestions are built
-- from.
CREATE INDEX idx_item_tags_tag ON item_tags(tag);
