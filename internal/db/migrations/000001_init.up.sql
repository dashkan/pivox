-- 000001_init.up.sql
-- Complete schema for Pivox.
--
-- Field ordering convention (per table):
--   1. id (PK)
--   2. Foreign keys / relationships
--   3. Identity fields (name, key_id, etc.)
--   4. Domain fields (display_name, description, config, etc.)
--   5. State / lifecycle
--   6. Etag / revision
--   7. Audit (created_by, updated_by, deleted_by)
--   8. Timestamps (create_time, update_time, delete_time, purge_time)
--
-- Other conventions:
--   PK: id UUID PRIMARY KEY DEFAULT uuidv7()
--   Etag: md5(now()::text) — deterministic per-transaction, regenerated on every write
--   Revision: monotonically incrementing per-row counter
--   Soft delete: delete_time (nullable), purge_time (nullable)

-- ============================================================================
-- Enum types
-- ============================================================================
CREATE TYPE resource_state AS ENUM ('ACTIVE', 'DELETE_REQUESTED');
CREATE TYPE custom_domain_state AS ENUM (
    'PENDING', 'PROVISIONING', 'ACTIVE', 'FAILED', 'DEACTIVATED'
);
CREATE TYPE role_member_type AS ENUM ('user', 'group');
CREATE TYPE project_role AS ENUM ('ADMIN', 'EDITOR', 'VIEWER');
CREATE TYPE project_member_type AS ENUM ('user', 'group');
CREATE TYPE invitation_state AS ENUM (
    'PENDING', 'ACCEPTED', 'DECLINED', 'REVOKED', 'EXPIRED'
);
CREATE TYPE storage_gateway_state AS ENUM (
    'PROVISIONING', 'ACTIVE', 'DEGRADED', 'OFFLINE'
);
CREATE TYPE cert_state AS ENUM (
    'PENDING', 'ACTIVE', 'EXPIRING', 'EXPIRED'
);
CREATE TYPE eviction_policy AS ENUM ('LRU', 'LFU');
CREATE TYPE agent_state AS ENUM (
    'CONNECTING', 'CONNECTED', 'DRAINING', 'UPGRADING', 'DISCONNECTED'
);
CREATE TYPE endpoint_state AS ENUM ('ACTIVE', 'INACTIVE', 'UNREACHABLE');

-- ============================================================================
-- operations (LRO storage)
-- ============================================================================
CREATE TABLE operations (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    prefix      TEXT NOT NULL DEFAULT '',
    done        BOOLEAN NOT NULL DEFAULT false,
    metadata    JSONB,
    result      JSONB,
    error_code  INTEGER,
    error_message TEXT,
    created_by  TEXT NOT NULL DEFAULT '',
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '30 days'
);
CREATE INDEX idx_operations_pending ON operations (create_time) WHERE done = false;
CREATE INDEX idx_operations_expire ON operations (expire_time) WHERE done = true;
CREATE INDEX idx_operations_prefix ON operations (prefix, create_time DESC);

-- ============================================================================
-- organizations
-- ============================================================================
CREATE TABLE organizations (
    id                    UUID PRIMARY KEY DEFAULT uuidv7(),
    -- identity
    name                  TEXT UNIQUE NOT NULL,
    -- domain
    display_name          TEXT NOT NULL DEFAULT '',
    annotations           JSONB NOT NULL DEFAULT '{}',
    tenant_id             TEXT NOT NULL DEFAULT '',
    -- Immutable founder pointer. `created_by_account_id` references the
    -- account row of whoever created this org (FK added after the
    -- `accounts` table is declared further down). Survives membership
    -- changes (the user can leave the org without breaking this FK).
    -- Owners are tracked separately via `users.role = 'owner'`; an
    -- org can have N owners and "≥1 owner" is enforced at the service
    -- mutation boundary, not here.
    created_by_account_id UUID,
    -- state
    state                 resource_state NOT NULL DEFAULT 'ACTIVE',
    -- versioning
    etag                  TEXT NOT NULL DEFAULT md5(now()::text),
    revision              INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by            TEXT NOT NULL DEFAULT '',
    updated_by            TEXT NOT NULL DEFAULT '',
    deleted_by            TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time           TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time           TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time           TIMESTAMPTZ,
    purge_time            TIMESTAMPTZ
);
CREATE INDEX idx_organizations_name ON organizations (name) WHERE delete_time IS NULL;
CREATE UNIQUE INDEX idx_organizations_tenant_id
  ON organizations (tenant_id) WHERE tenant_id != '' AND delete_time IS NULL;

-- ============================================================================
-- custom_domains (per-org, LRO-managed)
-- ============================================================================
CREATE TABLE custom_domains (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    domain      TEXT NOT NULL,
    -- state
    state       custom_domain_state NOT NULL DEFAULT 'PENDING',
    -- domain
    dns_records JSONB NOT NULL DEFAULT '[]',
    -- versioning
    etag        TEXT NOT NULL DEFAULT md5(now()::text),
    -- audit
    created_by  TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    verify_time TIMESTAMPTZ,
    -- constraints
    UNIQUE(org_id, domain)
);
CREATE INDEX idx_custom_domains_org ON custom_domains (org_id);
CREATE UNIQUE INDEX idx_custom_domains_domain ON custom_domains (domain);

-- ============================================================================
-- projects
-- ============================================================================
CREATE TABLE projects (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id         UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    name           TEXT NOT NULL,
    -- domain
    display_name   TEXT NOT NULL DEFAULT '',
    labels         JSONB NOT NULL DEFAULT '{}',
    -- state
    state          resource_state NOT NULL DEFAULT 'ACTIVE',
    -- versioning
    etag           TEXT NOT NULL DEFAULT md5(now()::text),
    revision       INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by     TEXT NOT NULL DEFAULT '',
    updated_by     TEXT NOT NULL DEFAULT '',
    deleted_by     TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time    TIMESTAMPTZ,
    purge_time     TIMESTAMPTZ,
    -- constraints
    UNIQUE(org_id, name)
);
CREATE INDEX idx_projects_org ON projects (org_id) WHERE delete_time IS NULL;

-- ============================================================================
-- storage_gateways (per-org, on-prem S3 reverse proxy + cache cluster)
-- ============================================================================
CREATE TABLE storage_gateways (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id              UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    name                TEXT NOT NULL,
    -- domain
    display_name        TEXT NOT NULL DEFAULT '',
    ip_addresses        TEXT[] NOT NULL DEFAULT '{}',
    registration_token  TEXT NOT NULL,
    target_version      TEXT NOT NULL DEFAULT '',
    current_version     TEXT NOT NULL DEFAULT '',
    hostname            TEXT NOT NULL DEFAULT '',
    annotations         JSONB NOT NULL DEFAULT '{}',
    -- state
    state               storage_gateway_state NOT NULL DEFAULT 'PROVISIONING',
    cert_state          cert_state NOT NULL DEFAULT 'PENDING',
    cert_expiry_time    TIMESTAMPTZ,
    -- versioning
    etag                TEXT NOT NULL DEFAULT md5(now()::text),
    revision            INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by          TEXT NOT NULL DEFAULT '',
    updated_by          TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(org_id, name)
);
CREATE INDEX idx_storage_gateways_org ON storage_gateways (org_id);
CREATE UNIQUE INDEX idx_storage_gateways_token
  ON storage_gateways (registration_token);

-- ============================================================================
-- storage_agents (per-gateway, server-managed via bidi gRPC)
-- ============================================================================
CREATE TABLE storage_agents (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    gateway_id      UUID NOT NULL REFERENCES storage_gateways(id) ON DELETE CASCADE,
    -- domain
    ip_address      TEXT NOT NULL DEFAULT '',
    hostname        TEXT NOT NULL DEFAULT '',
    version         TEXT NOT NULL DEFAULT '',
    cache_used_gb   INTEGER NOT NULL DEFAULT 0,
    -- state
    state           agent_state NOT NULL DEFAULT 'CONNECTING',
    cert_expiry_time TIMESTAMPTZ,
    -- timestamps
    join_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_time  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_storage_agents_gateway ON storage_agents (gateway_id);
CREATE UNIQUE INDEX idx_storage_agents_gateway_ip
  ON storage_agents (gateway_id, ip_address);

-- ============================================================================
-- storage_endpoints (S3-compatible bucket per gateway)
-- ============================================================================
CREATE TABLE storage_endpoints (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    gateway_id        UUID NOT NULL REFERENCES storage_gateways(id) ON DELETE CASCADE,
    -- identity
    name              TEXT NOT NULL,
    -- domain
    display_name      TEXT NOT NULL DEFAULT '',
    configuration     JSONB NOT NULL,  -- type-specific config (S3Configuration, etc.)
    -- cache
    cache_enabled     BOOLEAN NOT NULL DEFAULT true,
    cache_max_size_gb INTEGER NOT NULL DEFAULT 0,
    cache_eviction    eviction_policy NOT NULL DEFAULT 'LRU',
    cache_ttl_hours   INTEGER NOT NULL DEFAULT 0,
    annotations       JSONB NOT NULL DEFAULT '{}',
    -- state
    state             endpoint_state NOT NULL DEFAULT 'ACTIVE',
    -- versioning
    etag              TEXT NOT NULL DEFAULT md5(now()::text),
    revision          INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by        TEXT NOT NULL DEFAULT '',
    updated_by        TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(gateway_id, name)
);
CREATE INDEX idx_storage_endpoints_gateway ON storage_endpoints (gateway_id);

-- ============================================================================
-- storage_agent_audit (bidi message audit log, excludes heartbeat/telemetry)
-- ============================================================================
CREATE TABLE storage_agent_audit (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    gateway_id   UUID NOT NULL REFERENCES storage_gateways(id) ON DELETE CASCADE,
    agent_id     UUID,  -- NULL for pre-handshake messages
    -- message
    message_id   TEXT NOT NULL,
    direction    TEXT NOT NULL,  -- 'inbound' or 'outbound'
    message_type TEXT NOT NULL,  -- 'handshake', 'handshake_ack', 'config_update', etc.
    payload      JSONB,          -- message content (secrets redacted)
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_storage_agent_audit_gateway ON storage_agent_audit (gateway_id, create_time DESC);
CREATE INDEX idx_storage_agent_audit_agent ON storage_agent_audit (agent_id, create_time DESC);
CREATE INDEX idx_storage_agent_audit_time ON storage_agent_audit (create_time);

-- ============================================================================
-- tag_keys
-- ============================================================================
CREATE TABLE tag_keys (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    short_name      TEXT NOT NULL,
    namespaced_name TEXT UNIQUE NOT NULL,
    -- domain
    description     TEXT NOT NULL DEFAULT '',
    annotations     JSONB NOT NULL DEFAULT '{}',
    -- versioning
    etag            TEXT NOT NULL DEFAULT md5(now()::text),
    revision        INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by      TEXT NOT NULL DEFAULT '',
    updated_by      TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(org_id, short_name)
);
CREATE INDEX idx_tag_keys_org ON tag_keys (org_id);
CREATE INDEX idx_tag_keys_namespaced ON tag_keys (namespaced_name);

-- ============================================================================
-- tag_values
-- ============================================================================
CREATE TABLE tag_values (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    tag_key_id        UUID NOT NULL REFERENCES tag_keys(id) ON DELETE RESTRICT,
    -- identity
    short_name        TEXT NOT NULL,
    namespaced_name   TEXT UNIQUE NOT NULL,
    -- domain
    description       TEXT NOT NULL DEFAULT '',
    annotations       JSONB NOT NULL DEFAULT '{}',
    -- versioning
    etag              TEXT NOT NULL DEFAULT md5(now()::text),
    revision          INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by        TEXT NOT NULL DEFAULT '',
    updated_by        TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(tag_key_id, short_name)
);
CREATE INDEX idx_tag_values_tag_key ON tag_values (tag_key_id);
CREATE INDEX idx_tag_values_namespaced ON tag_values (namespaced_name);

-- ============================================================================
-- tag_bindings
-- ============================================================================
CREATE TYPE tag_binding_origin AS ENUM ('USER', 'SYSTEM', 'AI');

CREATE TABLE tag_bindings (
    id                        UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    parent_resource           TEXT NOT NULL,
    tag_value_id              UUID NOT NULL REFERENCES tag_values(id) ON DELETE RESTRICT,
    -- domain
    origin                    tag_binding_origin NOT NULL DEFAULT 'USER',
    annotations               JSONB NOT NULL DEFAULT '{}',
    -- versioning
    etag                      TEXT NOT NULL DEFAULT md5(now()::text),
    -- audit
    created_by                TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time               TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(parent_resource, tag_value_id)
);
CREATE INDEX idx_tag_bindings_parent ON tag_bindings (parent_resource);
CREATE INDEX idx_tag_bindings_tag_value ON tag_bindings (tag_value_id);
CREATE INDEX idx_tag_bindings_origin ON tag_bindings (parent_resource, origin);

-- ============================================================================
-- api_keys
-- ============================================================================
CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id       UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    key_id       TEXT NOT NULL,
    key_string   TEXT UNIQUE NOT NULL,
    -- domain
    display_name TEXT NOT NULL DEFAULT '',
    annotations  JSONB NOT NULL DEFAULT '{}',
    restrictions JSONB,
    -- versioning
    etag         TEXT NOT NULL DEFAULT md5(now()::text),
    revision     INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by   TEXT NOT NULL DEFAULT '',
    updated_by   TEXT NOT NULL DEFAULT '',
    deleted_by   TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time  TIMESTAMPTZ,
    purge_time   TIMESTAMPTZ,
    -- constraints
    UNIQUE(org_id, key_id)
);
CREATE INDEX idx_api_keys_org ON api_keys (org_id) WHERE delete_time IS NULL;
CREATE INDEX idx_api_keys_key_string ON api_keys (key_string) WHERE delete_time IS NULL;

-- ============================================================================
-- iam_policies (shared IAM storage)
-- ============================================================================
CREATE TABLE iam_policies (
    resource_id   UUID PRIMARY KEY,
    resource_type TEXT NOT NULL,
    policy        JSONB NOT NULL DEFAULT '{}',
    etag          TEXT NOT NULL DEFAULT md5(now()::text),
    -- audit
    created_by    TEXT NOT NULL DEFAULT '',
    updated_by    TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time   TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_iam_policies_type ON iam_policies (resource_type);

-- ============================================================================
-- accounts (global Firebase Auth cache — internal, no proto)
-- ============================================================================
CREATE TABLE accounts (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- identity (Firebase)
    firebase_uid    TEXT NOT NULL UNIQUE,
    -- domain (synced from Firebase)
    email           TEXT NOT NULL DEFAULT '',
    email_verified  BOOLEAN NOT NULL DEFAULT false,
    display_name    TEXT NOT NULL DEFAULT '',
    photo_url       TEXT NOT NULL DEFAULT '',
    disabled        BOOLEAN NOT NULL DEFAULT false,
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_time TIMESTAMPTZ
);
CREATE INDEX idx_accounts_email ON accounts (email);

-- ============================================================================
-- users (per-org membership)
--
-- One row per (org, account) pairing — the join that says "this account
-- has access to this org". Created in two ways:
--   1. By `CreateOrganization` for the founder, with role='owner'.
--   2. By `AcceptInvitation` (future) for invitees, role from the invite.
--
-- "≥1 owner per org" is invariant; enforced at the service mutation
-- boundary (role-change / membership-delete handlers reject ops that
-- would zero out owners). Not enforced by DB triggers — triggers
-- surprise readers and complicate test setup.
-- ============================================================================
CREATE TYPE org_role AS ENUM ('owner', 'member');

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    -- domain
    role       org_role NOT NULL DEFAULT 'member',
    -- versioning
    etag       TEXT NOT NULL DEFAULT md5(now()::text),
    revision   INTEGER NOT NULL DEFAULT 1,
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(org_id, account_id)
);
CREATE INDEX idx_users_org ON users (org_id);
CREATE INDEX idx_users_account ON users (account_id);
-- Lets us cheaply enforce "≥1 owner per org" by counting owner rows.
CREATE INDEX idx_users_org_owner ON users (org_id) WHERE role = 'owner';

-- FK from organizations.created_by_account_id → accounts.id, added
-- here because `accounts` was declared after `organizations` in this
-- migration. ON DELETE SET NULL: deleting an account preserves the
-- org but nulls out the founder pointer (the org survives, ownership
-- is tracked via `users.role` anyway).
ALTER TABLE organizations
  ADD CONSTRAINT fk_organizations_created_by_account
  FOREIGN KEY (created_by_account_id) REFERENCES accounts(id) ON DELETE SET NULL;

-- ============================================================================
-- project_members (user or group <-> project, fixed roles)
-- ============================================================================
CREATE TABLE project_members (
    -- relationships
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    member_id   UUID NOT NULL,
    member_type project_member_type NOT NULL,
    -- domain
    role        project_role NOT NULL,
    -- audit
    created_by  TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    PRIMARY KEY (project_id, member_id, member_type)
);
CREATE INDEX idx_project_members_member ON project_members (member_id, member_type);

-- ============================================================================
-- groups
-- ============================================================================
CREATE TABLE groups (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id       UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- domain
    display_name TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    annotations  JSONB NOT NULL DEFAULT '{}',
    -- state
    state        resource_state NOT NULL DEFAULT 'ACTIVE',
    -- versioning
    etag         TEXT NOT NULL DEFAULT md5(now()::text),
    revision     INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by   TEXT NOT NULL DEFAULT '',
    updated_by   TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_groups_org ON groups (org_id);

-- ============================================================================
-- group_members (user <-> group)
-- ============================================================================
CREATE TABLE group_members (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- audit
    created_by TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(group_id, user_id)
);
CREATE INDEX idx_group_members_group ON group_members (group_id);
CREATE INDEX idx_group_members_user ON group_members (user_id);

-- ============================================================================
-- permissions (system-defined catalog, read-only via ListPermissions RPC)
-- ============================================================================
CREATE TABLE permissions (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    -- identity
    permission_id TEXT NOT NULL UNIQUE,
    -- domain
    display_name  TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- roles (org-scoped, system + custom)
-- ============================================================================
CREATE TABLE roles (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id       UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- domain
    display_name TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    is_system    BOOLEAN NOT NULL DEFAULT false,
    annotations  JSONB NOT NULL DEFAULT '{}',
    -- state
    state        resource_state NOT NULL DEFAULT 'ACTIVE',
    -- versioning
    etag         TEXT NOT NULL DEFAULT md5(now()::text),
    revision     INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by   TEXT NOT NULL DEFAULT '',
    updated_by   TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_roles_org ON roles (org_id);

-- ============================================================================
-- role_permissions (role <-> permission)
-- ============================================================================
CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);
CREATE INDEX idx_role_permissions_permission ON role_permissions (permission_id);

-- ============================================================================
-- role_members (user or group <-> org role)
-- ============================================================================
CREATE TABLE role_members (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    member_id   UUID NOT NULL,
    member_type role_member_type NOT NULL,
    -- audit
    created_by  TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(role_id, member_id, member_type)
);
CREATE INDEX idx_role_members_role ON role_members (role_id);
CREATE INDEX idx_role_members_member ON role_members (member_id, member_type);

-- ============================================================================
-- invitations
-- ============================================================================
CREATE TABLE invitations (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role_id     UUID REFERENCES roles(id) ON DELETE SET NULL,
    -- domain
    email       TEXT NOT NULL,
    token       TEXT NOT NULL UNIQUE,
    -- state
    state       invitation_state NOT NULL DEFAULT 'PENDING',
    -- versioning
    etag        TEXT NOT NULL DEFAULT md5(now()::text),
    -- audit
    created_by  TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '7 days',
    accept_time TIMESTAMPTZ
);
CREATE INDEX idx_invitations_org ON invitations (org_id);
CREATE INDEX idx_invitations_email ON invitations (email);
CREATE INDEX idx_invitations_token ON invitations (token) WHERE state = 'PENDING';
CREATE INDEX idx_invitations_pending ON invitations (expire_time)
    WHERE state = 'PENDING';

-- ============================================================================
-- invitation_policies (singleton per org)
-- ============================================================================
CREATE TABLE invitation_policies (
    -- relationships
    org_id                        UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    -- domain
    disable_public_email_addresses BOOLEAN NOT NULL DEFAULT false,
    allowed_domains               TEXT[] NOT NULL DEFAULT '{}',
    disallowed_domains            TEXT[] NOT NULL DEFAULT '{}',
    -- versioning
    etag                          TEXT NOT NULL DEFAULT md5(now()::text),
    -- timestamps
    update_time                   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- public_email_domains (server-maintained, not exposed in API)
-- ============================================================================
CREATE TABLE public_email_domains (
    domain      TEXT PRIMARY KEY,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Seed: public email domains
-- ============================================================================
INSERT INTO public_email_domains (domain) VALUES
  ('gmail.com'),
  ('googlemail.com'),
  ('yahoo.com'),
  ('yahoo.co.uk'),
  ('yahoo.co.jp'),
  ('outlook.com'),
  ('hotmail.com'),
  ('hotmail.co.uk'),
  ('live.com'),
  ('msn.com'),
  ('aol.com'),
  ('icloud.com'),
  ('me.com'),
  ('mac.com'),
  ('mail.com'),
  ('protonmail.com'),
  ('proton.me'),
  ('zoho.com'),
  ('yandex.com'),
  ('yandex.ru'),
  ('gmx.com'),
  ('gmx.net'),
  ('fastmail.com'),
  ('tutanota.com'),
  ('tuta.com');

-- ============================================================================
-- Seed: permissions (org-level only; project access uses project_members roles)
-- ============================================================================
INSERT INTO permissions (permission_id, display_name, description) VALUES
  -- Organization management
  ('organizations.get', 'Get Organization', 'View organization details'),
  ('organizations.update', 'Update Organization', 'Modify organization settings'),
  ('organizations.delete', 'Delete Organization', 'Delete the organization'),
  ('organizations.getIamPolicy', 'Get Org IAM Policy', 'View org access policies'),
  ('organizations.setIamPolicy', 'Set Org IAM Policy', 'Modify org access policies'),
  -- Project creation (org-level; within-project access is project-role based)
  ('projects.create', 'Create Project', 'Create new projects in the organization'),
  -- User management
  ('users.get', 'Get User', 'View user details'),
  ('users.list', 'List Users', 'List users in the organization'),
  -- Group management
  ('groups.create', 'Create Group', 'Create new groups'),
  ('groups.get', 'Get Group', 'View group details'),
  ('groups.update', 'Update Group', 'Modify groups'),
  ('groups.delete', 'Delete Group', 'Delete groups'),
  ('groups.manageMembers', 'Manage Group Members', 'Add/remove group members'),
  -- Role management
  ('roles.create', 'Create Role', 'Create custom roles'),
  ('roles.get', 'Get Role', 'View role details'),
  ('roles.update', 'Update Role', 'Modify custom roles'),
  ('roles.delete', 'Delete Role', 'Delete custom roles'),
  ('roles.manageMembers', 'Manage Role Members', 'Add/remove role members'),
  -- Invitation management
  ('invitations.create', 'Create Invitation', 'Invite users to the organization'),
  ('invitations.get', 'Get Invitation', 'View invitation details'),
  ('invitations.list', 'List Invitations', 'List invitations in the organization'),
  ('invitations.delete', 'Delete Invitation', 'Revoke invitations'),
  ('invitations.updatePolicy', 'Update Invitation Policy', 'Modify invitation policy'),
  -- API key management
  ('apikeys.create', 'Create API Key', 'Create API keys'),
  ('apikeys.get', 'Get API Key', 'View API key details'),
  ('apikeys.update', 'Update API Key', 'Modify API keys'),
  ('apikeys.delete', 'Delete API Key', 'Delete API keys'),
  -- Storage gateway management
  ('storage.gateways.create', 'Create Storage Gateway', 'Create storage gateways'),
  ('storage.gateways.get', 'Get Storage Gateway', 'View storage gateway details'),
  ('storage.gateways.update', 'Update Storage Gateway', 'Modify storage gateways'),
  ('storage.gateways.delete', 'Delete Storage Gateway', 'Delete storage gateways'),
  ('storage.gateways.upgrade', 'Upgrade Storage Gateway', 'Trigger gateway upgrades'),
  ('storage.endpoints.create', 'Create Storage Endpoint', 'Create storage endpoints'),
  ('storage.endpoints.get', 'Get Storage Endpoint', 'View storage endpoint details'),
  ('storage.endpoints.update', 'Update Storage Endpoint', 'Modify storage endpoints'),
  ('storage.endpoints.delete', 'Delete Storage Endpoint', 'Delete storage endpoints'),
  ('storage.agents.get', 'Get Agent', 'View agent details'),
  ('storage.agents.drain', 'Drain Agent', 'Drain agents for maintenance'),
  ('storage.agents.remove', 'Remove Agent', 'Remove agents from gateway pool');

-- ============================================================================
-- auth_token_codes (short-lived opaque codes for Electron provider linking)
-- ============================================================================
CREATE TABLE auth_token_codes (
    code        UUID PRIMARY KEY DEFAULT uuidv7(),
    id_token    TEXT NOT NULL,
    consumed    BOOLEAN NOT NULL DEFAULT false,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '60 seconds'
);
CREATE INDEX idx_auth_token_codes_expire ON auth_token_codes (expire_time);

-- ============================================================================
-- delegated_auth_sessions (AUTHN-07: plugins delegate auth to the Pivox app)
--
-- Plugins hosted in third-party processes (NRCS ActiveX, Adobe UXP) cannot
-- safely perform interactive auth. Instead they create a session here, launch
-- the Pivox app via deep link, and poll until a custom token is available.
-- The app completes the session after the user signs in through any provider.
-- ============================================================================
CREATE TYPE delegated_auth_session_state AS ENUM (
    'PENDING', 'APPROVED', 'DENIED', 'EXPIRED'
);

CREATE TABLE delegated_auth_sessions (
    -- `gen_random_uuid()` (not `uuidv7()`) is deliberate — this code appears
    -- in URLs used for inter-process auth handoff and must not be sequential
    -- or time-guessable.
    code         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    state        delegated_auth_session_state NOT NULL DEFAULT 'PENDING',
    custom_token TEXT,
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time  TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_delegated_auth_sessions_expire ON delegated_auth_sessions (expire_time);

-- ============================================================================
-- pgvector extension (for asset semantic search)
-- ============================================================================
CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================================
-- Asset enum types
-- ============================================================================
CREATE TYPE asset_state AS ENUM (
    'PLACEHOLDER', 'PROCESSING', 'ACTIVE', 'FAILED', 'DELETE_REQUESTED'
);
CREATE TYPE asset_media_type AS ENUM (
    'IMAGE', 'VIDEO', 'AUDIO', 'GRAPHIC', 'DOCUMENT'
);
CREATE TYPE rendition_type AS ENUM (
    'THUMBNAIL_SMALL', 'THUMBNAIL_MEDIUM', 'THUMBNAIL_LARGE',
    'ANIMATED_PREVIEW', 'VIDEO_PROXY', 'AUDIO_PREVIEW', 'POSTER_FRAME'
);
CREATE TYPE request_state AS ENUM (
    'DRAFT', 'OPEN', 'IN_PROGRESS', 'DELIVERED',
    'APPROVED', 'REVISION_REQUESTED', 'REJECTED', 'CANCELLED'
);
CREATE TYPE request_priority AS ENUM ('LOW', 'NORMAL', 'HIGH', 'URGENT');
CREATE TYPE line_item_state AS ENUM (
    'PENDING', 'IN_PROGRESS', 'DELIVERED', 'APPROVED', 'REVISION_REQUESTED'
);

-- ============================================================================
-- assets
-- ============================================================================
CREATE TABLE assets (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    project_id          UUID NOT NULL REFERENCES projects(id),
    endpoint_id         UUID REFERENCES storage_endpoints(id),
    -- identity
    name                TEXT NOT NULL,
    -- domain
    display_name        TEXT NOT NULL DEFAULT '',
    import_path         TEXT NOT NULL DEFAULT '',
    filename            TEXT NOT NULL DEFAULT '',
    media_type          asset_media_type,
    content_type        TEXT NOT NULL DEFAULT '',
    checksum_sha256     TEXT NOT NULL DEFAULT '',
    size_bytes          BIGINT NOT NULL DEFAULT 0,
    technical_metadata  JSONB NOT NULL DEFAULT '{}',
    ai_description      TEXT NOT NULL DEFAULT '',
    transcription       TEXT NOT NULL DEFAULT '',
    duration_seconds    DOUBLE PRECISION,
    width               INTEGER,
    height              INTEGER,
    annotations         JSONB NOT NULL DEFAULT '{}',
    -- search
    search_vector       tsvector GENERATED ALWAYS AS (
      setweight(to_tsvector('english', coalesce(display_name, '')), 'A') ||
      setweight(to_tsvector('english', coalesce(ai_description, '')), 'B') ||
      setweight(to_tsvector('english', coalesce(transcription, '')), 'C')
    ) STORED,
    embedding           vector(768),
    -- state
    state               asset_state NOT NULL DEFAULT 'PLACEHOLDER',
    -- versioning
    etag                TEXT NOT NULL DEFAULT md5(now()::text),
    revision            INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by          TEXT NOT NULL DEFAULT '',
    updated_by          TEXT NOT NULL DEFAULT '',
    deleted_by          TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time         TIMESTAMPTZ,
    purge_time          TIMESTAMPTZ,
    expire_time         TIMESTAMPTZ,
    -- constraints
    UNIQUE(project_id, name)
);
CREATE INDEX idx_assets_project ON assets (project_id, create_time DESC) WHERE delete_time IS NULL;
CREATE INDEX idx_assets_state ON assets (project_id, state) WHERE delete_time IS NULL;
CREATE INDEX idx_assets_checksum ON assets (project_id, checksum_sha256) WHERE checksum_sha256 != '';
CREATE INDEX idx_assets_search ON assets USING GIN (search_vector);
CREATE INDEX idx_assets_import_path ON assets (project_id, import_path) WHERE delete_time IS NULL AND import_path != '';
CREATE INDEX idx_assets_expire ON assets (expire_time) WHERE expire_time IS NOT NULL AND delete_time IS NULL;

-- ============================================================================
-- asset_versions
-- ============================================================================
CREATE TABLE asset_versions (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    asset_id          UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    -- domain
    version_number    INTEGER NOT NULL,
    checksum_sha256   TEXT NOT NULL DEFAULT '',
    size_bytes        BIGINT NOT NULL DEFAULT 0,
    mime_type         TEXT NOT NULL DEFAULT '',
    storage_key       TEXT NOT NULL DEFAULT '',
    change_note       TEXT NOT NULL DEFAULT '',
    ingestion_error   TEXT NOT NULL DEFAULT '',
    -- audit
    created_by        TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(asset_id, version_number)
);
CREATE INDEX idx_asset_versions_asset ON asset_versions (asset_id, version_number DESC);

-- ============================================================================
-- asset_renditions
-- ============================================================================
CREATE TABLE asset_renditions (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    version_id      UUID NOT NULL REFERENCES asset_versions(id) ON DELETE CASCADE,
    -- domain
    type            rendition_type NOT NULL,
    storage_key     TEXT NOT NULL,
    mime_type       TEXT NOT NULL DEFAULT '',
    width           INTEGER,
    height          INTEGER,
    size_bytes      BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_renditions_version ON asset_renditions (version_id);

-- ============================================================================
-- asset_requests
-- ============================================================================
CREATE TABLE asset_requests (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    project_id        UUID NOT NULL REFERENCES projects(id),
    -- identity
    name              TEXT NOT NULL,
    -- domain
    display_name      TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    priority          request_priority NOT NULL DEFAULT 'NORMAL',
    assignee          TEXT NOT NULL DEFAULT '',
    annotations       JSONB NOT NULL DEFAULT '{}',
    -- state
    state             request_state NOT NULL DEFAULT 'DRAFT',
    -- versioning
    etag              TEXT NOT NULL DEFAULT md5(now()::text),
    revision          INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by        TEXT NOT NULL DEFAULT '',
    updated_by        TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    due_time          TIMESTAMPTZ,
    delivered_time    TIMESTAMPTZ,
    approved_time     TIMESTAMPTZ,
    -- constraints
    UNIQUE(project_id, name)
);
CREATE INDEX idx_asset_requests_project ON asset_requests (project_id, create_time DESC);
CREATE INDEX idx_asset_requests_state ON asset_requests (project_id, state);
CREATE INDEX idx_asset_requests_assignee ON asset_requests (assignee, state) WHERE assignee != '';

-- ============================================================================
-- asset_request_line_items
-- ============================================================================
CREATE TABLE asset_request_line_items (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    request_id        UUID NOT NULL REFERENCES asset_requests(id) ON DELETE CASCADE,
    asset_id          UUID REFERENCES assets(id),
    -- identity
    name              TEXT NOT NULL,
    -- domain
    display_name      TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    media_type        asset_media_type,
    annotations       JSONB NOT NULL DEFAULT '{}',
    -- state
    state             line_item_state NOT NULL DEFAULT 'PENDING',
    -- audit
    created_by        TEXT NOT NULL DEFAULT '',
    updated_by        TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(request_id, name)
);
CREATE INDEX idx_asset_request_line_items_request ON asset_request_line_items (request_id);
CREATE INDEX idx_asset_request_line_items_asset ON asset_request_line_items (asset_id) WHERE asset_id IS NOT NULL;

-- ============================================================================
-- Asset permissions
-- ============================================================================
INSERT INTO permissions (permission_id, display_name, description) VALUES
  ('assets.assets.get', 'Get Asset', 'View asset details'),
  ('assets.assets.list', 'List Assets', 'List assets in a project'),
  ('assets.assets.create', 'Create Asset', 'Create assets'),
  ('assets.assets.update', 'Update Asset', 'Modify asset metadata'),
  ('assets.assets.delete', 'Delete Asset', 'Soft-delete assets'),
  ('assets.assets.undelete', 'Undelete Asset', 'Restore soft-deleted assets'),
  ('assets.assets.import', 'Import Assets', 'Import assets from storage endpoint'),
  ('assets.requests.get', 'Get Request', 'View request details'),
  ('assets.requests.list', 'List Requests', 'List requests in a project'),
  ('assets.requests.create', 'Create Request', 'Create asset requests'),
  ('assets.requests.update', 'Update Request', 'Modify request details'),
  ('assets.requests.delete', 'Delete Request', 'Soft-delete requests'),
  ('assets.requests.assign', 'Assign Request', 'Assign artists to requests'),
  ('assets.requests.claim', 'Claim Request', 'Self-assign to open requests'),
  ('assets.requests.submit', 'Submit Request', 'Submit draft requests'),
  ('assets.requests.deliver', 'Deliver Request', 'Mark requests as delivered'),
  ('assets.requests.approve', 'Approve Request', 'Approve delivered requests'),
  ('assets.requests.reject', 'Reject Request', 'Reject delivered requests'),
  ('assets.requests.cancel', 'Cancel Request', 'Cancel requests'),
  ('assets.lineItems.get', 'Get Line Item', 'View line item details'),
  ('assets.lineItems.list', 'List Line Items', 'List line items in a request'),
  ('assets.lineItems.create', 'Create Line Item', 'Add line items to requests'),
  ('assets.lineItems.update', 'Update Line Item', 'Modify line item details'),
  ('assets.lineItems.delete', 'Delete Line Item', 'Remove line items from requests'),
  ('assets.lineItems.fulfill', 'Fulfill Line Item', 'Upload deliverable for line item');

-- ============================================================================
-- AI chat — ai_conversations
-- ============================================================================
CREATE TABLE ai_conversations (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    name            TEXT NOT NULL,  -- stable ID used in resource name
    -- domain
    title           TEXT NOT NULL DEFAULT '',
    -- True when the user explicitly set the title; tells the
    -- summarization path to skip auto-regeneration.
    title_user_set  BOOLEAN NOT NULL DEFAULT FALSE,
    description     TEXT NOT NULL DEFAULT '',
    archived        BOOLEAN NOT NULL DEFAULT FALSE,
    pinned          BOOLEAN NOT NULL DEFAULT FALSE,
    message_count   INTEGER NOT NULL DEFAULT 0,
    last_message_time TIMESTAMPTZ,
    -- etag/revision
    etag            TEXT NOT NULL DEFAULT md5(now()::text),
    revision        INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by      TEXT NOT NULL DEFAULT '',
    updated_by      TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(org_id, name)
);
-- Primary list index: (org_id, created_by) prefix always filtered on for
-- access control; `id DESC` at the end serves both the default newest-first
-- sort and cursor-based pagination (`WHERE id < $cursor`) as a pure index
-- scan. `uuidv7()` gives time-ordered + unique, so `id DESC` == chronological.
CREATE INDEX idx_ai_conversations_creator ON ai_conversations (org_id, created_by, id DESC);

-- Partial index for the "non-archived" filtered list, which is the common case.
CREATE INDEX idx_ai_conversations_active ON ai_conversations (org_id, created_by, id DESC) WHERE archived = FALSE;

-- Partial index for "pinned first" views.
CREATE INDEX idx_ai_conversations_pinned ON ai_conversations (org_id, created_by, id DESC) WHERE pinned = TRUE;

-- ============================================================================
-- AI chat — ai_messages
-- ============================================================================
CREATE TABLE ai_messages (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    conversation_id UUID NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    -- identity
    name            TEXT NOT NULL,
    -- domain
    role            TEXT NOT NULL,  -- "user" | "assistant" | "system" | "tool"
    parts           JSONB NOT NULL DEFAULT '[]',  -- serialized repeated MessagePart
    -- ordering
    sequence        BIGINT NOT NULL,  -- monotonic within conversation
    -- token budget tracking — heuristic (len(text)/4), not exact per-model tokenization
    token_count     INTEGER NOT NULL DEFAULT 0,
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(conversation_id, name),
    UNIQUE(conversation_id, sequence)
);
-- Cursor pagination: ORDER BY id DESC with conversation_id predicate.
-- UNIQUE(conversation_id, sequence) indexes sequence, not id — add explicit index for id-based pagination.
CREATE INDEX idx_ai_messages_conversation_id ON ai_messages (conversation_id, id DESC);

-- ============================================================================
-- AI chat — ai_artifact_versions (created before ai_artifacts so ai_artifacts can FK to latest_version_id)
-- ============================================================================
CREATE TABLE ai_artifact_versions (
    id                   UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships (artifact_id FK added after ai_artifacts table is created)
    artifact_id          UUID NOT NULL,
    -- identity
    name                 TEXT NOT NULL,  -- e.g. "v1", "v2"
    -- domain — inline mode (small text artifacts: code, markdown, svg)
    inline_data          BYTEA,
    inline_content_type  TEXT,
    inline_size_bytes    BIGINT CHECK (inline_size_bytes IS NULL OR inline_size_bytes <= 1048576),  -- 1 MB cap
    -- domain — asset mode (binary artifacts: image, pdf, video)
    asset_version_name   TEXT,  -- "organizations/.../assets/.../versions/..." pointer
    -- ordering
    sequence             INTEGER NOT NULL,  -- v1 = 1, v2 = 2, ...
    -- audit
    created_by           TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(artifact_id, name),
    UNIQUE(artifact_id, sequence),
    CHECK (
        (inline_data IS NOT NULL AND inline_content_type IS NOT NULL AND inline_size_bytes IS NOT NULL AND asset_version_name IS NULL)
        OR
        (inline_data IS NULL AND inline_content_type IS NULL AND inline_size_bytes IS NULL AND asset_version_name IS NOT NULL)
    )
);

-- ============================================================================
-- AI chat — ai_artifacts
-- ============================================================================
CREATE TABLE ai_artifacts (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    conversation_id UUID NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    -- identity
    name            TEXT NOT NULL,
    -- domain
    type            TEXT NOT NULL,   -- "code" | "markdown" | "svg" | "image" | ...
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    latest_version_id UUID REFERENCES ai_artifact_versions(id) DEFERRABLE INITIALLY DEFERRED,
    -- audit
    created_by      TEXT NOT NULL DEFAULT '',
    updated_by      TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(conversation_id, name)
);
CREATE INDEX idx_ai_artifacts_conversation ON ai_artifacts (conversation_id, id DESC);

-- Now add the FK from ai_artifact_versions back to ai_artifacts
ALTER TABLE ai_artifact_versions ADD CONSTRAINT fk_ai_artifact_versions_artifact
    FOREIGN KEY (artifact_id) REFERENCES ai_artifacts(id) ON DELETE CASCADE;

CREATE INDEX idx_ai_artifact_versions_artifact ON ai_artifact_versions (artifact_id, id DESC);
CREATE INDEX idx_ai_artifact_versions_asset ON ai_artifact_versions (asset_version_name) WHERE asset_version_name IS NOT NULL;

-- AI chat permissions
INSERT INTO permissions (permission_id, display_name, description) VALUES
  ('ai.conversations.get', 'Get Conversation', 'View conversation details'),
  ('ai.conversations.list', 'List Conversations', 'List conversations in an organization'),
  ('ai.conversations.create', 'Create Conversation', 'Create conversations'),
  ('ai.conversations.update', 'Update Conversation', 'Modify conversation details'),
  ('ai.conversations.delete', 'Delete Conversation', 'Delete conversations'),
  ('ai.messages.get', 'Get Message', 'View message details'),
  ('ai.messages.list', 'List Messages', 'List messages in a conversation'),
  ('ai.artifacts.get', 'Get Artifact', 'View artifact details'),
  ('ai.artifacts.list', 'List Artifacts', 'List artifacts in a conversation'),
  ('ai.artifacts.delete', 'Delete Artifact', 'Delete artifacts'),
  ('ai.artifactVersions.get', 'Get Artifact Version', 'View artifact version details'),
  ('ai.artifactVersions.list', 'List Artifact Versions', 'List artifact versions'),
  ('ai.artifactVersions.delete', 'Delete Artifact Version', 'Delete artifact versions'),
  ('ai.chat.stream', 'Stream Chat', 'Use AI chat streaming');
