-- name: CreateExpense :one
INSERT INTO expenses (id, trip_id, title, amount_minor, spent_on, payer_user_id, item_id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(title), sqlc.arg(amount_minor), sqlc.arg(spent_on), sqlc.arg(payer_user_id), sqlc.arg(item_id), sqlc.arg(created_at))
RETURNING *;

-- name: GetExpenseByID :one
SELECT * FROM expenses WHERE id = sqlc.arg(id);

-- Newest spending first, and created_at breaks the tie so two expenses on the
-- same day have a stable order rather than whatever the planner returns.
--
-- No visibility predicate, unlike the file and checklist listings: every
-- expense on a trip is visible to everyone on it. That is deliberate -- hidden
-- rows in a shared ledger make an incorrect total look correct.
-- name: ListExpensesByTrip :many
SELECT * FROM expenses
WHERE trip_id = sqlc.arg(trip_id)
ORDER BY spent_on DESC, created_at DESC;

-- The total the trip has spent, in minor units. Answered by the database
-- rather than by summing the list in Go, so a client showing a page of rows
-- still gets the whole total. COALESCE because SUM over no rows is NULL.
--
-- The CAST is not decoration. Without it sqlc cannot type the expression and
-- generates a method returning interface{} in both dialects, which compiles
-- and then needs a type assertion at every call site -- and the underlying
-- type differs per dialect, so the assertion would be right in one and panic
-- in the other. Same trick as the role column in ListTripsForUser.
-- name: SumExpensesByTrip :one
SELECT CAST(COALESCE(SUM(amount_minor), 0) AS BIGINT) AS total_minor FROM expenses
WHERE trip_id = sqlc.arg(trip_id);

-- The trip_id predicate is not redundant with the id: it is what stops an
-- expense id from one trip being edited through another trip authorization.
-- The same belt as DeleteChecklist and DeleteExpense below.
-- name: UpdateExpense :one
UPDATE expenses
SET title = sqlc.arg(title),
    amount_minor = sqlc.arg(amount_minor),
    spent_on = sqlc.arg(spent_on),
    payer_user_id = sqlc.arg(payer_user_id),
    item_id = sqlc.arg(item_id)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- name: DeleteExpense :execrows
DELETE FROM expenses WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);

-- name: CreateExpenseShare :exec
INSERT INTO expense_shares (expense_id, user_id)
VALUES (sqlc.arg(expense_id), sqlc.arg(user_id));

-- name: ListExpenseSharesByExpense :many
SELECT user_id FROM expense_shares WHERE expense_id = sqlc.arg(expense_id) ORDER BY user_id;

-- Every share on a trip in one query. The list endpoint needs the shares for
-- each of its rows, and asking per expense is a query per row.
-- name: ListExpenseSharesByTrip :many
SELECT s.expense_id, s.user_id FROM expense_shares s
JOIN expenses e ON e.id = s.expense_id
WHERE e.trip_id = sqlc.arg(trip_id)
ORDER BY s.expense_id, s.user_id;

-- The share set is replaced as a whole rather than patched member by member, so
-- an update deletes and reinserts inside one transaction. Two people editing
-- the same expense then produce one set or the other, never a mixture.
-- name: DeleteExpenseSharesByExpense :exec
DELETE FROM expense_shares WHERE expense_id = sqlc.arg(expense_id);
