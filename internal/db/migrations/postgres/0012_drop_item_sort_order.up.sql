-- Locations have no manual order, so items.sort_order goes.

-- See the SQLite side for the full account. In short: the column has been in
-- the schema since v1, never grew a UI, was never sent or read by any client,
-- and its only reader was the ORDER BY in ListItemsByTrip -- which it broke,
-- because the three writers disagreed about what to put in it.

-- The list now orders by created_at instead. This dialect stores that as
-- TIMESTAMPTZ and has always sorted it correctly; it is SQLite, storing TEXT,
-- that needed a layout change to go with this.
ALTER TABLE items DROP COLUMN sort_order;
