-- name: CreateStorageAgentAudit :exec
INSERT INTO storage_agent_audit (id, gateway_id, agent_id, message_id, direction, message_type, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListStorageAgentAuditByGateway :many
SELECT * FROM storage_agent_audit
WHERE gateway_id = $1
ORDER BY create_time DESC
LIMIT $2;

-- name: ListStorageAgentAuditByAgent :many
SELECT * FROM storage_agent_audit
WHERE agent_id = $1
ORDER BY create_time DESC
LIMIT $2;

-- name: DeleteExpiredStorageAgentAudit :execrows
DELETE FROM storage_agent_audit
WHERE create_time < now() - INTERVAL '90 days';
