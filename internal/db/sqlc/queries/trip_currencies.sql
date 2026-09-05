-- The extra currencies configured on one trip, each with its rate into the
-- trip main currency. Ordered by code so the settings form and the expense
-- picker list them the same way every time.
-- name: ListTripCurrencies :many
SELECT * FROM trip_currencies
WHERE trip_id = sqlc.arg(trip_id)
ORDER BY code;

-- name: CreateTripCurrency :one
INSERT INTO trip_currencies (trip_id, code, rate_ppb, created_at)
VALUES (sqlc.arg(trip_id), sqlc.arg(code), sqlc.arg(rate_ppb), sqlc.arg(created_at))
RETURNING *;

-- The set is replaced as a whole rather than patched code by code, so a save
-- deletes and reinserts inside one transaction -- the same shape as
-- DeleteExpenseSharesByExpense above it in expenses.sql, and for the same
-- reason. Two people editing the rates then produce one set or the other,
-- never a mixture.
-- name: DeleteTripCurrenciesByTrip :exec
DELETE FROM trip_currencies WHERE trip_id = sqlc.arg(trip_id);

-- How many expenses on this trip are recorded in each currency, for the guard
-- that refuses to remove a currency still in use. Rows in the trip main
-- currency store NULL and are not counted here: that code cannot be removed
-- through this table anyway.
--
-- The CAST is the same necessity as in SumExpensesByTrip: without it sqlc
-- types the count as interface{} and the underlying type differs per dialect.
-- name: CountExpensesByCurrency :many
SELECT currency, CAST(COUNT(*) AS BIGINT) AS expense_count FROM expenses
WHERE trip_id = sqlc.arg(trip_id) AND currency IS NOT NULL
GROUP BY currency
ORDER BY currency;
