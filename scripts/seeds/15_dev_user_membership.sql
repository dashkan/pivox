-- Dev user membership.
--
-- Binds the dev operator (ashkan.daie@gmail.com) as `owner` of
-- every dev-accessible org seeded in 12_dev_org_roles.sql so a
-- fresh `make db-seed` produces a multi-org DB where the
-- macOS/WinUI dotnet apps' org-selector dropdowns have meaningful
-- variety to pick from (and the macOS Library smoke test still
-- lands on a Meridian-bound user since Meridian carries the
-- seeded asset thumbnails).
--
-- Acme is deliberately excluded. acme has SSO configured
-- (02_acme_sso.sql) and is reserved for SSO end-to-end testing —
-- the SSO flow has to land in a "new member joining acme" UX, not
-- "user is already an owner of acme."
--
-- Idempotent:
--   - identities.ON CONFLICT (firebase_uid) preserves any
--     blocking-fn-populated state (display_name, email_verified,
--     last_login_time) from a real sign-in.
--   - org_members.WHERE NOT EXISTS sidesteps the UNIQUE(org_id,
--     user_id, role_id) constraint so re-running the seed is a
--     no-op rather than a constraint error.
--
-- To bind ashkan to an additional dev org: add the org's slug to
-- the ARRAY literal below AND add a roles INSERT block for that
-- org in 12_dev_org_roles.sql. The DO block here resolves the
-- org_id + owner role_id at runtime per slug, so the seed
-- gracefully NOTICEs (instead of failing) if either is missing —
-- helpful when test runs against a partial schema.
--
-- To bind additional dev users: copy the DO block, replace
-- `_ashkan_firebase_uid` + the identity values + the org list.

-- Runs inside the outer transaction from scripts/seed.sql — no
-- inner BEGIN/COMMIT (those would close the wrapping tx and leave
-- scripts/seed.sql's COMMIT firing on a no-tx state, raising a
-- WARNING).

DO $$
DECLARE
    -- Pivox identity. Pre-existing firebase_uid pinned so a
    -- reseed against a real-signed-in account preserves the
    -- blocking-fn-populated fields.
    _ashkan_firebase_uid CONSTANT TEXT := 'ScQytJWi2ycF3jiiBlRazncbfQB3';

    -- Dev orgs ashkan gets bound to. Mirror the set seeded with
    -- roles in 12_dev_org_roles.sql. acme is intentionally absent
    -- (SSO testing only).
    _dev_orgs CONSTANT TEXT[] := ARRAY[
        'meridian-broad',
        'pacific-coast-net',
        'heartland-media',
        'summit-sports',
        'starlight-studios'
    ];

    ashkan_id      UUID;
    org_slug       TEXT;
    org_id_var     UUID;
    owner_role_id  UUID;
    bound_count    INTEGER := 0;
    skipped_count  INTEGER := 0;
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

    -- 2) Bind ashkan as owner of each dev org. Each iteration is
    --    independent — a missing org or missing owner role logs a
    --    notice and continues rather than failing the whole seed.
    --    WHERE NOT EXISTS keeps reseeds clean against the
    --    UNIQUE(org_id, user_id, role_id) constraint.
    FOREACH org_slug IN ARRAY _dev_orgs
    LOOP
        SELECT id INTO org_id_var FROM organizations WHERE name = org_slug;
        IF org_id_var IS NULL THEN
            RAISE NOTICE 'Skipping membership: org "%" not found (expected in 01_organizations.sql).', org_slug;
            skipped_count := skipped_count + 1;
            CONTINUE;
        END IF;

        SELECT id INTO owner_role_id FROM roles
            WHERE org_id = org_id_var AND name = 'owner' AND is_system = true;
        IF owner_role_id IS NULL THEN
            RAISE NOTICE 'Skipping membership: owner role for "%" not found (expected in 12_dev_org_roles.sql).', org_slug;
            skipped_count := skipped_count + 1;
            CONTINUE;
        END IF;

        INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
        SELECT uuidv7(), org_id_var, owner_role_id, ashkan_id, ashkan_id
        WHERE NOT EXISTS (
            SELECT 1 FROM org_members
            WHERE org_id  = org_id_var
              AND user_id = ashkan_id
              AND role_id = owner_role_id
        );

        bound_count := bound_count + 1;
    END LOOP;

    RAISE NOTICE
        'Seeded dev user membership: ashkan (%) bound as owner of % dev org(s); % skipped.',
        ashkan_id, bound_count, skipped_count;
END $$;
