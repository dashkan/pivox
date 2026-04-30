-- name: UpsertFirebaseIdentity :one
-- Upserts a firebase_identity row synced from Firebase Auth.
-- On conflict (same firebase_uid), updates all mutable fields.
INSERT INTO firebase_identities (
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
    last_login_time = COALESCE(EXCLUDED.last_login_time, firebase_identities.last_login_time),
    update_time    = now()
RETURNING *;

-- name: GetFirebaseIdentityByUID :one
SELECT * FROM firebase_identities WHERE firebase_uid = $1;

-- GetFirebaseIdentityByID looks up by primary key. Used by
-- DeleteUser's DELETING_PIVOX_RECORDS phase to capture the
-- firebase_uid before the row is hard-deleted, so the subsequent
-- DELETING_FIREBASE_IDENTITY phase can call auth.DeleteUser(uid).
-- name: GetFirebaseIdentityByID :one
SELECT * FROM firebase_identities WHERE id = $1;

-- GetFirebaseIdentitiesByIDs is the batched lookup used by the audit
-- resolver to inflate Actor messages on resource reads. The IDs are
-- typically a deduped slice of cache misses; row order is not
-- guaranteed and the caller should index results by id.
-- name: GetFirebaseIdentitiesByIDs :many
SELECT * FROM firebase_identities WHERE id = ANY(@ids::uuid[]);
