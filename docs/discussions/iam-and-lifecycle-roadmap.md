# IAM, Lifecycle, and Spaces Roadmap

**Status**: phases 1–7 shipped. Open items rolled over to
[`sso-config-and-iam-rollovers.md`](./sso-config-and-iam-rollovers.md);
this doc is preserved as historical context for the multi-phase
plan. Don't add new tasks here — track them in the rollover doc.
**Owner**: Ashkan
**Started**: 2026-04-26
**Closed**: 2026-04-30

> **Update (2026): Firebase has since been removed — auth is
> Keycloak-only.** This roadmap was executed against Firebase Auth
> (`firebase_identities` table, Firebase blocking triggers, Firebase
> Admin SDK provider config, `internal/firebase/`). All of that is
> gone: the cloud verifies Keycloak OIDC access tokens via
> `internal/oidc`, the identity table is `identities` keyed on the
> Keycloak `sub`, provisioning flows from Keycloak (KC→Kafka→Pivox
> event sync, `internal/identitysync`), and Keycloak is the SSO/OAuth
> broker to customer IdPs. Read the Firebase mechanics below as the
> historical execution record. See `AGENTS.md` for the current model.

## Working agreement

- **TDD discipline.** Every behavior change in this roadmap lands test-first. Refactors require updating existing tests in the same change — do not delete tests for behavior that still exists.
- **Pre-prod freedom.** No `reserved` proto fields, no migration cruft. Edit `000001_init.up.sql` directly. Drop+recreate dev DB at will.
- **Stop at semantic forks.** When a sweep hits a wire-shape, error code, or RPC-name decision, surface options before burying the choice in a diff.
- **Update this doc as phases progress.** Check items off here when they ship. If reality diverges from the plan, update the plan — don't let it rot.

## Goals

Bring the cloud control plane to a coherent v1 shape that supports:

- **Multi-org users** with proper RBAC at org and Space scopes.
- **Account/org lifecycle** (delete, transfer, soft-delete + revival) without deadlocks.
- **SSO** via Firebase Auth project-level providers, with Pivox as source of truth for OIDC/SAML config.
- **Future-proofed proto surface** — drop GCP IAM machinery now, re-import `google/iam/v1` later when fine-grain sharing genuinely matters.

Non-goals: custom roles, conditional bindings, fine-grain `Get/SetIamPolicy`, and Firebase tenants. All deferred or removed.

## Architecture decisions

### Identity model

- **Firebase Auth as IdP**, project-level (no tenants).
- **`firebase_identities` table** mirrors Firebase users; populated by Firebase Auth blocking trigger calling `/internal/v1/syncFirebaseIdentity`.
- **`User` resource** at `organizations/{org}/users/{user}` is the per-org identity record. Independent of role assignments.

### IAM model — distributed across scope-owning services

**Locked principle (sub-decision #12):** *operations with scope-divergent
behavior live on the scope-owning service. Operations that are scope-uniform
stay on a cross-cutting service.* The earlier "single `Iam` service hosts
everything" framing is dropped — the multi-parent dispatch baked extra
runtime gates into a service that didn't own the resources it was gating.

| Resource | Pattern | Notes |
|---|---|---|
| `User` | `organizations/{org}/users/{user}` | Per-org identity. |
| `Group` | `organizations/{org}/groups/{group}` | Named user collection. Org-scoped only (no space variant in v1). |
| `Role` | `organizations/{org}/roles/{role}` | v1 read-only. 4 system roles: `owner`, `admin`, `editor`, `viewer`. Custom roles deferred. |
| `Permission` | `permissions/{permission}` | Global, read-only catalog. Code-defined. |
| `Member` | `organizations/{org}/members/{member}` *and* `organizations/{org}/spaces/{space}/members/{member}` | Shared message type in `iam/v1`. RPCs hosted on `Organizations` (org pattern) and `Spaces` (space pattern), each with its own URL-pattern-narrowed scope. |

**Service distribution:**

- **`Organizations` service** (org-scope IAM):
  - `Get/List/Create/Update/DeleteMember` — operates on `org_members` table, enforces ≥1-owner boundary
  - `TransferOwnership` — atomic two-row swap, org-only by URL pattern
  - `TestIamPermissions` — resolves against direct + group-derived org bindings
- **`Spaces` service** (space-scope IAM):
  - `Get/List/Create/Update/DeleteMember` — operates on `space_members` table, no boundary check
  - `TestIamPermissions` — resolves against direct space bindings *unioned with* parent-org inheritance (different operation than the org-scope variant)
- **`Iam` service** (scope-uniform residual):
  - `Get/List` on `User`, `Role`; `DeleteUser` LRO
  - `Get/List/Create/Update/Delete` on `Group`, plus group membership ops (groups are uniformly org-scoped)
  - `ListPermissions` (global catalog)

Cross-package shared types: `iam/v1/members.proto` defines the `Member` message + Get/List/Create/Update/Delete request types; `Organizations` and `Spaces` import and reuse them. `iam/v1/permissions.proto` similarly hosts the shared `TestIamPermissionsRequest/Response`.

**Permission resolution** at runtime (interceptor): union of direct Member + Group-derived bindings. Space scope inherits from org (decision to lock — see open decisions).

**Member backing schema (locked for phase 4): two tables — `org_members` and `space_members`.** No nullable polymorphic FK, no `scope_kind` column. Each table has a real FK to its parent (`org_members.org_id → organizations.id`, `space_members.space_id → spaces.id`) with `ON DELETE CASCADE`. Why two tables: structural integrity by design beats integrity by CHECK constraint — an `org_members` row physically cannot be misinterpreted as a space membership, closing a class of permission-table bugs that's expensive in IAM specifically. Adding new scope kinds later (if ever) is purely additive (new table, no migration of existing rows). The `Member` *proto* stays unified (one resource, multi-parent pattern); the two-table backing is a server-side implementation detail.

### Project → Space rename

`Project` is dev-coded and doesn't fit broadcast-media use cases (a sub-unit can be a brand, a show, or an event — News, Dateline, Elections). Renamed to **`Space`** (chosen over `Workspace` for being shorter and less corporate).

Resource paths: `organizations/{org}/spaces/{space}/...`. Service: `Spaces`.

### Lifecycle

**`DeleteOrganization` LRO** — soft delete (`delete_time` set, `purge_time = delete_time + 30d`), then purge. Caller must hold the `owner` role; `force=true` skips the grace window and synchronously cascades. Slug-typed confirmation is a client-side UX gate (the macOS / web app forces the user to retype the slug before calling) — not a wire field, since a malicious client can craft any payload it likes. RPC boundary gates org-scoped calls; child resources untouched on soft-delete (no cascade until purge). FAILED_PRECONDITION on active playout / pending LROs / outstanding billing.

**`UndeleteOrganization`** — clears `delete_time` during grace window. Revival is a feature.

**`DeleteUser` LRO** — blocked when sole owner of any active org. Two unblock paths: `TransferOwnership` (or promote another member to owner) on each affected org, OR `DeleteOrganization` on each. Soft-delete is enough to unblock — purge not required.

Cascade order inside `DeleteUser`: memberships → owned data → `firebase.Auth.DeleteUser(uid)` last. `onUserDeleted` Firebase webhook stays as idempotent safety net for console-driven deletes.

### SSO

**`SsoConfig` singleton** (AIP-156) at `organizations/{org}/ssoConfig`. Get + Update only (no Create/Delete/List).

Stores **full canonical OIDC/SAML config** — issuer, client_id, encrypted client_secret, redirect URIs / SAML metadata, attribute mappings, `verified_domains[]`, verification state — plus `firebase_provider_id` as runtime pointer. Pivox is source of truth; Firebase Auth is downstream projection. Updates flow Pivox API → DB write → Firebase API call (best-effort sync; reconciliation job catches drift).

**Sign-in flow:**
- Pre-auth `/v1/sso:resolve {email}` → `provider_id` (rate-limited).
- Post-auth check: token email domain ∈ `verified_domains`.

`client_secret` encrypted at rest (KMS column-encryption or `Secret` table with KMS-wrapped DEKs — decision in phase 3).

---

## Phase 1 — Cleanup

Pure removal/rename. Smallest blast radius. No new APIs.

### Tenant teardown ✅

- [x] Tests: deleted tenant-failure tests (behavior gone); stripped tenant mocks from remaining tests.
- [x] Drop `OrganizationsServer.CreateTenant` / `DeleteTenant` calls in `internal/service/organizations/server.go`.
- [x] Remove `TenantManager` and Create/Update/DeleteTenant from `internal/firebase/auth.go`.
- [x] Drop `organizations.tenant_id` column from `internal/db/migrations/000001_init.up.sql`.
- [x] Drop `SetOrganizationTenantID` query; sqlc regenerated.
- [x] Removed `Identity.TenantID` from `authn.Identity` (Firebase tenant claim no longer relevant).
- [x] Removed tenant comment in `organizations.proto`.

### CustomDomain rip ✅

- [x] No CustomDomain test files existed (verified via grep).
- [x] Deleted CustomDomain RPCs + messages from `pivox/api/v1/organizations.proto`.
- [x] Dropped `custom_domains` table + `custom_domain_state` enum from migration. **Kept `cert_state` — shared with `storage_gateways`.**
- [x] No ACME / DNS verification Go code existed (handlers were never implemented).
- [x] No sqlc queries existed for custom domains.
- [x] Generated Go regenerated via `make proto-generate-go` + sqlc.

### `accounts` → `firebase_identities` rename ✅

- [x] Tests: fixtures + assertions updated; `db.Account` → `db.FirebaseIdentity`, `AccountID` field → `FirebaseIdentityID`, helpers and mocks renamed.
- [x] Renamed `accounts` table → `firebase_identities` in migration. Index `idx_accounts_email` → `idx_firebase_identities_email`.
- [x] Updated FK references: `organizations.created_by_account_id` → `created_by_firebase_identity_id`. Constraint name + ON DELETE SET NULL behavior preserved. `users.account_id` → `firebase_identity_id`.
- [x] Renamed sqlc queries (`UpsertAccount` → `UpsertFirebaseIdentity`, `GetAccountByFirebaseUID` → `GetFirebaseIdentityByUID`, `ListUsersByAccount` → `ListUsersByFirebaseIdentity`, `ListOrganizationsForAccount` → `ListOrganizationsForFirebaseIdentity`); query file `accounts.sql` → `firebase_identities.sql`. Regenerated.
- [x] Updated Go references throughout `internal/server`, `internal/service/organizations`, `internal/filter`, `internal/testutil/mocks`.
- Note: public webhook URL (`/internal/v1/accounts:sync`), handler function name (`syncAccount`), request struct (`syncAccountRequest`), and response field (`account_id`) intentionally retained for phase 1.4 — coordinated with the Firebase Function redeploy.

### `syncAccount` → `syncFirebaseIdentity` endpoint rename ✅

Direct rename (no dual-route) per pre-prod / solo-dev constraints — brief deploy window is acceptable.

- [x] Server URL `/internal/v1/accounts:sync` → `/internal/v1/auth:syncFirebaseIdentity`. Handler `syncAccount` → `syncFirebaseIdentity`. Request struct, response field (`firebase_identity_id`), log keys, doc comments updated.
- [x] Firebase Function (`deployments/firebase/functions/src/index.ts`): URL string updated, response field parsing updated, internal helper `syncAccount` → `syncFirebaseIdentity`, exports `syncAccountOnCreate` / `syncAccountOnSignIn` → `syncFirebaseIdentityOnCreate` / `syncFirebaseIdentityOnSignIn`. `pnpm run build` verifies TypeScript.
- [x] Test names `TestSyncAccount_*` → `TestSyncFirebaseIdentity_*`; URL strings + response field assertions updated.
- [x] **Deployed server** (live ngrok endpoint).
- [x] **Deployed Firebase Functions** via `make firebase-deploy`. Old `syncAccountOn*` and the dead `githubOAuthCallback` cleaned up in the same deploy.
- [x] **Verified post-deploy via gcloud**:
  - Cloud Run services list: only the two new `syncfirebaseidentity*` services remain.
  - `gcloud functions list --v2` shows both `syncFirebaseIdentityOnCreate` and `syncFirebaseIdentityOnSignIn` ACTIVE.
  - Identity Platform blocking triggers (`identitytoolkit.googleapis.com/admin/v2/projects/<id>/config`) point at the new function URIs — `beforeCreate` and `beforeSignIn` rewired to `syncFirebaseIdentity*` URLs, fresh updateTime.
- [x] **New tooling**: `scripts/clean-fn-revisions.sh` + `make clean-fn-revisions` target. Auto-discovers Firebase-managed Cloud Run services from gcloud, deletes orphans (services not in `firebase functions:list`) and trims stale revisions (anything not actively serving). Dry-run by default; `FORCE=1` to delete.

### IAM proto trim + consolidation ✅

Scope grew during the phase: in addition to the planned trim, the IAM
surface was consolidated into a single `Iam` service per the design
discussion. `TestIamPermissions` was *not* preserved — phase 2 will
re-introduce it as part of the new `Iam` service alongside `Member`.

- [x] Deleted `pivox/iam/v1/iam_policy.proto` and `pivox/iam/v1/policy.proto` (Policy/Binding/conditional expressions, GetIamPolicy/SetIamPolicy/TestIamPermissions). No port of TestIamPermissions — phase 2 brings it back.
- [x] Removed IAM RPCs from `organizations.proto`, `projects.proto`, `tag_keys.proto`, `tag_values.proto`. Dropped iam_policy + policy imports.
- [x] **Consolidated three IAM services into one**: deleted `Roles`, `Users`, `Groups` services; created single `Iam` service in new `iam.proto`. All read paths (Get/ListUser, Get/ListRole, ListPermissions) and Group CRUD + GroupMember mgmt RPCs now live on `Iam`.
- [x] **Per-resource message files**: `roles.proto`, `users.proto`, `groups.proto` now hold messages only; new `permissions.proto` extracted from `roles.proto` (Permission is global, not role-scoped).
- [x] Deleted `internal/iam` package (Helper became empty after IAM RPCs removed).
- [x] Stripped `iam.Helper` field/handler delegation from `OrganizationsServer`, `ProjectsServer`, `TagKeysServer`, `TagValuesServer`, and `cmd/pivox-cloud/main.go`.
- [x] Dropped `iam_policies` table + `iam_policies.sql` query + generated `iam_policies.sql.go`. Removed seeded permission rows for `organizations.getIamPolicy` / `organizations.setIamPolicy`.
- [x] Trimmed `mocks.MockQuerier` of `GetIamPolicy`/`UpsertIamPolicy`/`DeleteIamPolicy` stubs.
- [x] Removed all IAM-Policy-shaped tests from unit + integration suites.
- [x] Added api-linter ignore for `core::0121::resource-must-support-get` on the `Iam` service (Permission is a read-only catalog).

### Phase 1 exit criteria ✅

- [x] `make lint-proto && make proto-format && make proto-generate && make tidy` clean.
- [x] `make build` clean.
- [x] `go test ./...` clean.
- [x] `go test -tags dev ./...` clean for organizations, projects, tags, server (pre-existing aichat/storageagent failures tracked separately, see "Pre-existing test failures").
- [x] `make lint` clean.
- [x] No `tenant`, `custom_domain`, `account` (in identity sense), `Policy`/`Binding`/`SetIamPolicy`/`AddRoleMembers` references remain in non-generated code.
- [x] Manual smoke: registration → org onboarding → AIChat verified (post phase 1.4 deploy via gcloud).
- [x] **`make api-lint`**: Pivox-IAM files all clean (`groups.proto`, `iam.proto`, `permissions.proto`, `roles.proto`, `users.proto` all `problems: []`). Pre-existing failures in `assets/v1/asset.proto`, `storage/v1/storage_gateway.proto`, `storage/v1/endpoint.proto`, `ai/v1/messages.proto` predate this phase; tracked under "Pre-existing test failures".
- [ ] `xcodebuild test -scheme PivoxTests` — macOS unit tests (run before phase 1 ships).

---

## Phase 1.5 — Rename `Project` → `Space` ✅

Absorbed into the Phase 4 step 1 schema sweep + the proto/Go renames
that landed alongside it. Verified post-Phase-4 that no items remain:

- [x] All protos renamed (`projects.proto` → `spaces.proto`; URL
      paths use `organizations/*/spaces/*` everywhere).
- [x] DB schema uses `spaces`, `space_id`, `space_members`,
      `idx_spaces_*` etc. No `project_*` artifacts.
- [x] Seeded permissions use `spaces.create`, `space.delete`, etc.
- [x] `internal/service/spaces/` exists; no `internal/service/projects/`.
- [x] sqlc queries + generated code use `space_id`.
- [x] `internal/crypto/encryptor_gcp.go`'s `projects/...` references
      are GCP-KMS resource names (intentional, not Pivox projects).
- [x] No `Project` / `projects` references in pivox-owned code
      (verified by grep — remaining hits are vendored Google protos
      under `api/proto/google/`).

---

## Phase 2 — IAM v1 proto ✅

Decisions locked before sweeping:

1. Space role inheritance: **union with org-level** (org owners are space owners by inheritance).
2. `{member}` URL segment: **`user-{id}` or `group-{id}`** (singular typed prefix). `Member.principal` is a **oneof** with `user` and `group` branches, each carrying its own `resource_reference` annotation.
3. `TransferOwnership`: **dedicated atomic RPC** (single transaction; never leaves a scope ownerless).
4. `DeleteUser`: **all through LRO** (cascade is a server-side state machine; phases surfaced via `DeleteUserMetadata.Phase` enum).
5. `Member` schema backing (phase 4): **two tables** (`org_members`, `space_members`) — structural integrity by design, no nullable polymorphic FK.

### Files

- [x] `pivox/iam/v1/members.proto` (NEW): `Member` message + 5 CRUD request/response messages + `TransferOwnershipRequest`.
- [x] `pivox/iam/v1/users.proto`: added `DeleteUserRequest` + `DeleteUserMetadata` (with `Phase` enum: `VALIDATING`, `REVOKING_MEMBERSHIPS`, `DELETING_PIVOX_RECORDS`, `DELETING_FIREBASE_IDENTITY`, `COMPLETED`).
- [x] `pivox/iam/v1/permissions.proto`: added `TestIamPermissionsRequest` + `TestIamPermissionsResponse`.
- [x] `pivox/iam/v1/iam.proto`: extended `Iam` service with new RPCs.

### RPCs added

- [x] **Members** (5 multi-parent RPCs): `GetMember`, `ListMembers`, `CreateMember`, `UpdateMember`, `DeleteMember` — all bind both `organizations/*/members/*` and `organizations/*/spaces/*/members/*`.
- [x] **Lifecycle**: `DeleteUser` returning `google.longrunning.Operation`; `TransferOwnership` returning `Member` (atomic role swap).
- [x] **Permission gating**: `TestIamPermissions` — multi-parent (org and space resource).

### Phase 2 exit criteria ✅

- [x] `make proto-format && make lint-proto && make proto-generate-go` clean.
- [x] `make api-lint`: all six iam/v1 protos `problems: []` (groups, iam, members, permissions, roles, users). Pre-existing failures elsewhere (storage/asset/ai/agent) unchanged.
- [x] `go build ./...` clean — generated stubs compile.
- [x] `go test ./...` clean.
- No server impl — phase 4 wires up handlers.

---

## Phase 3 — Lifecycle + SSO proto ✅

Shipped at commit `b49828d` and refined in `584790d` (phase 4 step 0,
which extracted `Domain` as its own org-level resource and removed the
short-lived `Sso` gRPC service).

### Org lifecycle

- [x] `DeleteOrganization` returns `google.longrunning.Operation` (sync version replaced).
- [x] `UndeleteOrganization` LRO.
- [x] `Organization.delete_time` / `purge_time` fields.
- [x] `Organization.state` enum (`STATE_UNSPECIFIED`, `ACTIVE`, `DELETE_REQUESTED`).
- [x] `DeleteOrganization` request: `{name, etag, force}`. Standard AIP-135 DELETE
      verb after the typed-confirm pattern was moved to client-side UX gate
      (sub-decision below). `force=true` skips the 30-day grace window and
      synchronously cascades.
- [x] `DeleteOrganizationMetadata` Phase enum: VALIDATING, CANCELLING_OPERATIONS,
      MARKING_DELETED, PURGING (force-only), COMPLETED.

### User lifecycle (proto only — handlers in phase 4)

- [x] `DeleteUser` LRO + `DeleteUserMetadata.Phase` enum landed in phase 2.
- [x] Sole-owner-blocking error code documented in proto comments
      (FAILED_PRECONDITION + structured detail listing affected orgs) —
      see `DeleteUserRequest` doc comment in `pivox/iam/v1/users.proto`.
- [x] `TransferOwnership` — dedicated atomic RPC on `Iam` service (locked: open decision #5).

### SSO

- [x] `SsoConfig` singleton message at `organizations/{org}/ssoConfig` per AIP-156.
- [x] Fields: `firebase_provider_id`, `display_name`, `enabled`,
      oneof `oidc | saml`, etag, timestamps. `verified_domains[]` was
      removed in phase 4 step 0 in favor of the org-level `Domain` resource.
- [x] `OidcConfig` and `SamlConfig` shapes match Firebase Admin SDK
      `OIDCProviderConfig` / `SAMLProviderConfig`.
- [x] `Get`, `Update` only (sync; not LROs — see sub-decision #11 below).
- [x] DNS-TXT verification flow lives in the `Domain` resource (extracted in
      step 0). `CreateDomain` returns an LRO that drives DNS polling end-to-end.
- [x] No `Sso.Resolve` gRPC RPC. Replaced by `POST /internal/v1/auth:resolveProvider`
      stdlib HTTP route on the existing `:8080` listener (sub-decision #7 below).
- [x] Secret-storage: **KMS column-encryption** for `client_secret` (locked: open decision #4).

### Phase 3 exit criteria

- [x] Proto pipeline clean (`make proto-format && make lint-proto && make api-lint && make proto-generate && make tidy`).
- [x] Build clean (`make build && go test ./...`).
- [x] Spec doc updated: this section + sub-decisions #6–#11 below.

---

## Phase 4 — Server impl + final proto cleanup

The big build. Strict TDD: write the unit/integration test first,
watch it fail, then implement.

Executed as 8 ordered steps. Each step ends in a clean gate (build,
tests, lint) and an audit-eligible commit boundary.

### Step 0 — Proto cleanup ✅ (commit `584790d`)

- [x] Drop `service Sso { ... }` from `sso.proto`. SSO config CRUD lives on Organizations.
- [x] Drop `VerifiedDomain` message + `repeated VerifiedDomain verified_domains` field from SsoConfig.
- [x] Add new `domains.proto` with org-level `Domain` resource (decoupled from SSO; future consumers may include email allowlist, branding).
- [x] Add 4 RPCs to `Organizations` service: `CreateDomain` (LRO), `ListDomains`, `GetDomain`, `DeleteDomain`.
- [x] `CreateDomainMetadata` Phase enum: AWAITING_DNS, VERIFIED, FAILED, EXPIRED.

### Step 1 — Schema sweep + sqlc + dev-DB recreate ✅

- [x] Init migration edit: new tables `org_members`, `space_members` (new shape),
      `domains`, `sso_configs`. New enums `principal_kind`, `domain_state`.
- [x] Drop old tables `role_members` and old `space_members`. Drop old enums
      `space_role`, `space_member_type`, `role_member_type`, `org_role`.
- [x] `users` loses `role` column; gains soft-delete (`delete_time`, `purge_time`, `deleted_by`).
- [x] `roles` gains stable `name` slug column (system-role machine identifier;
      `name='owner'/'admin'/'editor'/'viewer'` for the 4 system roles per org).
      `UNIQUE(org_id, name)`.
- [x] `domains.domain` `UNIQUE` globally + `CHECK (domain = lower(domain))` for
      case-insensitive single-claim enforcement.
- [x] `sso_configs.client_secret_ciphertext BYTEA` separate from `oidc_config`
      JSONB; KMS-encrypt on write. CHECK ensures exactly one of OIDC/SAML.
- [x] Permission catalog adds `domains.*`, `organizations.ssoConfig.*`, `members.*`, `users.delete`.
      Plus: catalog refactored to YAML-driven CRUD model with codegen, additional
      perms for `tags.*`, `spaces.{read,update,delete}`, `apiKeys.*` rename.
- [x] `users.sql` queries: `CreateUserMembership` no longer takes role,
      `CountOwnersByOrg` joins `org_members + roles + users` keyed on `roles.name='owner'`,
      `SoftDeleteUserMembership` added, `UpdateUserRole` removed.
- [x] sqlc regen + Go code fixes (CreateOrganization handler, mock querier, two test files).
- [x] Drop+recreate dev DB; `make db-seed` clean; storage agent registration tokens present.
- [x] Code-reviewer audit before commit.

### Step 2 — Permission interceptor ✅

- [x] Test fixtures: orgs with various member/group/role bindings.
- [x] Resolver: caller → effective role at target scope, with org→space inheritance
      (locked: open decision #1 — union with org-level).
- [x] Static `(role, permission) → allow` map in code.
- [x] **YAML-driven permission catalog** (`internal/permission/permissions.yaml`)
      with `cmd/gen-permissions` emitting typed constants + matrix. CRUD model
      (collapsed `*.list` + `*.get` → `*.read`); AI sub-resources rolled into
      `ai.conversations.*`. 73 permissions total.
- [x] **Proto-annotation permission gating**: `pivox.permission.v1.{required_permission,
      exempt, scope_field}` extends MethodOptions on every gated RPC across
      Organizations, Spaces, Iam, ApiKeys, Tags, Storage, Assets, Requests, AiChat
      (~110 RPCs).
- [x] `cmd/gen-permission-registry` walks `protoregistry.GlobalFiles`, emits
      `internal/server/permission_registry_gen.go` with the union Registry +
      Exempt set. Drift-guard tests assert (a) every RPC on every gated service
      is wired, (b) every emitted permission ID is in `permission.All`.
- [x] `make proto-generate` chains into `make generate` so proto annotation
      edits regenerate the registry automatically.
- [x] Wire into gRPC server interceptor chain — both unary
      (`PermissionInterceptor`) and streaming (`PermissionStreamInterceptor`,
      gates on first RecvMsg). Wired in `cmd/pivox-cloud/main.go` after Auth +
      Membership, before Validate.
- [x] `ResolvedOrg` / `ResolvedSpace` attached to ctx so handlers don't repeat
      slug → row lookups. `MustResolvedOrgFromContext` /
      `MustResolvedSpaceFromContext` for handler-side assertion.
- [x] `Organizations.TestIamPermissions` and `Spaces.TestIamPermissions`
      handlers reuse the same resolver (each builds its own scope-shaped
      `Target` — `OrgTarget` or `SpaceTarget`). Per locked sub-decision #14.
      SECURITY-commented — both are exempt from the interceptor (gating
      would be circular) and resolve caller identity themselves.

### Step 3 — Member / Group / Role handlers ✅

(Restructured per locked sub-decisions #12–#15: scope-divergent IAM ops
moved to scope-owning services. Original "single Iam mega-service"
plan is obsolete.)

- [x] **`Organizations` service gains:**
      - `Get/List/Create/Update/DeleteMember` — single-scope dispatch
        on `org_members` table; tx-wrapped ≥1-owner boundary on Update
        and Delete.
      - `TransferOwnership` — atomic two-row swap inside one transaction;
        returns `TransferOwnershipResponse {new_owner, previous_owner}`.
      - `TestIamPermissions` — resolves direct + group-derived org bindings.
- [x] **`Spaces` service gains:**
      - `Get/List/Create/Update/DeleteMember` — single-scope dispatch
        on `space_members` table; no boundary check.
      - `TestIamPermissions` — resolves direct + group-derived space
        bindings unioned with parent-org inheritance.
- [x] **`Iam` service keeps:**
      - `Get/List` on `User`, `Role`; `DeleteUser` proto contract (LRO impl in Step 5).
      - `Get/List/Create/Update/Delete` on `Group` + `Add/Remove/ListGroupMembers`.
      - `ListPermissions`.
- [x] `CreateOrganization` handler grows: seed 4 system roles for the new org,
      then insert `org_members` row binding the founder to the system 'owner' role.
      Restore the deferred owner-binding test assertion (tracked in step 1).

> Step 2 (Permission interceptor) was the natural follow-on to Step 3
> handlers; both shipped together across commits 4a91cf2 (catalog) →
> 38ec1f7 (scaffolding) → 8ea5368/cd23ee9/2be9c29 (per-service
> registries) → 9d28a4a (proto-annotation rewrite + interceptor live).

### Step 4 — Org lifecycle ✅

- [x] `Organizations.DeleteOrganization` LRO orchestrator. State machine matching
      `DeleteOrganizationMetadata.Phase`: VALIDATING → CANCELLING_OPERATIONS →
      MARKING_DELETED|PURGING → COMPLETED. `force=true` takes the PURGING branch.
- [x] Cancellation of in-flight org-scoped LROs via the `operations.org_id`
      reverse pointer (added in this commit). LROs opt in by calling
      `lro.Manager.CreateAndRunForOrg`. Today only DeleteOrganization itself
      uses the LRO machinery (passes NULL to avoid self-cancel); future LROs
      (asset imports, domain verifications, gateway upgrades) populate the
      column when implemented.
- [x] Soft-delete gate at RPC boundary: org-scoped reads succeed with metadata;
      mutations return `FAILED_PRECONDITION`. `organizations.delete` is
      explicitly allowed against a DELETE_REQUESTED org so the handler can
      dispatch UndeleteOrganization or surface re-delete errors itself.
- [x] `Organizations.UndeleteOrganization` LRO clears `delete_time`/`purge_time`,
      restores `state=ACTIVE`. Grace-window check (purge_time > now) enforced
      in the SQL.
- [x] Slug freed at purge time, not at soft-delete (enforced by the global
      UNIQUE constraint on `organizations.name`).
- [x] `force=true` requires a non-empty etag pinning the row revision —
      destructive op safety.

### Step 5 — User lifecycle ✅

- [x] `Iam.DeleteUser` LRO orchestrator (`DeleteUserMetadata.Phase`: VALIDATING,
      REVOKING_MEMBERSHIPS, DELETING_PIVOX_RECORDS, DELETING_FIREBASE_IDENTITY,
      COMPLETED). Self-delete via `users/me`. Path: `organizations/{org}/users/{user}`
      where `{user}` is the per-org users.id UUID or `me`.
- [x] Sole-owner check: `org_members WHERE role_id=<system owner>` group-by-org,
      keyed on caller's firebase_identity_id. Blocks with FAILED_PRECONDITION
      and lists affected orgs in the message so the caller can route to
      `Organizations.TransferOwnership` or `DeleteOrganization`.
- [x] Cascade order: org_members + space_members revoked explicitly; users +
      group_members removed via FK ON DELETE CASCADE when the firebase_identity
      row is hard-deleted; `auth.DeleteUser(uid)` last so a partial failure
      leaves a recoverable Firebase identity. Idempotent on already-gone rows
      so retry-from-mid-cascade is safe.
- [x] `authn.Service.DeleteUser` interface + Firebase impl that swallows
      `auth.IsUserNotFound` errors for retry idempotency.
- [ ] `onUserDeleted` Firebase webhook handler: idempotent — no-op if user
      already gone. Out-of-process, lives in `deployments/firebase/functions/`;
      separate commit.
      **Open semantic question (block before implementing): when a Firebase
      user is deleted out-of-band (Console / gcloud) and they're the sole
      owner of one or more orgs, what should the webhook do?** DeleteUser
      LRO uses refuse-with-FailedPrecondition; the webhook has no caller
      to show an error to. Options:
        (a) refuse — Pivox state diverges from Firebase, no recovery path.
        (b) proceed, leave the org ownerless, loud alert. Admin reconciles.
        (c) auto-promote next-admin — implicit policy.
        (d) auto-soft-delete affected orgs — 30-day grace gives ops time.
      Two new files when picked: Go internal endpoint + TS Cloud Function.
      Realistic scope: ~2-3 hours including the integration test.

### Step 6 — Workers (purge + verify-DNS), in-process ✅

> **DNS verification is mocked for v1.** The stub resolver returns
> "TXT matches" unconditionally (sub-decision #10). `CreateDomain` LRO
> transitions PENDING → VERIFIED on the next tick regardless of real DNS
> state. Real `net.Resolver`-backed verification wires up before any
> external admin uses SSO. Do not implement real DNS polling in this
> phase — ship the stub, document the gap, move on.

- [x] New `internal/workers/` package. `PurgeWorker` and
      `VerifyDomainWorker`, both expose `Run(ctx) error`. Common `loop`
      helper, `RunAll(ctx, logger, ws...)` for fleet startup.
- [x] Dependencies (`*pgxpool.Pool`, `db.Querier`, logger, DNSResolver,
      tick interval) injected; no reach into HTTP/gRPC server internals.
      Trivially transferable to a dedicated `cmd/pivox-purge-worker/` binary
      later (sub-decision #9).
- [x] Purge worker: scans `ListOrgsPastPurgeTime`, drives final cascade
      via `PurgeOrganization`. FK ON DELETE CASCADE handles children.
      Slug freed on row delete (UNIQUE constraint on `organizations.name`).
      Per-row failure logged + skipped so a stuck row doesn't stall the batch.
- [x] Verify-DNS worker: ticks PENDING domains to VERIFIED via the
      stub `LookupTXT`. Lookup failure / empty-record path keeps the row
      PENDING for retry next tick. Backoff schedule (2 min × 1h → 30 min × 24h
      → 6h × 6d → EXPIRED) is wire-only this phase — with the stub every tick
      "succeeds", so backoff has no observable effect; the real-DNS commit
      grows the schedule into actual per-row state.
- [x] Postgres advisory lock per worker type via
      `withAdvisoryLock(pool, lockID, work)`. Session-scoped via a held
      pool connection; auto-released if the conn is returned to the pool
      mid-flight. Distinct lock IDs in `advisory_lock.go` (`purgeWorkerLockID`,
      `verifyDomainWorkerLockID`) so different worker types don't serialize
      against each other.
- [x] `DNSResolver` interface + `StubDNSResolver` impl. Always returns a
      single fake TXT record. Logged at INFO on every call so production
      telemetry makes the fake DNS path obvious. Real impl deferred until
      pre-prod SSO go-live.
- [x] Wired in `cmd/pivox-cloud/main.go`: workers run alongside the gRPC
      server via `workers.RunAll`; ctx cancellation stops both, WaitGroup
      blocks the binary until they exit.

### Step 7 — Domain RPC handlers ✅

- [x] `Organizations.CreateDomain` — generates a 32-byte CSPRNG token
      (43-char unpadded base64url), writes the domains row with
      `state=PENDING`, dispatches an LRO whose work fn long-polls the row
      (30s interval, 7-day grace) until the verify-domain worker flips
      state to VERIFIED or FAILED. Initial metadata carries the
      `verification_token` so clients display DNS instructions immediately
      without waiting for the LRO. Returns `ALREADY_EXISTS` for
      globally-claimed domains via the pgconn unique-violation, *without*
      disclosing the holding org (security: error message names only the
      domain string, never the holder).
- [x] `Organizations.GetDomain`, `ListDomains` — sync. Reads from
      `ResolvedOrg` in ctx; no extra DB calls beyond the row fetch.
- [x] `Organizations.DeleteDomain` — sync. Three preconditions in order:
        1. etag check (when client supplied);
        2. last-VERIFIED-domain-on-enabled-SSO guard (FAILED_PRECONDITION
           when removing this row would leave an enabled SsoConfig with
           zero verified domains);
        3. cancel any in-flight CreateDomain LRO via
           `CancelDomainOpsForDomain` (matches by `metadata->>'domain'`
           since LROs created here populate that field). DB error in
           cancel surfaces as Internal — we don't delete the row while a
           verifying goroutine still runs.
- [x] `convert.DomainToProto` — wraps the row with the per-org slug to
      construct the resource name. State enum mapping covers PENDING /
      VERIFIED / FAILED / unspecified.
- [x] Permission gating: `domains.{create,read,delete}` already in catalog
      from Step 2; interceptor enforces.

### Step 8 — SSO config + `auth:resolveProvider`

- [x] `Organizations.GetSsoConfig` / `UpdateSsoConfig` handlers (sync, not LRO).
- [x] On Update: validate → KMS-encrypt new `client_secret` if provided →
      Firebase Admin SDK `CreateOidcProvider` / `UpdateOidcProvider` →
      DB upsert. Failure on Firebase aborts before local write so state
      stays consistent.
- [x] KMS column-encryption via existing `internal/crypto/encryptor_gcp.go` path
      (locked: open decision #4). Drift reconciliation job lands later, not phase 4.
- [x] `POST /internal/v1/auth:resolveProvider { email }` → `{ provider_id }`.
      Hand-written handler in `InternalHooks`, sibling of `auth:exchangeToken`.
      Reuses existing `rateLimit` middleware.
- [x] Lookup chain: `email → domain → domains WHERE org_id AND state=VERIFIED →
      sso_configs WHERE enabled=true → firebase_provider_id`.
- [x] SAML wired through Firebase Admin SDK — `CreateSamlProvider` /
      `UpdateSamlProvider` / `DeleteSamlProvider` mirror the OIDC
      methods; UpdateSsoConfig handler dispatches based on the
      `oneof config` selection.

### Step 8b — Audit-driven hardening (post-Step 8 reviewer pass)

A full audit of Phase 4 surfaced 13 findings (3 HIGH, 7 MED, 3 LOW).
All resolved in this step before moving on to integration tests.

- [x] **HIGH #1.** `CountOwnersByOrg` only counted `principal_kind='user'`
      bindings, silently excluding group-owners. Mixed user+group owner
      configs gave false positives on the ≥1-owner guard. Fixed via
      EXISTS subqueries that count both principal kinds.
- [x] **HIGH #2.** `lro.Manager.runWork` used `context.Background()` so
      `CancelOperation` only marked the DB row done — running goroutines
      ran to completion and overwrote the cancel state. Fixed by adding
      a per-op `context.CancelFunc` registry; `CancelOperation` now
      invokes the registered cancel so the goroutine observes
      `ctx.Done()` and aborts cleanly.
- [x] **MED #4.** Member handlers (org + space) re-issued
      `GetOrganizationByName` / `GetSpaceByName` instead of using the
      `ResolvedOrg` / `ResolvedSpace` already attached by the
      interceptor. Refactored to read from context with defensive
      slug-match assertions.
- [x] **MED #5.** `ListMembers` (org + space) silently truncated at
      1000 rows with no `next_page_token`. Implemented offset-based
      AIP-132 pagination with `page_size` (default 50, max 500) and
      opaque base-10 `page_token` cursors.
- [x] **MED #6.** Concurrent `UpdateSsoConfig` on a fresh org both saw
      `ErrNoRows` and both called `CreateOidcProvider`. Resolved via
      idempotent Firebase-side fallback: try Create then fall through
      to Update on `ErrAlreadyExists`; try Update then fall through to
      Create on `ErrNotFound`. Adds `authn.ErrAlreadyExists` /
      `authn.ErrNotFound` sentinels so the org service doesn't import
      firebase directly.
- [x] **MED #7.** Force-path `PurgeOrganization` had no race-guard, so
      a concurrent soft-delete + undelete cycle could leave the LRO
      operating on a row whose state had drifted since handler-time
      validation. Added etag pinning to the SQL (`WHERE id=$1 AND
      etag=$2`); LRO surfaces `FailedPrecondition` on drift.
- [x] **MED #8.** `CancelDomainOpsForDomain` and
      `CancelRunningOpsForOrg` were bulk SQL UPDATEs that marked
      operations done but didn't notify in-replica goroutines, leaving
      windows where verify/orchestrator goroutines could write to rows
      about to be deleted. Both queries now `RETURNING id`; the
      handler invokes `lro.Manager.CancelLocal(ids...)` to fire local
      cancel funcs immediately.
- [x] **MED #9.** `lro.Manager.runWork` notified listeners
      unconditionally even when `CompleteOperation` /
      `FailOperation` DB write failed, leaving callers waking up to a
      stuck `done=false` row. Notification now only fires on
      successful DB write; failures are logged and recovered on next
      restart via `RecoverPending`.
- [x] **MED #10.** SAML promoted from "Unimplemented" stub to full
      Firebase Admin SDK integration. See Step 8 entry above.
- [x] **LOW #11.** `verifyTickFn` / `purgeTickFn` test helpers were
      hand-copied subsets of the real `tick` body. Refactored
      `tick` → `tick` + `processBatch`; tests now exercise
      `processBatch` directly without the advisory lock.
- [x] **LOW #12.** `X-Forwarded-For` was honored unconditionally for
      rate-limit identity, defeatable by any client that could reach
      the server directly. Added `Config.TrustedProxies` (CIDR list);
      empty default = fail closed (header ignored, key on
      `RemoteAddr`); when `RemoteAddr` is in a trusted CIDR, picks the
      leftmost untrusted XFF entry.
- [x] **LOW #13.** `UpsertSsoConfig` SQL had a misleading comment
      conflating "empty bytes" with "NULL via sqlc.narg". Rewrote the
      comment to describe both bindings accurately and document that
      "clear secret" is intentionally not a query option.

#### Audit findings deferred to Step 9 (integration tests)

- The roadmap-locked Phase 4 exit criteria (below) require real-DB
  integration tests for soft-delete→revive, DeleteUser blocking flows,
  CreateDomain LRO state machine, and end-to-end permission
  interceptor coverage. These need a test-mode interceptor pipeline
  (the existing scaffolding doesn't wire one up) and represent
  ~1500 LOC of net-new test code. Tracked as Step 9 to keep the
  audit-fix diff focused and reviewable.

### Phase 4 exit criteria — Step 9 (integration tests) ✅

Built on a shared end-to-end test harness in
`internal/testutil/grpcharness/` (real Postgres testcontainer + the
production interceptor chain: Auth + MembershipRequired + Permission
+ Validate). Each item below is a separate commit; the harness is
reusable by Phase 5+ space-scoped tests without modification.

- [x] All new/changed RPCs covered by integration tests. (Lifecycle,
      Member CRUD, Domain LRO, DeleteUser/DeleteAccount split,
      permission matrix — see items below.)
- [x] Permission interceptor tests cover org+space, user+group,
      inheritance, deny paths. (`internal/server/permission_e2e_test.go`)
- [x] Soft-delete → revive end-to-end test.
      (`internal/service/organizations/lifecycle_e2e_test.go`)
- [x] `DeleteAccount` blocking → unblock-via-transfer /
      unblock-via-org-delete tests. (`internal/service/iam/lifecycle_e2e_test.go`
      — also covers the org-scoped `DeleteUser` after the A2 split.)
- [x] `UpdateSsoConfig` → Firebase Admin SDK side-effect test (with mock).
      (Covered by the existing unit tests in
      `internal/service/organizations/sso_test.go`, which exercise the
      full handler against a mock implementing `authn.Service` —
      including the create-or-update fallback for both OIDC and SAML.)
- [x] `CreateDomain` LRO drives PENDING → VERIFIED → EXPIRED through
      stubbed DNS resolver. (`internal/service/organizations/domains_e2e_test.go`)
- [x] Native macOS app rebuilds against regenerated stubs.
      Verified via `xcodebuild build -project build-xcode/Pivox.xcodeproj
      -scheme Pivox` — clean BUILD SUCCEEDED including the new
      `Iam.DeleteAccount` RPC + `accounts/me` resource stubs.
- [ ] `make build && go test ./... && make api-lint && make lint` clean.
      (Open: `internal/service/aichat` build-fail and
      `internal/storageagent` runtime flake are pre-existing, not
      Phase-4-introduced. Logged in roadmap "pre-existing test failures"
      section. `make api-lint` has a pre-existing storage-gateway
      lint warning unrelated to Phase 4.)

---

## Phase 5 — Spaces impl

Wire existing (post-rename) `Spaces` RPCs to schema. `Spaces` proto largely already defined.

- [x] Update tests for existing `Projects`-now-`Spaces` server unit + integration tests. *(P5.1: slimmed unit tests to validation-surface only; obsolete `server_integration_test.go` removed; coverage migrated to grpcharness E2E.)*
- [x] `CreateSpace`: seed default space-level Member binding (creator → owner). *(P5.1: founder owner seed atomic with space create; pinned by `TestE2E_CreateSpace_SeedsFounderOwnerBinding`.)*
- [x] Audit-class fixes for Get/Update/Delete/Undelete/List handlers — use resolved-context (`MustResolvedOrgFromContext` / `MustResolvedSpaceFromContext`), populate `created_by`/`updated_by`/`deleted_by` from caller identity, FailedPrecondition on non-ACTIVE state, slug-mismatch defensive checks. *(P5.1.)*
- [x] Soft-delete-aware gate for spaces: added `GetSpaceByNameForGate` mirror of the org variant; permission interceptor now resolves soft-deleted spaces so reads-during-grace and `UndeleteSpace` reach the handler. Also dropped the `delete_time IS NULL` filter from `GetSpaceParentOrg` so the resolver can fold in org-level inheritance for soft-deleted spaces. *(P5.1.)*
- [x] Space-scope soft-delete gate enforcement: added `enforceSpaceSoftDeleteGate` mirror of `enforceSoftDeleteGate`. Mutating RPCs against a DELETE_REQUESTED space surface FAILED_PRECONDITION at the interceptor (single source of truth, matches org-scope semantics); `spaces.delete` passes through so UndeleteSpace works. Per-handler state guards in UpdateSpace removed (gate is authoritative). Pinned by `TestE2E_SoftDeletedSpace_BlocksMutationsAtGate`. *(P5.1.)*
- [x] Member-write atomicity + audit fields: org-scope and space-scope `CreateMember`/`UpdateMember` now wrap `GetSystemRole` inside the same tx as the principal-existence check + insert/update — closes the role-rename race. `created_by` is now populated on `org_members` and `space_members` inserts (was silently dropped). *(P5.1, audit-class.)*
- [x] `DeleteSpace`: same soft-delete + purge pattern as orgs. *(P5.2: DeleteSpace + UndeleteSpace converted to async LROs with phase progression; force=true synchronous cascade with etag pinning; SpacePurgeWorker drives the post-grace cascade. Schema: assets.space_id and asset_requests.space_id now ON DELETE CASCADE so PurgeExpiredSpace transitively wipes children.)*
- [x] Inheritance from org level for permission resolution (decision-locked above). *(Phase 4; verified end-to-end in P5.1 by `TestE2E_UndeleteSpace_RestoresSoftDeletedSpace` exercising the org-inheritance path.)*
- [x] Asset / AssetRequest / Tag* / Dashboard handlers updated for `space_id` column. *(Phase 1.5.)*

### Phase 5 exit criteria

- [ ] All space-scoped RPCs pass integration tests.
- [ ] Permission resolution honors org→space inheritance.
- [ ] Native macOS app can create + list + delete spaces (manual smoke).

---

## Phase 6 — UI

macOS-first; Windows shell mirrors after.

### Members management

- [ ] Members list view (per org, per space).
- [ ] Invite member: email lookup → existing user OR pending invite.
- [ ] Remove member.
- [ ] Update role.
- [ ] Group management: list, create, add/remove users.

### Org settings

- [ ] Display name editing (already partially done via OrgService).
- [ ] Slug shown as immutable.
- [ ] Transfer ownership flow.
- [ ] Delete org flow with slug-typed confirmation.
- [ ] Soft-delete state surfaced ("scheduled for deletion on …" banner).
- [ ] Undelete affordance during grace.

### Account settings

- [ ] Account delete flow.
- [ ] Sole-owner blocking surfaces affected orgs with quick links to TransferOwnership / DeleteOrganization.
- [ ] Email change, profile editing.

### SSO config

- [ ] Per-org SSO config form (OIDC + SAML variants).
- [ ] Domain verification UI.
- [ ] Test login button.

### Phase 6 exit criteria

- [ ] Account + org deletion possible end-to-end via UI.
- [ ] No "manual SQL to clear sole-owner block" required for normal flows.
- [ ] SSO config UI exercised end-to-end against real Firebase project.

---

## Future / on-demand

- [ ] **`Organizations.LeaveOrganization`** — member self-leave-org RPC.
      Open product question: SSO orgs need IdP-controlled offboarding (a
      user shouldn't be able to leave unilaterally because the IdP would
      re-add them on next sign-in), email/social orgs may allow self-leave.
      Implementation requires either per-org policy config or per-IdP
      semantics. Defer until product clarity. Today's gap: a member who
      wants out has to ask an owner/admin to remove them via
      `Iam.DeleteUser` (org-scoped).
- [ ] Custom roles: lift `UNIMPLEMENTED` on `CreateRole`/`UpdateRole`/`DeleteRole` when there's a real customer use case.
- [ ] Conditional bindings: re-import `google/type/expr.proto`, attach to `Member.condition`.
- [ ] Re-import `google/iam/v1/iam_policy.proto` for full `GetIamPolicy`/`SetIamPolicy` projection over `members` table — when fine-grain sharing arrives.
- [ ] `Group` cross-org? Today scoped to single org. Cross-org sharing is a future feature.
- [ ] Audit log for IAM mutations.
## Phase 7 — User identity unification + AI chat re-parent

One commit, several coupled changes. The trigger is the AI chat re-parent (paths under `organizations/{org}/users/{user}/conversations/{...}`), but it forced a deeper question — what *is* "user id"? The current chain has three identifiers (`firebase_uid` string, `firebase_identities.id` UUID, per-org `users.id` UUID); the per-org layer adds no value once we've decided memberships should ride parent-org lifecycle. Collapsing it lets the client construct paths from a Firebase token claim with zero round-trips and aligns with how every other platform (AWS, GCP, Firebase itself) does user identity.

### Schema changes

- **Drop the `users` table entirely.** No per-org user row.
- **`firebase_identities.id` becomes the universal user-uuid.** It's already a UUID, already created on first sign-in, already 1:1 with the firebase identity.
- **Repoint principal references** to `firebase_identities.id`:
  - `org_members.principal_id` (was `users.id`)
  - `space_members.principal_id` (was `users.id`)
  - `group_members.user_id` (was `users.id`)
- **`ai_conversations.creator_id`** = `firebase_identities.id` (UUID FK). The existing `created_by` TEXT column (firebase UID) stays as the audit-only field.
- **No soft-delete on membership tables** (`org_members`, `space_members`, `group_members`). They ride the parent's lifecycle. See "Why no LRO on membership ops" below.
- **Pre-prod scope**: edit `000001_init.up.sql` directly, drop+recreate dev DB.

### Firebase custom claim

- Server-side blocking func sets a `pivox_user_id` claim on the Firebase ID token, populated from `firebase_identities.id` at identity-sync time.
- Client reads the claim from the verified token; uses it to build paths.
- We already use blocking funcs (SSO bootstrap), so this is one more claim, not new infrastructure.
- Auth interceptor surfaces the claim as `pivox.UserID(ctx)` for handlers that need it.

### Resource path changes (AI chat)

- `organizations/{org}/conversations/{c}` → `organizations/{org}/users/{user}/conversations/{c}`.
- Same depth added to `messages/{m}`, `artifacts/{a}`, `artifacts/{a}/versions/{v}`.
- `{user}` is `firebase_identities.id` (UUID). Strict parse — no `me` sentinel, no special casing, matches every other user-rooted path.
- Affects `pivox/ai/v1/conversations.proto`, `messages.proto`, `artifacts.proto`, `ai_chat.proto` (RPC URL bindings + `ArtifactEnd.artifact_version`).
- `ListConversations` parent shifts to `organizations/{org}/users/{user}` (per-user listing).
- `GenerateContent` / `StreamGenerateContent` `parent` stays `organizations/{org}` (org-scoped); the `conversation` field carries the full new path when stateful.

### Handler changes (`internal/service/aichat/`)

- Parsers for the new path shape (`names.go`).
- `resolveConversation` enforces:
  1. Path's `user-uuid` matches the row's `creator_id` (defensive — surfaces NotFound on mismatch so a malformed path can't probe ownership)
  2. Caller's `pivox_user_id` claim matches the path's `user-uuid` OR caller carries `ai.conversations.readAll` / `deleteAll`
- Creator-only ops (Create, Update, Delete artifact version, Summarize, GenerateContent stateful): no audit-bypass.
- Read ops (Get conversation, List conversations, Get message, List messages, Get artifact, etc.): allow audit bypass via `*All` perm.

### Permission catalog

- New: `ai.conversations.readAll` [owner, admin] — audit/compliance read-any.
- New: `ai.conversations.deleteAll` [owner] — departed-employee cleanup.
- Keep base CRUD perms (`ai.conversations.{read,create,update,delete}`) granted to all roles — every member uses AI chat for their own conversations. The handler creator-check enforces the privacy boundary on top.
- `ai.chat.stream` extended to viewer (a viewer-role user still gets a personal AI chat experience).

### Why no LRO on membership ops

LRO + grace exists to make destructive cascades reversible. Apply only when removal is genuinely unrecoverable:

| RPC | Has LRO? | Should have LRO? | Why |
|---|---|---|---|
| `DeleteOrganization` | yes | yes | Cascade wipes all org data — assets, conversations, everything. No way back without backup. |
| `DeleteSpace` | yes | yes | Same, scoped to space. |
| `DeleteAccount` | yes | yes | Cross-org cascade + firebase identity deletion + can't be undone by re-add. |
| `Iam.DeleteUser` (org-scoped) | yes | **NO — drop in Phase 7** | Removes a tiny pointer row. User content stays (created_by / creator_id reference firebase_identities.id, which survives). Recovery = re-run `CreateMember`. |
| `Iam.DeleteGroup` | no | no | Already sync. Re-creatable, bindings re-creatable. |
| `Iam.DeleteRole` (when ships) | no | no | Re-creatable. |
| `DeleteMember` (org + space) | no | no | Already sync. |

Net change in Phase 7: `Iam.DeleteUser` becomes sync hard-delete. Drops `userLifecyclePrefix`, the soft-delete-grace queries (`SoftDeleteUserInOrg`, `CountOrgOwnersExcludingUser`, etc.), and the LRO orchestration (`runDeleteUser`).

### Native macOS client

- `OrgService` reads `pivox_user_id` from the Firebase token (via `Auth.currentUser.getIDTokenResult()`, which exposes claims).
- Path construction in AI chat callsites (`ConversationListViewModel.swift`, `AIChatContainerView.swift`, `ConversationViewModel.swift`) uses the claim's UUID directly.
- Stale-conversation guard in `AIChatContainerView` keeps working — `orgPrefix` returns the first two path segments either way.

### Test fixture sweep

- Every aichat test gets the new path shape with the per-test fixture user-uuid.
- Add `mockCallerWithClaim(uid string, identityID uuid.UUID)` test helper that wires the auth-context with the claim populated. Drops the `GetFirebaseIdentityByUID` + `GetUserMembership` chain mocks.
- `internal/permission/matrix_test.go`: viewer cantDo list updated (drop `AiConversationsCreate`; add `AiConversationsReadAll`, `AiConversationsDeleteAll`).
- `internal/permission/matrix_test.go` `TestPermissions_MigrationMatchesConstants`: new perms seeded in init migration.

### Out of scope for Phase 7

- The Phase 6 UI work for spaces / orgs / SSO config. Phase 7 is backend + macOS-shared-models only.
- Cross-org user identity flows beyond what unification gives for free.
- Audit log for IAM mutations (still future).

---

## Pre-existing test failures (address before phase 1 ships)

Surfaced during phase 1 but predate this roadmap. Confirmed broken on `HEAD~2` (i.e., before phase 1 work began). Tracking here so they don't get lost — fix before we cut a release.

### `internal/service/aichat/server_integration_test.go` — compile failure (`-tags dev` only)

The test references proto + interface shapes that have since been refactored:

- `aiv1.ClientEvent`, `aiv1.ClientEvent_Message`, `aiv1.UserMessage` — old proto message names from before the bidi-stream conversion.
- `client.Stream` — old RPC name; current shape uses `Send`-style API.
- `*fixedModel does not implement model.LanguageModel (missing method Name)` — `LanguageModel` interface gained a `Name()` method; the test stub wasn't updated.

Gated by `//go:build dev` so `go test ./...` (no tags) skips it. Surfaces only on `go test -tags dev`.

**Fix:** rewrite the test against the current `AiChat` service shape and update `fixedModel` to satisfy the current `model.LanguageModel` interface.

- [ ] Repair or rewrite `internal/service/aichat/server_integration_test.go` against current proto + `LanguageModel` interface.

### `internal/storageagent/TestConnect_FullHandshake` — runtime failure

Mock expectations not satisfied:

```
handshake: handshake: stream closed while waiting for response
Expected "ListStorageEndpointsByGateway" to have been called with: [...]
but no actual calls happened
```

The handshake completes early (or silently fails), so the post-handshake `ListStorageEndpointsByGateway` mock is never invoked. Could be timing-dependent flake or test drift from current handshake flow.

**Fix:** debug-session on the handshake stream lifecycle, then either repair the mock setup or replace the brittle stream-mocking with a real-loopback test fixture.

- [ ] Diagnose and fix `internal/storageagent/TestConnect_FullHandshake`.

---

## Locked decisions

Original (locked before phase 2):

1. **Space role inheritance.** ✅ **Locked: union with org-level.**
   Org owner is automatically space owner. Simpler reasoning, matches GCP norm.
2. **`Member` resource ID shape.** ✅ **Locked: `members/{user-id}` or `members/{group-id}` (singular typed prefix).**
   `Member.principal` is a oneof. One binding per (principal, scope) by construction.
3. **`Group` ↔ `Team` naming.** ✅ **Locked: `Group`.** Industry-standard, no collision with Space-as-team metaphor.
4. **SSO secret storage.** ✅ **Locked: KMS column-encryption for v1.** Migrate to envelope-DEK later if rotation requirements grow.
5. **`TransferOwnership`.** ✅ **Locked: dedicated atomic RPC.** Single transaction; no window of zero or two owners.

New (locked during phase 3 / phase 4 step 0):

6. **Domain verification is an LRO, not a sync RPC.** ✅ **Locked.**
   `CreateDomain` returns `google.longrunning.Operation`. `CreateDomainMetadata.Phase`
   surfaces AWAITING_DNS / VERIFIED / FAILED / EXPIRED; the verification token lives
   in the operation metadata so admins can read it immediately. Re-verification gets
   a separate `VerifyDomain` RPC if/when admins need it (deferred — YAGNI). Reasoning:
   server takes responsibility for repeated DNS checks at a backoff schedule until
   propagation succeeds or the grace window elapses; that's textbook LRO work.

7. **`auth:resolveProvider` is stdlib HTTP, not gRPC.** ✅ **Locked.**
   `POST /internal/v1/auth:resolveProvider {email} → {provider_id}` mounts on the
   existing `:8080` listener under `/internal/v1/`, sibling of `auth:exchangeToken`
   et al. No new gRPC listener (50053 idea is dead). Rate-limited per-IP by
   `ipRateLimiter` in Go (matches established pattern). Reasoning: single-purpose,
   public, unauthenticated, idempotent, GET-shaped lookup; gRPC's strengths
   (typed clients, streaming, bidi) don't apply.

8. **`Domain` is org-level, not SSO-config-scoped.** ✅ **Locked.**
   Path `organizations/{org}/domains/{domain}`. SSO is the first consumer; future
   features (email allowlist, custom invitation domains, branding) consume the
   same resource without renaming. Linkage to SsoConfig is implicit (any of the
   org's `state=VERIFIED` domains route to its `enabled=true` SsoConfig — AIP-156
   enforces one SsoConfig per org so the mapping is unambiguous).

9. **Purge worker host: in-process goroutine in `pivox-cloud`.** ✅ **Locked for v1; transferable design.**
   Workers live in `internal/workers/` with dependencies injected and no reach into
   HTTP/gRPC server internals. Postgres advisory lock for multi-replica safety.
   `cmd/pivox-purge-worker/` binary becomes a one-line `main` calling the same
   package when rolling-deploy correctness becomes load-bearing.

10. **DNS reconciler is the LRO, not a separate worker.** ✅ **Locked.**
    The `CreateDomain` LRO drives DNS polling end-to-end; there's no separate
    "reconciler" service. Phase 5/6 originally had a reconciler line — folded
    into the LRO. v1 ships with a stub DNS resolver that returns "TXT matches"
    unconditionally so end-to-end domain claiming works in dev without real DNS;
    real resolver wires up before any external admin uses SSO.

11. **`UpdateSsoConfig` is sync, not LRO.** ✅ **Locked.**
    Validate → KMS-encrypt → DB write → Firebase Admin SDK `*ProviderConfig`
    call. Sub-second median; no Phase enum to surface. If the Firebase call
    fails, return error to caller; drift reconciliation is a separate concern,
    not an Operation tied to this Update call. (Inconsistent with
    `UpdateOrganization`-as-LRO; the latter is the one that should be downgraded
    to sync, not this one upgraded — out of scope for phase 4.)

12. **IAM service redistribution: scope-divergent ops live on the scope-owning service.** ✅ **Locked.**
    Triggered by surfacing the placement of `TransferOwnership`, then
    generalized: the original "single `Iam` service hosts everything"
    framing forced multi-parent dispatch into a service that didn't own
    the resources it was gating, with extra runtime checks on every
    handler ("if path is space-scoped, do X; otherwise do Y"). The split:
    `Member` CRUD, `TransferOwnership`, and `TestIamPermissions` move to
    `Organizations` and `Spaces` services per scope; `User` reads,
    `DeleteUser`, `Role` reads, `Group` CRUD, and `ListPermissions` stay
    on `Iam` (uniformly org-scoped or global, no scope divergence).
    URL patterns at the new homes constrain scope by construction —
    invalid states (e.g., space-scoped TransferOwnership) become
    unrepresentable rather than runtime-rejected.

13. **`TransferOwnership` lives on `Organizations` with a typed response.** ✅ **Locked.**
    Path: `POST /v1/{name=organizations/*}:transferOwnership`. Request
    is `{name=organizations/{org}, new_owner=organizations/{org}/users/{user}}`;
    response is a custom `TransferOwnershipResponse {new_owner, previous_owner}`
    (option C from the response-type fork — `Empty` flagged by api-linter,
    `Member` and `Organization` returns less informative for the
    caller's UI update path).

14. **`TestIamPermissions` is two operations sharing a wire shape, not one.** ✅ **Locked.**
    Org-scope variant runs one query (`GetEffectiveOrgRoles`); space-scope
    variant unions direct space bindings with parent-org inheritance
    (three queries). Hosted on both `Organizations.TestIamPermissions`
    and `Spaces.TestIamPermissions` — splitting surfaces the divergent
    semantics in the API rather than hiding them behind a runtime
    dispatch on the `resource` field shape.

15. **`Member` request/response messages stay shared in `iam/v1`.** ✅ **Locked.**
    `Organizations.GetMember` and `Spaces.GetMember` both reference
    `pivox.iam.v1.GetMemberRequest`/`Member`. api-linter accepts
    cross-package message reuse without complaint — confirmed by running
    `make api-lint` after the split. No need for a separate
    `pivox/types/v1` package.

16. **`apierr.HandleResourceError` keys on `pgconn.PgError.Code` + named SQLSTATE constants, not string-matching driver messages.** ✅ **Locked.**
    The previous `strings.Contains(err.Error(), "duplicate key")` pattern
    is fragile across pgx versions and locales. New constants in
    `internal/apierr/pgstate.go` cover the common SQL standard codes
    (`PgUniqueViolation` 23505, `PgForeignKeyViolation` 23503,
    `PgNotNullViolation` 23502, `PgCheckViolation` 23514,
    `PgSerializationFailure` 40001). Today only `PgUniqueViolation`
    is consumed; the others document the namespace for future handlers.

17. **`DeleteSpace` gains `force` field for parity with `DeleteOrganization`.** ✅ **Locked.**
    `DeleteSpaceRequest.force` (bool, optional) — when true, bypasses
    the 30-day grace and synchronously cascades child data + frees
    the slug. Same semantics + same Phase-enum extension pattern that
    DeleteOrganization uses.

## Tracked risks (resurface when relevant)

- **Founder owner-binding test downgraded in step 1.** `TestUnit_CreateOrganization_CreatesOwnerMembership`
  in `internal/service/organizations/server_unit_test.go` was downgraded to
  assert only the user-row creation (3 args), not the owner-role binding,
  because step 1 dropped `users.role` and step 3+ adds the org_members row.
  **Surface again at phase 4 step 3:** restore the owner-binding tx assertion
  (org_members row with role_id pointing to the system 'owner' role) before
  shipping that handler change.

- **"Race acceptable for v1" is not my call to make on IAM-layer code.**
  During step 3b3 I drafted UpdateMember/DeleteMember with sequential
  check-then-mutate on the ≥1-owner boundary, justified as "Pivox is
  solo-dev pre-prod, race probability ≈ 0." The user pushed back: that's
  a security/correctness decision, not a momentum tradeoff. Locked
  decision afterward: tx-wrapped boundary checks for ≥1-owner gates,
  consistent with the existing organizations-service pattern. **General
  rule going forward:** semantic risks (correctness, security, atomicity)
  must be surfaced before code lands; "v1 acceptable" reasoning should
  flag user-decision territory, not silently ship.

- **Apply "make invalid states unrepresentable" uniformly when forking.**
  The TransferOwnership move (sub-decision #13) was prompted by the user
  pointing out wire-level scope enforcement beats runtime guards. I
  initially under-applied that logic and only the user's follow-up on
  TestIamPermissions (sub-decision #14) generalized it. **General rule
  going forward:** when a fork lets URL patterns / type narrowing
  enforce a constraint, prefer that over runtime validation, even when
  the runtime validation looks "fine."
