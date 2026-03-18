-- name: CreateAuthTokenCode :one
-- Stores a Firebase ID token behind a short-lived opaque code.
INSERT INTO auth_token_codes (id_token)
VALUES ($1)
RETURNING *;

-- name: ConsumeAuthTokenCode :one
-- Atomically consumes a code and returns the ID token.
-- Returns no rows if the code doesn't exist, is expired, or was already consumed.
UPDATE auth_token_codes
SET consumed = true
WHERE code = $1
  AND consumed = false
  AND expire_time > now()
RETURNING *;

-- name: DeleteExpiredAuthTokenCodes :exec
-- Cleanup: remove codes older than 10 minutes (all should be expired by then).
DELETE FROM auth_token_codes
WHERE expire_time < now() - INTERVAL '10 minutes';
