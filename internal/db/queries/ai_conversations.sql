-- name: CreateConversation :one
INSERT INTO ai_conversations (org_id, name, title, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;

-- name: GetConversationByName :one
SELECT * FROM ai_conversations WHERE org_id = $1 AND name = $2;

-- name: GetConversationByID :one
SELECT * FROM ai_conversations WHERE id = $1;

-- name: ListConversationsByCreator :many
SELECT * FROM ai_conversations
WHERE org_id = $1 AND created_by = $2
ORDER BY create_time DESC
LIMIT $3 OFFSET $4;

-- name: ListConversationsByCreatorActive :many
SELECT * FROM ai_conversations
WHERE org_id = $1 AND created_by = $2 AND archived = FALSE
ORDER BY create_time DESC
LIMIT $3 OFFSET $4;

-- name: CountConversationsByCreator :one
SELECT count(*) FROM ai_conversations
WHERE org_id = $1 AND created_by = $2;

-- name: UpdateConversation :one
UPDATE ai_conversations
SET title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    archived = COALESCE(sqlc.narg('archived'), archived),
    pinned = COALESCE(sqlc.narg('pinned'), pinned),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: DeleteConversation :exec
DELETE FROM ai_conversations WHERE id = $1;

-- name: IncrementConversationMessageCount :exec
UPDATE ai_conversations
SET message_count = message_count + 1,
    last_message_time = now(),
    update_time = now()
WHERE id = $1;
