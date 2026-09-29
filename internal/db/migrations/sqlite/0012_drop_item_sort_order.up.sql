-- Locations have no manual order, so items.sort_order goes.

-- The column has been in the schema since v1 and never grew a UI: nothing
-- reorders locations, no client ever sent or read the field, and the only
-- reader was the ORDER BY in ListItemsByTrip. What it did instead was break
-- that ordering. Three writers disagreed about its meaning -- the seeder wrote
-- the index, the batch endpoint wrote max plus one, and the single create that
-- the New location button uses wrote 0 -- so a location added by hand sorted
-- ahead of everything the assistant or the seeder had added, which is to say
-- in the middle of a list whose sort is called "As added".

-- Manual ordering that people do arrange lives on itinerary_entries, per day,
-- and keeps its own sort_order.

-- The list now orders by created_at, which is what "as added" meant all along.
-- SQLite stores that as TEXT, so see 0012 in the Postgres tree and the note on
-- timeLayout in internal/db/sqlite_store.go for why the layout had to gain a
-- fixed-width fractional part first.

-- SQLite has supported DROP COLUMN since 3.35 and the driver is far newer; the
-- column carries no index and no constraint, so no table rebuild is needed.
ALTER TABLE items DROP COLUMN sort_order;
