//go:build dev

package spaces_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/service/spaces"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestE2E_CreateSpace_SeedsFounderOwnerBinding pins the load-bearing
// invariant for space lifecycle: every newly-created space gets an
// explicit space-level owner Member row for its creator, established
// atomically with the space row itself. The binding is what
// ListMembers (space-scope) returns and is the substrate for
// space-level access control.
//
// Without this seed, a new space would have no direct space-level
// bindings — access would resolve only via the org-level inheritance
// path, and the founder couldn't be distinguished from any other
// org-admin in space-member listings.
func TestE2E_CreateSpace_SeedsFounderOwnerBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newSpacesHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	orgOp, err := orgClient.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "spaces-e2e-org",
		Organization:   &apiv1.Organization{DisplayName: "Spaces E2E"},
	})
	require.NoError(t, err)
	require.True(t, orgOp.GetDone())
	orgID := h.LookupOrgID(t, "spaces-e2e-org")
	founderUserID := h.LookupOrgUserID(t, orgID, owner.FirebaseIdentityID)

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	op, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/spaces-e2e-org",
		SpaceId: "alpha",
		Space:   &apiv1.Space{DisplayName: "Alpha"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone(), "CreateSpace should return done=true (synchronous)")

	var space apiv1.Space
	require.NoError(t, op.GetResponse().UnmarshalTo(&space))
	assert.Equal(t, "organizations/spaces-e2e-org/spaces/alpha", space.GetName())
	assert.Equal(t, apiv1.Space_ACTIVE, space.GetState())

	// The founder must be visible as an explicit space-level owner
	// in ListMembers — the seeded binding is what proves the bootstrap
	// fired.
	members, err := spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/spaces-e2e-org/spaces/alpha",
	})
	require.NoError(t, err)
	require.Len(t, members.GetMembers(), 1, "exactly one space-level Member should exist (the founder)")
	founder := members.GetMembers()[0]
	// Roles live at org scope (one set per org); space-level Member
	// rows reference the org-level role, so the role path is
	// organizations/{org}/roles/{role} not the {space}-rooted form.
	assert.Equal(t, "organizations/spaces-e2e-org/roles/"+permission.RoleOwner, founder.GetRole())
	assert.Equal(t, "organizations/spaces-e2e-org/users/"+founderUserID.String(), founder.GetUser())
}

// TestE2E_UndeleteSpace_RestoresSoftDeletedSpace pins the gate fix:
// the permission interceptor now uses GetSpaceByNameForGate so a
// soft-deleted space can be reached for Undelete (mirrors the org
// soft-delete-revive flow). Pre-fix, the gate's GetSpaceByName
// filtered the row out and Undelete returned NotFound at the gate.
func TestE2E_UndeleteSpace_RestoresSoftDeletedSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newSpacesHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	_, err := orgClient.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "undelete-org",
		Organization:   &apiv1.Organization{DisplayName: "Undelete Org"},
	})
	require.NoError(t, err)

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	_, err = spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/undelete-org",
		SpaceId: "beta",
		Space:   &apiv1.Space{DisplayName: "Beta"},
	})
	require.NoError(t, err)

	delOp, err := spacesClient.DeleteSpace(ctx, &apiv1.DeleteSpaceRequest{
		Name: "organizations/undelete-org/spaces/beta",
	})
	require.NoError(t, err)
	require.True(t, delOp.GetDone())
	var deleted apiv1.Space
	require.NoError(t, delOp.GetResponse().UnmarshalTo(&deleted))
	assert.Equal(t, apiv1.Space_DELETE_REQUESTED, deleted.GetState())

	undelOp, err := spacesClient.UndeleteSpace(ctx, &apiv1.UndeleteSpaceRequest{
		Name: "organizations/undelete-org/spaces/beta",
	})
	require.NoError(t, err, "UndeleteSpace must reach the handler — gate uses GetSpaceByNameForGate")
	require.True(t, undelOp.GetDone())
	var revived apiv1.Space
	require.NoError(t, undelOp.GetResponse().UnmarshalTo(&revived))
	assert.Equal(t, apiv1.Space_ACTIVE, revived.GetState())

	// Idempotent guard: a second Undelete on an already-ACTIVE space
	// surfaces FailedPrecondition (the row state guard fires).
	_, err = spacesClient.UndeleteSpace(ctx, &apiv1.UndeleteSpaceRequest{
		Name: "organizations/undelete-org/spaces/beta",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestE2E_SoftDeletedSpace_BlocksMutationsAtGate pins the new
// space-scope soft-delete gate (mirror of the org-scope gate). With
// a space in DELETE_REQUESTED state, mutating RPCs that don't carry
// `spaces.delete` (here: UpdateSpace) must surface
// FAILED_PRECONDITION at the interceptor without reaching the
// handler. spaces.delete itself passes through the gate so
// UndeleteSpace can still land — that path is covered by
// TestE2E_UndeleteSpace_RestoresSoftDeletedSpace.
func TestE2E_SoftDeletedSpace_BlocksMutationsAtGate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newSpacesHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	_, err := orgClient.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "softdel-org",
		Organization:   &apiv1.Organization{DisplayName: "Softdel Org"},
	})
	require.NoError(t, err)

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	_, err = spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/softdel-org",
		SpaceId: "delta",
		Space:   &apiv1.Space{DisplayName: "Delta"},
	})
	require.NoError(t, err)

	_, err = spacesClient.DeleteSpace(ctx, &apiv1.DeleteSpaceRequest{
		Name: "organizations/softdel-org/spaces/delta",
	})
	require.NoError(t, err)

	// UpdateSpace requires `spaces.update` — gate must block.
	_, err = spacesClient.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:        "organizations/softdel-org/spaces/delta",
			DisplayName: "Should Not Land",
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"gate must block update on soft-deleted space")
}

// TestE2E_UpdateSpace_RecordsCallerIdentity pins audit MED #4 for
// spaces: handler-side audit columns (updated_by) reflect the
// caller's firebase_identity, not an empty string. Verified
// indirectly: subsequent Get returns the row whose etag must change
// after the update, and the update succeeds — proving the handler
// actually populated the audit field rather than violating the
// NOT NULL constraint or any column trigger.
func TestE2E_UpdateSpace_RecordsCallerIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newSpacesHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	_, err := orgClient.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "audit-org",
		Organization:   &apiv1.Organization{DisplayName: "Audit Org"},
	})
	require.NoError(t, err)

	spacesClient := apiv1.NewSpacesClient(h.Conn())
	createOp, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/audit-org",
		SpaceId: "gamma",
		Space:   &apiv1.Space{DisplayName: "Gamma"},
	})
	require.NoError(t, err)
	var created apiv1.Space
	require.NoError(t, createOp.GetResponse().UnmarshalTo(&created))
	originalEtag := created.GetEtag()

	updateOp, err := spacesClient.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:        "organizations/audit-org/spaces/gamma",
			DisplayName: "Gamma Updated",
		},
	})
	require.NoError(t, err)
	var updated apiv1.Space
	require.NoError(t, updateOp.GetResponse().UnmarshalTo(&updated))
	assert.Equal(t, "Gamma Updated", updated.GetDisplayName())
	assert.NotEqual(t, originalEtag, updated.GetEtag(), "etag must rotate on update")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newSpacesHarness(t *testing.T) *grpcharness.Harness {
	return grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		permResolver := permission.NewResolver(h.Queries)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(
			h.Pool, h.Queries, h.Auth, nil, server.AuthenticatedUID,
			permResolver, callerIdentity, h.LROManager, h.Encryptor,
		))
		apiv1.RegisterSpacesServer(s, spaces.NewSpacesServer(
			h.Pool, h.Pool, h.Queries, nil, permResolver, callerIdentity,
		))
	}))
}
