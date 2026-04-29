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

	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/iam"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// ===========================================================================
// DeleteAccount — global Pivox + Firebase cascade.
// ===========================================================================

// TestE2E_DeleteAccount_SoleOwnerBlocked pins cross-org sole-owner
// blocking. Founder is the only owner of an active org; DeleteAccount
// completes the LRO with FAILED_PRECONDITION naming the blocking org.
func TestE2E_DeleteAccount_SoleOwnerBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())

	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	createOrg(t, orgClient, "blocked-org", "Blocked Org")

	op, err := iamClient.DeleteAccount(context.Background(), &iampb.DeleteAccountRequest{
		Name: "accounts/me",
	})
	require.NoError(t, err, "DeleteAccount RPC creates the LRO; failure surfaces on the LRO state")
	final := waitOpExpectFailure(t, h, op, "DeleteAccount")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode())
	assert.Contains(t, final.GetError().GetMessage(), "blocked-org")
}

// TestE2E_DeleteAccount_UnblockViaTransferOwnership pins recovery
// path 1: founder transfers ownership, then deletes their account.
// After transfer the founder is no longer sole owner of any org, so
// the cross-org sole-owner check passes and the account cascade runs.
func TestE2E_DeleteAccount_UnblockViaTransferOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	createOrg(t, orgClient, "transfer-org", "Transfer Org")
	orgID := h.LookupOrgID(t, "transfer-org")

	successor := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "successor"})
	successorUserID := h.SeedMembership(t, orgID, successor, grpcharness.RoleAdmin)

	// Pre-transfer DeleteAccount must still block.
	op, err := iamClient.DeleteAccount(ctx, &iampb.DeleteAccountRequest{Name: "accounts/me"})
	require.NoError(t, err)
	final := waitOpExpectFailure(t, h, op, "pre-transfer DeleteAccount")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode())

	// Transfer ownership to successor.
	_, err = orgClient.TransferOwnership(ctx, &apiv1.TransferOwnershipRequest{
		Name:     "organizations/transfer-org",
		NewOwner: "organizations/transfer-org/users/" + successorUserID.String(),
	})
	require.NoError(t, err)

	// Founder (now admin) deletes their account. Cross-org cascade
	// runs; sole-owner check passes since successor owns the org.
	op2, err := iamClient.DeleteAccount(ctx, &iampb.DeleteAccountRequest{Name: "accounts/me"})
	require.NoError(t, err)
	waitOp(t, h, op2, "post-transfer DeleteAccount")
}

// TestE2E_DeleteAccount_UnblockViaDeleteOrg pins recovery path 2:
// soft-delete the only blocking org first; sole-owner check excludes
// soft-deleted orgs (ListSoleOwnerOrgsForFirebaseIdentity has
// `o.delete_time IS NULL`), so DeleteAccount then succeeds.
func TestE2E_DeleteAccount_UnblockViaDeleteOrg(t *testing.T) {
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

	deleteOp, err := orgClient.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/doomed-org",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization")

	op, err := iamClient.DeleteAccount(ctx, &iampb.DeleteAccountRequest{Name: "accounts/me"})
	require.NoError(t, err)
	waitOp(t, h, op, "DeleteAccount")
}

// TestE2E_DeleteAccount_RejectsNonMeName confirms the singleton
// shape: only `accounts/me` is valid; a different account name
// returns InvalidArgument.
func TestE2E_DeleteAccount_RejectsNonMeName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	iamClient := iampb.NewIamClient(h.Conn())
	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)

	_, err := iamClient.DeleteAccount(context.Background(), &iampb.DeleteAccountRequest{
		Name: "accounts/someone-else",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// ===========================================================================
// DeleteUser — org-scoped removal of a user from one org.
// ===========================================================================

// TestE2E_DeleteUser_AdminRemovesUserFromOrg pins the happy path:
// an org admin removes another user from their org. The user's
// per-org bindings + users row in this org are gone, but their
// firebase_identity is untouched and they remain a member of OTHER
// orgs (proving the cascade is org-scoped).
func TestE2E_DeleteUser_AdminRemovesUserFromOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	// Owner creates two orgs.
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrg(t, orgClient, "org-a", "Org A")
	createOrg(t, orgClient, "org-b", "Org B")
	orgAID := h.LookupOrgID(t, "org-a")
	orgBID := h.LookupOrgID(t, "org-b")

	// Target is a member of BOTH orgs.
	target := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "target"})
	targetUserAID := h.SeedMembership(t, orgAID, target, grpcharness.RoleEditor)
	_ = h.SeedMembership(t, orgBID, target, grpcharness.RoleEditor)

	// Owner removes target from Org A.
	op, err := iamClient.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/org-a/users/" + targetUserAID.String(),
	})
	require.NoError(t, err)
	waitOp(t, h, op, "DeleteUser org-a")

	// Org A: target's user row is soft-deleted (purge_time set).
	users, err := h.Queries.ListUsersByFirebaseIdentity(ctx, target.FirebaseIdentityID)
	require.NoError(t, err)
	// ListUsersByFirebaseIdentity excludes soft-deleted users
	// (u.delete_time IS NULL), so target should now appear in
	// Org B only.
	gotOrgs := map[string]bool{}
	for _, u := range users {
		gotOrgs[u.OrgID.String()] = true
	}
	assert.False(t, gotOrgs[orgAID.String()],
		"target's per-org users row in Org A should be soft-deleted (excluded from query)")
	assert.True(t, gotOrgs[orgBID.String()],
		"target's per-org users row in Org B should be untouched")
}

// TestE2E_DeleteUser_LastOwnerBlocked: removing the only owner of an
// org leaves it ownerless — the org-local sole-owner check refuses
// with FAILED_PRECONDITION.
func TestE2E_DeleteUser_LastOwnerBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	// Owner creates an org. They're the sole owner.
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrg(t, orgClient, "single-owner-org", "Single Owner Org")
	orgID := h.LookupOrgID(t, "single-owner-org")
	ownerUserID := h.LookupOrgUserID(t, orgID, owner.FirebaseIdentityID)

	// Add a successor as Admin (does NOT count as an owner).
	other := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "other"})
	h.SeedMembership(t, orgID, other, grpcharness.RoleAdmin)

	// Owner attempts to remove themselves — would leave 0 owners.
	op, err := iamClient.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/single-owner-org/users/" + ownerUserID.String(),
	})
	require.NoError(t, err)
	final := waitOpExpectFailure(t, h, op, "DeleteUser last-owner")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode())
	assert.Contains(t, final.GetError().GetMessage(), "no owners")
}

// TestE2E_DeleteUser_RejectsMeTarget: the literal `me` is not
// supported on this RPC. Falls through to uuid.Parse failure with
// generic InvalidArgument.
func TestE2E_DeleteUser_RejectsMeTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	createOrg(t, orgClient, "any-org", "Any Org")

	_, err := iamClient.DeleteUser(ctx, &iampb.DeleteUserRequest{
		Name: "organizations/any-org/users/me",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// ===========================================================================
// helpers
// ===========================================================================

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
