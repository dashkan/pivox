-- name: CreateDelegatedAuthSession :one
-- Creates a new delegated auth session. The code and expiry are chosen by the
-- server so we can control both TTL and the entropy source (crypto/rand).
INSERT INTO delegated_auth_sessions (code, expire_time)
VALUES ($1, $2)
RETURNING *;

-- name: CompleteDelegatedAuthSession :one
-- Transitions a pending session to approved and stores the minted custom token.
-- Only unexpired pending sessions match — a no-row result means the session
-- was never created, already completed, or has expired.
UPDATE delegated_auth_sessions
SET state = 'APPROVED',
    custom_token = $2
WHERE code = $1
  AND state = 'PENDING'
  AND expire_time > now()
RETURNING *;

-- name: ConsumeDelegatedAuthSession :one
-- Atomically deletes an approved session and returns its custom token. This is
-- the poll path — a single statement ensures the token is single-use even
-- under concurrent pollers. No-row result means the session is still pending,
-- already consumed, or expired; callers distinguish pending via GetDelegatedAuthSessionState.
DELETE FROM delegated_auth_sessions
WHERE code = $1
  AND state = 'APPROVED'
  AND expire_time > now()
RETURNING custom_token;

-- name: GetDelegatedAuthSessionState :one
-- Returns the state of a session without mutating it. Used by pollers to
-- distinguish "still pending" from "expired/unknown" after a failed consume.
SELECT state
FROM delegated_auth_sessions
WHERE code = $1
  AND expire_time > now();

-- name: DeleteExpiredDelegatedAuthSessions :exec
-- Cleanup: remove sessions past their expiry. Run periodically.
DELETE FROM delegated_auth_sessions
WHERE expire_time < now();
