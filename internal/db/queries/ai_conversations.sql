-- name: CreateConversation :one
-- `created_by` doubles as the conversation owner / authorization
-- key (the `users/{user}` resource path segment). NOT NULL on the
-- column; every conversation has a creator.
INSERT INTO ai_conversations (org_id, name, title, description, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- GetConversationByName looks up a conversation by (org, name)
-- without an ownership filter. The handler enforces creator-only or
-- `*All`-permission access on top of this. Used by the read/update/
-- delete handlers as the row-fetch step; they then compare
-- `created_by` against the path's user-uuid AND the caller's
-- `pivox_user_id` claim before returning.
-- name: GetConversationByName :one
SELECT * FROM ai_conversations WHERE org_id = $1 AND name = $2;

-- name: GetConversationByID :one
SELECT * FROM ai_conversations WHERE id = $1;

-- GetConversationByIDForUpdate is the locking variant used by
-- runGenerate / persistInputMessage inside their per-message
-- transactions. The persistence sequence is
-- GetNextSequenceForConversation → CreateMessage →
-- IncrementConversationMessageCount; without a serializing lock on
-- the conversation row, two concurrent persists could read the
-- same MAX(sequence)+1 and both try to insert with that value,
-- producing a unique-constraint violation on
-- (conversation_id, sequence). Locking the conversation row
-- FOR UPDATE at the start of the tx forces concurrent persists
-- to queue, so each computes a fresh sequence under the lock.
-- name: GetConversationByIDForUpdate :one
SELECT * FROM ai_conversations WHERE id = $1 FOR UPDATE;

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
