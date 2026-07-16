-- name: CreateConnector :one
-- id is app-generated (uuid.New) so the caller has it before the write
-- (mirrors the Secret create path, keeping ids caller-visible for logging).
INSERT INTO connectors (id, org_id, space_id, slug, display_name, description, config, agent, annotations, created_by, updated_by)
VALUES ($1, $2, sqlc.narg('space_id'), $3, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: GetConnector :one
SELECT * FROM connectors WHERE id = $1;

-- name: GetConnectorByParent :one
-- Resolves a Connector from its parent + slug (the resource-name leaf).
-- space_id IS NOT DISTINCT FROM treats NULL (org-scoped) as a matchable value.
SELECT * FROM connectors
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND slug = sqlc.arg('slug');

-- GetConnectorByParentForUpdate resolves a Connector by parent + slug AND locks
-- the row for the update/delete tx, so the etag check and the write serialize
-- against a concurrent mutation. The slug (not the uuid) is the resource-name
-- leaf, so update/delete resolve their target by scope + slug in one statement.
-- name: GetConnectorByParentForUpdate :one
SELECT * FROM connectors
WHERE org_id = $1
  AND space_id IS NOT DISTINCT FROM sqlc.narg('space_id')
  AND slug = sqlc.arg('slug')
FOR UPDATE;

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

-- ListConnectors is NOT a static sqlc query: it is a dynamic AIP-160
-- filtered + AIP-132 sorted + compound-cursor keyset list assembled by
-- internal/filter.BuildListQuery (base scope org_id + space_id IS NOT DISTINCT
-- FROM, layered filter/order_by/keyset). See internal/service/connectors and
-- docs/aip-list-transpiler-procedure.md.

-- name: ListDistinctConnectorAgentsInOrg :many
-- The org-rollup "agents in use" facet: the distinct non-empty agent values
-- across the whole org — org-direct connectors AND every space — matching the
-- org-level list's base scope. Cloud connectors (agent = '') are excluded.
-- Connectors hard-delete, so there is no soft-delete predicate.
SELECT DISTINCT agent FROM connectors
WHERE org_id = $1 AND agent <> ''
ORDER BY agent;

-- name: ListDistinctConnectorAgentsInSpace :many
-- The space-scoped "agents in use" facet: the distinct non-empty agent values
-- within one space, matching a space-level list's base scope.
SELECT DISTINCT agent FROM connectors
WHERE org_id = $1 AND space_id = $2 AND agent <> ''
ORDER BY agent;

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
SELECT c.id, c.slug, c.org_id, c.space_id
FROM connector_secret_refs r
JOIN connectors c ON c.id = r.connector_id
WHERE r.secret_id = $1
ORDER BY c.slug;
