package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOrgScopeFromPath_BareOrgName(t *testing.T) {
	got, err := OrgScopeFromPath("name", "organizations/acme")
	require.NoError(t, err)
	assert.Equal(t, ScopeOrg, got.Kind)
	assert.Equal(t, "acme", got.Slug)
}

func TestOrgScopeFromPath_NestedOrgChildResource(t *testing.T) {
	// Sub-resources like ssoConfig, domains, members, invitations all
	// nest under organizations/{org}/...; the helper must yield the
	// parent org slug regardless of depth.
	cases := []string{
		"organizations/acme/ssoConfig",
		"organizations/acme/domains/example.com",
		"organizations/acme/invitations/inv-123",
		"organizations/acme/members/usr-1",
		"organizations/acme/invitationPolicy",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			got, err := OrgScopeFromPath("name", path)
			require.NoError(t, err)
			assert.Equal(t, "acme", got.Slug)
		})
	}
}

func TestOrgScopeFromPath_EmptyIsInvalidArgument(t *testing.T) {
	_, err := OrgScopeFromPath("name", "")
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrgScopeFromPath_WrongPrefixIsInvalidArgument(t *testing.T) {
	cases := []string{
		"users/u-1",           // wrong root
		"acme",                // missing collection prefix
		"organizations",       // missing slug segment
		"organizations/",      // empty slug
		"/organizations/acme", // leading slash
		"organization/acme",   // wrong collection (singular)
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			_, err := OrgScopeFromPath("name", path)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestSpaceScopeFromPath_BareSpace(t *testing.T) {
	got, err := SpaceScopeFromPath("name", "organizations/acme/spaces/design")
	require.NoError(t, err)
	assert.Equal(t, ScopeSpace, got.Kind)
	assert.Equal(t, "acme", got.OrgSlug)
	assert.Equal(t, "design", got.Slug)
}

func TestSpaceScopeFromPath_NestedSpaceChild(t *testing.T) {
	cases := []string{
		"organizations/acme/spaces/design/members/usr-1",
		"organizations/acme/spaces/design/assets/asset-42",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			got, err := SpaceScopeFromPath("name", path)
			require.NoError(t, err)
			assert.Equal(t, "acme", got.OrgSlug)
			assert.Equal(t, "design", got.Slug)
		})
	}
}

func TestSpaceScopeFromPath_InvalidIsInvalidArgument(t *testing.T) {
	cases := []string{
		"",                                  // empty
		"organizations/acme",                // missing space segment entirely
		"organizations/acme/spaces",         // missing space slug
		"organizations/acme/spaces/",        // empty space slug
		"organizations//spaces/design",      // empty org slug
		"organizations/acme/space/design",   // wrong collection (singular)
		"orgs/acme/spaces/design",           // wrong root
		"/organizations/acme/spaces/design", // leading slash
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			_, err := SpaceScopeFromPath("name", path)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}
