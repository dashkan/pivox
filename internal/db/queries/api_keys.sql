-- name: CreateApiKey :one
INSERT INTO api_keys (id, org_id, key_id, display_name, key_string, annotations, restrictions, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
RETURNING *;

-- name: GetApiKey :one
SELECT * FROM api_keys WHERE id = $1 AND delete_time IS NULL;

-- name: GetApiKeyByOrgAndKeyID :one
SELECT * FROM api_keys WHERE org_id = $1 AND key_id = $2 AND delete_time IS NULL;

-- name: GetApiKeyIncludingDeleted :one
SELECT * FROM api_keys WHERE id = $1;

-- name: GetApiKeyString :one
SELECT key_string FROM api_keys WHERE id = $1 AND delete_time IS NULL;

-- name: UpdateApiKey :one
UPDATE api_keys
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    restrictions = COALESCE(sqlc.narg('restrictions'), restrictions),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: SoftDeleteApiKey :one
UPDATE api_keys
SET delete_time = now(),
    purge_time = now() + INTERVAL '30 days',
    revision = revision + 1,
    deleted_by = $2,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: UndeleteApiKey :one
UPDATE api_keys
SET delete_time = NULL,
    purge_time = NULL,
    deleted_by = '',
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NOT NULL
RETURNING *;

-- name: LookupApiKeyByKeyString :one
SELECT * FROM api_keys WHERE key_string = $1 AND delete_time IS NULL;
