# `internal/service/organizations` — test rebuild spec

Behaviors the deleted `MockQuerier` tests *intended* to verify, distilled
into a checklist for the grpcharness rewrite. Skip the call-shape
assertions that were pure mock theater. Mark each behavior:

- `[x]` already covered by an existing grpcharness file (don't duplicate)
- `[ ]` needs a new test in the rewrite
- `~~strike~~` deliberately dropped — the original test was call-shape
  garbage with no behavioral signal

The rewrite goes through `grpcharness.New(t, WithOrganizationsServer(), …)`
or extends the existing `*_e2e_test.go` files. No new MockQuerier code.

## Existing grpcharness coverage (kept as-is)

- `domains_e2e_test.go` — CreateDomain happy/expire/duplicate
- `lifecycle_e2e_test.go` — full soft-delete + revive cycle
- `lifecycle_undelete_river_e2e_test.go` — River-driven undelete
- `members_e2e_test.go` — member CRUD, last-owner guard, pagination,
  cross-org rejection, FK-race NotFound
- `server_integration_test.go` — CreateOrganization happy + duplicate
  rejection

## CreateOrganization

- [x] Creates org row with founder slug
- [x] Rejects duplicate slug with `AlreadyExists`
- ~~Auto-generates slug when `OrganizationId` is empty~~ —
  unreachable through the public RPC: protovalidate rejects empty
  organization_id with the AIP-122 regex. Dead code removed
  (#77).
- [x] Seeds owner-binding atomically with org row
  (`TestIntegration_CreateOrganization_SeedsFounderBinding`)
- [x] Seeds the four system roles (owner/admin/editor/viewer)
  (`TestIntegration_CreateOrganization_SeedsSystemRoles`)
- [x] Permission gate: handler requires authenticated caller (the
  interceptor chain enforces this; bypassing it produced the
  `MustPivoxUserID` panic that started this whole sweep)
- ~~"BeginTransactionError"~~ — pgx tx-error injection is the kind of
  failure-mode test that belongs in `internal/db/RunInTx` package
  tests, not service-handler tests
- ~~"CommitFailure"~~ — same

## GetOrganization

- [x] Happy path returns the row (`server_integration_test.go`)
- [x] `NotFound`/`PermissionDenied` for unknown slug
  (`TestIntegration_GetOrganization_NotFound`)
- [ ] Slug-mismatch in resource name returns `InvalidArgument`
  (was `TestUnit_GetOrganization_SlugMismatch`)
- [ ] Malformed name (`organizations/`, no slug) returns
  `InvalidArgument`

## ListOrganizations

- [x] Returns only orgs the caller is a member of
  (`TestIntegration_ListOrganizations_OnlyCallerOrgs`)
- [x] Caller with no orgs returns empty list (not an error)
  (`TestIntegration_ListOrganizations_EmptyForUnaffiliatedCaller`)
- [ ] Unauthenticated caller rejected (covered by interceptor; one
  test confirming the surface)
- ~~"PaginationFieldsIgnored"~~ — old test asserted that
  `ListOrganizations` didn't honor PageSize/PageToken. Either the
  proto declares pagination (and we should support it) or it doesn't
  (and the test is moot). Decide first; test follows.
- ~~"IdentityLookupError"~~ — DB error injection at this layer is mock
  theater. Real coverage comes from "down DB → handler returns
  `Internal`" via pgxmock or from chaos testing, not here.

## DeleteOrganization (soft path)

- [x] Soft-delete transitions row to DELETE_REQUESTED, sets
  delete/purge times, bumps etag (`lifecycle_e2e_test.go`)
- [x] Re-soft-delete on DELETE_REQUESTED row → FailedPrecondition
- [x] Etag mismatch → FailedPrecondition
- [ ] Cancels in-flight org-scoped LROs (CANCELLING_OPERATIONS phase).
  Original test mocked `CancelRunningOpsForOrg`; the real test
  should enqueue a long-running org-scoped LRO via NewLro, fire
  DeleteOrganization, observe the LRO transitions to cancelled
- [ ] Permission gate: only `organizations.delete` (owner) — admin/
  editor/viewer all rejected. Cover via the soft-delete-gate
  matrix already partly tested in permission_e2e_test.go but
  worth a focused assertion here

## DeleteOrganization (force path)

- [x] Force=true requires non-empty etag → FailedPrecondition
  (`TestE2E_DeleteUndelete_EtagGuards/force_without_etag_rejected`)
- [ ] Force=true purges row + cascades children. Seed an org with a
  space + member + api key, force-delete, assert all gone via
  GetOrganization NotFound + ListSpaces empty
- [x] Force=true with stale etag (drift between handler validation
  and LRO worker firing) → FailedPrecondition
  (`TestE2E_DeleteUndelete_EtagGuards/force_with_stale_etag_rejected`)
- [x] Force=true purges row + cascades children + frees slug
  (`TestE2E_DeleteOrganization_ForceCascadesChildren`)

## UndeleteOrganization

- [x] Happy path restores ACTIVE state via River worker
  (`lifecycle_undelete_river_e2e_test.go`)
- [x] On non-DELETE_REQUESTED row → FailedPrecondition
  (`lifecycle_e2e_test.go` Step 6)
- [x] Etag mismatch → FailedPrecondition
  (`TestE2E_DeleteUndelete_EtagGuards/undelete_with_mismatched_etag_rejected`)
- [x] After purge_time has elapsed → worker fails the operation
  with FailedPrecondition
  (`TestE2E_UndeleteOrganization_AfterPurgeTimeFails`)

## Soft-delete gate (cross-cutting)

The "soft-delete gate" is the rule that DELETE_REQUESTED orgs only
allow reads + UndeleteOrganization, not arbitrary mutations. The
deleted `lifecycle_test.go` had three tests for this; coverage now
lives in:

- [x] Active org passes any permission (covered by every other
  test — every test starts with an ACTIVE org and exercises
  mutations)
- [x] Deleted org allows reads (Step 3 of `lifecycle_e2e_test.go`)
- [x] Deleted org rejects mutations — table-driven matrix
  (CreateDomain + CreateMember) in
  `TestE2E_DeleteRequestedOrg_RejectsMutations`

## Domains

CRUD covered by `domains_e2e_test.go`. The deleted tests covered:

- [x] CreateDomain rejects empty domain string (caught by
  protovalidate, see `domains_e2e_test.go`)
- [ ] CreateDomain `domain_id` must match `Domain.domain` or be empty
  — was `TestCreateDomain_DomainIDMustMatchOrBeEmpty`. Deserves
  a single grpcharness test.
- [x] Duplicate domain in any org → AlreadyExists with no leak of
  which org (`TestE2E_CreateDomain_DuplicateAlreadyExists`)
- [x] DeleteDomain: not-found → NotFound
  (`TestE2E_DeleteDomain_Matrix/not_found`)
- [x] DeleteDomain: etag mismatch → FailedPrecondition
  (`TestE2E_DeleteDomain_Matrix/etag_mismatch`)
- [ ] DeleteDomain: last-verified domain on enabled SsoConfig →
  FailedPrecondition (the sso-coupling guard) — needs SSO test
  setup, deferred to a follow-up
- [ ] DeleteDomain: VERIFIED + an extra VERIFIED on the org →
  allowed
- [x] DeleteDomain: PENDING row + no SsoConfig → succeeds
  (`TestE2E_DeleteDomain_Matrix/pending_row_deletes_without_SSO_guard`)
- [ ] ListDomains: returns rows (basic coverage; pagination is
  worth its own test if any handler uses it)
- ~~"GenerateVerificationToken_Unique"~~ — pure-function test of
  a token generator. If it's worth covering at all, it's a
  pure-function test in the same package, not a handler test.
  Promote to `verify_token_test.go` (no mocks) or drop.
- ~~"ParseDomainSegment_*"~~ — three pure-function tests for a
  string-parsing helper. Promote to a small pure-function test
  file or fold into the handler-level Get/Create tests where the
  parsing is exercised.
- ~~"GetDomain_*"~~ — happy/not-found pair. Cover via the existing
  CreateDomain → GetDomain flow in `domains_e2e_test.go`.

### RunVerifyDomain

The deleted tests stubbed the domain-verification orchestrator
state machine. After the River cutover (commit 4e5e3aa), this
runs in the worker process; the e2e file already has a `t.Skip`
for `startVerifyWorker` pointing at "test needs migration per
#71 Phase 2." That migration is what restores this coverage:

- [ ] Verified on first DNS check → row → VERIFIED, LRO completes
  with the proto
- [ ] DNS resolver returns wrong record → row stays PENDING, LRO
  eventually expires after grace
- [ ] Row hard-deleted mid-verify → worker terminal-fails the LRO
- [ ] Worker context cancellation → LRO neither completes nor
  fails, stays pending until next worker tick

These migrate to a separate test file
(`internal/workers/verify_domain_e2e_test.go` or similar)
running the actual River worker against a real DB, same shape
as `lifecycle_undelete_river_e2e_test.go`.

## SsoConfig (sso_test.go was the largest deleted file)

This is the heaviest rewrite — SSO has external Firebase calls
(via the `authn.Service` interface) that the harness already
stubs. The behaviors:

- [x] UpdateSsoConfig OIDC create-then-update round-trip
  (`TestE2E_SsoConfig_OidcRoundTrip`)
- [x] GetSsoConfig + UpdateSsoConfig responses omit plaintext
  client_secret (`TestE2E_SsoConfig_OmitsPlaintextSecret`)
- [x] UpdateSsoConfig persists the secret to the bytea column
  (`TestE2E_SsoConfig_PersistsClientSecret`). Tests run against
  `cryptotest.Encryptor`, which round-trips ciphertext that is
  distinguishable from plaintext; the real KMS round-trip is not
  exercised in tests (would require live GCP creds). Follow-up
  below.
- [x] UpdateSsoConfig validation rejection matrix
  (`TestE2E_SsoConfig_RejectsInvalidConfig` — covers neither-oidc-
  nor-saml, oidc empty response_type)
- [ ] At-rest encryption boundary verification with a real KMS
  encryptor. Tests today run through `cryptotest.Encryptor`; a
  real-KMS path would need live GCP creds and a project setup —
  tracked separately.
- [x] UpdateSsoConfig: first-time create calls
  `authn.Service.CreateOidcProvider` with the right config
  (`TestE2E_SsoConfig_FirebaseCreateOnFirstUpdate`)
- [ ] UpdateSsoConfig: subsequent update calls
  `authn.Service.UpdateOidcProvider`
- [x] UpdateSsoConfig: Firebase failure → row not persisted
  (atomicity) (`TestE2E_SsoConfig_FirebaseFailureLeavesNoRow`)
- [x] UpdateSsoConfig: `Create` returns AlreadyExists → handler
  falls through to Update
  (`TestE2E_SsoConfig_FirebaseAlreadyExistsFallsThroughToUpdate`)
- [ ] UpdateSsoConfig: `Update` returns NotFound → handler falls
  through to Create
- [ ] SAML happy path + SAML missing required fields rejected
- [ ] AssertSsoConfigName: org slug mismatch → InvalidArgument
- ~~"NilDepsFailLoud"~~ — Config-struct-with-panic constructor
  contract. Already enforced at `NewOrganizationsServer`; a
  separate test for this is mock theater.

## Shape of the rewrite

One file per logical area, reusing the patterns already in this
package's `*_e2e_test.go` files:

- `lifecycle_e2e_test.go` — gets the force-path coverage + soft-
  delete gate matrix added
- `domains_e2e_test.go` — gets the DeleteDomain matrix +
  CreateDomain `domain_id` validation
- `members_e2e_test.go` — already comprehensive; no rewrite
- `server_integration_test.go` — gets the CreateOrganization
  follow-up assertions (founder binding, system roles, slug
  auto-gen) and the ListOrganizations cases
- `sso_e2e_test.go` — **NEW** file. SsoConfig coverage end-to-end
  with `WithAuth(mockAuth)` for the Firebase boundary
- `verify_token_test.go` — **NEW** small pure-function file for
  the token generator + parser helpers (if worth keeping at all)
- Domain-verify worker tests live in
  `internal/workers/verify_domain_e2e_test.go` (separate package)

## Out of scope here

- The `parseTagKeyParent` / convert layer fixes for tags are #73,
  not part of orgs rewrite.
- `internal/db.RunInTx` failure-mode tests belong in that package,
  not here.
