-- name: CreateTagKey :one
INSERT INTO tag_keys (id, org_id, short_name, namespaced_name, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetTagKey :one
SELECT * FROM tag_keys WHERE id = $1;

-- name: GetTagKeyByNamespacedName :one
SELECT * FROM tag_keys WHERE namespaced_name = $1;

-- name: UpdateTagKey :one
UPDATE tag_keys
SET description = COALESCE(sqlc.narg('description'), description),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteTagKey :exec
DELETE FROM tag_keys WHERE id = $1;

-- name: CountTagValuesByTagKey :one
SELECT count(*) FROM tag_values WHERE tag_key_id = $1;
