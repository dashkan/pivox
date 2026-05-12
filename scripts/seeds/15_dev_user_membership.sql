-- Dev user membership — Phase 6c.4 smoke fixture.
--
-- Binds the dev operator (ashkan.daie@gmail.com) as `owner` of
-- Meridian Broadcasting so a fresh `make db-seed` produces a DB
-- where the macOS app's sign-in flow lands on a user who already
-- belongs to the org carrying the seeded Library thumbnails. Without
-- this, every reseed wipes the membership and the operator has to
-- re-bind via the UI before Library renders anything.
--
-- Idempotent:
--   - identities.ON CONFLICT (firebase_uid) preserves any
--     blocking-fn-populated state (display_name, email_verified,
--     last_login_time) from a real sign-in.
--   - org_members.WHERE NOT EXISTS sidesteps the UNIQUE(org_id,
--     user_id, role_id) constraint so re-running the seed is a
--     no-op rather than a constraint error.
--
-- To bind additional dev users:
--   - Add an INSERT INTO identities row with their firebase_uid +
--     email (ON CONFLICT DO NOTHING).
--   - Add an org_members INSERT inside the DO block referencing
--     their UID + the desired role.
--
-- To bind the same user to a different org, copy the
-- `meridian_owner_id` lookup pattern with the target org's slug
-- and add a parallel org_members INSERT.
--
-- This file deliberately does NOT touch the acme SSO seed
-- (`dev_acme_sso.sql`) — that flow is opt-in for SSO testing and
-- runs via its own command, not through `make db-seed`.

-- Runs inside the outer transaction from scripts/seed.sql — no
-- inner BEGIN/COMMIT (those would close the wrapping tx and leave
-- scripts/seed.sql's COMMIT firing on a no-tx state, raising a
-- WARNING). The DO block's local transactional semantics are
-- implicit anyway.

DO $$
DECLARE
    ashkan_id          UUID;
    meridian_org_id    UUID;
    meridian_owner_id  UUID;
    _ashkan_firebase_uid CONSTANT TEXT := 'ScQytJWi2ycF3jiiBlRazncbfQB3';
BEGIN
    -- 1) Identity. ON CONFLICT (firebase_uid) lets a real sign-in
    --    overwrite the seeded skeleton with live Firebase data
    --    (display_name, email_verified, photo_url) without
    --    clobbering it on subsequent reseeds.
    INSERT INTO identities (id, firebase_uid, email, email_verified)
    VALUES (uuidv7(), _ashkan_firebase_uid, 'ashkan.daie@gmail.com', true)
    ON CONFLICT (firebase_uid) DO NOTHING;

    SELECT id INTO ashkan_id FROM identities
        WHERE firebase_uid = _ashkan_firebase_uid;

    -- 2) Resolve Meridian Broadcasting's org_id + owner role_id.
    --    Both are seeded earlier in the chain (01_organizations.sql
    --    + 12_meridian_roles.sql); we resolve at runtime rather than
    --    hardcoding because the role's UUID is uuidv7()-generated
    --    at seed time and isn't stable across runs.
    SELECT id INTO meridian_org_id FROM organizations
        WHERE name = 'meridian-broad';
    IF meridian_org_id IS NULL THEN
        RAISE EXCEPTION 'meridian-broad org not found — seed must run after 01_organizations.sql';
    END IF;

    SELECT id INTO meridian_owner_id FROM roles
        WHERE org_id = meridian_org_id AND name = 'owner' AND is_system = true;
    IF meridian_owner_id IS NULL THEN
        RAISE EXCEPTION 'meridian-broad owner role not found — seed must run after 12_meridian_roles.sql';
    END IF;

    -- 3) Bind ashkan as owner. WHERE NOT EXISTS rather than ON
    --    CONFLICT because org_members has a composite UNIQUE that
    --    isn't exposed as a name-based constraint clause sqlc
    --    can target cleanly.
    INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
    SELECT uuidv7(), meridian_org_id, meridian_owner_id, ashkan_id, ashkan_id
    WHERE NOT EXISTS (
        SELECT 1 FROM org_members
        WHERE org_id  = meridian_org_id
          AND user_id = ashkan_id
          AND role_id = meridian_owner_id
    );

    RAISE NOTICE
        'Seeded dev user membership: ashkan (%) bound as owner of meridian-broad (%).',
        ashkan_id, meridian_org_id;
END $$;
