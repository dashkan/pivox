-- name: CreateStorageGateway :one
INSERT INTO storage_gateways (id, org_id, name, display_name, ip_addresses, registration_token, hostname, cache_max_size_gb, cache_eviction, cache_ttl_hours, annotations, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
RETURNING *;

-- name: GetStorageGateway :one
SELECT * FROM storage_gateways WHERE id = $1;

-- name: GetStorageGatewayByName :one
SELECT * FROM storage_gateways WHERE org_id = $1 AND name = $2;

-- name: GetStorageGatewayByToken :one
SELECT * FROM storage_gateways WHERE registration_token = $1;

-- name: UpdateStorageGateway :one
UPDATE storage_gateways
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    ip_addresses = COALESCE(sqlc.narg('ip_addresses'), ip_addresses),
    target_version = COALESCE(sqlc.narg('target_version'), target_version),
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

-- name: DeleteStorageGateway :exec
DELETE FROM storage_gateways WHERE id = $1;

-- name: UpdateStorageGatewayState :exec
UPDATE storage_gateways
SET state = $2, update_time = now(), etag = md5(now()::text)
WHERE id = $1;

-- name: UpdateStorageGatewayCert :exec
UPDATE storage_gateways
SET cert_state = $2, cert_expiry_time = $3, update_time = now(), etag = md5(now()::text)
WHERE id = $1;

-- name: UpdateStorageGatewayVersion :exec
UPDATE storage_gateways
SET current_version = $2, update_time = now(), etag = md5(now()::text)
WHERE id = $1;

-- name: RotateRegistrationToken :one
UPDATE storage_gateways
SET registration_token = $2, update_time = now(), etag = md5(now()::text)
WHERE id = $1
RETURNING *;
