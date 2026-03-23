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
CREATE TYPE endpoint_engine AS ENUM ('S3', 'RUSTFS', 'GCS', 'MINIO');
CREATE TYPE endpoint_state AS ENUM ('ACTIVE', 'INACTIVE', 'UNREACHABLE');
CREATE TYPE credential_state AS ENUM ('UNSET', 'SET', 'INVALID');

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
    owner_id              UUID,  -- FK to users(id), added after users table exists
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
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time TIMESTAMPTZ,
    verify_time TIMESTAMPTZ,
    -- constraints
    UNIQUE(org_id, domain)
);
CREATE INDEX idx_custom_domains_org ON custom_domains (org_id) WHERE delete_time IS NULL;
CREATE UNIQUE INDEX idx_custom_domains_domain ON custom_domains (domain) WHERE delete_time IS NULL;

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
    cache_max_size_gb   INTEGER NOT NULL DEFAULT 0,
    cache_eviction      eviction_policy NOT NULL DEFAULT 'LRU',
    cache_ttl_hours     INTEGER NOT NULL DEFAULT 0,
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
    engine            endpoint_engine NOT NULL,
    endpoint_uri      TEXT NOT NULL,
    bucket            TEXT NOT NULL,
    region            TEXT NOT NULL DEFAULT '',
    credentials       JSONB,  -- encrypted at rest, never returned via API
    annotations       JSONB NOT NULL DEFAULT '{}',
    -- state
    state             endpoint_state NOT NULL DEFAULT 'ACTIVE',
    credential_state  credential_state NOT NULL DEFAULT 'UNSET',
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
CREATE TABLE tag_bindings (
    id                        UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    parent_resource           TEXT NOT NULL,
    tag_value_id              UUID NOT NULL REFERENCES tag_values(id) ON DELETE RESTRICT,
    -- domain
    annotations               JSONB NOT NULL DEFAULT '{}',
    -- versioning
    etag                      TEXT NOT NULL DEFAULT md5(now()::text),
    -- audit
    created_by                TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time               TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time               TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(parent_resource, tag_value_id)
);
CREATE INDEX idx_tag_bindings_parent ON tag_bindings (parent_resource);
CREATE INDEX idx_tag_bindings_tag_value ON tag_bindings (tag_value_id);

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
    updated_by    TEXT NOT NULL DEFAULT '',
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
-- users (per-org membership, created on invitation accept)
-- ============================================================================
CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
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

-- Deferred FK: organizations.owner_id -> users.id
-- (org and owner user are created in the same transaction)
ALTER TABLE organizations
  ADD CONSTRAINT fk_organizations_owner
  FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE SET NULL
  DEFERRABLE INITIALLY DEFERRED;

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
    deleted_by   TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time  TIMESTAMPTZ,
    purge_time   TIMESTAMPTZ
);
CREATE INDEX idx_groups_org ON groups (org_id) WHERE delete_time IS NULL;

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
    deleted_by   TEXT NOT NULL DEFAULT '',
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time  TIMESTAMPTZ,
    purge_time   TIMESTAMPTZ
);
CREATE INDEX idx_roles_org ON roles (org_id) WHERE delete_time IS NULL;

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
    inviter     TEXT NOT NULL DEFAULT '',
    -- state
    state       invitation_state NOT NULL DEFAULT 'PENDING',
    -- versioning
    etag        TEXT NOT NULL DEFAULT md5(now()::text),
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
