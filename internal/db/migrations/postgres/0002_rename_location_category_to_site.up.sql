ALTER TABLE items DROP CONSTRAINT items_category_check;

UPDATE items SET category = 'site' WHERE category = 'location';

ALTER TABLE items ADD CONSTRAINT items_category_check CHECK (category IN ('site', 'stay', 'transport'));
