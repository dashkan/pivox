-- name: CreateConnector :one
-- id is app-generated (uuid.New) so the caller has it before the write
-- (mirrors the Secret create path, keeping ids caller-visible for logging).
INSERT INTO connectors (id, org_id, space_id, connector_id, display_name, description, config, agent, annotations, created_by, updated_by)
VALUES ($1, $2, sqlc.narg('space_id'), $3, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: GetConnector :one
SELECT * FROM connectors WHERE id = $1;

-- name: GetConnectorByParent :one
-- Resolves a Connector from its parent + slug. space_id IS NOT DISTINCT FROM
-- treats NULL (org-scoped) as a matchable value.
SELECT * FROM connectors
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND connector_id = $2;

-- GetConnectorForUpdate locks the row for the update/delete tx so the etag
-- check and the write serialize against a concurrent update.
-- name: GetConnectorForUpdate :one
SELECT * FROM connectors WHERE id = $1 FOR UPDATE;

-- name: UpdateConnector :one
-- Masked update: a nil arg leaves the column unchanged.
UPDATE connectors
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    config = COALESCE(sqlc.narg('config'), config),
    agent = COALESCE(sqlc.narg('agent'), agent),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteConnector :exec
DELETE FROM connectors WHERE id = $1;

-- name: ListConnectorsByParent :many
-- Keyset pagination on id. Fetch page_limit+1 to detect a next page.
-- (AIP-160 filter / order_by are not yet wired — ordered by id.)
SELECT * FROM connectors
WHERE org_id = @org_id
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor'))
ORDER BY id
LIMIT @page_limit;
