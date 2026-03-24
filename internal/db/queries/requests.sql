-- name: CreateRequest :one
INSERT INTO requests (id, project_id, name, display_name, description, state, priority, assignee, due_time, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING *;

-- name: GetRequest :one
SELECT * FROM requests WHERE id = $1;

-- name: GetRequestByName :one
SELECT * FROM requests WHERE project_id = $1 AND name = $2;

-- name: ListRequestsByProject :many
SELECT * FROM requests
WHERE project_id = $1 AND delete_time IS NULL
ORDER BY create_time DESC
LIMIT $2 OFFSET $3;

-- name: ListRequestsByProjectWithDeleted :many
SELECT * FROM requests
WHERE project_id = $1
ORDER BY create_time DESC
LIMIT $2 OFFSET $3;

-- name: CountRequestsByProject :one
SELECT count(*) FROM requests WHERE project_id = $1 AND delete_time IS NULL;

-- name: UpdateRequest :one
UPDATE requests
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    priority = COALESCE(sqlc.narg('priority'), priority),
    due_time = COALESCE(sqlc.narg('due_time'), due_time),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestState :one
UPDATE requests
SET state = $2,
    revision = revision + 1,
    updated_by = $3,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestAssignee :one
UPDATE requests
SET assignee = $2,
    state = $3,
    revision = revision + 1,
    updated_by = $4,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestDelivered :one
UPDATE requests
SET state = 'DELIVERED',
    delivered_time = now(),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: UpdateRequestApproved :one
UPDATE requests
SET state = 'APPROVED',
    approved_time = now(),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: SoftDeleteRequest :exec
UPDATE requests
SET state = 'CANCELLED',
    delete_time = now(),
    purge_time = now() + INTERVAL '30 days',
    deleted_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1;
