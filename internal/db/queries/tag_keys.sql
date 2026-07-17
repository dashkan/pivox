-- name: CreateTagKey :one
INSERT INTO tag_keys (id, org_id, short_name, namespaced_name, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetTagKey :one
SELECT * FROM tag_keys WHERE id = $1;

-- GetTagKeyByOrgAndID is the org-scoped fetch used by every tag
-- value/binding handler to authorize the parent tag key against the
-- caller's resolved org. The permission interceptor authorizes the
-- caller-attested org SLUG in the request path, but a leaf tag-key
-- UUID in that path could belong to a DIFFERENT org; filtering by
-- org_id here closes that cross-org IDOR at the query, so a mismatch
-- returns pgx.ErrNoRows (mapped to NotFound) rather than the other
-- org's row.
-- name: GetTagKeyByOrgAndID :one
SELECT * FROM tag_keys WHERE id = $1 AND org_id = $2;

-- GetTagKeyForUpdate is the locking variant used by DeleteTagKey
-- inside its tx to serialize the empty-check + DELETE against
-- concurrent CreateTagValue inserts. The FOR UPDATE row lock
-- conflicts with the FK SHARE lock that a child INSERT takes on
-- this row — so a concurrent CreateTagValue blocks until our tx
-- commits or rolls back, eliminating the TOCTOU window between
-- "no children" and "delete parent".
-- name: GetTagKeyForUpdate :one
SELECT * FROM tag_keys WHERE id = $1 FOR UPDATE;

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
