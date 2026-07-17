package spaces_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// AIP-160/132 filter + order_by coverage for space-scope ListMembers on
// the shared filter.BuildListQuery keyset engine. Base scope is
// space_id; filter/order_by narrow within it.

func spaceMemberRoleSlugs(ms []*iampb.Member) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		r := m.GetRole()
		if i := strings.LastIndex(r, "/"); i >= 0 {
			r = r[i+1:]
		}
		out = append(out, r)
	}
	return out
}

// TestE2E_ListSpaceMembers_OrderByRole pins order_by=role (asc vs desc)
// at space scope.
func TestE2E_ListSpaceMembers_OrderByRole(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newSpacesHarness(t)
	ctx := context.Background()

	owned := h.SeedOwnedOrg(t, "sp-order", "SP Order", "founder")
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	op, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/sp-order",
		SpaceId: "eng",
		Space:   &apiv1.Space{DisplayName: "Engineering"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	spaceID := h.LookupSpaceID(t, owned.Row.ID, "eng")

	for _, r := range []string{grpcharness.RoleViewer, grpcharness.RoleAdmin, grpcharness.RoleEditor} {
		ident := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "sm-" + r})
		h.SeedSpaceMembership(t, owned.Row.ID, spaceID, ident, r)
	}

	asc, err := spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent:  "organizations/sp-order/spaces/eng",
		OrderBy: "role",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "editor", "owner", "viewer"}, spaceMemberRoleSlugs(asc.GetMembers()))

	desc, err := spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent:  "organizations/sp-order/spaces/eng",
		OrderBy: "role desc",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer", "owner", "editor", "admin"}, spaceMemberRoleSlugs(desc.GetMembers()))
	assert.NotEqual(t, spaceMemberRoleSlugs(asc.GetMembers()), spaceMemberRoleSlugs(desc.GetMembers()))
}

// TestE2E_ListSpaceMembers_FilterByRole pins the role filter at space
// scope, matched by full role resource name.
func TestE2E_ListSpaceMembers_FilterByRole(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newSpacesHarness(t)
	ctx := context.Background()

	owned := h.SeedOwnedOrg(t, "sp-filter", "SP Filter", "founder")
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	op, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/sp-filter",
		SpaceId: "eng",
		Space:   &apiv1.Space{DisplayName: "Engineering"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	spaceID := h.LookupSpaceID(t, owned.Row.ID, "eng")

	for _, r := range []string{grpcharness.RoleEditor, grpcharness.RoleViewer} {
		ident := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "sm-" + r})
		h.SeedSpaceMembership(t, owned.Row.ID, spaceID, ident, r)
	}

	resp, err := spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/sp-filter/spaces/eng",
		Filter: `role = "organizations/sp-filter/roles/editor"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"editor"}, spaceMemberRoleSlugs(resp.GetMembers()))
}

// TestE2E_ListSpaceMembers_ScopeIsolation pins that a space list is
// bounded by its space: a second space's bindings never leak in.
func TestE2E_ListSpaceMembers_ScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newSpacesHarness(t)
	ctx := context.Background()

	owned := h.SeedOwnedOrg(t, "sp-iso", "SP Iso", "founder")
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	for _, sp := range []string{"eng", "ops"} {
		op, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
			Parent:  "organizations/sp-iso",
			SpaceId: sp,
			Space:   &apiv1.Space{DisplayName: sp},
		})
		require.NoError(t, err)
		require.True(t, op.GetDone())
	}
	engID := h.LookupSpaceID(t, owned.Row.ID, "eng")
	opsID := h.LookupSpaceID(t, owned.Row.ID, "ops")

	engOnly := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "eng-only"})
	h.SeedSpaceMembership(t, owned.Row.ID, engID, engOnly, grpcharness.RoleEditor)
	opsOnly := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "ops-only"})
	h.SeedSpaceMembership(t, owned.Row.ID, opsID, opsOnly, grpcharness.RoleEditor)

	resp, err := spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/sp-iso/spaces/eng",
	})
	require.NoError(t, err)
	// eng has founder owner + eng-only editor = 2.
	assert.Len(t, resp.GetMembers(), 2, "eng list bounded to eng bindings")
	names := map[string]struct{}{}
	for _, m := range resp.GetMembers() {
		names[m.GetName()] = struct{}{}
	}
	assert.NotContains(t, names, "organizations/sp-iso/spaces/eng/members/user-"+opsOnly.IdentityID.String())
}

// TestE2E_ListSpaceMembers_UnknownFieldsRejected pins whitelist
// enforcement at space scope.
func TestE2E_ListSpaceMembers_UnknownFieldsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newSpacesHarness(t)
	ctx := context.Background()

	h.SeedOwnedOrg(t, "sp-reject", "SP Reject", "founder")
	spacesClient := apiv1.NewSpacesClient(h.Conn())
	op, err := spacesClient.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/sp-reject",
		SpaceId: "eng",
		Space:   &apiv1.Space{DisplayName: "Engineering"},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	_, err = spacesClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent:  "organizations/sp-reject/spaces/eng",
		OrderBy: "displayName",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
