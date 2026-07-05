-- Dev seed for the acme org tenant scaffold: identity, org, roles,
-- verified domain, and the ashkan@acme.com owner binding. Part of the
-- standard seed chain (`make db-seed` → `scripts/seed.sql`).
--
-- SSO config (the KC IdP + client secret) is NOT seeded here — it lives
-- in Keycloak now (managed via the KC Admin API), not the Pivox DB.
-- See the org/SSO refactor for the SsoConfig-as-KC-facade work.
--
-- `ashkan@acme.com` is the SSO test identity. The pinned identity UUID
-- below is the pivox-realm `sub` minted when the acme user is brokered
-- in via the oidc.acme IdP (identities.id == KC sub). Frozen to the
-- real brokered login; see 15_dev_user_membership.sql for the full
-- motivation. DO NOT regenerate without forcing a re-match.
--
-- Idempotent. Re-runnable.

\set ON_ERROR_STOP on

BEGIN;

DO $$
DECLARE
    -- Founder's Firebase localId. Legacy column; retained for the
    -- identities upsert until the Firebase→KC identity seed cleanup.
    _ashkan_firebase_uid CONSTANT TEXT := '2YCxpX5nmQXT5fmjri30SA3ra8t2';

    -- Pinned identity UUID for ashkan@acme.com — the pivox-realm `sub`
    -- minted when the acme user is brokered in via the oidc.acme IdP
    -- (identities.id == KC sub). See 15_dev_user_membership.sql header.
    _ashkan_id CONSTANT UUID := 'b8913e86-52ef-4a1d-8d80-f85e244d6529';

    -- Tier 0001 org UUID for acme. The dev orgs in 01_organizations.sql
    -- occupy suffixes 0001..000a; local-corp in 11_local_corp.sql owns
    -- 000b. Acme takes 000c — first free slot.
    _acme_id CONSTANT UUID := '0192a000-0001-7000-8000-00000000000c';

    -- Tier 0052 role UUIDs for acme (matches 12_dev_org_roles.sql).
    _acme_owner_id  CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0001';
    _acme_admin_id  CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0002';
    _acme_editor_id CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0003';
    _acme_viewer_id CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0004';

    -- Tier 0054 domain, tier 0053 org_members.
    _acme_domain_id CONSTANT UUID := '0192a000-0054-7000-8000-0000000c0001';
    _acme_member_id CONSTANT UUID := '0192a000-0053-7000-8000-0000000c0002';

    ashkan_id UUID := _ashkan_id;
    acme_id   UUID := _acme_id;
    owner_id  UUID := _acme_owner_id;
BEGIN
    -- 0) Founder identity. Pinned id + ON CONFLICT (firebase_uid) keeps
    --    the row stable across re-seeds.
    INSERT INTO identities (id, firebase_uid, email, email_verified)
    VALUES (_ashkan_id, _ashkan_firebase_uid, 'ashkan@acme.com', true)
    ON CONFLICT (firebase_uid) DO NOTHING;

    -- 1) acme organization (idempotent). created_by is back-filled on
    --    conflict so a re-seed after the founder identity exists
    --    populates it without overwriting a non-NULL value.
    INSERT INTO organizations (id, name, display_name, created_by)
    VALUES (_acme_id, 'acme', 'Acme Inc.', ashkan_id)
    ON CONFLICT (name) DO UPDATE SET
        created_by = COALESCE(organizations.created_by, EXCLUDED.created_by);

    -- 2) System roles. UNIQUE(org_id, name) makes ON CONFLICT a safe
    --    no-op when the seed is re-run.
    INSERT INTO roles (id, org_id, name, display_name, description, is_system)
    VALUES
        (_acme_owner_id,  acme_id, 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
        (_acme_admin_id,  acme_id, 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
        (_acme_editor_id, acme_id, 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
        (_acme_viewer_id, acme_id, 'viewer', 'Viewer', 'Read-only access across the organization.', true)
    ON CONFLICT (org_id, name) DO NOTHING;

    -- 3) Verified domain `acme.com`. Globally UNIQUE; CHECK enforces
    --    lowercase. (Domain verification moves to Keycloak in the
    --    org/SSO refactor; kept here as dev data for now.)
    INSERT INTO domains (id, org_id, domain, verification_token, state, verified_time)
    VALUES (_acme_domain_id, acme_id, 'acme.com', 'dev-seed-token', 'VERIFIED', now())
    ON CONFLICT (domain) DO NOTHING;

    -- 4) Bind ashkan as owner of acme. The WHERE NOT EXISTS guard
    --    (rather than a UNIQUE-violation ON CONFLICT) is what makes
    --    this idempotent — re-running the seed after the row exists
    --    is a no-op rather than a constraint error.
    INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
    SELECT _acme_member_id, acme_id, owner_id, ashkan_id, ashkan_id
    WHERE NOT EXISTS (
        SELECT 1 FROM org_members
        WHERE org_id = acme_id
          AND user_id = ashkan_id
          AND role_id = owner_id
    );

    RAISE NOTICE 'Seeded acme org + bound ashkan as owner (org=%, ashkan=%).', acme_id, ashkan_id;
END $$;

COMMIT;
