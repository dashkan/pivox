-- name: UpsertIdentity :one
-- Upserts an identity row synced from the upstream auth provider
-- (currently Firebase). On conflict (same firebase_uid), updates all
-- mutable fields. The `firebase_uid` column name is kept because it
-- still specifically holds a Firebase UID — the table name dropped
-- the prefix because identities will eventually carry non-Firebase
-- principal sources too.
--
-- Soft-delete revival: if the existing row is `is_deleted = true`
-- (the same Firebase UID is being recycled — e.g. after a prior
-- DeleteAccount + Firebase re-signup with a reused UID), the
-- conflict path resets `is_deleted` to false and clears
-- `delete_time`. Without this the row would stay tombstoned, the
-- new user could not sign in via `GetIdentityByFirebaseUID` (which
-- excludes tombstones), AND the new user's PII would be written
-- onto a row whose `id` is still referenced by the previous
-- identity's audit trail — leaking that PII through every cached
-- *_by Actor lookup. UID recycling is rare in production (Firebase
-- normally issues a fresh UID on re-signup) but the constraint
-- forces this to be defensible regardless.
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
    is_deleted     = false,
    delete_time    = NULL,
    update_time    = now()
RETURNING *;

-- GetIdentityByFirebaseUID is the active-sign-in lookup — soft-deleted
-- rows are excluded so a recycled Firebase UID can't accidentally
-- resolve to a tombstoned identity.
-- name: GetIdentityByFirebaseUID :one
SELECT * FROM identities WHERE firebase_uid = $1 AND is_deleted = false;

-- GetIdentityByID looks up by primary key. Used by DeleteUser's
-- DELETING_PIVOX_RECORDS phase to capture the firebase_uid before
-- the row is soft-deleted, so the subsequent
-- DELETING_FIREBASE_IDENTITY phase can call auth.DeleteUser(uid).
-- Returns soft-deleted rows too (callers like the resolver need them
-- to render is_deleted=true Actor placeholders).
-- name: GetIdentityByID :one
SELECT * FROM identities WHERE id = $1;

-- GetIdentitiesByIDs is the batched lookup used by the audit
-- resolver to inflate Actor messages on resource reads. The IDs are
-- typically a deduped slice of cache misses; row order is not
-- guaranteed and the caller should index results by id. Returns
-- soft-deleted rows so the resolver can flag is_deleted on the
-- returned Actor and blank PII.
-- name: GetIdentitiesByIDs :many
SELECT * FROM identities WHERE id = ANY(@ids::uuid[]);

-- SoftDeleteIdentity tombstones an identity: blanks PII, flips
-- is_deleted, stamps delete_time. The row is preserved so any
-- *_by audit field that points at this identity continues to
-- resolve (the audit resolver renders is_deleted=true with empty
-- PII rather than dropping the reference).
--
-- Returns the row's id so the caller can verify the UPDATE actually
-- landed. The predicate excludes already-soft-deleted rows so a
-- second call surfaces ErrNoRows — the caller is expected to
-- distinguish "real first-time tombstone" from "wrong ID / already
-- tombstoned" rather than silently no-op-and-continue (a wrong id
-- would otherwise let the LRO proceed to auth.DeleteUser with a
-- firebase_uid that doesn't belong to the row we thought we
-- deleted).
-- name: SoftDeleteIdentity :one
UPDATE identities SET
    is_deleted     = true,
    email          = '',
    display_name   = '',
    photo_url      = '',
    delete_time    = now(),
    update_time    = now()
WHERE id = $1 AND is_deleted = false
RETURNING id;
