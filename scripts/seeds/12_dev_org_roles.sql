-- Dev-org system roles.
--
-- Roles in Pivox are per-org (UNIQUE(org_id, name) per
-- internal/db/migrations/000001_init.up.sql), so every org that
-- wants org_members bound to it needs the 4 system roles seeded
-- separately. dev_acme_sso.sql seeds them for `acme` as part of
-- its larger SSO bootstrap; the orgs here don't have SSO seeds of
-- their own, so this file fills the gap — every dev who runs
-- `make db-seed` gets a usable role catalog across a handful of
-- dev orgs without per-user one-off snippets.
--
-- The orgs covered here mirror the set in
-- `15_dev_user_membership.sql` (the dev user gets bound as owner
-- of each). To add another dev-accessible org: add it to BOTH this
-- file's _dev_orgs array and the array in
-- `15_dev_user_membership.sql`.
--
-- Acme is deliberately NOT included. Acme's roles come from
-- `02_acme_sso.sql` and acme exists for SSO testing only — keeping
-- the dev operator's membership out of acme means signing in via
-- SSO produces a clean "new user joining the org" experience rather
-- than landing in an already-populated org.
--
-- Lookup by slug, not by hardcoded UUID. Hardcoded UUIDs duplicate
-- the source of truth in `01_organizations.sql` and silently rot
-- when an org's UUID changes there (the role insert simply fails
-- the FK and rolls the whole seed transaction back, with no error
-- pointing at which org). Slug lookups produce a targeted NOTICE
-- and continue when the org isn't found, mirroring the
-- 15_dev_user_membership.sql resilience pattern.
--
-- Idempotent via ON CONFLICT (org_id, name) DO NOTHING — safe to
-- re-run.

-- Runs inside the outer transaction from scripts/seed.sql — no
-- inner BEGIN/COMMIT.

DO $$
DECLARE
    -- (slug, owner_uuid, admin_uuid, editor_uuid, viewer_uuid) per dev org.
    --
    -- UUIDs are pinned, tier 0052 in the seed convention
    -- (`0192a000-TTTT-7000-8000-VVVVVVVVVVVV`). Suffix encodes
    -- (org_index, role_index): the last 8 hex chars are
    -- OOOOOOOORRRR where OOOOOOOO mirrors the org's UUID suffix
    -- from 01_organizations.sql and RRRR is 0001=owner,
    -- 0002=admin, 0003=editor, 0004=viewer.
    --
    -- Pinned (instead of uuidv7()) because reseeding with fresh
    -- role UUIDs would invalidate cached role-id references in
    -- any system that snapshots them — and matches the broader
    -- "reseeds don't break sessions" pattern; see
    -- 15_dev_user_membership.sql header for motivation.
    _dev_org_roles CONSTANT TEXT[][] := ARRAY[
        ['meridian-broad',
            '0192a000-0052-7000-8000-000000010001',
            '0192a000-0052-7000-8000-000000010002',
            '0192a000-0052-7000-8000-000000010003',
            '0192a000-0052-7000-8000-000000010004'],
        ['pacific-coast-net',
            '0192a000-0052-7000-8000-000000020001',
            '0192a000-0052-7000-8000-000000020002',
            '0192a000-0052-7000-8000-000000020003',
            '0192a000-0052-7000-8000-000000020004'],
        ['heartland-media',
            '0192a000-0052-7000-8000-000000030001',
            '0192a000-0052-7000-8000-000000030002',
            '0192a000-0052-7000-8000-000000030003',
            '0192a000-0052-7000-8000-000000030004'],
        ['summit-sports',
            '0192a000-0052-7000-8000-000000050001',
            '0192a000-0052-7000-8000-000000050002',
            '0192a000-0052-7000-8000-000000050003',
            '0192a000-0052-7000-8000-000000050004'],
        ['starlight-studios',
            '0192a000-0052-7000-8000-0000000a0001',
            '0192a000-0052-7000-8000-0000000a0002',
            '0192a000-0052-7000-8000-0000000a0003',
            '0192a000-0052-7000-8000-0000000a0004'],
        ['local-corp',
            '0192a000-0052-7000-8000-0000000b0001',
            '0192a000-0052-7000-8000-0000000b0002',
            '0192a000-0052-7000-8000-0000000b0003',
            '0192a000-0052-7000-8000-0000000b0004']
    ];

    org_slug    TEXT;
    org_id_var  UUID;
    inserted    INTEGER;
    seeded_orgs INTEGER := 0;
    skipped     INTEGER := 0;
    new_roles   INTEGER := 0;
BEGIN
    FOR i IN 1 .. array_length(_dev_org_roles, 1) LOOP
        org_slug := _dev_org_roles[i][1];

        SELECT id INTO org_id_var FROM organizations WHERE name = org_slug;
        IF org_id_var IS NULL THEN
            RAISE NOTICE 'Skipping roles for org %: not in organizations table.', org_slug;
            skipped := skipped + 1;
            CONTINUE;
        END IF;

        INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
            (_dev_org_roles[i][2]::UUID, org_id_var, 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
            (_dev_org_roles[i][3]::UUID, org_id_var, 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
            (_dev_org_roles[i][4]::UUID, org_id_var, 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
            (_dev_org_roles[i][5]::UUID, org_id_var, 'viewer', 'Viewer', 'Read-only access across the organization.', true)
        ON CONFLICT (org_id, name) DO NOTHING;

        GET DIAGNOSTICS inserted = ROW_COUNT;
        new_roles := new_roles + inserted;
        seeded_orgs := seeded_orgs + 1;
    END LOOP;

    RAISE NOTICE
        'Seeded dev-org roles: % org(s) covered, % new role(s) inserted, % skipped.',
        seeded_orgs, new_roles, skipped;
END $$;
