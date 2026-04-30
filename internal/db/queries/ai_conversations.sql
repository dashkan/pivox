-- name: CreateConversation :one
INSERT INTO ai_conversations (org_id, creator_id, name, title, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- GetConversationByName looks up a conversation by (org, name)
-- without an ownership filter. The handler enforces creator-only or
-- `*All`-permission access on top of this. Used by the read/update/
-- delete handlers as the row-fetch step; they then compare
-- `creator_id` against the path's user-uuid AND the caller's
-- `pivox_user_id` claim before returning.
-- name: GetConversationByName :one
SELECT * FROM ai_conversations WHERE org_id = $1 AND name = $2;

-- name: GetConversationByID :one
SELECT * FROM ai_conversations WHERE id = $1;

-- ListConversations/CountConversations replaced by filter.Query in the
-- service layer — see internal/filter/declarations.go ConversationFilter.

-- name: UpdateConversation :one
-- A user-driven title write (UpdateConversation with `title` in the
-- update mask) flips `title_user_set` to true so subsequent
-- `:summarize` calls won't overwrite it. The boolean is only ever
-- raised here, never lowered — once a user has curated a title, that
-- intent is sticky.
UPDATE ai_conversations
SET title = COALESCE(sqlc.narg('title'), title),
    title_user_set = CASE
        WHEN sqlc.narg('title')::text IS NOT NULL THEN TRUE
        ELSE title_user_set
    END,
    description = COALESCE(sqlc.narg('description'), description),
    archived = COALESCE(sqlc.narg('archived'), archived),
    pinned = COALESCE(sqlc.narg('pinned'), pinned),
    revision = revision + 1,
    updated_by = $2,
    update_time = now(),
    etag = md5(now()::text)
WHERE id = $1
RETURNING *;

-- name: SetAutoTitle :one
-- Server-driven title write (the `:summarize` path). Does NOT flip
-- `title_user_set` — that's the whole point of the flag.
UPDATE ai_conversations
SET title = $2,
    revision = revision + 1,
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
