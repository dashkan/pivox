package spaces

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

const (
	testRegOrg   = "acme"
	testRegSpace = "design"
)

func orgPath() string   { return "organizations/" + testRegOrg }
func spacePath() string { return orgPath() + "/spaces/" + testRegSpace }

func TestRegistryEntries_OrgAndSpaceScopeMix(t *testing.T) {
	cases := []struct {
		method    string
		want      string
		req       any
		scope     server.ScopeKind
		spaceSlug string // empty for org-scope
	}{
		// Org-scope: ListSpaces (parent = org), CreateSpace (parent = org)
		{"/pivox.api.v1.Spaces/ListSpaces",
			permission.SpacesRead,
			&apiv1.ListSpacesRequest{Parent: orgPath()},
			server.ScopeOrg, ""},
		{"/pivox.api.v1.Spaces/CreateSpace",
			permission.SpacesCreate,
			&apiv1.CreateSpaceRequest{Parent: orgPath()},
			server.ScopeOrg, ""},

		// Space-scope: Get/Update/Delete/Undelete + the 4 Member CRUD RPCs
		{"/pivox.api.v1.Spaces/GetSpace",
			permission.SpacesRead,
			&apiv1.GetSpaceRequest{Name: spacePath()},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/UpdateSpace",
			permission.SpacesUpdate,
			&apiv1.UpdateSpaceRequest{Space: &apiv1.Space{Name: spacePath()}},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/DeleteSpace",
			permission.SpacesDelete,
			&apiv1.DeleteSpaceRequest{Name: spacePath()},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/UndeleteSpace",
			// Undelete reverses Delete; same destruction-class tier.
			permission.SpacesDelete,
			&apiv1.UndeleteSpaceRequest{Name: spacePath()},
			server.ScopeSpace, testRegSpace},

		// Members at space scope
		{"/pivox.api.v1.Spaces/GetMember",
			permission.MembersRead,
			&iamv1.GetMemberRequest{Name: spacePath() + "/members/u-1"},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/ListMembers",
			permission.MembersRead,
			&iamv1.ListMembersRequest{Parent: spacePath()},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/CreateMember",
			permission.MembersCreate,
			&iamv1.CreateMemberRequest{Parent: spacePath()},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/UpdateMember",
			permission.MembersUpdate,
			&iamv1.UpdateMemberRequest{Member: &iamv1.Member{Name: spacePath() + "/members/u-1"}},
			server.ScopeSpace, testRegSpace},
		{"/pivox.api.v1.Spaces/DeleteMember",
			permission.MembersDelete,
			&iamv1.DeleteMemberRequest{Name: spacePath() + "/members/u-1"},
			server.ScopeSpace, testRegSpace},
	}

	reg := RegistryEntries()
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			entry, ok := reg[tc.method]
			require.Truef(t, ok, "method %s missing from registry", tc.method)
			assert.Equal(t, tc.want, entry.Permission)
			got, err := entry.Extract(tc.req)
			require.NoError(t, err)
			assert.Equal(t, tc.scope, got.Kind)
			assert.Equal(t, testRegOrg, parentOrg(got))
			if tc.scope == server.ScopeSpace {
				assert.Equal(t, tc.spaceSlug, got.Slug)
			} else {
				assert.Equal(t, testRegOrg, got.Slug)
			}
		})
	}
}

// parentOrg returns the org slug for either an org-scoped or
// space-scoped ScopeRef so the table tests can assert parent-org
// uniformly.
func parentOrg(s server.ScopeRef) string {
	if s.Kind == server.ScopeSpace {
		return s.OrgSlug
	}
	return s.Slug
}

func TestRegistryEntries_RejectsInvalidPaths(t *testing.T) {
	reg := RegistryEntries()
	t.Run("space-scope/wrong-prefix", func(t *testing.T) {
		_, err := reg["/pivox.api.v1.Spaces/GetSpace"].Extract(&apiv1.GetSpaceRequest{Name: "users/me"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
	t.Run("space-scope/wrong-child-collection", func(t *testing.T) {
		// Pins the wedge between org-scope and space-scope extractors:
		// `organizations/{org}/teams/{team}` is structurally an
		// org-child but not a space-child, so SpaceScopeFromPath
		// must reject it. Without this guard a typo'd collection
		// segment would silently pass slug validation.
		_, err := reg["/pivox.api.v1.Spaces/GetSpace"].Extract(&apiv1.GetSpaceRequest{
			Name: "organizations/" + testRegOrg + "/teams/foo",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
	t.Run("org-scope/wrong-prefix", func(t *testing.T) {
		_, err := reg["/pivox.api.v1.Spaces/ListSpaces"].Extract(&apiv1.ListSpacesRequest{Parent: "users/me"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestExemptMethods_TestIamPermissionsOnly(t *testing.T) {
	want := map[string]bool{
		"/pivox.api.v1.Spaces/TestIamPermissions": true,
	}
	got := ExemptMethods()
	assert.Equal(t, want, got)
}

func TestRegistryAndExempt_NoOverlap(t *testing.T) {
	reg := RegistryEntries()
	exempt := ExemptMethods()
	for method := range exempt {
		_, dup := reg[method]
		assert.Falsef(t, dup, "method %q is in both registry and exempt", method)
	}
}

func TestRegistryCoversAllProtoMethods(t *testing.T) {
	uncovered := server.AssertRegistryCoversService(
		&apiv1.Spaces_ServiceDesc,
		RegistryEntries(),
		ExemptMethods(),
	)
	assert.Empty(t, uncovered,
		"every Spaces RPC must be in RegistryEntries() or ExemptMethods(); these are unwired: %v",
		uncovered)
}
