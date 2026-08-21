-- name: CreateChecklist :one
INSERT INTO checklists (id, trip_id, title, sort_order, created_at, visibility, owner_user_id)
VALUES (sqlc.arg(id), sqlc.arg(trip_id), sqlc.arg(title), sqlc.arg(sort_order), sqlc.arg(created_at), sqlc.arg(visibility), sqlc.arg(owner_user_id))
RETURNING *;

-- name: GetChecklistByID :one
SELECT * FROM checklists WHERE id = sqlc.arg(id);

-- A personal list belongs to whoever created it and never appears in anyone
-- other listing. The same predicate guards loadChecklist, for the reason the
-- files one is written twice: this hides a list, that stops a remembered id from
-- reaching it.
--
-- A NULL owner_user_id matches nobody, which is the intended failure. See
-- migration 0010.
-- name: ListChecklistsByTrip :many
SELECT * FROM checklists
WHERE trip_id = sqlc.arg(trip_id)
  AND (visibility <> 'personal' OR owner_user_id = sqlc.arg(user_id))
ORDER BY sort_order;

-- name: DeleteChecklist :execrows
DELETE FROM checklists WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id);

-- name: CreateChecklistItem :one
INSERT INTO checklist_items (id, checklist_id, text, checked, sort_order, created_at)
VALUES (sqlc.arg(id), sqlc.arg(checklist_id), sqlc.arg(text), sqlc.arg(checked), sqlc.arg(sort_order), sqlc.arg(created_at))
RETURNING *;

-- name: ListChecklistItemsByChecklist :many
SELECT * FROM checklist_items WHERE checklist_id = sqlc.arg(checklist_id) ORDER BY sort_order;

-- name: SetChecklistItemChecked :one
UPDATE checklist_items SET checked = sqlc.arg(checked) WHERE id = sqlc.arg(id) AND checklist_id = sqlc.arg(checklist_id)
RETURNING *;

-- name: DeleteChecklistItem :execrows
DELETE FROM checklist_items WHERE id = sqlc.arg(id) AND checklist_id = sqlc.arg(checklist_id);

-- Separate from the title update below, and from anything about items: only the
-- author of a list may change who sees it, where an editor may rename or tick a
-- shared one. Two authorization rules should not share one statement.
-- name: SetChecklistVisibility :one
UPDATE checklists SET visibility = sqlc.arg(visibility)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- Renaming a list, which had no endpoint at all before Stage 14 Milestone 8: a
-- title was write-once, so fixing a typo meant deleting the list and its items.
-- name: UpdateChecklistTitle :one
UPDATE checklists SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND trip_id = sqlc.arg(trip_id)
RETURNING *;

-- Editing an item after the fact. Write-once was the wrong lifetime here too,
-- for a line of text you are going to re-read all week.
-- name: UpdateChecklistItemText :one
UPDATE checklist_items SET text = sqlc.arg(text)
WHERE id = sqlc.arg(id) AND checklist_id = sqlc.arg(checklist_id)
RETURNING *;

-- Every personal list belonging to one user on one trip, for the moment they
-- stop being a member. Same treatment as their personal files.
-- name: ListPersonalChecklistsForUser :many
SELECT * FROM checklists
WHERE trip_id = sqlc.arg(trip_id) AND owner_user_id = sqlc.arg(user_id) AND visibility = 'personal';
