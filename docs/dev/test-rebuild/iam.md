# `internal/service/iam` — test rebuild spec

## Existing grpcharness coverage (kept)

- `lifecycle_e2e_test.go` — partial DeleteUser / DeleteAccount
  end-to-end coverage

## DeleteUser (org-scope)

- [ ] Removes a user's bindings from one org cleanly (org-members
  + space-members within that org)
- [ ] Bindings in *other orgs* are untouched (boundary check —
  load-bearing, was `TestDeleteUser_FullCascade`)
- [ ] Last-owner guard: refuses to remove the only org-owner →
  `FailedPrecondition`
- [ ] Multi-owner case: removal allowed
- [ ] `me` target rejected → use DeleteAccount instead
- [ ] Org-slug mismatch in path → `InvalidArgument`
- [ ] Permission gate: caller needs `users.delete` (admin+)

## DeleteAccount (self-only)

- [ ] Caller-driven account deletion is the LRO orchestrator; pin
  the cascade end-to-end:
  - Caller's identities row hard-deleted (or soft-deleted —
    decide based on current SQL)
  - All org_members/space_members rows for caller hard-deleted
  - Firebase identity revoked (assert via `WithAuth(mockAuth)`
    that `DeleteUser` was called)
  - All API keys created by caller deleted
  - Operations created by caller's `created_by` either deleted or
    re-attributed (verify the SQL behavior)
- [ ] Sole-owner block: caller is the only owner of an org →
  refuses with `FailedPrecondition` (must transfer ownership or
  delete the org first)
- [ ] Non-`me` target name → `InvalidArgument`
- [ ] Caller-resolution failure surfaces as `Unauthenticated`
- [ ] Auth (Firebase) failure mid-LRO → operation marked failed,
  identities row not committed (atomicity guard — this was the
  intent of `TestRunDeleteAccount_AuthFailureSurfaces`)

## Roles + Permissions reads

- [ ] `ListPermissions` returns the full catalog
- [ ] `GetRole` happy path (system role on an org)
- [ ] `GetRole`: invalid path → `InvalidArgument`
- [ ] `GetRole`: org NotFound → `NotFound`
- [ ] `GetRole`: role NotFound on existing org → `NotFound`
- [ ] `ListRoles` returns the four system roles per org

## Drop list

- ~~`TestDeleteAccount_FailsLoudOnNilDeps`~~ — constructor-panic
  behavior, not service behavior. Already enforced by the Config
  pattern.
- ~~`TestRunDeleteAccount_AlreadyGoneSurfacesInternal`~~ — mock
  theater (DB returns "no rows" mid-LRO, handler returns Internal).
  Real behavior either succeeds or surfaces NotFound; one
  integration test covers it organically.

## Shape of the rewrite

- `lifecycle_e2e_test.go` — extend with the missing DeleteUser /
  DeleteAccount cases, the sole-owner-block, the auth-failure
  atomicity case
- `roles_e2e_test.go` — **NEW** file for the read surface (small)
