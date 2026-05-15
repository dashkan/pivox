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
-- file's INSERT and the array in `15_dev_user_membership.sql`.
--
-- Acme is deliberately NOT included. Acme's roles come from
-- `02_acme_sso.sql` and acme exists for SSO testing only — keeping
-- the dev operator's membership out of acme means signing in via
-- SSO produces a clean "new user joining the org" experience rather
-- than landing in an already-populated org.
--
-- Pattern + role copy mirror dev_acme_sso.sql:140-149.
-- Idempotent via ON CONFLICT (org_id, name) DO NOTHING — safe to
-- re-run.

-- Meridian Broadcasting Group (id: ...000000000001)
INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'viewer', 'Viewer', 'Read-only access across the organization.', true)
ON CONFLICT (org_id, name) DO NOTHING;

-- Pacific Coast Networks (id: ...000000000002)
INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
    (uuidv7(), '0192a000-0001-7000-8000-000000000002', 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000002', 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000002', 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000002', 'viewer', 'Viewer', 'Read-only access across the organization.', true)
ON CONFLICT (org_id, name) DO NOTHING;

-- Heartland Media Corporation (id: ...000000000003)
INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
    (uuidv7(), '0192a000-0001-7000-8000-000000000003', 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000003', 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000003', 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000003', 'viewer', 'Viewer', 'Read-only access across the organization.', true)
ON CONFLICT (org_id, name) DO NOTHING;

-- Summit Sports Media (id: ...000000000005)
INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
    (uuidv7(), '0192a000-0001-7000-8000-000000000005', 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000005', 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000005', 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000005', 'viewer', 'Viewer', 'Read-only access across the organization.', true)
ON CONFLICT (org_id, name) DO NOTHING;

-- Starlight Studios International (id: ...00000000000a)
INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
    (uuidv7(), '0192a000-0001-7000-8000-00000000000a', 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
    (uuidv7(), '0192a000-0001-7000-8000-00000000000a', 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
    (uuidv7(), '0192a000-0001-7000-8000-00000000000a', 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
    (uuidv7(), '0192a000-0001-7000-8000-00000000000a', 'viewer', 'Viewer', 'Read-only access across the organization.', true)
ON CONFLICT (org_id, name) DO NOTHING;
