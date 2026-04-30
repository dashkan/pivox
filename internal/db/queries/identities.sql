-- name: UpsertIdentity :one
-- Upserts an identity row synced from the upstream auth provider
-- (currently Firebase). On conflict (same firebase_uid), updates all
-- mutable fields. The `firebase_uid` column name is kept because it
-- still specifically holds a Firebase UID — the table name dropped
-- the prefix because identities will eventually carry non-Firebase
-- principal sources too.
INSERT INTO identities (
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
    last_login_time = COALESCE(EXCLUDED.last_login_time, identities.last_login_time),
    update_time    = now()
RETURNING *;

-- name: GetIdentityByFirebaseUID :one
SELECT * FROM identities WHERE firebase_uid = $1;

-- GetIdentityByID looks up by primary key. Used by
-- DeleteUser's DELETING_PIVOX_RECORDS phase to capture the
-- firebase_uid before the row is hard-deleted, so the subsequent
-- DELETING_FIREBASE_IDENTITY phase can call auth.DeleteUser(uid).
-- name: GetIdentityByID :one
SELECT * FROM identities WHERE id = $1;

-- GetIdentitiesByIDs is the batched lookup used by the audit
-- resolver to inflate Actor messages on resource reads. The IDs are
-- typically a deduped slice of cache misses; row order is not
-- guaranteed and the caller should index results by id.
-- name: GetIdentitiesByIDs :many
SELECT * FROM identities WHERE id = ANY(@ids::uuid[]);
