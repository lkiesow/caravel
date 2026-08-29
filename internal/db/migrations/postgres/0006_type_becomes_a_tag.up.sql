-- The free-text type becomes a tag, and the column goes.
--
-- items.type was always a tag list that could hold exactly one tag. It was
-- TEXT NOT NULL DEFAULT '' with no validation anywhere, no lookup table and no
-- enum -- domain.go described it in so many words as a free-text tag -- and the
-- UI offered it as a single text box beside the category. Stage 26 added real
-- tags, which left the app with two overlapping free-text classification
-- fields, one of which could hold one value. This removes the weaker one.
--
-- Unlike Stage 25 dropping item_dates, nothing is being discarded: every
-- non-empty type becomes a tag on the same location, which is lossless and
-- mechanical because the two meant the same thing.
--
-- The NOT EXISTS guard is a case-insensitive check, not a plain ON CONFLICT.
-- The primary key on (item_id, tag) is exact, so a location tagged "hotel"
-- whose type reads "Hotel" would otherwise end up carrying both -- and that is
-- a state the application itself never produces, since it deduplicates a
-- location tag set case-insensitively on write. Skipping the type in that case
-- keeps the spelling somebody chose as a tag rather than adding a second one.
INSERT INTO item_tags (item_id, tag)
SELECT i.id, TRIM(i.type)
FROM items i
WHERE TRIM(i.type) <> ''
  AND NOT EXISTS (
    SELECT 1 FROM item_tags t
    WHERE t.item_id = i.id AND LOWER(t.tag) = LOWER(TRIM(i.type))
  );

-- SQLite has supported this since 3.35 and the bundled driver reports 3.53, so
-- no table rebuild is needed and the index on (trip_id, category) survives.
ALTER TABLE items DROP COLUMN type;
