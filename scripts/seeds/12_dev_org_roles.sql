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
    -- Dev orgs that get the four system roles seeded. Mirror with
    -- _dev_orgs in 15_dev_user_membership.sql. acme intentionally
    -- absent (SSO-only — see 02_acme_sso.sql).
    _dev_orgs CONSTANT TEXT[] := ARRAY[
        'meridian-broad',
        'pacific-coast-net',
        'heartland-media',
        'summit-sports',
        'starlight-studios'
    ];

    org_slug    TEXT;
    org_id_var  UUID;
    inserted    INTEGER;
    seeded_orgs INTEGER := 0;
    skipped     INTEGER := 0;
    new_roles   INTEGER := 0;
BEGIN
    FOREACH org_slug IN ARRAY _dev_orgs
    LOOP
        SELECT id INTO org_id_var FROM organizations WHERE name = org_slug;
        IF org_id_var IS NULL THEN
            RAISE NOTICE 'Skipping roles for org %: not in organizations table.', org_slug;
            skipped := skipped + 1;
            CONTINUE;
        END IF;

        INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
            (uuidv7(), org_id_var, 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
            (uuidv7(), org_id_var, 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
            (uuidv7(), org_id_var, 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
            (uuidv7(), org_id_var, 'viewer', 'Viewer', 'Read-only access across the organization.', true)
        ON CONFLICT (org_id, name) DO NOTHING;

        GET DIAGNOSTICS inserted = ROW_COUNT;
        new_roles := new_roles + inserted;
        seeded_orgs := seeded_orgs + 1;
    END LOOP;

    RAISE NOTICE
        'Seeded dev-org roles: % org(s) covered, % new role(s) inserted, % skipped.',
        seeded_orgs, new_roles, skipped;
END $$;
