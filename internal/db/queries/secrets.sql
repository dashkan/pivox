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

-- name: ListSecretsByParent :many
-- Keyset pagination on id. Fetch page_limit+1 to detect a next page.
-- (AIP-160 filter / order_by are not yet wired — ordered by id.)
SELECT * FROM secrets
WHERE org_id = @org_id
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;
