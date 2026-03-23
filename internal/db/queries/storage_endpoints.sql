-- name: CreateStorageEndpoint :one
INSERT INTO storage_endpoints (id, gateway_id, name, display_name, configuration, annotations, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
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
