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
-- Schemas
-- ============================================================================
-- River queue lives in its own schema so its tables (river_job,
-- river_queue, river_leader, etc.) are visually separated from the
-- application schema. River runs its own migrations programmatically
-- via rivermigrate at pivox-worker boot — this just creates the
-- namespace they land in.
CREATE SCHEMA IF NOT EXISTS river;

-- ============================================================================
-- Enum types
-- ============================================================================
CREATE TYPE resource_state AS ENUM ('ACTIVE', 'DELETE_REQUESTED');
-- Principal kind for both org_members and space_members. Two tables
-- give structural integrity by design (an org_members row physically
-- cannot be misinterpreted as a space membership). Principal kind is
-- now discriminated by which of (user_id, group_id) is populated —
-- no separate enum needed.
CREATE TYPE domain_state AS ENUM ('PENDING', 'VERIFIED', 'FAILED');
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
    -- AIP-151 parent resource: the full resource name the LRO
    -- operates against (e.g., "organizations/acme/spaces/dev" or
    -- "accounts/me"). Required and non-empty — every LRO targets a
    -- resource, there are no unscoped operations. The public Operation
    -- proto's `name` is constructed as `{parent}/operations/{id}`, per
    -- AIP-151's "ends with operations/{unique_id}" requirement.
    parent      TEXT NOT NULL CHECK (parent <> ''),
    done        BOOLEAN NOT NULL DEFAULT false,
    metadata    JSONB,
    result      JSONB,
    error_code  INTEGER,
    error_message TEXT,
    -- Optional reverse pointer to the org this LRO operates against,
    -- set at CreateAndRun time when known. Used by
    -- CancelRunningOpsForOrg in DeleteOrganization's
    -- CANCELLING_OPERATIONS phase to interrupt in-flight org-scoped
    -- LROs (asset imports, domain verifications, gateway upgrades,
    -- etc.) before the cascade deletes their target rows.
    --
    -- ON DELETE SET NULL (not CASCADE) so the operations row survives
    -- a force-path PurgeOrganization — losing its org reference but
    -- keeping its result/error so the LRO that drove the purge can
    -- still update its own row at completion.
    --
    -- NULL for ops that aren't org-scoped or where the org isn't
    -- known at create time (e.g. CreateOrganization). NULL also for
    -- DeleteOrganization itself: a self-pointing org_id would cause
    -- the LRO to cancel itself in the CANCELLING_OPERATIONS phase.
    -- The FK to organizations(id) is added at the bottom of this
    -- migration (forward reference: `operations` is declared before
    -- `organizations` so the inline REFERENCES fails to resolve).
    org_id      UUID,
    -- Optional reverse pointer to the space this LRO operates against.
    -- NULL for org-scoped or account-scoped ops. The authz scope of an
    -- operation is space_id when set, else org_id, else the caller's own
    -- account (created_by). FK added at the bottom of this migration
    -- (forward reference, like org_id); ON DELETE SET NULL so the op row
    -- survives a space purge.
    space_id    UUID,
    created_by UUID,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '30 days'
);
CREATE INDEX idx_operations_pending ON operations (create_time) WHERE done = false;
CREATE INDEX idx_operations_expire ON operations (expire_time) WHERE done = true;
CREATE INDEX idx_operations_parent ON operations (parent, create_time DESC);
-- Partial index supports CancelRunningOpsForOrg: only the running
-- subset is queried, and only for ops with a populated org_id.
CREATE INDEX idx_operations_org_pending ON operations (org_id) WHERE done = false AND org_id IS NOT NULL;

-- LRO completion notification: fires pg_notify on every
-- false→true transition of `done`. Receivers (LROManager.listen)
-- LISTEN on the `pivox_lro_done` channel and dispatch to in-process
-- WaitOperation listeners.
--
-- Why a trigger instead of inline pg_notify in each query: the
-- writers split across many call sites (CompleteOperation,
-- FailOperation, CancelOperation, CancelRunningOpsForOrg, the
-- LRO worker bookkeeping commits) and live in different processes
-- (pivox-cloud, pivox-worker). One trigger captures every commit
-- atomically with the row update — receivers only see the
-- notification after the row is visible to subsequent reads.
--
-- Payload is the operation UUID as text. Truncated to fit Postgres'
-- 8000-byte NOTIFY limit, but UUIDs are well under that.
CREATE OR REPLACE FUNCTION notify_lro_done() RETURNS trigger AS $$
BEGIN
    IF NEW.done = TRUE AND OLD.done = FALSE THEN
        PERFORM pg_notify('pivox_lro_done', NEW.id::text);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER operations_notify_done
    AFTER UPDATE OF done ON operations
    FOR EACH ROW
    EXECUTE FUNCTION notify_lro_done();

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
    -- state
    state                 resource_state NOT NULL DEFAULT 'ACTIVE',
    -- versioning
    etag                  TEXT NOT NULL DEFAULT md5(now()::text),
    revision              INTEGER NOT NULL DEFAULT 1,
    -- audit. `created_by` doubles as the immutable founder pointer
    -- (FK to identities; soft-delete-only policy means it
    -- never dangles, so we don't need a separate "founder UUID"
    -- column). Ownership is tracked via `org_members` rows bound
    -- to the system 'owner' role; "≥1 owner" is enforced at the
    -- service mutation boundary, not here. updated_by / deleted_by
    -- nullable because the row may have never been touched / soft-
    -- deleted. FK added below the identities declaration
    -- (forward-ref).
    created_by            UUID,
    updated_by            UUID,
    deleted_by            UUID,
    -- timestamps
    create_time           TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time           TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time           TIMESTAMPTZ,
    purge_time            TIMESTAMPTZ
);
CREATE INDEX idx_organizations_name ON organizations (name) WHERE delete_time IS NULL;

-- ============================================================================
-- spaces
-- ============================================================================
CREATE TABLE spaces (
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
    created_by UUID,
    updated_by UUID,
    deleted_by UUID,
    -- timestamps
    create_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time    TIMESTAMPTZ,
    purge_time     TIMESTAMPTZ,
    -- constraints
    UNIQUE(org_id, name)
);
CREATE INDEX idx_spaces_org ON spaces (org_id) WHERE delete_time IS NULL;

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
    created_by UUID,
    updated_by UUID,
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
    created_by UUID,
    updated_by UUID,
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
    created_by UUID,
    updated_by UUID,
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
    created_by UUID,
    updated_by UUID,
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
    created_by UUID,
    -- timestamps
    create_time               TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(parent_resource, tag_value_id)
);
CREATE INDEX idx_tag_bindings_parent ON tag_bindings (parent_resource);
CREATE INDEX idx_tag_bindings_tag_value ON tag_bindings (tag_value_id);
CREATE INDEX idx_tag_bindings_origin ON tag_bindings (parent_resource, origin);

-- ============================================================================
-- dashboards (USER_MANAGED, space-scoped). SYSTEM_MANAGED dashboards
-- (the org-level Library catalog) are virtual — generated from
-- internal/dashboard/system at request time and have no DB row. The
-- management_mode column carries forward as a guard for the day a
-- SYSTEM_MANAGED row needs to be importable; for v1 every row this
-- table holds is USER_MANAGED.
--
-- Storage shape: the full Dashboard proto is marshaled into the
-- payload JSONB column on every write. display_name and description
-- are mirrored as scalar columns for AIP-160 filter / index use
-- (filterable fields per dashboards.proto are displayName and
-- createTime). Read paths reconstruct the proto from payload and
-- overlay the column-mirrored audit + timestamp fields.
-- ============================================================================
CREATE TABLE dashboards (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    space_id        UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    -- identity
    name            TEXT NOT NULL, -- AIP slug, scoped per (space_id, name)
    -- domain mirrors (filterable / indexable surface of the proto)
    display_name    TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    -- governance
    management_mode TEXT NOT NULL DEFAULT 'USER_MANAGED'
        CHECK (management_mode IN ('USER_MANAGED', 'SYSTEM_MANAGED')),
    -- payload (full Dashboard proto marshaled)
    payload         JSONB NOT NULL,
    -- versioning
    etag            TEXT NOT NULL DEFAULT md5(now()::text),
    revision        INTEGER NOT NULL DEFAULT 1,
    -- audit (FKs added below in the ALTER TABLE block — matches
    -- the codebase convention so this table can be created before
    -- the identities table)
    created_by      UUID,
    updated_by      UUID,
    deleted_by      UUID,
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time     TIMESTAMPTZ,
    purge_time      TIMESTAMPTZ,
    -- constraints
    UNIQUE(space_id, name)
);
CREATE INDEX idx_dashboards_space ON dashboards (space_id, create_time DESC) WHERE delete_time IS NULL;

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
    created_by UUID,
    updated_by UUID,
    deleted_by UUID,
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
-- identities (global Firebase Auth cache — internal, no proto)
-- ============================================================================
CREATE TABLE identities (
    -- id IS the Keycloak `sub` (a UUID) for KC-provisioned principals,
    -- passed in by the KC event-sync upsert rather than generated. The
    -- uuidv7() default covers any row created without an explicit id.
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    -- domain (synced from Keycloak). Soft-delete blanks these out;
    -- only `id` is durably preserved so historical *_by audit
    -- references remain stable.
    email           TEXT NOT NULL DEFAULT '',
    email_verified  BOOLEAN NOT NULL DEFAULT false,
    display_name    TEXT NOT NULL DEFAULT '',
    photo_url       TEXT NOT NULL DEFAULT '',
    disabled        BOOLEAN NOT NULL DEFAULT false,
    -- soft-delete. is_deleted=true means the identity has been
    -- deleted by the user (or by an admin); the row is preserved
    -- so audit *_by references continue to resolve, but PII is
    -- blanked and the row is excluded from active sign-in lookups.
    -- Hard-delete is intentionally not exposed — terminal purge
    -- is a separate, manually-invoked operation.
    is_deleted      BOOLEAN NOT NULL DEFAULT false,
    -- timestamps
    create_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_time TIMESTAMPTZ,
    delete_time     TIMESTAMPTZ
);
-- Email lookup index excludes soft-deleted (whose email is blanked
-- anyway) so the index stays small and active.
CREATE INDEX idx_identities_email ON identities (email) WHERE is_deleted = false;
-- Enforce one-active-identity-per-email. Soft-deleted rows blank
-- email at delete time, but the partial predicate is the durable
-- guard against re-introduction.
CREATE UNIQUE INDEX idx_identities_email_unique ON identities (email)
  WHERE is_deleted = false AND email <> '';

-- ============================================================================
-- Per-org user identity has been unified with `identities`
-- (Phase 7). The previous per-org `users` join row was dropped in
-- favor of using `identities.id` as the universal user UUID
-- across the API:
--
--   - `org_members.principal_id` (when principal_kind='user') →
--     references `identities.id`
--   - `space_members.principal_id` (when principal_kind='user') →
--     same
--   - `group_members.user_id` → references `identities.id`
--   - `ai_conversations.created_by` → same (added in the AI chat
--     re-parent commit that ships alongside this one)
--
-- Membership in an org = the existence of at least one `org_members`
-- row binding the firebase_identity to that org via a role. Removing
-- a user from an org is a hard-delete of those `org_members`/
-- `space_members`/`group_members` rows; the `identities`
-- row itself is unaffected.
--
-- Clients learn their own user UUID from the `sub` of their Keycloak
-- id_token (sub == identities.id).
-- ============================================================================

-- Forward-referenced FKs from audit columns → identities(id),
-- added here because `identities` is declared after the
-- audit-bearing tables in this migration. NO `ON DELETE` clauses —
-- identity rows are soft-deleted only (Phase 7+ policy), so the FKs
-- never dangle and cascade behavior never fires. Tables declared
-- AFTER identities (groups, role_*, members, domains,
-- sso_configs, ai_*, etc.) carry their FKs inline at column
-- declaration; only this block handles the forward-refs.
ALTER TABLE operations
  ADD CONSTRAINT fk_operations_created_by FOREIGN KEY (created_by) REFERENCES identities(id);
ALTER TABLE organizations
  ADD CONSTRAINT fk_organizations_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_organizations_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_organizations_deleted_by FOREIGN KEY (deleted_by) REFERENCES identities(id);
ALTER TABLE spaces
  ADD CONSTRAINT fk_spaces_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_spaces_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_spaces_deleted_by FOREIGN KEY (deleted_by) REFERENCES identities(id);
ALTER TABLE storage_gateways
  ADD CONSTRAINT fk_storage_gateways_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_storage_gateways_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id);
ALTER TABLE storage_endpoints
  ADD CONSTRAINT fk_storage_endpoints_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_storage_endpoints_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id);
ALTER TABLE tag_keys
  ADD CONSTRAINT fk_tag_keys_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_tag_keys_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id);
ALTER TABLE tag_values
  ADD CONSTRAINT fk_tag_values_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_tag_values_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id);
ALTER TABLE tag_bindings
  ADD CONSTRAINT fk_tag_bindings_created_by FOREIGN KEY (created_by) REFERENCES identities(id);
ALTER TABLE api_keys
  ADD CONSTRAINT fk_api_keys_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_api_keys_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_api_keys_deleted_by FOREIGN KEY (deleted_by) REFERENCES identities(id);
ALTER TABLE dashboards
  ADD CONSTRAINT fk_dashboards_created_by FOREIGN KEY (created_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_dashboards_updated_by FOREIGN KEY (updated_by) REFERENCES identities(id),
  ADD CONSTRAINT fk_dashboards_deleted_by FOREIGN KEY (deleted_by) REFERENCES identities(id);

-- FK from operations.org_id → organizations(id), deferred for the
-- same reason: `operations` is declared above `organizations` (so the
-- LRO Manager can reference operation-state queries from anywhere),
-- and the inline REFERENCES would fail at table-creation time.
ALTER TABLE operations
  ADD CONSTRAINT fk_operations_org
  FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE SET NULL;
ALTER TABLE operations
  ADD CONSTRAINT fk_operations_space
  FOREIGN KEY (space_id) REFERENCES spaces(id) ON DELETE SET NULL;

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
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
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
    user_id    UUID NOT NULL REFERENCES identities(id) ON DELETE CASCADE,
    -- audit
    created_by UUID REFERENCES identities(id),
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
    -- identity. `name` is the stable slug — both the trailing segment
    -- of the AIP resource name (`organizations/{org}/roles/{name}`)
    -- AND the machine identifier for system roles. The 4 system roles
    -- per org are seeded with `name` ∈ {'owner','admin','editor','viewer'}
    -- and `is_system = true`. Custom roles get caller-chosen slugs.
    -- Pin queries that need the system owner role on (org_id, name='owner', is_system=true).
    name         TEXT NOT NULL,
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
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- timestamps
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints
    UNIQUE(org_id, name)
);
CREATE INDEX idx_roles_org ON roles (org_id);
-- Cheap (org_id, system-owner) lookup for "≥1 owner per org" enforcement.
CREATE INDEX idx_roles_org_system_name ON roles (org_id, name) WHERE is_system = true;

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
-- org_members (role binding at org scope)
--
-- One row per (org, principal, role) tuple. The single source of
-- truth for "who has what role in this org." Each row binds a
-- principal (a user-row or group-row in this org) to a role-row
-- in this org.
--
-- See companion `space_members` below for the same shape at space
-- scope. Two physical tables, not one with a `scope_kind` column —
-- structural integrity by design (an org_members row physically
-- cannot be misinterpreted as a space membership).
-- ============================================================================
CREATE TABLE org_members (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    -- principal: exactly one of user_id or group_id is set, enforced
    -- by the XOR check below. The columns themselves discriminate —
    -- no separate principal_kind enum is needed. Filtered unique
    -- indexes per-column give us "one binding per (org, principal)"
    -- without a polymorphic UNIQUE.
    user_id     UUID REFERENCES identities(id),
    group_id    UUID REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT org_members_principal_xor CHECK (
        (user_id IS NOT NULL AND group_id IS NULL) OR
        (user_id IS NULL AND group_id IS NOT NULL)
    ),
    -- versioning
    etag        TEXT NOT NULL DEFAULT md5(now()::text),
    revision    INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_org_members_org ON org_members (org_id);
CREATE INDEX idx_org_members_role ON org_members (role_id);
-- Filtered unique indexes replace the prior UNIQUE(org_id,
-- principal_kind, principal_id) tuple. PostgreSQL treats NULL as
-- distinct in unique indexes, so we need the WHERE predicate to
-- enforce uniqueness on the live (populated) column only.
CREATE UNIQUE INDEX idx_org_members_user ON org_members (org_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX idx_org_members_group ON org_members (org_id, group_id) WHERE group_id IS NOT NULL;

-- ============================================================================
-- space_members (role binding at space scope)
--
-- Companion to `org_members`. Rows here represent space-level role
-- bindings only; org-level inheritance (org owners are space owners
-- by transitivity) is resolved at query time, not denormalized.
-- ============================================================================
CREATE TABLE space_members (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    -- principal: exactly one of user_id or group_id is set (XOR
    -- check below). Same shape as org_members; see the structural
    -- notes there.
    user_id     UUID REFERENCES identities(id),
    group_id    UUID REFERENCES groups(id) ON DELETE CASCADE,
    CONSTRAINT space_members_principal_xor CHECK (
        (user_id IS NOT NULL AND group_id IS NULL) OR
        (user_id IS NULL AND group_id IS NOT NULL)
    ),
    -- versioning
    etag        TEXT NOT NULL DEFAULT md5(now()::text),
    revision    INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- timestamps
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_space_members_space ON space_members (space_id);
CREATE INDEX idx_space_members_role ON space_members (role_id);
CREATE UNIQUE INDEX idx_space_members_user ON space_members (space_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX idx_space_members_group ON space_members (space_id, group_id) WHERE group_id IS NOT NULL;

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
    created_by UUID REFERENCES identities(id),
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
-- domains (org-claimed DNS domains, verified via TXT)
--
-- One row per claimed domain. Created in PENDING via CreateDomain,
-- driven to VERIFIED (or FAILED on grace-window expiry) by a
-- background worker that polls DNS for the TXT record at
-- `_pivox-verify.<domain>` matching `verification_token`. The
-- corresponding `operations` row carries the live LRO state.
--
-- `domain` is globally UNIQUE: a domain can be claimed by at most
-- one org. Concurrent claims surface as ALREADY_EXISTS without
-- disclosing which org holds the existing claim.
-- ============================================================================
CREATE TABLE domains (
    id                 UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id             UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity. `domain` is canonicalized to lowercase by the
    -- application before insert; the CHECK enforces that on the
    -- database side as well so a buggy caller can't sneak in
    -- "Acme.com" as a parallel claim of "acme.com". UNIQUE then
    -- enforces global single-claim.
    domain             TEXT NOT NULL UNIQUE CHECK (domain = lower(domain)),
    -- domain
    verification_token TEXT NOT NULL,
    -- state
    state              domain_state NOT NULL DEFAULT 'PENDING',
    -- versioning
    etag               TEXT NOT NULL DEFAULT md5(now()::text),
    revision           INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- timestamps
    create_time        TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time        TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_time      TIMESTAMPTZ
);
CREATE INDEX idx_domains_org ON domains (org_id);
CREATE INDEX idx_domains_state ON domains (org_id, state);

-- ============================================================================
-- sso_configs (per-org SSO/IDP configuration; AIP-156 singleton)
--
-- One row per organization (UNIQUE on org_id). `oidc_config` and
-- `saml_config` are mutually exclusive — exactly one is non-null,
-- enforced by CHECK. `client_secret_ciphertext` is the KMS-encrypted
-- OIDC client_secret (separate from oidc_config so it can be rotated
-- and access-controlled independently); decrypted only at use sites.
-- Linkage to verified domains is implicit via `domains.org_id`.
-- ============================================================================
CREATE TABLE sso_configs (
    id                         UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    org_id                     UUID NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,
    -- identity
    firebase_provider_id       TEXT NOT NULL DEFAULT '',
    -- domain
    display_name               TEXT NOT NULL DEFAULT '',
    enabled                    BOOLEAN NOT NULL DEFAULT false,
    oidc_config                JSONB,
    saml_config                JSONB,
    client_secret_ciphertext   BYTEA,
    -- versioning
    etag                       TEXT NOT NULL DEFAULT md5(now()::text),
    revision                   INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- timestamps
    create_time                TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time                TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- constraints: exactly one of oidc_config / saml_config is set.
    CHECK ((oidc_config IS NOT NULL) <> (saml_config IS NOT NULL))
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
-- Seed: permissions (org-level only; space access uses space_members roles)
-- ============================================================================
INSERT INTO permissions (permission_id, display_name, description) VALUES
  -- Organization management
  ('organizations.read', 'Read Organization', 'View organization details'),
  ('organizations.update', 'Update Organization', 'Modify organization settings'),
  ('organizations.delete', 'Delete Organization', 'Delete or restore the organization (covers UndeleteOrganization too — same destruction-class tier)'),
  ('organizations.transferOwnership', 'Transfer Ownership', 'Atomically transfer the owner role to another member'),
  -- SSO config (singleton sub-resource; tighter role list than the parent org)
  ('organizations.ssoConfig.read', 'Read SSO Config', 'View SSO configuration'),
  ('organizations.ssoConfig.update', 'Update SSO Config', 'Modify SSO configuration'),
  -- Space creation (org-level; within-space access is space-role based)
  ('spaces.create', 'Create Space', 'Create new spaces in the organization'),
  ('spaces.read', 'Read Space', 'View and list spaces'),
  ('spaces.update', 'Update Space', 'Modify space metadata'),
  ('spaces.delete', 'Delete Space', 'Delete or restore a space (covers UndeleteSpace too — same destruction-class tier). Admin-allowed because spaces have narrower blast radius than orgs (workgroup vs whole tenant); compare organizations.delete which is owner-only.'),
  ('tags.create', 'Create Tag', 'Create new tag keys, values, or bindings'),
  ('tags.read', 'Read Tags', 'View and list tag keys, values, and bindings'),
  ('tags.update', 'Update Tag', 'Modify tag keys or values'),
  ('tags.delete', 'Delete Tag', 'Delete tag keys, values, or bindings'),
  -- User management
  ('users.read', 'Read Users', 'View and list users'),
  ('users.delete', 'Delete User', 'Delete a user globally (LRO; cascades memberships)'),
  -- Group management
  ('groups.create', 'Create Group', 'Create new groups'),
  ('groups.read', 'Read Groups', 'View and list groups'),
  ('groups.update', 'Update Group', 'Modify groups'),
  ('groups.delete', 'Delete Group', 'Delete groups'),
  ('groups.manageMembers', 'Manage Group Members', 'Add/remove group members'),
  -- Role management
  ('roles.create', 'Create Role', 'Create custom roles'),
  ('roles.read', 'Read Roles', 'View and list roles'),
  ('roles.update', 'Update Role', 'Modify custom roles'),
  ('roles.delete', 'Delete Role', 'Delete custom roles'),
  ('roles.manageMembers', 'Manage Role Members', 'Add/remove role members'),
  -- Invitation management
  ('invitations.create', 'Create Invitation', 'Invite users to the organization'),
  ('invitations.read', 'Read Invitations', 'View and list invitations'),
  ('invitations.delete', 'Delete Invitation', 'Revoke invitations'),
  ('invitations.updatePolicy', 'Update Invitation Policy', 'Modify invitation policy'),
  -- API key management
  ('apiKeys.create', 'Create API Key', 'Create API keys'),
  ('apiKeys.read', 'Read API Keys', 'View and list API keys'),
  ('apiKeys.update', 'Update API Key', 'Modify API keys'),
  ('apiKeys.delete', 'Delete API Key', 'Delete API keys'),
  -- Dashboard management (org-level system catalog + space-level user
  -- dashboards share one permission set; SYSTEM_MANAGED targets reject
  -- mutation regardless of role via a data-driven handler guard, not IAM)
  ('dashboards.read', 'Read Dashboards', 'View and list dashboards (covers both org-level system catalog and space-level user dashboards)'),
  ('dashboards.create', 'Create Dashboard', 'Create user-managed dashboards in a space. Owner/admin only — dashboards are workspace structure, not day-to-day content (matches spaces.create tier).'),
  ('dashboards.update', 'Update Dashboard', 'Modify a user-managed dashboard. SYSTEM_MANAGED dashboards reject mutation regardless of role.'),
  ('dashboards.delete', 'Delete Dashboard', 'Delete a user-managed dashboard. SYSTEM_MANAGED dashboards reject deletion regardless of role.'),
  -- Domain management
  ('domains.create', 'Create Domain', 'Claim a DNS domain for the organization'),
  ('domains.read', 'Read Domains', 'View and list domains'),
  ('domains.delete', 'Delete Domain', 'Release a domain claim'),
  -- Member (role bindings at org and space scope)
  ('members.create', 'Create Member', 'Bind a principal to a role'),
  ('members.read', 'Read Members', 'View and list role bindings at a scope'),
  ('members.update', 'Update Member', 'Change a member''s role'),
  ('members.delete', 'Delete Member', 'Remove a role binding'),
  -- Storage gateway management
  ('storage.gateways.create', 'Create Storage Gateway', 'Create storage gateways'),
  ('storage.gateways.read', 'Read Storage Gateways', 'View and list storage gateways'),
  ('storage.gateways.update', 'Update Storage Gateway', 'Modify storage gateways'),
  ('storage.gateways.delete', 'Delete Storage Gateway', 'Delete storage gateways'),
  ('storage.gateways.upgrade', 'Upgrade Storage Gateway', 'Trigger gateway upgrades'),
  ('storage.endpoints.create', 'Create Storage Endpoint', 'Create storage endpoints'),
  ('storage.endpoints.read', 'Read Storage Endpoints', 'View and list storage endpoints'),
  ('storage.endpoints.update', 'Update Storage Endpoint', 'Modify storage endpoints'),
  ('storage.endpoints.delete', 'Delete Storage Endpoint', 'Delete storage endpoints'),
  ('storage.agents.read', 'Read Agents', 'View and list agents'),
  ('storage.agents.drain', 'Drain Agent', 'Drain agents for maintenance'),
  ('storage.agents.remove', 'Remove Agent', 'Remove agents from gateway pool');

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
    space_id            UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
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
    -- DEFAULT zero vector so the column never returns NULL via pgx's
    -- row scanner. pgvector-go v0.3.0's `pgvector.Vector` (non-pointer)
    -- panics in DecodeBinary on a 0-byte (NULL) payload; the right
    -- semantic answer is `*pgvector.Vector` everywhere, but that's a
    -- bigger churn. The DEFAULT keeps the schema honest about "always
    -- present" and matches what internal/testutil/db.go has been doing
    -- via post-migration ALTER for tests since pgvector landed.
    embedding           vector(768) NOT NULL DEFAULT array_fill(0, ARRAY[768])::vector,
    -- state
    state               asset_state NOT NULL DEFAULT 'PLACEHOLDER',
    -- versioning
    etag                TEXT NOT NULL DEFAULT md5(now()::text),
    revision            INTEGER NOT NULL DEFAULT 1,
    -- audit
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    deleted_by UUID REFERENCES identities(id),
    -- timestamps
    create_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time         TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_time         TIMESTAMPTZ,
    purge_time          TIMESTAMPTZ,
    expire_time         TIMESTAMPTZ,
    -- constraints
    UNIQUE(space_id, name)
);
CREATE INDEX idx_assets_space ON assets (space_id, create_time DESC) WHERE delete_time IS NULL;
CREATE INDEX idx_assets_state ON assets (space_id, state) WHERE delete_time IS NULL;
CREATE INDEX idx_assets_checksum ON assets (space_id, checksum_sha256) WHERE checksum_sha256 != '';
CREATE INDEX idx_assets_search ON assets USING GIN (search_vector);
CREATE INDEX idx_assets_import_path ON assets (space_id, import_path) WHERE delete_time IS NULL AND import_path != '';
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
    created_by UUID REFERENCES identities(id),
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
    space_id          UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
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
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- timestamps
    create_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    due_time          TIMESTAMPTZ,
    delivered_time    TIMESTAMPTZ,
    approved_time     TIMESTAMPTZ,
    -- constraints
    UNIQUE(space_id, name)
);
CREATE INDEX idx_asset_requests_space ON asset_requests (space_id, create_time DESC);
CREATE INDEX idx_asset_requests_state ON asset_requests (space_id, state);
CREATE INDEX idx_asset_requests_assignee ON asset_requests (assignee, state) WHERE assignee != '';

-- ============================================================================
-- asset_request_line_items
-- ============================================================================
CREATE TABLE asset_request_line_items (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    -- relationships
    request_id        UUID NOT NULL REFERENCES asset_requests(id) ON DELETE CASCADE,
    -- ON DELETE SET NULL so a cross-space asset purge (space B's
    -- asset is referenced from a line_item in space A's request)
    -- doesn't block the cascade. The line_item survives with a null
    -- asset_id; the request still holds it as a pending pointer
    -- the user can re-link or remove. Without SET NULL, force-
    -- deleting a space (or the post-grace SpacePurgeWorker) would
    -- abort with an FK violation any time a line_item in another
    -- space pointed at one of the doomed assets.
    asset_id          UUID REFERENCES assets(id) ON DELETE SET NULL,
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
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
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
  ('assets.assets.read', 'Read Assets', 'View and list assets'),
  ('assets.assets.create', 'Create Asset', 'Create assets'),
  ('assets.assets.update', 'Update Asset', 'Modify asset metadata'),
  ('assets.assets.delete', 'Delete Asset', 'Soft-delete assets'),
  ('assets.assets.undelete', 'Undelete Asset', 'Restore soft-deleted assets'),
  ('assets.assets.import', 'Import Assets', 'Import assets from storage endpoint'),
  ('assets.requests.read', 'Read Requests', 'View and list requests'),
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
  ('assets.lineItems.read', 'Read Line Items', 'View and list line items'),
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
    -- audit. `created_by` is load-bearing: it doubles as the
    -- conversation owner's identities.id (the resource
    -- path carries it as `users/{user}`), and the handler enforces
    -- `created_by == caller's identity id` unless the caller
    -- holds `ai.conversations.readAll` / `deleteAll`. NOT NULL —
    -- every conversation has a creator. Soft-delete-only on
    -- identities means the FK never dangles.
    created_by      UUID NOT NULL REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
    -- stream lease — one active stream per conversation at a time. A
    -- StreamGenerateContent call sets `lock_holder` to its session
    -- UUID and `lock_expires_at` to NOW()+15s, then heartbeats every
    -- 5s to extend the TTL. Concurrent submits get apierr.Aborted;
    -- concurrent Delete/Update get apierr.FailedPrecondition.
    -- Lease auto-expires if the server holding it crashes (15s
    -- ceiling) so the next session can take over by treating an
    -- expired holder as "free".
    lock_holder       UUID,
    lock_expires_at   TIMESTAMPTZ,
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

-- Partial index covering only rows with an active lease — most
-- conversations have no lease most of the time. Used by the
-- expired-lease sweep (future) and avoids touching the main hot
-- indexes when the acquire query needs to check expiry.
CREATE INDEX idx_ai_conversations_lock_expires ON ai_conversations (lock_expires_at)
    WHERE lock_holder IS NOT NULL;

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
    created_by UUID REFERENCES identities(id),
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
    created_by UUID REFERENCES identities(id),
    updated_by UUID REFERENCES identities(id),
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

-- AI chat permissions. Conversations are personal post-Phase-7;
-- messages, artifacts, and artifact versions are facets of a
-- conversation — their reads roll up to ai.conversations.read, and
-- artifact/version mutations roll up to
-- ai.conversations.update/delete. The base CRUD perms gate
-- org-level eligibility; the handler enforces creator-only
-- ownership on top so a viewer can manage their OWN conversations
-- but cannot reach a peer's. The `*All` audit perms bypass the
-- creator check for legal-hold and departed-employee cleanup
-- workflows.
INSERT INTO permissions (permission_id, display_name, description) VALUES
  ('ai.conversations.read', 'Read Own Conversations', 'View and list the caller''s own conversations, messages, artifacts, and artifact versions'),
  ('ai.conversations.create', 'Create Conversation', 'Create conversations under the caller''s own user-uuid'),
  ('ai.conversations.update', 'Update Own Conversation', 'Modify the caller''s own conversations and their artifacts'),
  ('ai.conversations.delete', 'Delete Own Conversation', 'Delete the caller''s own conversations and their artifacts'),
  ('ai.conversations.readAll', 'Read All Conversations', 'Read any user''s conversations in the organization (audit/compliance)'),
  ('ai.conversations.deleteAll', 'Delete All Conversations', 'Delete any user''s conversations in the organization (departed-employee cleanup)'),
  ('ai.chat.stream', 'Stream Chat', 'Use AI chat streaming');
