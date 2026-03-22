-- name: CreateAgent :one
INSERT INTO agents (id, gateway_id, ip_address, hostname, version, state)
VALUES ($1, $2, $3, $4, $5, 'CONNECTED')
RETURNING *;

-- name: GetAgent :one
SELECT * FROM agents WHERE id = $1;

-- name: GetAgentByGatewayAndIP :one
SELECT * FROM agents WHERE gateway_id = $1 AND ip_address = $2;

-- name: ListAgentsByGateway :many
SELECT * FROM agents WHERE gateway_id = $1 ORDER BY join_time;

-- name: UpdateAgentState :one
UPDATE agents SET state = $2, last_seen_time = now() WHERE id = $1 RETURNING *;

-- name: UpdateAgentHeartbeat :exec
UPDATE agents SET last_seen_time = now() WHERE id = $1;

-- name: UpdateAgentVersion :exec
UPDATE agents SET version = $2, last_seen_time = now() WHERE id = $1;

-- name: UpdateAgentCacheUsed :exec
UPDATE agents SET cache_used_gb = $2, last_seen_time = now() WHERE id = $1;

-- name: UpdateAgentCert :exec
UPDATE agents SET cert_expiry_time = $2, last_seen_time = now() WHERE id = $1;

-- name: DeleteAgent :exec
DELETE FROM agents WHERE id = $1;

-- name: CountAgentsByGateway :one
SELECT count(*) FROM agents WHERE gateway_id = $1;

-- name: CountConnectedAgentsByGateway :one
SELECT count(*) FROM agents WHERE gateway_id = $1 AND state = 'CONNECTED';
