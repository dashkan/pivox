//go:build dev

package iam_test

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/iam"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestE2E_DeleteUser_SoleOwnerBlocked pins the sole-owner-blocking
// guard end-to-end. Founder1 is the only owner of Org1. DeleteUser
// is an LRO whose VALIDATING phase runs the sole-owner check and
// completes the operation with FAILED_PRECONDITION naming Org1 —
// without this, deletion would leave Org1 ownerless.
//
// The RPC itself returns nil (the operation is created); the
// failure surfaces as the operation's error_code/error_message
// after the goroutine runs the sole-owner check. We wait on the
// operation and assert its terminal state.
func TestE2E_DeleteUser_SoleOwnerBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())

	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)

	createOrg(t, orgClient, "blocked-org", "Blocked Org")

	op, err := iamClient.DeleteUser(context.Background(), &iampb.DeleteUserRequest{
		Name: "organizations/blocked-org/users/me",
	})
	require.NoError(t, err, "DeleteUser RPC creates the LRO; the failure is on the LRO state")
	final := waitOpExpectFailure(t, h, op, "DeleteUser")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode(),
		"sole-owner DeleteUser must fail with FailedPrecondition")
	assert.Contains(t, final.GetError().GetMessage(), "blocked-org",
		"error must name the blocking org so the caller knows what to fix")
}

// TestE2E_DeleteUser_UnblockViaTransferOwnership covers recovery
// path 1: founder promotes successor to owner via TransferOwnership,
// then the new owner (NOT the demoted founder) executes DeleteUser
// against the founder's user. After demotion the founder is admin,
// and admin doesn't carry `users.delete` — the destructive verb is
// owner-only by design. So self-delete-via-transfer is structurally
// "transfer THEN have the new owner remove you", not "transfer then
// self-delete". This test pins that path; if the permission matrix
// ever grants users.delete to admin, the assertion shape needs to
// be revisited.
func TestE2E_DeleteUser_UnblockViaTransferOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	// Step 1: founder creates the org, becomes sole owner.
	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	createOrg(t, orgClient, "transfer-org", "Transfer Org")
	orgID := h.LookupOrgID(t, "transfer-org")

	// Step 2: seed a successor member as Admin (via direct DB; the
	// invitation flow isn't required for this test scope). Capture
	// the founder's per-org user uuid by listing org_members — the
	// founder's user row was created by CreateOrganization but its
	// id wasn't returned in the proto response.
	successor := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "successor"})
	successorUserID := h.SeedMembership(t, orgID, successor, grpcharness.RoleAdmin)
	founderUserID := h.LookupOrgUserID(t, orgID, founder.FirebaseIdentityID)

	// Step 3: confirm DeleteUser still blocks before the transfer.
	op, err := iamClient.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/transfer-org/users/me",
	})
	require.NoError(t, err)
	final := waitOpExpectFailure(t, h, op, "pre-transfer DeleteUser")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode(),
		"pre-transfer DeleteUser must still block")

	// Step 4: founder transfers ownership to successor.
	_, err = orgClient.TransferOwnership(ctx, &apiv1.TransferOwnershipRequest{
		Name:     "organizations/transfer-org",
		NewOwner: "organizations/transfer-org/users/" + successorUserID.String(),
	})
	require.NoError(t, err)

	// Step 5: switch to the new owner; they delete the demoted
	// founder's user (the founder is now admin, no longer sole
	// owner of any org, so the sole-owner check passes).
	h.SetCaller(successor)
	op2, err := iamClient.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/transfer-org/users/" + founderUserID.String(),
	})
	require.NoError(t, err, "post-transfer DeleteUser by new owner must succeed")
	waitOp(t, h, op2, "post-transfer DeleteUser")
}

// TestE2E_DeleteUser_UnblockViaDeleteOrg covers recovery path 2:
// Founder soft-deletes their only org first; sole-owner check
// excludes soft-deleted orgs (see ListSoleOwnerOrgsForFirebaseIdentity
// query — `o.delete_time IS NULL` filter), so DeleteUser then
// succeeds.
func TestE2E_DeleteUser_UnblockViaDeleteOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	createOrg(t, orgClient, "doomed-org", "Doomed Org")

	// Soft-delete the org first.
	deleteOp, err := orgClient.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/doomed-org",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization")

	// Now DeleteUser should succeed — soft-deleted orgs don't
	// count as sole-owner blockers.
	op, err := iamClient.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/doomed-org/users/me",
	})
	require.NoError(t, err, "DeleteUser must succeed after the only blocking org is soft-deleted")
	waitOp(t, h, op, "DeleteUser")
}

// newIamHarness wires up the harness with both Organizations and
// Iam services — DeleteUser flows touch both (TransferOwnership +
// DeleteOrganization on Organizations; DeleteUser itself on Iam).
func newIamHarness(t *testing.T) *grpcharness.Harness {
	return grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(
			h.Pool, h.Queries, h.Auth, nil, server.AuthenticatedUID,
			nil, callerIdentity, h.LROManager, h.Encryptor,
		))
		iampb.RegisterIamServer(s, iam.NewIamServer(
			h.Queries, h.Auth, callerIdentity, h.LROManager,
		))
	}))
}

// createOrg is a thin helper for the test setup phase; tests that
// only need the founder/owner shape don't have to assert the LRO
// shape every time.
func createOrg(t *testing.T, c apiv1.OrganizationsClient, slug, displayName string) {
	t.Helper()
	op, err := c.CreateOrganization(context.Background(), &apiv1.CreateOrganizationRequest{
		OrganizationId: slug,
		Organization:   &apiv1.Organization{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
}

func waitOp(t *testing.T, h *grpcharness.Harness, op interface{ GetName() string }, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	final, err := h.LROManager.WaitOperation(ctx, op.GetName())
	require.NoError(t, err, "WaitOperation(%s) failed", label)
	require.True(t, final.GetDone(), "%s should be done", label)
	if final.GetError() != nil {
		t.Fatalf("%s LRO failed: code=%d msg=%s",
			label, final.GetError().GetCode(), final.GetError().GetMessage())
	}
}

// waitOpExpectFailure is the inverse of waitOp: assert the LRO
// completed with a populated error rather than a result. Returns
// the final Operation so the caller can inspect the error code.
func waitOpExpectFailure(t *testing.T, h *grpcharness.Harness, op interface{ GetName() string }, label string) *longrunningpb.Operation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	final, err := h.LROManager.WaitOperation(ctx, op.GetName())
	require.NoError(t, err, "WaitOperation(%s) failed", label)
	require.True(t, final.GetDone(), "%s should be done", label)
	require.NotNil(t, final.GetError(), "%s LRO must complete with an error", label)
	return final
}
