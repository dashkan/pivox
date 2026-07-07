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
-- UUIDs are PINNED, not uuidv7()-minted:
--
--   The Firebase blocking function stamps a `pivox_user_id` custom
--   claim onto the user's ID token at sign-in, with the value of
--   `identities.id`. A reseed that re-mints that UUID invalidates
--   every Firebase ID token cached client-side (Firebase SDK keeps
--   them in IndexedDB), and token refresh does NOT re-issue
--   custom claims — only a fresh sign-in does. So a `make db-seed`
--   silently broke every dev's signed-in session and made spaces
--   list 403 until they signed out + back in.
--
--   Pinning ashkan's identity UUID to the value his current
--   Firebase ID token already carries makes reseeds non-invalidating.
--   Membership rows use synthetic deterministic UUIDs derived from
--   (org_slug, user) so they also survive reseeds.
--
-- To bind ashkan to an additional dev org: add a (slug, member_uuid)
-- pair to `_dev_org_memberships` below AND add a roles INSERT block
-- for that org in 12_dev_org_roles.sql. Allocate a fresh
-- `0192a000-0053-7000-8000-OOOOOOOOUUUU` member_uuid where OOOOOOOO
-- mirrors the org's UUID suffix and UUUU is the user index (0001 =
-- ashkan.daie). The DO block here resolves the org_id + owner
-- role_id at runtime per slug, so the seed gracefully NOTICEs
-- (instead of failing) if either is missing — helpful when test
-- runs against a partial schema.
--
-- To bind additional dev users: copy the DO block, replace
-- `_ashkan_firebase_uid` + the identity values + the org list.

-- Runs inside the outer transaction from scripts/seed.sql — no
-- inner BEGIN/COMMIT (those would close the wrapping tx and leave
-- scripts/seed.sql's COMMIT firing on a no-tx state, raising a
-- WARNING).

DO $$
DECLARE
    -- Pinned identity UUID. Under Keycloak this IS the user's `sub`
    -- (identities.id == KC sub), so it's frozen to the real KC login
    -- for ashkan.daie@gmail.com — a fresh seed then maps straight to
    -- that login with no manual remap. (Electron still resolves via
    -- _ashkan_firebase_uid above: the blocking fn stamps this id as the
    -- Firebase token's pivox_user_id, so both logins hit one identity.)
    -- DO NOT regenerate — every change forces re-matching the live KC
    -- user across every dev environment using this seed.
    _ashkan_id CONSTANT UUID := '4814ec27-5e21-4756-ad98-e17f69c5a166';

    -- Dev orgs ashkan gets bound to. Mirror the set seeded with
    -- roles in 12_dev_org_roles.sql. acme is intentionally absent
    -- (SSO testing only).
    --
    -- (slug, synthetic_member_uuid). The UUIDs are static
    -- per (org, ashkan) pair; tier 0053 in the seed UUID
    -- convention (`0192a000-TTTT-7000-8000-VVVVVVVVVVVV`).
    _dev_org_memberships CONSTANT TEXT[][] := ARRAY[
        ['meridian-broad',    '0192a000-0053-7000-8000-000000010001'],
        ['pacific-coast-net', '0192a000-0053-7000-8000-000000020001'],
        ['heartland-media',   '0192a000-0053-7000-8000-000000030001'],
        ['summit-sports',     '0192a000-0053-7000-8000-000000050001'],
        ['starlight-studios', '0192a000-0053-7000-8000-0000000a0001']
    ];

    ashkan_id      UUID := _ashkan_id;
    org_slug       TEXT;
    member_uuid    UUID;
    org_id_var     UUID;
    owner_role_id  UUID;
    inserted       INTEGER;
    -- Count of (org_id, user_id, role_id) rows newly written. A
    -- reseed that re-binds the same set has bound=0 because the
    -- WHERE NOT EXISTS suppresses the INSERT — the bound number
    -- reflects net new memberships, not iterations.
    bound          INTEGER := 0;
    skipped        INTEGER := 0;
BEGIN
    -- 1) Identity. Pinned id (the KC `sub`) + ON CONFLICT (id) lets a
    --    real sign-in populate the seeded skeleton with live KC data
    --    (display_name, email_verified, photo_url) without clobbering
    --    it on subsequent reseeds.
    INSERT INTO identities (id, email, email_verified)
    VALUES (_ashkan_id, 'ashkan.daie@gmail.com', true)
    ON CONFLICT (id) DO NOTHING;

    -- 2) Bind ashkan as owner of each dev org. Each iteration is
    --    independent — a missing org or missing owner role logs a
    --    notice and continues rather than failing the whole seed.
    --    WHERE NOT EXISTS keeps reseeds clean against the
    --    UNIQUE(org_id, user_id, role_id) constraint.
    FOR i IN 1 .. array_length(_dev_org_memberships, 1) LOOP
        org_slug    := _dev_org_memberships[i][1];
        member_uuid := _dev_org_memberships[i][2]::UUID;

        SELECT id INTO org_id_var FROM organizations WHERE name = org_slug;
        IF org_id_var IS NULL THEN
            RAISE NOTICE 'Skipping membership for %: org not in organizations table.', org_slug;
            skipped := skipped + 1;
            CONTINUE;
        END IF;

        SELECT id INTO owner_role_id FROM roles
            WHERE org_id = org_id_var AND name = 'owner' AND is_system = true;
        IF owner_role_id IS NULL THEN
            RAISE NOTICE 'Skipping membership for %: owner role missing.', org_slug;
            skipped := skipped + 1;
            CONTINUE;
        END IF;

        INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
        SELECT member_uuid, org_id_var, owner_role_id, ashkan_id, ashkan_id
        WHERE NOT EXISTS (
            SELECT 1 FROM org_members
            WHERE org_id  = org_id_var
              AND user_id = ashkan_id
              AND role_id = owner_role_id
        );

        -- Count rows actually written, not loop iterations. A
        -- reseed against an already-bound org skips the INSERT and
        -- correctly contributes 0 to the bound count.
        GET DIAGNOSTICS inserted = ROW_COUNT;
        bound := bound + inserted;
    END LOOP;

    RAISE NOTICE
        'Seeded dev user membership: ashkan (%) — % new binding(s), % skipped.',
        ashkan_id, bound, skipped;
END $$;
