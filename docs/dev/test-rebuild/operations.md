# `internal/service/operations` — test rebuild spec

The Operations service is the public AIP-151 LRO surface
(GetOperation, ListOperations, WaitOperation, CancelOperation,
DeleteOperation). It's a thin wrapper over `lro.Manager`; the
heavy lifting (atomic enqueue, work execution) is tested via the
LRO-using services (organizations, etc.).

## Existing coverage

None directly — `lro.Manager` is unit-tested, and the LRO surface
is exercised end-to-end every time a service test calls a handler
that returns an Operation.

## What's worth a focused test

- [ ] `GetOperation` returns the row by name
- [ ] `GetOperation`: empty name → `InvalidArgument`
- [ ] `GetOperation`: malformed name → `InvalidArgument`
- [ ] `ListOperations`: returns operations the caller has access to.
  This is the access-control story for LROs that needs to be
  pinned — today operations have an `org_id` column and the
  filter respects it; verify the caller-without-org-membership
  doesn't see operations from orgs they're not in.
- [ ] `ListOperations`: pagination round-trip + default page size
- [ ] `WaitOperation`: returns immediately when operation is
  already done
- [ ] `WaitOperation`: returns when operation completes mid-call
  (use the React-driven LRO from organizations to set this up)
- [ ] `WaitOperation`: respects context deadline — returns
  whatever-is-current when the deadline fires, doesn't hang
- [ ] `CancelOperation`: in-flight org-scoped LRO transitions to
  cancelled state, observed via subsequent GetOperation
- [ ] `CancelOperation`: empty name → `InvalidArgument`
- [ ] `DeleteOperation`: removes terminal operation
- [ ] `DeleteOperation`: refuses to delete a still-running operation
- [ ] `DeleteOperation`: empty name → `InvalidArgument`

## Drop list

- ~~Per-method "InvalidArgument when X is empty"~~ matrix as separate
  tests — protovalidate enforces required fields at the boundary;
  one happy + one explicitly-bad per method covers the wire contract.

## Shape of the rewrite

- `server_e2e_test.go` — **NEW** file using grpcharness with
  WithOrganizationsServer + an Operations server registration.
  Covers the behaviors above end-to-end, leaning on
  CreateOrganization / DeleteOrganization to produce real
  operations rows.
