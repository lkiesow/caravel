-- Back to four categories.

-- Anything filed as food, an event or a shop becomes a site, which is where
-- such a place would have been filed before this migration existed. Lossy, and
-- unavoidable: the constraint being restored has no room for the values.
UPDATE items SET category = 'site' WHERE category IN ('food', 'event', 'shop');

ALTER TABLE items DROP CONSTRAINT items_category_check;
ALTER TABLE items ADD CONSTRAINT items_category_check
    CHECK (category IN ('site', 'stay', 'transport', 'area'));
