-- CreateOperation creates a new operation row. `org_id` is the
-- optional reverse pointer that links the LRO to its target org
-- (NULL for ops that aren't org-scoped or where the org isn't
-- known at create time, including DeleteOrganization which must
-- not self-cancel).
-- name: CreateOperation :one
INSERT INTO operations (id, parent, metadata, created_by, org_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetOperation :one
SELECT * FROM operations WHERE id = $1;

-- name: ListOperations :many
SELECT * FROM operations
WHERE (sqlc.narg('parent_filter')::text IS NULL OR parent = sqlc.narg('parent_filter'))
ORDER BY create_time DESC
LIMIT $1;

-- name: CompleteOperation :one
UPDATE operations
SET done = true, result = $2, update_time = now()
WHERE id = $1
RETURNING *;

-- name: FailOperation :one
UPDATE operations
SET done = true, error_code = $2, error_message = $3, update_time = now()
WHERE id = $1
RETURNING *;

-- name: UpdateOperationMetadata :exec
UPDATE operations
SET metadata = $2, update_time = now()
WHERE id = $1;

-- name: CancelOperation :one
UPDATE operations
SET done = true, error_code = 1, error_message = 'cancelled by user', update_time = now()
WHERE id = $1 AND done = false
RETURNING *;

-- name: DeleteOperation :exec
DELETE FROM operations WHERE id = $1;

-- name: ListPendingOperations :many
SELECT * FROM operations WHERE done = false ORDER BY create_time ASC;

-- name: DeleteExpiredOperations :exec
DELETE FROM operations WHERE done = true AND expire_time < now();
