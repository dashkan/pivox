// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// claimsAuthService returns a caller Identity carrying the full token
// claim set (email, `name`), keyed by bearer token — the default
// harness stub returns only the UID, so the whoami test wires this to
// exercise the claim-sourced fields.
type claimsAuthService struct {
	mu    sync.Mutex
	byTok map[string]*authn.Identity
}

func newClaimsAuthService() *claimsAuthService {
	return &claimsAuthService{byTok: map[string]*authn.Identity{}}
}

func (c *claimsAuthService) set(token string, id *authn.Identity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byTok[token] = id
}

func (c *claimsAuthService) VerifyToken(_ context.Context, token string) (*authn.Identity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id, ok := c.byTok[token]; ok {
		return id, nil
	}
	return &authn.Identity{UID: token}, nil
}

// newMcpHarness spins up a harness with the Organizations, Spaces, and
// Mcp services registered — enough to seed orgs/spaces through the real
// AIP handlers and read them back through the MCP surface.
func newMcpHarness(t *testing.T, opts ...grpcharness.Option) *grpcharness.Harness {
	t.Helper()
	base := []grpcharness.Option{
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithMcpServer(),
	}
	return grpcharness.New(t, append(base, opts...)...)
}

// createOrgAs creates an org owned by the harness's CURRENT caller.
// Unlike SeedOwnedOrg it does not seed/switch the identity, so a single
// caller can be placed in several orgs.
func createOrgAs(t *testing.T, h *grpcharness.Harness, slug, displayName string) {
	t.Helper()
	client := apiv1.NewOrganizationsClient(h.Conn())
	op, err := client.CreateOrganization(context.Background(), &apiv1.CreateOrganizationRequest{
		OrganizationId: slug,
		Organization:   &apiv1.Organization{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
}

// TestE2E_Mcp_GetAccount pins the whoami: identity fields are sourced
// off the verified token, and the method is membership-exempt (a
// memberless caller still learns who they are).
func TestE2E_Mcp_GetAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	auth := newClaimsAuthService()
	h := newMcpHarness(t, grpcharness.WithAuth(auth))
	client := mcpv1.NewMcpServiceClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{
		UID: "mcp-loner", Email: "loner@example.test", DisplayName: "Lone Ranger",
	})
	auth.set(caller.UID, &authn.Identity{
		UID:    caller.UID,
		Email:  "loner@example.test",
		Claims: map[string]any{"name": "Lone Ranger"},
	})
	h.SetCaller(caller)

	resp, err := client.GetAccount(context.Background(), &mcpv1.GetAccountRequest{})
	require.NoError(t, err, "whoami is membership-exempt; a memberless caller must succeed")
	assert.Equal(t, caller.UID, resp.GetSubject())
	assert.Equal(t, "loner@example.test", resp.GetEmail())
	assert.Equal(t, "Lone Ranger", resp.GetDisplayName())
}

// TestE2E_Mcp_ListOrgs covers the happy path (caller's orgs only,
// slug-sorted) plus the case-insensitive name_prefix filter.
func TestE2E_Mcp_ListOrgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMcpHarness(t)
	client := mcpv1.NewMcpServiceClient(h.Conn())

	// One caller placed in three orgs.
	owner := h.SeedOwnedOrg(t, "alpha", "Alpha", "mcp-lo")
	_ = owner
	createOrgAs(t, h, "alback", "Alback")
	createOrgAs(t, h, "beta", "Beta")

	all, err := client.ListOrgs(context.Background(), &mcpv1.ListOrgsRequest{})
	require.NoError(t, err)
	gotSlugs := slugsOf(all.GetOrgs())
	assert.Equal(t, []string{"alback", "alpha", "beta"}, gotSlugs, "caller's orgs, slug-sorted")

	pref, err := client.ListOrgs(context.Background(), &mcpv1.ListOrgsRequest{NamePrefix: "AL"})
	require.NoError(t, err)
	assert.Equal(t, []string{"alback", "alpha"}, slugsOf(pref.GetOrgs()),
		"name_prefix is a case-insensitive slug prefix")
}

// TestE2E_Mcp_GetOrg pins the membership gate: a member reads the org;
// a non-member (member elsewhere) and a lookup for a nonexistent org
// both get the SAME NotFound, so existence never leaks.
func TestE2E_Mcp_GetOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMcpHarness(t)
	client := mcpv1.NewMcpServiceClient(h.Conn())

	h.SeedOwnedOrg(t, "acme", "Acme Inc", "mcp-go") // caller = acme owner

	org, err := client.GetOrg(context.Background(), &mcpv1.GetOrgRequest{Org: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "acme", org.GetSlug())
	assert.Equal(t, "Acme Inc", org.GetDisplayName())

	// A member's lookup of a nonexistent org is NotFound.
	_, err = client.GetOrg(context.Background(), &mcpv1.GetOrgRequest{Org: "ghost"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	// Switch to an outsider who is a member of a different org. acme
	// exists, but the outsider has no binding — fail closed with the
	// same NotFound as the nonexistent org above.
	h.SeedOwnedOrg(t, "other", "Other", "mcp-go2") // caller = other owner
	_, err = client.GetOrg(context.Background(), &mcpv1.GetOrgRequest{Org: "acme"})
	assert.Equal(t, codes.NotFound, status.Code(err),
		"a non-member must not distinguish a real org from a missing one")

	// Empty org is a caller error, not NotFound.
	_, err = client.GetOrg(context.Background(), &mcpv1.GetOrgRequest{Org: ""})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_Mcp_ListSpaces exercises the static ListSpacesForMCP keyset
// query: the required org param, membership gating, and the keyset page
// boundary. Seeding pageSize+1 spaces and paging fully asserts the strict
// `id > cursor` resume neither drops nor duplicates the boundary row.
func TestE2E_Mcp_ListSpaces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMcpHarness(t)
	client := mcpv1.NewMcpServiceClient(h.Conn())

	h.SeedOwnedOrg(t, "acme", "Acme Inc", "mcp-ls") // caller = acme owner
	// Seed pageSize+1 (3 spaces at page size 2) so page 1 fills and page 2
	// carries exactly the boundary row.
	h.SeedOwnedSpace(t, "acme", "sp-a", "Space A")
	h.SeedOwnedSpace(t, "acme", "sp-b", "Space B")
	h.SeedOwnedSpace(t, "acme", "sp-c", "Space C")

	// org is required.
	_, err := client.ListSpaces(context.Background(), &mcpv1.ListSpacesRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "org is required for v1 list_spaces")

	// Page 1 (size 2) + page 2 must together cover all three spaces via the
	// opaque keyset page token, with no drop or duplicate at the boundary.
	p1, err := client.ListSpaces(context.Background(), &mcpv1.ListSpacesRequest{Org: "acme", PageSize: 2})
	require.NoError(t, err)
	require.Len(t, p1.GetSpaces(), 2)
	require.NotEmpty(t, p1.GetNextPageToken(), "more spaces remain, so a token must be set")

	p2, err := client.ListSpaces(context.Background(), &mcpv1.ListSpacesRequest{
		Org: "acme", PageSize: 2, PageToken: p1.GetNextPageToken(),
	})
	require.NoError(t, err)
	require.Len(t, p2.GetSpaces(), 1)
	assert.Empty(t, p2.GetNextPageToken(), "last page has no token")

	gotSlugs := make([]string, 0, 3)
	for _, sp := range append(p1.GetSpaces(), p2.GetSpaces()...) {
		assert.Equal(t, "acme", sp.GetOrg())
		gotSlugs = append(gotSlugs, sp.GetSlug())
	}
	// ElementsMatch fails on either a dropped or a duplicated row.
	assert.ElementsMatch(t, []string{"sp-a", "sp-b", "sp-c"}, gotSlugs,
		"the two pages together cover every space exactly once")

	// A non-member org fails closed with NotFound.
	h.SeedOwnedOrg(t, "other", "Other", "mcp-ls2") // caller = outsider
	_, err = client.ListSpaces(context.Background(), &mcpv1.ListSpacesRequest{Org: "acme"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestE2E_Mcp_ListSpaces_NamePrefix pins the case-insensitive display-name
// prefix match of the static query.
func TestE2E_Mcp_ListSpaces_NamePrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMcpHarness(t)
	client := mcpv1.NewMcpServiceClient(h.Conn())

	h.SeedOwnedOrg(t, "acme", "Acme Inc", "mcp-np")
	h.SeedOwnedSpace(t, "acme", "sp-prod-1", "Prod One")
	h.SeedOwnedSpace(t, "acme", "sp-prod-2", "Prod Two")
	h.SeedOwnedSpace(t, "acme", "sp-stage", "Stage")

	// Lowercase query against capitalized display names proves the match is
	// case-insensitive (ILIKE), a prefix (Stage is excluded), and anchored.
	resp, err := client.ListSpaces(context.Background(), &mcpv1.ListSpacesRequest{
		Org: "acme", NamePrefix: "prod",
	})
	require.NoError(t, err)
	gotSlugs := make([]string, 0, 2)
	for _, sp := range resp.GetSpaces() {
		gotSlugs = append(gotSlugs, sp.GetSlug())
	}
	assert.ElementsMatch(t, []string{"sp-prod-1", "sp-prod-2"}, gotSlugs,
		"name_prefix is a case-insensitive display-name prefix; non-matches excluded")
}

// TestE2E_Mcp_ListSpaces_NamePrefix_Literal proves name_prefix is a PURE
// LITERAL prefix at the SQL layer: LIKE metacharacters in the caller's input
// ('*', '%', '_', '\') match themselves, never as wildcards. The only
// wildcard is the implicit trailing anchor. This guards the ESCAPE '\' fix
// against the old AIP-160 grammar quirks (an embedded '*' meaning "any", a
// bare '\' being swallowed as the escape char, unescaped '%'/'_').
func TestE2E_Mcp_ListSpaces_NamePrefix_Literal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMcpHarness(t)
	client := mcpv1.NewMcpServiceClient(h.Conn())

	h.SeedOwnedOrg(t, "acme", "Acme Inc", "mcp-lit")
	// Seed pairs where a metacharacter-literal name sits next to a name that a
	// wildcard interpretation of the query would spuriously match.
	h.SeedOwnedSpace(t, "acme", "sp-star", "p*d literal-star")
	h.SeedOwnedSpace(t, "acme", "sp-xany", "pXd wildcard-would-hit")
	h.SeedOwnedSpace(t, "acme", "sp-under", "a_b literal-underscore")
	h.SeedOwnedSpace(t, "acme", "sp-axb", "axb underscore-would-hit")
	h.SeedOwnedSpace(t, "acme", "sp-pct", "50% literal-percent")
	h.SeedOwnedSpace(t, "acme", "sp-back", `a\b literal-backslash`)

	cases := []struct {
		name   string
		prefix string
		want   []string
	}{
		{"asterisk is literal, not a wildcard", "p*d", []string{"sp-star"}},
		{"underscore is literal, not single-char wildcard", "a_b", []string{"sp-under"}},
		{"percent is literal, not multi-char wildcard", "50%", []string{"sp-pct"}},
		{"backslash is literal, not the escape char", `a\b`, []string{"sp-back"}},
		{"trailing wildcard still anchors the prefix", "p", []string{"sp-star", "sp-xany"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.ListSpaces(context.Background(), &mcpv1.ListSpacesRequest{
				Org: "acme", NamePrefix: tc.prefix,
			})
			require.NoError(t, err)
			gotSlugs := make([]string, 0, len(resp.GetSpaces()))
			for _, sp := range resp.GetSpaces() {
				gotSlugs = append(gotSlugs, sp.GetSlug())
			}
			assert.ElementsMatch(t, tc.want, gotSlugs,
				"name_prefix %q must match only the literal-prefix rows", tc.prefix)
		})
	}
}

// TestE2E_Mcp_GetSpace pins the space membership gate and its uniform
// fail-closed NotFound.
func TestE2E_Mcp_GetSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newMcpHarness(t)
	client := mcpv1.NewMcpServiceClient(h.Conn())

	h.SeedOwnedOrg(t, "acme", "Acme Inc", "mcp-gs")
	h.SeedOwnedSpace(t, "acme", "sp-a", "Space A")

	sp, err := client.GetSpace(context.Background(), &mcpv1.GetSpaceRequest{Org: "acme", Space: "sp-a"})
	require.NoError(t, err)
	assert.Equal(t, "acme", sp.GetOrg())
	assert.Equal(t, "sp-a", sp.GetSlug())
	assert.Equal(t, "Space A", sp.GetDisplayName())

	// Missing space → NotFound.
	_, err = client.GetSpace(context.Background(), &mcpv1.GetSpaceRequest{Org: "acme", Space: "ghost"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	// Outsider → same NotFound, no existence leak.
	h.SeedOwnedOrg(t, "other", "Other", "mcp-gs2")
	_, err = client.GetSpace(context.Background(), &mcpv1.GetSpaceRequest{Org: "acme", Space: "sp-a"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func slugsOf(orgs []*mcpv1.Organization) []string {
	out := make([]string, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, o.GetSlug())
	}
	return out
}
