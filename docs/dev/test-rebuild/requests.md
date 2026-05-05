# `internal/service/requests` — test rebuild spec

## Existing grpcharness coverage (kept)

- `server_integration_test.go` — Create → Submit → Assign → Deliver
  → Approve happy path; Reject workflow; Cancel workflow; List
  with pagination + show_deleted

## CreateRequest

- [x] Happy path with line items
- [x] No-line-items succeeds (covered organically)
- [ ] Permission gate: `requests.create` on parent space
- [ ] Cross-org / unknown parent → NotFound or InvalidArgument
- [ ] Rolls back atomically when CreateLineItem fails mid-fan-out
  (was `TestCreateRequest_CreateLineItemError` — important
  invariant; covered properly by inserting an asset that violates
  a constraint and asserting the request row didn't land)

## State machine — each transition explicit

The deleted unit tests exhaustively covered every state-transition
branch. The state machine is load-bearing (workflow correctness +
permission scope per state); each transition deserves coverage:

- [x] DRAFT → OPEN (Submit) happy path + invalid-state rejection
- [x] OPEN → IN_PROGRESS (Assign) + IN_PROGRESS → IN_PROGRESS
  (re-assign)
- [ ] OPEN → IN_PROGRESS (Claim — caller assigns themselves)
- [x] IN_PROGRESS → DELIVERED
- [x] DELIVERED → APPROVED, DELIVERED → REJECTED
- [ ] APPROVED → IN_PROGRESS (RequestRevision)
- [x] DRAFT/OPEN/IN_PROGRESS → CANCELLED
- [ ] APPROVED → CANCELLED rejected with `FailedPrecondition`
- [ ] CANCELLED → CANCELLED rejected (idempotency / state guard)

One table-driven test covering all valid + invalid transitions is
fine; doesn't need 30 separate test functions.

## UpdateRequest

- [ ] Field-mask: due_time, annotations, display_name, description
  independently
- [ ] No-mask: all writable fields
- [ ] Update on a deleted/cancelled request: behavior decision
  needed (today probably succeeds; might want FailedPrecondition)

## ListRequests

- [x] Default + pagination + show_deleted
- [ ] Filter by state (the proto declares this; verify it works)
- [ ] Filter by assignee (same)

## Drop list

- ~~All `*_DBError` tests~~ — mock theater
- ~~All `*_InvalidName` tests~~ — protovalidate covers
- ~~`TestResolveSpace_SpaceNotFound`~~ — internal helper, covered
  organically when an integration test passes a bad parent

## Shape of the rewrite

- `server_integration_test.go` — extend with one table-driven
  state-machine test, field-mask matrix, filter coverage. Aim
  for ~5 added test functions, not 30.
