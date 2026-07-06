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

-- name: DeleteConnectorSecretRefs :exec
-- Clears a connector's tracked secret refs. Called inside the connector-write
-- tx before re-inserting the current set, so the ref table always mirrors the
-- config's secret("…") references.
DELETE FROM connector_secret_refs WHERE connector_id = $1;

-- name: InsertConnectorSecretRefs :exec
-- Batch-inserts a connector's resolved secret refs in one round trip: the
-- connector_id pairs with each element of the secret_ids array via unnest.
-- ON CONFLICT DO NOTHING tolerates the same secret being referenced twice in
-- one config (distinct names resolving to the same secret id).
INSERT INTO connector_secret_refs (connector_id, secret_id)
SELECT @connector_id::uuid, unnest(@secret_ids::uuid[])
ON CONFLICT DO NOTHING;

-- name: ConnectorsReferencingSecret :many
-- The DeleteSecret guard's lookup: connectors that reference a given secret,
-- with enough identity (slug + scope) to name them in the FailedPrecondition
-- error. Ordered by slug for a stable, readable message.
SELECT c.id, c.connector_id, c.org_id, c.space_id
FROM connector_secret_refs r
JOIN connectors c ON c.id = r.connector_id
WHERE r.secret_id = $1
ORDER BY c.connector_id;
