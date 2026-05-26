package organizations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
	"github.com/dashkan/pivox/internal/workers"
)

// TestE2E_OrgSoftDeleteRevive exercises the full DeleteOrganization
// (soft path) → UndeleteOrganization round-trip end-to-end through
// the production interceptor chain.
//
// Cycle:
//
//  1. Owner creates an org. State=ACTIVE.
//  2. Owner soft-deletes it. State=DELETE_REQUESTED, delete_time +
//     purge_time set, etag bumped.
//  3. Reads still work during the grace window.
//  4. A second DeleteOrganization on the DELETE_REQUESTED org
//     surfaces FailedPrecondition (the handler's state guard,
//     distinct from the soft-delete-gate's permission rejection
//     that's covered in A4). Confirms the row's state pin is honored.
//  5. Owner undeletes. State=ACTIVE, delete_time/purge_time cleared,
//     etag bumped again.
//  6. A second UndeleteOrganization on the now-ACTIVE org surfaces
//     FailedPrecondition — confirms the inverse state guard.
//
// The fine-grained "soft-delete gate allow/deny matrix" check (e.g.
// members.create rejected on DELETE_REQUESTED) lives in A4's
// permission-interceptor E2E test, not here.
func TestE2E_OrgSoftDeleteRevive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Step 1: create the org as the founding owner.
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	createOp, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "revive-me",
		Organization:   &apiv1.Organization{DisplayName: "Revive Me"},
	})
	require.NoError(t, err)
	require.True(t, createOp.GetDone())

	var created apiv1.Organization
	require.NoError(t, createOp.GetResponse().UnmarshalTo(&created))
	require.Equal(t, apiv1.Organization_ACTIVE, created.GetState())
	originalEtag := created.GetEtag()

	// Step 2: soft-delete (force=false, no etag — non-force is
	// allowed without etag pinning per handler contract).
	deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization")

	// Step 3: reads still work during grace.
	got, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.NoError(t, err, "reads remain allowed during grace window")
	assert.Equal(t, apiv1.Organization_DELETE_REQUESTED, got.GetState())
	assert.NotEmpty(t, got.GetDeleteTime())
	assert.NotEmpty(t, got.GetPurgeTime())
	require.NotEqual(t, originalEtag, got.GetEtag(),
		"soft-delete must bump etag")

	// Step 4: redundant DeleteOrganization on DELETE_REQUESTED row
	// rejected by the handler's state guard.
	_, err = client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.Error(t, err, "second soft-delete on DELETE_REQUESTED row must fail")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	// Step 5: undelete with the correct etag.
	undeleteOp, err := client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/revive-me",
		Etag: got.GetEtag(),
	})
	require.NoError(t, err)
	waitOp(t, h, undeleteOp, "UndeleteOrganization")

	revived, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1.Organization_ACTIVE, revived.GetState())
	assert.Empty(t, revived.GetDeleteTime(), "revived org must clear delete_time")
	assert.Empty(t, revived.GetPurgeTime(), "revived org must clear purge_time")
	assert.NotEqual(t, got.GetEtag(), revived.GetEtag(),
		"undelete must bump etag")

	// Step 6: redundant UndeleteOrganization on now-ACTIVE row
	// rejected by the inverse state guard.
	_, err = client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.Error(t, err, "undelete on ACTIVE row must fail")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestE2E_DeleteUndelete_EtagGuards is the etag-guard rejection
// matrix for org lifecycle. Each case fresh-creates an org via
// the standard createOrg helper, drives it to its precondition
// state, calls Delete or Undelete with a bad etag, and asserts
// FailedPrecondition + an error message that mentions etag.
//
// Behaviors pinned:
//
//   - force=true without etag rejected (destructive ops bypass
//     the 30-day grace window, must pin row revision)
//   - force=true with stale etag rejected (concurrent revision
//     bump invalidates the caller's read)
//   - Undelete with non-matching etag rejected (optional etag,
//     but if supplied must match)
func TestE2E_DeleteUndelete_EtagGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "etag-guard-owner"})
	h.SetCaller(owner)

	// Each case is self-contained: it produces the bad-etag operation
	// to invoke and the request that should be rejected. Sharing the
	// outer harness/owner is fine — cases use distinct slugs so they
	// don't collide.
	cases := []struct {
		name string
		slug string // valid AIP slug per ^[a-z][a-z0-9-]{3,19}$
		op   func(t *testing.T, slug string, original *apiv1.Organization) error
	}{
		{
			name: "force without etag rejected",
			slug: "etag-noforce",
			op: func(t *testing.T, slug string, _ *apiv1.Organization) error {
				_, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
					Name:  "organizations/" + slug,
					Force: true,
				})
				return err
			},
		},
		{
			name: "force with stale etag rejected",
			slug: "etag-stale",
			op: func(t *testing.T, slug string, original *apiv1.Organization) error {
				// Bump revision behind the handler's back so the
				// original etag is stale by validation time.
				_, err := h.Pool.Exec(ctx, `
					UPDATE organizations
					   SET etag = md5(now()::text || revision::text || 'drift'),
					       revision = revision + 1
					 WHERE name = $1`, slug)
				require.NoError(t, err, "setup: bump revision")

				_, err = client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
					Name:  "organizations/" + slug,
					Force: true,
					Etag:  original.GetEtag(),
				})
				return err
			},
		},
		{
			name: "undelete with mismatched etag rejected",
			slug: "etag-undelete",
			op: func(t *testing.T, slug string, _ *apiv1.Organization) error {
				deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
					Name: "organizations/" + slug,
				})
				require.NoError(t, err, "setup: soft-delete")
				waitOp(t, h, deleteOp, "soft-delete setup")

				_, err = client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
					Name: "organizations/" + slug,
					Etag: "definitely-not-the-real-etag",
				})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			org := createOrg(t, client, tc.slug, tc.name)

			err := tc.op(t, tc.slug, org)

			require.Error(t, err, "operation must be rejected")
			assert.Equal(t, codes.FailedPrecondition, status.Code(err),
				"etag-guard rejections surface as FailedPrecondition")
			assert.Contains(t, status.Convert(err).Message(), "etag",
				"error message should mention etag")
		})
	}
}

// TestE2E_DeleteOrganization_ForceCascadesChildren pins the
// destructive-op semantics: force=true hard-deletes the org row
// and FK CASCADE removes children. Post-conditions:
//
//   - Org row gone (Get returns NotFound or PermissionDenied —
//     the layered interceptor chain may catch it before reaching
//     the handler)
//   - Child rows gone (org_members for this org row is empty —
//     the founder binding seeded by CreateOrganization is the
//     canary, but we add an extra member to make the cascade
//     visible beyond the founder)
//   - Slug is reusable (creating an org with the same slug
//     succeeds — proves no orphan child holds the slug via FK)
func TestE2E_DeleteOrganization_ForceCascadesChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "force-owner"})
	h.SetCaller(owner)
	created := createOrg(t, client, "force-cascade", "Force Cascade")

	// Add a second member so the cascade is visible beyond the
	// founder's auto-seeded owner binding.
	teammate := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "teammate"})
	orgID := h.LookupOrgID(t, "force-cascade")
	h.SeedMembership(t, orgID, teammate, grpcharness.RoleEditor)

	// Sanity: two members exist before the purge.
	before, err := h.Queries.ListOrgMembers(ctx, db.ListOrgMembersParams{
		OrgID: orgID, Offset: 0, Limit: 100,
	})
	require.NoError(t, err)
	require.Len(t, before, 2, "setup: founder + teammate must be members")

	// Force-purge with the correct etag.
	deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name:  "organizations/force-cascade",
		Force: true,
		Etag:  created.GetEtag(),
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization force")

	// Org row gone.
	_, err = client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/force-cascade",
	})
	require.Error(t, err, "purged org must not be readable")
	st, _ := status.FromError(err)
	assert.Contains(t, []codes.Code{codes.NotFound, codes.PermissionDenied}, st.Code(),
		"unknown org surfaces as NotFound or PermissionDenied "+
			"depending on which interceptor fires first")

	// Children gone — query directly via Queries to avoid
	// coupling this assertion to the Members RPC.
	after, err := h.Queries.ListOrgMembers(ctx, db.ListOrgMembersParams{
		OrgID: orgID, Offset: 0, Limit: 100,
	})
	require.NoError(t, err)
	assert.Empty(t, after, "FK CASCADE must remove all org_members rows")

	// Slug is reusable.
	_ = createOrg(t, client, "force-cascade", "Force Cascade v2")
}

// TestE2E_UndeleteOrganization_AfterPurgeTimeFails pins the
// worker terminal-fail path. The UndeleteOrganization SQL has
// `WHERE state = 'DELETE_REQUESTED' AND purge_time > now()`; if
// purge_time has elapsed, the UPDATE returns no rows. The worker
// maps the resulting pgx.ErrNoRows to FailedPrecondition via
// FailOperation, and the LRO completes with that error rather
// than succeeding.
//
// The handler accepts the request (no synchronous validation of
// purge_time happens at the handler — it's owned by the worker's
// SQL action), so the failure surfaces via the LRO error field,
// not as an RPC error on the Undelete call itself.
func TestE2E_UndeleteOrganization_AfterPurgeTimeFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "purge-owner"})
	h.SetCaller(owner)
	_ = createOrg(t, client, "purge-elapsed", "Purge Elapsed")

	deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/purge-elapsed",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "soft-delete setup")

	// Backdate purge_time so the worker's SQL guard treats the
	// grace window as elapsed. In production the 30-day window
	// elapses naturally; tests don't have that luxury.
	_, err = h.Pool.Exec(ctx,
		`UPDATE organizations SET purge_time = now() - INTERVAL '1 second' WHERE name = $1`,
		"purge-elapsed")
	require.NoError(t, err, "setup: backdate purge_time")

	undeleteOp, err := client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/purge-elapsed",
	})
	require.NoError(t, err, "handler accepts the request — failure surfaces via the LRO")

	final := waitOpUntilDone(t, h, undeleteOp, 5*time.Second, "UndeleteOrganization post-purge")
	require.True(t, final.GetDone(), "LRO must reach a terminal state")
	require.NotNil(t, final.GetError(),
		"worker must terminal-fail the LRO when purge_time has elapsed")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode(),
		"post-purge undelete failure surfaces as FailedPrecondition")
}

// TestE2E_DeleteRequestedOrg_RejectsMutations is the soft-delete
// gate matrix: a DELETE_REQUESTED org accepts reads + Undelete
// but rejects mutations across the OrganizationsServer surface.
//
// Coverage spans two distinct mutation RPCs to demonstrate the
// gate fires generically (not just for one handler):
//
//   - CreateDomain — a child-resource creation
//   - CreateMember — a child-resource creation in the iam domain
//
// Reads remaining allowed during the grace window is covered by
// TestE2E_OrgSoftDeleteRevive Step 3; not duplicated here.
//
// Each rejection surfaces as FailedPrecondition (handler state
// guard) or PermissionDenied (interceptor-level soft-delete gate)
// depending on which check fires first; both satisfy the contract.
func TestE2E_DeleteRequestedOrg_RejectsMutations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Setup: shared DELETE_REQUESTED org used by every case.
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "gate-owner"})
	h.SetCaller(owner)
	_ = createOrg(t, client, "gate-test", "Gate Test")
	orgID := h.LookupOrgID(t, "gate-test")

	// Pre-seed a target identity for the CreateMember case so its
	// failure mode is "the gate said no" rather than "principal
	// doesn't exist."
	target := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "gate-target"})
	targetUserID := h.SeedUserMembershipOnly(t, orgID, target)

	deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/gate-test",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "soft-delete setup")

	// Each case attempts one mutation against the DELETE_REQUESTED
	// org and returns the gRPC error.
	cases := []struct {
		name string
		mut  func() error
	}{
		{
			name: "CreateDomain rejected",
			mut: func() error {
				_, err := client.CreateDomain(ctx, &apiv1.CreateDomainRequest{
					Parent: "organizations/gate-test",
					Domain: &apiv1.Domain{Domain: "example.com"},
				})
				return err
			},
		},
		{
			name: "CreateMember rejected",
			mut: func() error {
				_, err := client.CreateMember(ctx, &iampb.CreateMemberRequest{
					Parent: "organizations/gate-test",
					Member: &iampb.Member{
						Principal: &iampb.Member_User{
							User: "organizations/gate-test/users/" + targetUserID.String(),
						},
						Role: "organizations/gate-test/roles/editor",
					},
				})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mut()
			require.Error(t, err, "mutation must be rejected on DELETE_REQUESTED org")
			st, _ := status.FromError(err)
			assert.Contains(t,
				[]codes.Code{codes.FailedPrecondition, codes.PermissionDenied},
				st.Code(),
				"soft-delete gate rejects mutations as FailedPrecondition or PermissionDenied")
		})
	}
}

// newLifecycleHarness wires up a harness with the Organizations
// service + LRO manager + an in-process River client running every
// org-lifecycle worker that's been ported off the legacy goroutine
// path (#69 Phase 5+). Lifecycle tests reuse this across all e2e
// cases in this file. Wiring matches cmd/pivox-cloud/main.go's
// production OrganizationsServer construction — anything passed as
// nil here means the test doesn't exercise that path (e.g.,
// resolver/codec are unused without space-scoped or appkey paths).
func newLifecycleHarness(t *testing.T) *grpcharness.Harness {
	h := grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		permResolver := permission.NewResolver(h.Queries)
		codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
		require.NoError(t, err)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Auth:       h.Auth,
			Codec:      codec,
			Resolver:   permResolver,
			LROManager: h.LROManager,
			Encryptor:  h.Encryptor,
		}))
	}))
	startOrgLifecycleWorkers(t, h)
	return h
}

// startOrgLifecycleWorkers registers the post-Phase-5 org-lifecycle
// workers on an in-process River client. Wraps
// grpcharness.StartRiverWorkers so the boilerplate
// (river.NewClient + Start + t.Cleanup(Stop)) lives in one place.
// Add new workers here as more org-scoped LROs port off the legacy
// goroutine path.
func startOrgLifecycleWorkers(t *testing.T, h *grpcharness.Harness) {
	t.Helper()
	h.StartRiverWorkers(t, func(rw *river.Workers) {
		river.AddWorker(rw, &workers.UndeleteOrgWorker{Pool: h.Pool, Logger: grpcharness.SilentLogger()})
		river.AddWorker(rw, &workers.DeleteOrgWorker{Pool: h.Pool, Logger: grpcharness.SilentLogger()})
	})
}

// waitOp blocks until the LRO is done. Polls the operations row
// because LROs split between the legacy in-process goroutine path
// (DeleteOrganization, etc.) and the River-driven path
// (UndeleteOrganization, post-Phase-5). Manager.WaitOperation's
// listener channel only fires for the legacy path; polling covers
// both uniformly.
func waitOp(t *testing.T, h *grpcharness.Harness, op interface{ GetName() string }, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.LROManager.GetOperation(context.Background(), op.GetName())
		require.NoError(t, err, "%s: GetOperation failed", label)
		if got.GetDone() {
			if got.GetError() != nil {
				t.Fatalf("%s LRO failed: code=%d msg=%s",
					label, got.GetError().GetCode(), got.GetError().GetMessage())
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s LRO not done within deadline", label)
}
