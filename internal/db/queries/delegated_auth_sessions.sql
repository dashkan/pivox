-- name: CreateDelegatedAuthSession :one
-- Creates a new delegated auth session. The code and expiry are chosen by the
-- server so we can control both TTL and the entropy source (crypto/rand).
INSERT INTO delegated_auth_sessions (code, expires_at)
VALUES ($1, $2)
RETURNING *;

-- name: CompleteDelegatedAuthSession :one
-- Transitions a pending session to ready and stores the minted custom token.
-- Only unexpired pending sessions match — a no-row result means the session
-- was never created, already completed, or has expired.
UPDATE delegated_auth_sessions
SET status = 'ready',
    custom_token = $2
WHERE code = $1
  AND status = 'pending'
  AND expires_at > now()
RETURNING *;

-- name: ConsumeDelegatedAuthSession :one
-- Atomically deletes a ready session and returns its custom token. This is
-- the poll path — a single statement ensures the token is single-use even
-- under concurrent pollers. No-row result means the session is still pending,
-- already consumed, or expired; callers distinguish pending via GetDelegatedAuthSessionStatus.
DELETE FROM delegated_auth_sessions
WHERE code = $1
  AND status = 'ready'
  AND expires_at > now()
RETURNING custom_token;

-- name: GetDelegatedAuthSessionStatus :one
-- Returns the status of a session without mutating it. Used by pollers to
-- distinguish "still pending" from "expired/unknown" after a failed consume.
SELECT status
FROM delegated_auth_sessions
WHERE code = $1
  AND expires_at > now();

-- name: DeleteExpiredDelegatedAuthSessions :exec
-- Cleanup: remove sessions past their expiry. Run periodically.
DELETE FROM delegated_auth_sessions
WHERE expires_at < now();
