# IAM, Lifecycle, and Spaces Roadmap

**Status**: in progress
**Owner**: Ashkan
**Started**: 2026-04-26

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

### IAM model (single `Iam` service)

| Resource | Pattern | Notes |
|---|---|---|
| `User` | `organizations/{org}/users/{user}` | Per-org identity. |
| `Group` | `organizations/{org}/groups/{group}` | Named user collection. |
| `Role` | `organizations/{org}/roles/{role}` | v1 read-only. 4 system roles: `owner`, `admin`, `editor`, `viewer`. Custom roles deferred. |
| `Permission` | `permissions/{permission}` | Global, read-only catalog. Code-defined. |
| `Member` | `organizations/{org}/members/{member}` *and* `organizations/{org}/spaces/{space}/members/{member}` | One resource, multi-parent. `principal` = `users/*` or `groups/*`; `role` = ref to a `Role`. The single source of truth for "who has what role where." |

The `Iam` service exposes:
- Get/List/Create/Update/Delete on Members + Groups.
- Get/List on Roles + Permissions (read-only in v1).
- Get/List on Users; `DeleteUser` LRO.
- `TestIamPermissions` for UI gating.

**Permission resolution** at runtime (interceptor): union of direct Member + Group-derived bindings. Space scope inherits from org (decision to lock — see open decisions).

### Project → Space rename

`Project` is dev-coded and doesn't fit broadcast-media use cases (a sub-unit can be a brand, a show, or an event — News, Dateline, Elections). Renamed to **`Space`** (chosen over `Workspace` for being shorter and less corporate).

Resource paths: `organizations/{org}/spaces/{space}/...`. Service: `Spaces`.

### Lifecycle

**`DeleteOrganization` LRO** — soft delete (`delete_time` set, `purge_time = delete_time + 30d`), then purge. Triggered by any owner with slug-typed confirmation. RPC boundary gates org-scoped calls; child resources untouched (no cascade soft-delete). FAILED_PRECONDITION on active playout / pending LROs / outstanding billing.

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

### `syncAccount` → `syncFirebaseIdentity` endpoint rename

- [ ] Add new handler at `/internal/v1/syncFirebaseIdentity`. Old handler keeps working (dual-route).
- [ ] Update Firebase Function source to call new URL.
- [ ] Deploy via root make target.
- [ ] Verify with a registration roundtrip.
- [ ] Delete old route and old handler.

### IAM proto trim (set up phase 2 cleanly)

- [ ] **Port `TestIamPermissions` request/response messages out of `iam_policy.proto`** into a location that survives (likely the new consolidated `iam.proto`, or stage as a temporary holding file).
- [ ] Delete `pivox/iam/v1/iam_policy.proto` (after the port).
- [ ] Delete `pivox/iam/v1/policy.proto` (`Policy`/`Binding`/conditional expressions).
- [ ] Trim `pivox/iam/v1/roles.proto`: drop `CreateRole`/`UpdateRole`/`DeleteRole` and `AddRoleMembers`/`RemoveRoleMembers`/`ListRoleMembers`. Keep `Role` message + `GetRole`/`ListRoles`/`ListPermissions`+`Permission`. (Can fold into phase 2 file consolidation.)
- [ ] Drop iam_policy + policy imports in `projects.proto` (and any other consumers).
- [ ] Remove role-CRUD seed code if any exists.

### Phase 1 exit criteria

- [ ] `make lint-proto && make api-lint && make proto-format && make proto-generate && make tidy` clean.
- [ ] `make build` clean.
- [ ] `xcodebuild test -scheme PivoxTests` clean (macOS Swift unit tests).
- [ ] `go test ./...` clean.
- [ ] Registration → org onboarding → AIChat manual smoke clean.
- [ ] No `tenant`, `custom_domain`, `account` (in identity sense), `Policy`/`Binding`/`SetIamPolicy`/`AddRoleMembers` references remain in code (excluding generated files for tools we don't own).

---

## Phase 1.5 — Rename `Project` → `Space`

Mechanical sweep across **9 protos, 1 migration, ~10+ Go service/convert/test dirs, and all generated code**. Pre-prod, drop+recreate the dev DB.

### Protos

- [ ] `pivox/api/v1/projects.proto` → `pivox/api/v1/spaces.proto`. Service `Projects` → `Spaces`. All messages.
- [ ] `pivox/api/v1/dashboards.proto` — patterns + URL paths.
- [ ] `pivox/api/v1/tag_keys.proto` — patterns + URL paths (multi-parent stays).
- [ ] `pivox/api/v1/tag_values.proto` — patterns + URL paths.
- [ ] `pivox/api/v1/tag_bindings.proto` — patterns + URL paths.
- [ ] `pivox/assets/v1/asset.proto` — patterns + URL paths (Asset, AssetVersion).
- [ ] `pivox/assets/v1/request.proto` — patterns + URL paths (AssetRequest, LineItem).
- [ ] `pivox/ai/v1/artifacts.proto` — comment-only references update.
- [ ] `pivox/agent/v1/agent.proto` — comment-only references in denied-pattern docs.
- [ ] `make lint-proto && make api-lint && make proto-format && make proto-generate && make tidy`.

### DB

- [ ] Rename `projects` → `spaces`. `project_role` → `space_role`. `project_member_type` → (likely deleted in phase 4 in favor of unified `members` table — leave intact for phase 1.5).
- [ ] Rename `assets.project_id` → `space_id`.
- [ ] Rename `asset_requests.project_id` → `space_id`.
- [ ] Rename indexes: `idx_projects_org`, `idx_project_members_member`, `idx_assets_project*`, `idx_asset_requests_project*`.
- [ ] Update seeded permissions: `projects.create` → `spaces.create`, etc.
- [ ] Drop+recreate dev DB.

### sqlc + Go

- [ ] Rename queries in `internal/db/queries/asset_requests.sql`, `internal/db/queries/assets.sql`. Regenerate via `make sqlc`.
- [ ] Rename `internal/service/projects/` → `internal/service/spaces/`.
- [ ] Update all imports + references in `internal/iam`, `internal/convert`, `internal/resource`, `internal/filter`, `internal/lro`, `internal/apierr`, `internal/server`.
- [ ] Update test fixtures and pattern strings.
- [ ] **Verify `internal/crypto/encryptor_gcp.go`'s `projects/...` is GCP-KMS (Google Cloud project), not Pivox project. Leave alone.**

### Phase 1.5 exit criteria

- [ ] `make build` clean.
- [ ] `go test ./...` clean.
- [ ] Native macOS app rebuilds with regenerated PivoxModels (`xcodebuild -scheme Pivox`).
- [ ] No `Project` / `projects` references in pivox-owned code (except GCP-KMS path).

---

## Phase 2 — IAM v1 proto

Land the new `Iam` service shape. Proto-only; server impl follows in phase 4.

### File layout

- [ ] Decide: single `pivox/iam/v1/iam.proto` for the service + all messages, OR keep `users.proto`/`groups.proto`/`roles.proto`/`permissions.proto`/`members.proto` split with one `Iam` service spanning files. Recommend split for readability.
- [ ] Create / update files.

### Resources

- [ ] `Member` message with multi-parent pattern: `organizations/{org}/members/{member}` + `organizations/{org}/spaces/{space}/members/{member}`. Fields: `principal` (string ref to `users/*` or `groups/*`), `role` (ref to `organizations/*/roles/*`), `create_time`, `etag`.
- [ ] `Group` message + per-group user list (or `GroupMember` sub-resource — decide).
- [ ] `Role` message: read-only fields, `system: true` for v1.
- [ ] `Permission` message: global pattern `permissions/{permission}`.
- [ ] `User` message updated: drop in-line role field if any (role lives on Member).

### Service RPCs

- [ ] Members: `GetMember`, `ListMembers`, `CreateMember`, `UpdateMember`, `DeleteMember`.
- [ ] Groups: `GetGroup`, `ListGroups`, `CreateGroup`, `UpdateGroup`, `DeleteGroup`, group-membership RPCs.
- [ ] Roles: `GetRole`, `ListRoles`. (`Create/Update/DeleteRole` stubbed to UNIMPLEMENTED in phase 4.)
- [ ] Permissions: `GetPermission`, `ListPermissions`.
- [ ] Users: `GetUser`, `ListUsers`, `CreateUser` (or fold into Member.Create), `DeleteUser` returning `google.longrunning.Operation`.
- [ ] `TestIamPermissions` (ported from old iam_policy.proto).

### Phase 2 exit criteria

- [ ] Proto pipeline clean.
- [ ] Generated Go + Swift compile.
- [ ] No callers yet — server returns UNIMPLEMENTED for all new RPCs.

---

## Phase 3 — Lifecycle + SSO proto

### Org lifecycle

- [ ] `DeleteOrganization` returns `google.longrunning.Operation` (was synchronous if applicable).
- [ ] `UndeleteOrganization` RPC.
- [ ] `Organization.delete_time` / `purge_time` fields.
- [ ] `Organization.state` enum if useful (`ACTIVE`, `DELETE_REQUESTED`).

### User lifecycle

- [ ] `DeleteUser` LRO (already added in phase 2 — cross-link the spec here).
- [ ] Sole-owner-blocking error code documented (`FAILED_PRECONDITION` with structured detail listing affected orgs).
- [ ] `TransferOwnership` design: dedicated RPC on `Member` (atomic two-row swap) **vs** two `UpdateMember` calls (race window). Recommend dedicated.

### SSO

- [ ] `SsoConfig` singleton message at `organizations/{org}/ssoConfig` per AIP-156.
- [ ] Fields: `firebase_provider_id`, `kind` (OIDC|SAML), `oidc_config` (issuer, client_id, encrypted client_secret, redirect URIs), `saml_config` (metadata XML, attribute mappings), `verified_domains[]`, `verification_state`, `enabled`.
- [ ] `Get`, `Update` only.
- [ ] `Domains.Verify` flow if needed (DNS TXT). Decide whether to add now or defer.
- [ ] `Sso.Resolve` RPC: `{email}` → `{provider_id}`. Rate-limited.
- [ ] Decide secret-storage strategy: KMS column-encryption vs `Secret` table with wrapped DEKs.

### Phase 3 exit criteria

- [ ] Proto pipeline clean.
- [ ] Spec doc added (this doc) updated with finalized message shapes.

---

## Phase 4 — Server impl for phases 2–3

The big build. Strict TDD: write the unit/integration test first, watch it fail, then implement.

### Schema

- [ ] `members` table: scope (org or space), principal_kind (user|group), principal_id, role_id. Unique on (scope, principal). FK cascades on parent delete.
- [ ] `groups` table + `group_members` table.
- [ ] `roles` table seeded with 4 system roles per org at create time.
- [ ] `permissions` static — code-defined, exposed via `ListPermissions`.
- [ ] `sso_configs` table (1 per org, optional). Columns include encrypted secret material.
- [ ] Soft-delete: `organizations.delete_time` / `purge_time`. (Same on `spaces` and `users` — for consistency.)
- [ ] Drop+recreate dev DB.

### Permission interceptor

- [ ] Test fixtures: orgs with various member/group/role bindings.
- [ ] Resolver: caller → effective role at target scope, considering inheritance.
- [ ] Static `(role, permission) → allow` map.
- [ ] Wire into gRPC server interceptor chain.
- [ ] `TestIamPermissions` reuses resolver.

### Org lifecycle

- [ ] `DeleteOrganization` LRO orchestrator. State machine. Cancellation of in-flight LROs.
- [ ] Soft-delete gate at RPC boundary: org-scoped reads succeed with metadata; mutations return `FAILED_PRECONDITION`.
- [ ] `UndeleteOrganization` clears `delete_time`.
- [ ] Purge worker: scheduled job hard-deletes orgs past `purge_time`. FK cascade does the rest.
- [ ] Slug freed at purge time, not at soft-delete.

### User lifecycle

- [ ] `DeleteUser` LRO orchestrator.
- [ ] Sole-owner check: scan members for `role = owner`, group by org, count. If any org has only this user as owner, return `FAILED_PRECONDITION` with structured detail.
- [ ] Cascade memberships → owned data → `firebase.Auth.DeleteUser(uid)`.
- [ ] `onUserDeleted` webhook handler: idempotent — if user already gone in our DB, no-op.
- [ ] `TransferOwnership` atomic implementation.

### SSO

- [ ] `SsoConfig` Get/Update handlers.
- [ ] On Update: write DB, then call Firebase Admin SDK `CreateOIDCProviderConfig` / `CreateSAMLProviderConfig` / `Update*ProviderConfig`.
- [ ] Reconciliation job (or test hook) detects drift.
- [ ] Encrypted-secret storage wired via chosen strategy (KMS column or Secret table).
- [ ] `Sso.Resolve` handler with rate limiting.
- [ ] Domain verification flow (DNS TXT) if scoped in.

### Phase 4 exit criteria

- [ ] All RPCs land with full integration test coverage.
- [ ] Permission interceptor tests cover org+space, user+group, inheritance, deny paths.
- [ ] Soft-delete + revive end-to-end test.
- [ ] User-delete blocking + unblock-via-transfer + unblock-via-org-delete tests.
- [ ] SSO Update → Firebase Admin SDK side-effect test (with mock).

---

## Phase 5 — Spaces impl

Wire existing (post-rename) `Spaces` RPCs to schema. `Spaces` proto largely already defined.

- [ ] Update tests for existing `Projects`-now-`Spaces` server unit + integration tests.
- [ ] `CreateSpace`: seed default space-level Member binding (creator → owner).
- [ ] `DeleteSpace`: same soft-delete + purge pattern as orgs.
- [ ] Inheritance from org level for permission resolution (decision-locked above).
- [ ] Asset / AssetRequest / Tag* / Dashboard handlers updated for `space_id` column.

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

- [ ] Custom roles: lift `UNIMPLEMENTED` on `CreateRole`/`UpdateRole`/`DeleteRole` when there's a real customer use case.
- [ ] Conditional bindings: re-import `google/type/expr.proto`, attach to `Member.condition`.
- [ ] Re-import `google/iam/v1/iam_policy.proto` for full `GetIamPolicy`/`SetIamPolicy` projection over `members` table — when fine-grain sharing arrives.
- [ ] `Group` cross-org? Today scoped to single org. Cross-org sharing is a future feature.
- [ ] Audit log for IAM mutations.

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

## Open decisions (lock before phase 2)

1. **Space role inheritance.** Does space-level `effective_role(user)` union with org-level `effective_role(user)` (GCP-style), or is space membership fully explicit?
   - Recommend: union (org owner is automatically space owner). Simpler reasoning, matches GCP norm.

2. **`Member` resource ID shape.** `members/{principal_id}` (one binding per principal per scope, deduplication-by-construction) vs `members/{generated_id}` with `principal` as a separate field (more flexible but allows weird states).
   - Recommend: `members/{principal_id}`. Forbids duplicates by construction. Lookup-by-principal is the dominant query.

3. **`Group` ↔ `Team` naming.** Keep `Group`? User has called them "groups/teams" — pick one.
   - Recommend: `Group`. Industry-standard, no collision with Space-as-team metaphor.

4. **SSO secret storage.** KMS column-encryption (simpler) vs `Secret` table with KMS-wrapped DEKs (envelope, more flexible).
   - Recommend: KMS column-encryption for v1. Migrate to envelope later if rotation requirements grow.

5. **`TransferOwnership`** — dedicated RPC vs two `UpdateMember` calls.
   - Recommend: dedicated. Atomicity matters (no window of zero or two owners).
