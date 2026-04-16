-- name: CreateConversation :one
INSERT INTO conversations (org_id, creator_uid, name, title, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetConversationByName :one
SELECT * FROM conversations WHERE org_id = $1 AND name = $2 AND delete_time IS NULL;

-- name: GetConversationByID :one
SELECT * FROM conversations WHERE id = $1 AND delete_time IS NULL;

-- name: ListConversationsByCreator :many
SELECT * FROM conversations
WHERE org_id = $1 AND creator_uid = $2 AND delete_time IS NULL
ORDER BY create_time DESC
LIMIT $3 OFFSET $4;

-- name: ListConversationsByCreatorActive :many
SELECT * FROM conversations
WHERE org_id = $1 AND creator_uid = $2 AND archived = FALSE AND delete_time IS NULL
ORDER BY create_time DESC
LIMIT $3 OFFSET $4;

-- name: CountConversationsByCreator :one
SELECT count(*) FROM conversations
WHERE org_id = $1 AND creator_uid = $2 AND delete_time IS NULL;

-- name: UpdateConversation :one
UPDATE conversations
SET title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    archived = COALESCE(sqlc.narg('archived'), archived),
    pinned = COALESCE(sqlc.narg('pinned'), pinned),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1 AND delete_time IS NULL
RETURNING *;

-- name: DeleteConversation :exec
UPDATE conversations SET delete_time = now() WHERE id = $1 AND delete_time IS NULL;

-- name: IncrementConversationMessageCount :exec
UPDATE conversations
SET message_count = message_count + 1,
    last_message_time = now(),
    update_time = now()
WHERE id = $1;
