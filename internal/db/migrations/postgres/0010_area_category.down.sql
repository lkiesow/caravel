-- Back to three categories.

-- Anything filed as an area becomes a site, which is where such a place would
-- have been filed before this migration existed. Lossy, and unavoidable: the
-- constraint being restored has no room for the value.
UPDATE items SET category = 'site' WHERE category = 'area';

ALTER TABLE items DROP CONSTRAINT items_category_check;
ALTER TABLE items ADD CONSTRAINT items_category_check
    CHECK (category IN ('site', 'stay', 'transport'));
