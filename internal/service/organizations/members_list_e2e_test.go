package organizations_test

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

// AIP-160/132 filter + order_by coverage for org-scope ListMembers, on
// the shared filter.BuildListQuery keyset engine. The base scope
// (org_id) is the non-negotiable partition; filter/order_by layer on
// top and can only narrow. Mirrors the connectors list suite.

// roleSlug extracts the role leaf ("owner", "editor", …) from a
// Member's role resource name.
func roleSlug(m *iampb.Member) string {
	r := m.GetRole()
	if i := strings.LastIndex(r, "/"); i >= 0 {
		return r[i+1:]
	}
	return r
}

func memberRoleSlugs(ms []*iampb.Member) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, roleSlug(m))
	}
	return out
}

// TestE2E_ListOrgMembers_OrderByRole pins that order_by=role sorts by
// the role slug ascending and desc reverses it — proving the engine
// honors order_by (the pre-migration handler ignored it, returning
// create_time order regardless).
func TestE2E_ListOrgMembers_OrderByRole(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "mem-order", "Mem Order")
	orgID := h.LookupOrgID(t, "mem-order")

	// Founder is owner. Add admin/editor/viewer bindings so the role
	// order is a non-trivial permutation of creation order.
	for _, r := range []string{grpcharness.RoleViewer, grpcharness.RoleAdmin, grpcharness.RoleEditor} {
		ident := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "m-" + r})
		h.SeedMembership(t, orgID, ident, r)
	}

	asc, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent:  "organizations/mem-order",
		OrderBy: "role",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "editor", "owner", "viewer"}, memberRoleSlugs(asc.GetMembers()))

	desc, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent:  "organizations/mem-order",
		OrderBy: "role desc",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"viewer", "owner", "editor", "admin"}, memberRoleSlugs(desc.GetMembers()))

	// asc and desc must differ (the migration's load-bearing behavior).
	assert.NotEqual(t, memberRoleSlugs(asc.GetMembers()), memberRoleSlugs(desc.GetMembers()))
}

// TestE2E_ListOrgMembers_FilterByRole pins that filter narrows to the
// members holding a given role, matched by full role resource name.
func TestE2E_ListOrgMembers_FilterByRole(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "mem-filter", "Mem Filter")
	orgID := h.LookupOrgID(t, "mem-filter")

	for _, r := range []string{grpcharness.RoleEditor, grpcharness.RoleAdmin, grpcharness.RoleViewer} {
		ident := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "m-" + r})
		h.SeedMembership(t, orgID, ident, r)
	}

	resp, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/mem-filter",
		Filter: `role = "organizations/mem-filter/roles/editor"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"editor"}, memberRoleSlugs(resp.GetMembers()),
		"filter must narrow to exactly the editor binding")
}

// TestE2E_ListOrgMembers_FilterByPrincipalKind pins the principal_kind
// filter (user vs group), which discriminates on the user_id/group_id
// XOR columns.
func TestE2E_ListOrgMembers_FilterByPrincipalKind(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "mem-kind", "Mem Kind")
	orgID := h.LookupOrgID(t, "mem-kind")

	// One extra user binding + one group binding. Founder owner is a
	// user, so users total = 2, groups = 1.
	extra := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "extra-user"})
	h.SeedMembership(t, orgID, extra, grpcharness.RoleEditor)
	groupIdent := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "grp-user"})
	h.SeedGroupMembership(t, orgID, groupIdent, grpcharness.RoleViewer)

	users, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/mem-kind",
		Filter: `principal_kind = user`,
	})
	require.NoError(t, err)
	assert.Len(t, users.GetMembers(), 2, "two user bindings (founder owner + extra editor)")
	for _, m := range users.GetMembers() {
		assert.NotNil(t, m.GetUser(), "principal_kind=user must return only user members")
	}

	groups, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/mem-kind",
		Filter: `principal_kind = group`,
	})
	require.NoError(t, err)
	assert.Len(t, groups.GetMembers(), 1, "one group binding")
	assert.NotEmpty(t, groups.GetMembers()[0].GetGroup())
}

// TestE2E_ListOrgMembers_ScopeIsolationCrossTenant pins that a member
// list is bounded by its org: org A's list never returns org B's
// bindings, even when both are owned by the same caller.
func TestE2E_ListOrgMembers_ScopeIsolationCrossTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "iso-a", "Iso A")
	createOrg(t, orgClient, "iso-b", "Iso B")
	orgA := h.LookupOrgID(t, "iso-a")
	orgB := h.LookupOrgID(t, "iso-b")

	// Distinct extra member in each org.
	aOnly := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "a-only"})
	h.SeedMembership(t, orgA, aOnly, grpcharness.RoleEditor)
	bOnly := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "b-only"})
	h.SeedMembership(t, orgB, bOnly, grpcharness.RoleEditor)

	resp, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/iso-a",
	})
	require.NoError(t, err)
	names := map[string]struct{}{}
	for _, m := range resp.GetMembers() {
		names[m.GetName()] = struct{}{}
	}
	bMemberName := "organizations/iso-a/members/user-" + bOnly.IdentityID.String()
	// b-only's binding lives in org B; it must not surface in A's list
	// under ANY name.
	assert.NotContains(t, names, bMemberName)
	assert.Contains(t, names, "organizations/iso-a/members/user-"+aOnly.IdentityID.String())
	// A has founder owner + a-only editor = 2 members exactly.
	assert.Len(t, resp.GetMembers(), 2, "org A list is bounded to org A's bindings")
}

// TestE2E_ListOrgMembers_UnknownFieldsRejected pins whitelist
// enforcement: unknown filter/order_by fields surface InvalidArgument.
func TestE2E_ListOrgMembers_UnknownFieldsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMembersHarness(t)
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "mem-reject", "Mem Reject")

	_, err := orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent: "organizations/mem-reject",
		Filter: `secret = "x"`,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = orgClient.ListMembers(ctx, &iampb.ListMembersRequest{
		Parent:  "organizations/mem-reject",
		OrderBy: "principal_kind",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
