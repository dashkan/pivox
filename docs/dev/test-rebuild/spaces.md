# `internal/service/spaces` — test rebuild spec

## Existing grpcharness coverage (kept)

- `lifecycle_e2e_test.go` — CreateSpace founder-binding, soft-delete +
  revive, SpacePurgeWorker cascade

## CreateSpace

- [x] Creates space + founder owner-member binding atomically
- [ ] Auto-generates slug when `SpaceId` is empty
- [ ] Rejects malformed `parent` (no `organizations/X` prefix) →
  `InvalidArgument`
- [ ] Permission gate: `spaces.create` (admin+ on the org)
- [ ] Refuses on a DELETE_REQUESTED parent org → `FailedPrecondition`
  (soft-delete-gate matrix)

## GetSpace / UpdateSpace / DeleteSpace / UndeleteSpace

- [x] DeleteSpace + UndeleteSpace happy paths in `lifecycle_e2e_test.go`
- [ ] GetSpace: NotFound for unknown slug
- [ ] GetSpace: slug mismatch in resource name → `InvalidArgument`
- [ ] UpdateSpace: field-mask honored (display_name, labels, annotations)
- [ ] UpdateSpace: name-segment mismatch in body vs URL → `InvalidArgument`
- [ ] DeleteSpace: non-ACTIVE state → `FailedPrecondition`
- [ ] UndeleteSpace: non-DELETE_REQUESTED state → `FailedPrecondition`
- [ ] UndeleteSpace: after purge_time elapsed → `FailedPrecondition`
  via worker terminal-failure path

## ListSpaces

- [ ] Returns spaces in the parent org only
- [ ] Pagination round-trip
- [ ] `show_deleted=true` includes DELETE_REQUESTED rows
- [ ] Permission filtering: caller sees only spaces they have access to
  (org-admins see all; space-bound users see only their bound spaces)

## Members (space scope)

- [ ] CreateMember: space-scoped binding succeeds for org-admin caller
- [ ] CreateMember: caller without `members.create` rejected
- [ ] GetMember: space-scoped name resolves
- [ ] GetMember: group-principal name shape (was
  `TestSpaces_GetMember_GroupPrincipal`)
- [ ] ListMembers: returns direct space bindings only (org-level
  bindings NOT auto-included). Was
  `TestSpaces_ListMembers_DirectBindingsOnly` — important behavior;
  org admins still need to be queryable separately.
- [ ] DeleteMember: removes binding, last-owner guard at space scope
  (or document that space scope has no last-owner constraint, which
  it currently doesn't)

## Drop list

- ~~`TestUnit_*_InvalidName`~~ matrix — invalid resource names are
  caught by protovalidate at the interceptor; one happy-path + one
  malformed test per handler is plenty.
- ~~`TestUnit_*_SlugMismatch`~~ matrix — same; one focused test per
  handler covers it without a 1:1 port.
- ~~Pure DB-error injection~~ tests (`*_DBError`) — call-shape
  garbage. Real DB failure handling is exercised by integration
  tests' organic error paths.

## Shape of the rewrite

- `lifecycle_e2e_test.go` — extend with field-mask + state-transition
  matrix
- `members_e2e_test.go` — **NEW** file mirroring orgs members_e2e shape
- `list_e2e_test.go` — **NEW** file for ListSpaces variations
