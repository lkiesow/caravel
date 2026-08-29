-- A location dates are its days on the itinerary, so the table that held them
-- separately goes away.
--
-- Until Stage 25 there were two unrelated ways to say when a location happened.
-- item_dates hung off the item and was read by exactly one screen -- the Dates
-- card on the location page. itinerary_entries joined days to items and drove
-- the itinerary tab. Nothing connected them, so a hotel set to 5-7 September
-- showed a date on its own page and left those three itinerary days empty,
-- which is the bug that opened the stage. stage-01.md had anticipated the gap
-- and proposed a one-shot bridge between the two; that bridge was never built,
-- and a second source of truth is what it would have institutionalised.
--
-- The itinerary won because it holds strictly more: an entry knows its position
-- within its day and can carry a note, and a day can carry notes of its own.
-- What a location page calls its dates is now those days with consecutive runs
-- collapsed into ranges, computed on read.
--
-- The rows here are DISCARDED rather than migrated. Expanding a range into one
-- entry per day is easy enough, but the two models have disagreed for as long
-- as both existed, and no rule for reconciling them would be more than a guess
-- at what somebody meant. Caravel has published no release, and
-- docs/running/upgrading.md already says a pre-release database cannot be
-- carried forward, so the honest thing is to drop them and say so.
--
-- The label, all_day, start_time and end_time columns go with the table. None
-- of them ever had a UI. A time on an itinerary entry is a real feature and is
-- in the backlog; it belongs on the entry, since a 15:00 check-in is a fact
-- about a day rather than about a range.
DROP INDEX IF EXISTS idx_item_dates_item_id;
DROP TABLE IF EXISTS item_dates;

-- ListItineraryDatesByItem filters on item_id, which had no index of its own --
-- the only one on this table is by day. It also helps the cascade when a
-- location is deleted.
CREATE INDEX idx_itinerary_entries_item_id ON itinerary_entries(item_id);
