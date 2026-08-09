ALTER TABLE items DROP CONSTRAINT items_category_check;

UPDATE items SET category = 'location' WHERE category = 'site';

ALTER TABLE items ADD CONSTRAINT items_category_check CHECK (category IN ('location', 'stay', 'transport'));
