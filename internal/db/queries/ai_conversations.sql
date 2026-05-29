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

-- AcquireConversationLease tries to take the per-conversation stream
-- lease. Succeeds (returns 1 row) when the lease is unheld, expired,
-- or already held by the same session_uid (idempotent re-acquire on
-- retry). Returns 0 rows when another session holds an unexpired
-- lease — the caller maps this to apierr.Aborted("active stream").
--
-- TTL = 30s. The 30s value is the SLO for "after pivox-cloud crashes
-- holding this lease, how long before another session can acquire?"
-- During healthy operation the heartbeat refreshes every 10s as long
-- as the upstream model is still producing bytes; if it stops, the
-- heartbeat actively aborts the stream so the lease releases via the
-- normal path. TTL is the backstop for process death, not the primary
-- expiration mechanism.
--
-- name: AcquireConversationLease :one
UPDATE ai_conversations
SET lock_holder = $2,
    lock_expires_at = now() + interval '30 seconds'
WHERE id = $1
  AND (
    lock_holder IS NULL
    OR lock_expires_at < now()
    OR lock_holder = $2
  )
RETURNING id;

-- HeartbeatConversationLease extends `lock_expires_at` for an active
-- lease the caller still owns AND that hasn't already expired.
-- Returns 0 rows in two cases the caller treats identically (cancel
-- the stream):
--   1. Lease holder is now someone else — invariant violation
--      (heartbeat stopped without aborting; should not happen).
--   2. Lease has expired since the previous heartbeat (e.g. we
--      skipped extensions during a stall, the row went past
--      lock_expires_at, and we're now trying to "revive" it).
-- The `lock_expires_at > now()` guard is what makes case 2 a hard
-- abort rather than a silent re-extension — once expired, a stalled
-- stream loses the lease cleanly even if no one else acquires it.
--
-- name: HeartbeatConversationLease :one
UPDATE ai_conversations
SET lock_expires_at = now() + interval '30 seconds'
WHERE id = $1 AND lock_holder = $2 AND lock_expires_at > now()
RETURNING id;

-- ReleaseConversationLease drops the lease on stream end. Idempotent
-- and safe to call multiple times (e.g. defer + explicit). 0 rows
-- means the lease was already released or taken over — both are
-- terminal states the caller doesn't need to act on.
--
-- name: ReleaseConversationLease :exec
UPDATE ai_conversations
SET lock_holder = NULL,
    lock_expires_at = NULL
WHERE id = $1 AND lock_holder = $2;

-- IsConversationLocked reports whether a conversation currently has
-- an active (non-expired) lease. Used by DeleteConversation and
-- UpdateConversation to reject mid-stream mutations.
--
-- name: IsConversationLocked :one
SELECT (lock_holder IS NOT NULL AND lock_expires_at > now())::boolean AS locked
FROM ai_conversations
WHERE id = $1;
