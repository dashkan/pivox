-- ListPendingDomains returns all domains in PENDING state, ordered
-- by oldest-first. Used by VerifyDomainWorker to drive PENDING →
-- VERIFIED transitions. The 1000-row LIMIT is a defensive backstop
-- (real load is well below that — every active org has 0–3 domains).
-- name: ListPendingDomains :many
SELECT * FROM domains WHERE state = 'PENDING' ORDER BY create_time ASC LIMIT 1000;

-- MarkDomainVerified flips a PENDING domain to VERIFIED, sets
-- verified_time, and bumps revision/etag. Refuses to fire on a
-- non-PENDING row so a race with a concurrent FAILED transition is
-- absorbed (returns no rows).
-- name: MarkDomainVerified :one
UPDATE domains
SET state         = 'VERIFIED',
    verified_time = now(),
    update_time   = now(),
    revision      = revision + 1,
    etag          = md5(now()::text || revision::text)
WHERE id = $1 AND state = 'PENDING'
RETURNING *;

-- MarkDomainFailed flips a PENDING domain to FAILED. Same race-
-- safety as MarkDomainVerified.
-- name: MarkDomainFailed :one
UPDATE domains
SET state       = 'FAILED',
    update_time = now(),
    revision    = revision + 1,
    etag        = md5(now()::text || revision::text)
WHERE id = $1 AND state = 'PENDING'
RETURNING *;
