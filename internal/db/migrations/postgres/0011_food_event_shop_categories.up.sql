-- Three more location categories: food, event and shop.

-- Four categories left three common kinds of place with nowhere to go, so all
-- three were filed as sites: a restaurant or bar, an event you go to for one
-- evening rather than a place that is always open, and a shop or market. Each
-- is something you look for on the map as a group, which is what a category is
-- for -- tags stay the axis for anything finer.

-- The constraint is the one 0010 named explicitly; the SQLite side has to
-- rebuild the whole table for the same change.
ALTER TABLE items DROP CONSTRAINT items_category_check;
ALTER TABLE items ADD CONSTRAINT items_category_check
    CHECK (category IN ('site', 'stay', 'transport', 'area', 'food', 'event', 'shop'));
