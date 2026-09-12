-- A fourth location category: area.

-- The three categories were site, stay and transport -- a place to visit, a
-- place to sleep, a way of getting there. None of them fits a district, a
-- valley or a national park: a thing you are in rather than a point you go to.
-- It gets its own colour on the map for the same reason the other three have
-- one.

-- The constraint in 0001 is unnamed, so Postgres named it itself, by its own
-- rule: <table>_<column>_check. The SQLite side has to rebuild the whole table
-- for the same change.
ALTER TABLE items DROP CONSTRAINT items_category_check;
ALTER TABLE items ADD CONSTRAINT items_category_check
    CHECK (category IN ('site', 'stay', 'transport', 'area'));
