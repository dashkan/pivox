-- name: CreateSpace :one
INSERT INTO spaces (id, org_id, name, display_name, labels, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetSpace :one
SELECT * FROM spaces WHERE id = $1 AND delete_time IS NULL;

-- name: GetSpaceIncludingDeleted :one
SELECT * FROM spaces WHERE id = $1;

-- name: GetSpaceByName :one
SELECT * FROM spaces WHERE org_id = $1 AND name = $2 AND delete_time IS NULL;

-- name: UpdateSpace :one
UPDATE spaces
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    labels = COALESCE(sqlc.narg('labels'), labels),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: SoftDeleteSpace :one
UPDATE spaces
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

-- name: UndeleteSpace :one
UPDATE spaces
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
