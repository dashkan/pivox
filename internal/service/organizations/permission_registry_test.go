package organizations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// TestRegistryEntries_AllExtractorsYieldOrgScope drives every gated
// RPC's extractor with a representative request and asserts the
// scope is the org-slug we encoded in the path. Locks the
// (method → permission, extract) wiring against typo / wrong-field
// drift before the interceptor is enabled in main.go.
func TestRegistryEntries_AllExtractorsYieldOrgScope(t *testing.T) {
	const slug = "acme"

	cases := []struct {
		method string
		want   string // expected permission_id
		req    any
	}{
		// Organization
		{"/pivox.api.v1.Organizations/GetOrganization",
			permission.OrganizationsRead,
			&apiv1.GetOrganizationRequest{Name: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/UpdateOrganization",
			permission.OrganizationsUpdate,
			&apiv1.UpdateOrganizationRequest{Organization: &apiv1.Organization{Name: "organizations/" + slug}}},
		{"/pivox.api.v1.Organizations/DeleteOrganization",
			permission.OrganizationsDelete,
			&apiv1.DeleteOrganizationRequest{Name: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/UndeleteOrganization",
			// Undelete reverses Delete; same destruction-class tier.
			permission.OrganizationsDelete,
			&apiv1.UndeleteOrganizationRequest{Name: "organizations/" + slug}},

		// SSO config (singleton sub-resource)
		{"/pivox.api.v1.Organizations/GetSsoConfig",
			permission.OrganizationsSsoConfigRead,
			&apiv1.GetSsoConfigRequest{Name: "organizations/" + slug + "/ssoConfig"}},
		{"/pivox.api.v1.Organizations/UpdateSsoConfig",
			permission.OrganizationsSsoConfigUpdate,
			&apiv1.UpdateSsoConfigRequest{SsoConfig: &apiv1.SsoConfig{Name: "organizations/" + slug + "/ssoConfig"}}},

		// Domains
		{"/pivox.api.v1.Organizations/CreateDomain",
			permission.DomainsCreate,
			&apiv1.CreateDomainRequest{Parent: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/ListDomains",
			permission.DomainsRead,
			&apiv1.ListDomainsRequest{Parent: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/GetDomain",
			permission.DomainsRead,
			&apiv1.GetDomainRequest{Name: "organizations/" + slug + "/domains/example.com"}},
		{"/pivox.api.v1.Organizations/DeleteDomain",
			permission.DomainsDelete,
			&apiv1.DeleteDomainRequest{Name: "organizations/" + slug + "/domains/example.com"}},

		// Invitations
		{"/pivox.api.v1.Organizations/CreateInvitation",
			permission.InvitationsCreate,
			&apiv1.CreateInvitationRequest{Parent: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/ListInvitations",
			permission.InvitationsRead,
			&apiv1.ListInvitationsRequest{Parent: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/DeleteInvitation",
			permission.InvitationsDelete,
			&apiv1.DeleteInvitationRequest{Name: "organizations/" + slug + "/invitations/inv-1"}},
		{"/pivox.api.v1.Organizations/GetInvitationPolicy",
			permission.InvitationsRead,
			&apiv1.GetInvitationPolicyRequest{Name: "organizations/" + slug + "/invitationPolicy"}},
		{"/pivox.api.v1.Organizations/UpdateInvitationPolicy",
			permission.InvitationsUpdatePolicy,
			&apiv1.UpdateInvitationPolicyRequest{InvitationPolicy: &apiv1.InvitationPolicy{Name: "organizations/" + slug + "/invitationPolicy"}}},

		// Members (org-scope)
		{"/pivox.api.v1.Organizations/GetMember",
			permission.MembersRead,
			&iamv1.GetMemberRequest{Name: "organizations/" + slug + "/members/usr-1"}},
		{"/pivox.api.v1.Organizations/ListMembers",
			permission.MembersRead,
			&iamv1.ListMembersRequest{Parent: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/CreateMember",
			permission.MembersCreate,
			&iamv1.CreateMemberRequest{Parent: "organizations/" + slug}},
		{"/pivox.api.v1.Organizations/UpdateMember",
			permission.MembersUpdate,
			&iamv1.UpdateMemberRequest{Member: &iamv1.Member{Name: "organizations/" + slug + "/members/usr-1"}}},
		{"/pivox.api.v1.Organizations/DeleteMember",
			permission.MembersDelete,
			&iamv1.DeleteMemberRequest{Name: "organizations/" + slug + "/members/usr-1"}},

		// Transfer ownership (org-scope; resource is the new-owner member name)
		{"/pivox.api.v1.Organizations/TransferOwnership",
			permission.OrganizationsTransferOwnership,
			&apiv1.TransferOwnershipRequest{Name: "organizations/" + slug + "/members/usr-2"}},
	}

	reg := RegistryEntries()
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			entry, ok := reg[tc.method]
			require.True(t, ok, "method %s missing from registry", tc.method)
			assert.Equal(t, tc.want, entry.Permission, "permission_id mismatch for %s", tc.method)
			got, err := entry.Extract(tc.req)
			require.NoError(t, err, "extractor returned error for %s", tc.method)
			assert.Equal(t, server.ScopeOrg, got.Kind)
			assert.Equal(t, slug, got.Slug, "wrong slug extracted from %s", tc.method)
		})
	}
}

// TestRegistryEntries_RejectsInvalidPaths checks a representative
// extractor surfaces InvalidArgument on a malformed path. The shape
// validation lives in OrgScopeFromPath (covered exhaustively in
// scope_helpers_test.go); this test pins that the extractors actually
// route through that helper.
func TestRegistryEntries_RejectsInvalidPaths(t *testing.T) {
	reg := RegistryEntries()
	entry := reg["/pivox.api.v1.Organizations/GetOrganization"]
	_, err := entry.Extract(&apiv1.GetOrganizationRequest{Name: "users/me"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestExemptMethods_BootstrapAndPermissionTest pins the explicit
// exempt set. Adding to this list is a security-sensitive change —
// each entry must be safe to invoke without a permission gate.
func TestExemptMethods_BootstrapAndPermissionTest(t *testing.T) {
	// Bootstrap: caller has no membership yet (or is acquiring it).
	// TestIamPermissions: doesn't gate on a permission, asks "what
	// perms do I have"; handler does its own caller resolution.
	want := map[string]bool{
		"/pivox.api.v1.Organizations/CreateOrganization": true,
		"/pivox.api.v1.Organizations/ListOrganizations":  true,
		"/pivox.api.v1.Organizations/AcceptInvitation":   true,
		"/pivox.api.v1.Organizations/DeclineInvitation":  true,
		"/pivox.api.v1.Organizations/GetInvitation":      true,
		"/pivox.api.v1.Organizations/TestIamPermissions": true,
	}
	got := ExemptMethods()
	assert.Equal(t, want, got)
}

// TestRegistryAndExempt_NoOverlap is a fast guard against the
// PermissionInterceptor construction-time panic.
func TestRegistryAndExempt_NoOverlap(t *testing.T) {
	reg := RegistryEntries()
	exempt := ExemptMethods()
	for method := range exempt {
		_, dup := reg[method]
		assert.Falsef(t, dup, "method %q is in both registry and exempt", method)
	}
}

// TestRegistryCoversAllProtoMethods is the build-time completeness
// guard: every RPC declared on the Organizations service descriptor
// must be in the registry OR the exempt set. Adding a proto RPC
// without wiring its permission gate will fail this test, which
// is the failure mode we want — at build, not at first call in prod.
func TestRegistryCoversAllProtoMethods(t *testing.T) {
	uncovered := server.AssertRegistryCoversService(
		&apiv1.Organizations_ServiceDesc,
		RegistryEntries(),
		ExemptMethods(),
	)
	assert.Empty(t, uncovered,
		"every Organizations RPC must be in RegistryEntries() or ExemptMethods(); these are unwired: %v",
		uncovered)
}
