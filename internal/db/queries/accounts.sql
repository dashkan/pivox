-- name: UpsertAccount :one
-- Upserts an account synced from Firebase Auth.
-- On conflict (same firebase_uid), updates all mutable fields.
INSERT INTO accounts (
    firebase_uid,
    email,
    email_verified,
    display_name,
    photo_url,
    disabled,
    last_login_time
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (firebase_uid) DO UPDATE SET
    email          = EXCLUDED.email,
    email_verified = EXCLUDED.email_verified,
    display_name   = EXCLUDED.display_name,
    photo_url      = EXCLUDED.photo_url,
    disabled       = EXCLUDED.disabled,
    last_login_time = COALESCE(EXCLUDED.last_login_time, accounts.last_login_time),
    update_time    = now()
RETURNING *;

-- name: GetAccountByFirebaseUID :one
SELECT * FROM accounts WHERE firebase_uid = $1;
