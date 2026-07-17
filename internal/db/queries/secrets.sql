-- name: CreateSecret :one
-- id is app-generated (uuid.New) so the caller has it for AAD binding
-- before the value is encrypted.
INSERT INTO secrets (id, org_id, space_id, slug, display_name, value_ciphertext, annotations, created_by, updated_by)
VALUES ($1, $2, sqlc.narg('space_id'), $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: GetSecret :one
SELECT * FROM secrets WHERE id = $1;

-- name: GetSecretByParent :one
-- Resolves a Secret from its parent + slug (the resource-name leaf).
-- space_id IS NOT DISTINCT FROM treats NULL (org-scoped) as a matchable value.
SELECT * FROM secrets
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND slug = sqlc.arg('slug');

-- GetSecretByParentForUpdate resolves a Secret by parent + slug AND locks the
-- row, so the etag check and the write serialize against a concurrent rotate.
-- The slug is the resource-name leaf, so update/delete resolve their target by
-- scope + slug in one statement.
-- name: GetSecretByParentForUpdate :one
SELECT * FROM secrets
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND slug = sqlc.arg('slug')
FOR UPDATE;

-- name: UpdateSecret :one
-- Masked update: a nil arg leaves the column unchanged. value_ciphertext is
-- only passed when `value` is in the field mask (a non-empty new value).
UPDATE secrets
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    value_ciphertext = COALESCE(sqlc.narg('value_ciphertext'), value_ciphertext),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteSecret :exec
DELETE FROM secrets WHERE id = $1;

-- ListSecrets is NOT a sqlc query: it is built dynamically by
-- filter.BuildListQuery (AIP-160 filter + AIP-132 order_by + compound-cursor
-- keyset), scanned by filter.ScanSecrets, exactly like ListConnectors. See
-- internal/service/secrets/server.go.
