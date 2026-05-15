-- Meridian Broadcasting system roles.
--
-- Roles in Pivox are per-org (UNIQUE(org_id, name) per
-- internal/db/migrations/000001_init.up.sql), so every org that
-- wants org_members bound to it needs the 4 system roles seeded
-- separately. dev_acme_sso.sql seeds them for `acme` as part of
-- its larger SSO bootstrap; Meridian doesn't have an SSO seed
-- of its own, so this file fills the gap structurally — every
-- dev who runs `make db-seed` gets Meridian's role catalog
-- without needing a per-user one-off snippet.
--
-- Pattern + role copy mirror dev_acme_sso.sql:140-149.
-- Idempotent via ON CONFLICT (org_id, name).

INSERT INTO roles (id, org_id, name, display_name, description, is_system) VALUES
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
    (uuidv7(), '0192a000-0001-7000-8000-000000000001', 'viewer', 'Viewer', 'Read-only access across the organization.', true)
ON CONFLICT (org_id, name) DO NOTHING;
