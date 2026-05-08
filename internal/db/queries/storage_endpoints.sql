-- name: CreateStorageEndpoint :one
INSERT INTO storage_endpoints (id, gateway_id, name, display_name, configuration, cache_enabled, cache_max_size_gb, cache_eviction, cache_ttl_hours, annotations, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
RETURNING *;

-- name: GetStorageEndpoint :one
SELECT * FROM storage_endpoints WHERE id = $1;

-- name: GetStorageEndpointByName :one
SELECT * FROM storage_endpoints WHERE gateway_id = $1 AND name = $2;

-- name: ListStorageEndpointsByGateway :many
SELECT * FROM storage_endpoints WHERE gateway_id = $1 ORDER BY create_time;

-- name: UpdateStorageEndpoint :one
UPDATE storage_endpoints
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    configuration = COALESCE(sqlc.narg('configuration'), configuration),
    cache_enabled = COALESCE(sqlc.narg('cache_enabled'), cache_enabled),
    cache_max_size_gb = COALESCE(sqlc.narg('cache_max_size_gb'), cache_max_size_gb),
    cache_eviction = COALESCE(sqlc.narg('cache_eviction'), cache_eviction),
    cache_ttl_hours = COALESCE(sqlc.narg('cache_ttl_hours'), cache_ttl_hours),
    annotations = COALESCE(sqlc.narg('annotations'), annotations),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteStorageEndpoint :exec
DELETE FROM storage_endpoints WHERE id = $1;

-- name: UpdateStorageEndpointState :exec
UPDATE storage_endpoints SET state = $2, update_time = now(), etag = md5(now()::text) WHERE id = $1;

-- name: ListStorageEndpointShortNamesByOrg :many
-- Returns the DISTINCT endpoint short names across every gateway in
-- an org. Used by CreateStorageSession (#27 phase 2) to enumerate
-- the prefix-pattern endpoint segments — patterns are glob-matched
-- against `/{endpoint-short-name}/{rest}` URL paths at
-- internal/storageagent/http.go, so the controller has to know the
-- same set of short names the agent will route under.
--
-- NOTE: returns the full endpoint resource name's trailing path
-- component conceptually; in this schema `storage_endpoints.name`
-- already IS the short name (the trailing segment of the AIP
-- resource name). Distinct because different gateways in the same
-- org may register endpoints with the same short name (intentional
-- — each gateway has its own routing keyspace; identical short
-- names across gateways collapse to one pattern).
SELECT DISTINCT se.name
  FROM storage_endpoints se
  JOIN storage_gateways sg ON sg.id = se.gateway_id
 WHERE sg.org_id = $1
 ORDER BY se.name;
