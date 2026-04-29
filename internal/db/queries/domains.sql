-- CreateDomain inserts a new PENDING domain. UNIQUE(domain) is a
-- global single-claim constraint — a duplicate insert returns
-- pgconn unique-violation, which the handler maps to ALREADY_EXISTS
-- WITHOUT disclosing the holding org.
-- name: CreateDomain :one
INSERT INTO domains (org_id, domain, verification_token, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- GetDomainByID looks up a domain row by primary key, scoped to an
-- org. Used by the CreateDomain LRO's polling work fn and by
-- GetDomain handler.
-- name: GetDomainByID :one
SELECT * FROM domains WHERE id = $1 AND org_id = $2;

-- GetDomainByName resolves a Domain resource path's trailing
-- segment (the domain string) to a row, scoped to org. Used by
-- GetDomain handler.
-- name: GetDomainByName :one
SELECT * FROM domains WHERE domain = $1 AND org_id = $2;

-- ListDomainsByOrg returns all domains for an org, oldest-first.
-- 100-row LIMIT is a defensive backstop; the typical org has a
-- handful of claimed domains.
-- name: ListDomainsByOrg :many
SELECT * FROM domains WHERE org_id = $1 ORDER BY create_time ASC LIMIT 100;

-- DeleteDomain removes a domain row. The handler runs preconditions
-- (cancel in-flight LROs, last-VERIFIED-domain-on-enabled-SSO check)
-- before this fires.
-- name: DeleteDomain :exec
DELETE FROM domains WHERE id = $1 AND org_id = $2;

-- CountVerifiedDomainsByOrg counts state=VERIFIED domains for an
-- org. Used by DeleteDomain's "last VERIFIED domain on enabled SSO"
-- precondition.
-- name: CountVerifiedDomainsByOrg :one
SELECT count(*) FROM domains WHERE org_id = $1 AND state = 'VERIFIED';

-- CancelDomainOpsForDomain marks running CreateDomain LROs for the
-- given (org, domain) pair as cancelled. The match is on
-- metadata->>'domain' (set by runVerifyDomain) AND on the
-- operations.org_id reverse pointer (populated by
-- CreateAndRunForOrg). The org_id filter is defense-in-depth: it
-- prevents a cross-org cancel even in the hypothetical case where
-- the same domain string ever appeared in two orgs (impossible
-- today thanks to UNIQUE(domain), but cheap insurance).
--
-- error_code = 1 is gRPC codes.Cancelled.
-- name: CancelDomainOpsForDomain :exec
UPDATE operations
SET done          = true,
    error_code    = 1,
    error_message = 'cancelled by DeleteDomain',
    update_time   = now()
WHERE done = false
  AND prefix = 'domains'
  AND org_id = @org_id
  AND metadata->>'domain' = @domain_name::text;

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
