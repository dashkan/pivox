-- name: CreateMessage :one
INSERT INTO messages (conversation_id, name, role, parts, sequence, token_count)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetMessageByName :one
SELECT * FROM messages WHERE conversation_id = $1 AND name = $2;

-- name: ListMessagesByConversation :many
SELECT * FROM messages
WHERE conversation_id = $1
ORDER BY sequence ASC
LIMIT $2 OFFSET $3;

-- name: ListMessagesNewestFirst :many
-- Fetches messages newest-first for budget truncation in Go.
-- Caller walks rows accumulating token_count and stops when budget is exceeded.
SELECT * FROM messages
WHERE conversation_id = $1
ORDER BY sequence DESC
LIMIT $2;

-- name: CountMessagesByConversation :one
SELECT count(*) FROM messages WHERE conversation_id = $1;

-- name: SumTokensByConversation :one
SELECT COALESCE(SUM(token_count), 0)::bigint FROM messages WHERE conversation_id = $1;

-- name: GetNextSequenceForConversation :one
SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE conversation_id = $1;
