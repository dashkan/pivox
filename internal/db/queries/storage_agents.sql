-- name: CreateStorageAgent :one
INSERT INTO storage_agents (id, gateway_id, ip_address, hostname, version, state)
VALUES ($1, $2, $3, $4, $5, 'CONNECTED')
RETURNING *;

-- name: GetStorageAgent :one
SELECT * FROM storage_agents WHERE id = $1;

-- name: GetStorageAgentByGatewayAndIP :one
SELECT * FROM storage_agents WHERE gateway_id = $1 AND ip_address = $2;

-- name: ListStorageAgentsByGateway :many
SELECT * FROM storage_agents WHERE gateway_id = $1 ORDER BY join_time;

-- name: UpdateStorageAgentState :one
UPDATE storage_agents SET state = $2, last_seen_time = now() WHERE id = $1 RETURNING *;

-- name: UpdateStorageAgentHeartbeat :exec
UPDATE storage_agents SET last_seen_time = now() WHERE id = $1;

-- name: UpdateStorageAgentVersion :exec
UPDATE storage_agents SET version = $2, last_seen_time = now() WHERE id = $1;

-- name: UpdateStorageAgentCacheUsed :exec
UPDATE storage_agents SET cache_used_gb = $2, last_seen_time = now() WHERE id = $1;

-- name: UpdateStorageAgentCert :exec
UPDATE storage_agents SET cert_expiry_time = $2, last_seen_time = now() WHERE id = $1;

-- name: DeleteStorageAgent :exec
DELETE FROM storage_agents WHERE id = $1;

-- name: CountStorageAgentsByGateway :one
SELECT count(*) FROM storage_agents WHERE gateway_id = $1;

-- name: CountConnectedStorageAgentsByGateway :one
SELECT count(*) FROM storage_agents WHERE gateway_id = $1 AND state = 'CONNECTED';
