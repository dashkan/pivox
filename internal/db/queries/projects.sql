-- name: CreateProject :one
INSERT INTO projects (id, org_id, name, display_name, labels, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1 AND delete_time IS NULL;

-- name: GetProjectIncludingDeleted :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectByName :one
SELECT * FROM projects WHERE org_id = $1 AND name = $2 AND delete_time IS NULL;

-- name: UpdateProject :one
UPDATE projects
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    labels = COALESCE(sqlc.narg('labels'), labels),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: SoftDeleteProject :one
UPDATE projects
SET state = 'DELETE_REQUESTED',
    delete_time = now(),
    purge_time = now() + INTERVAL '30 days',
    revision = revision + 1,
    deleted_by = $2,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: UndeleteProject :one
UPDATE projects
SET state = 'ACTIVE',
    delete_time = NULL,
    purge_time = NULL,
    deleted_by = '',
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NOT NULL
RETURNING *;
