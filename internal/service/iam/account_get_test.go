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

package iam_test

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
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// claimsAuthService is a test authn.Service that returns a caller
// Identity carrying the full token claim set (email, `name`,
// `organization`), keyed by bearer token. The production OIDC verifier
// populates these off a real Keycloak access token; the default
// grpcharness stub returns only the UID, so GetAccount tests — which
// assert on claim-sourced fields — wire this via grpcharness.WithAuth.
type claimsAuthService struct {
	mu    sync.Mutex
	byTok map[string]*authn.Identity
}

func newClaimsAuthService() *claimsAuthService {
	return &claimsAuthService{byTok: map[string]*authn.Identity{}}
}

// set registers the Identity returned for the given bearer token. The
// Identity's UID must equal the token — the auth interceptor parses it
// as identities.id — mirroring a Keycloak token whose `sub` is that
// UUID.
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
	// Unknown token: mirror the default harness stub — the bearer token
	// IS the identity UID (a UUID), no extra claims.
	return &authn.Identity{UID: token}, nil
}

// TestE2E_GetAccount_OrgScopedToken pins the whoami happy path: a
// token carrying an `organization` claim resolves active_organization
// to that org, with role/display_name IDENTICAL to what
// ListAccountOrganizations reports for it, plus the identity fields
// sourced straight off the token claims.
func TestE2E_GetAccount_OrgScopedToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	auth := newClaimsAuthService()
	h := newIamHarness(t, grpcharness.WithAuth(auth))
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{
		UID:         "founder",
		Email:       "founder@acme.test",
		DisplayName: "Founder Name",
	})
	auth.set(caller.UID, &authn.Identity{
		UID:   caller.UID,
		Email: "founder@acme.test",
		Claims: map[string]any{
			"name":         "Founder Name",
			"organization": "acme",
		},
	})
	h.SetCaller(caller)
	createOrg(t, orgClient, "acme", "Acme Inc")

	resp, err := iamClient.GetAccount(context.Background(), &iampb.GetAccountRequest{
		Name: "accounts/me",
	})
	require.NoError(t, err)
	assert.Equal(t, "accounts/me", resp.GetName())
	assert.Equal(t, caller.UID, resp.GetSubject(), "subject is the token's sub == identities.id")
	assert.Equal(t, "founder@acme.test", resp.GetEmail(), "email sourced from the token's email claim")
	assert.Equal(t, "Founder Name", resp.GetDisplayName(), "display_name sourced from the token's name claim")

	ao := resp.GetActiveOrganization()
	require.NotNil(t, ao, "org-scoped token must resolve active_organization")
	assert.Equal(t, "organizations/acme", ao.GetOrganization())
	assert.Equal(t, "Acme Inc", ao.GetDisplayName())
	assert.Equal(t, "owner", ao.GetRole(), "creator is auto-bound owner")

	// The whole point of sharing the resolution: active_organization's
	// role/display_name must be byte-identical to what the list
	// endpoint reports for the same org.
	listResp, err := iamClient.ListAccountOrganizations(context.Background(), &iampb.ListAccountOrganizationsRequest{
		Parent: "accounts/me",
	})
	require.NoError(t, err)
	require.Len(t, listResp.GetAccountOrganizations(), 1)
	assert.Equal(t, listResp.GetAccountOrganizations()[0].GetRole(), ao.GetRole())
	assert.Equal(t, listResp.GetAccountOrganizations()[0].GetDisplayName(), ao.GetDisplayName())
	assert.Equal(t, listResp.GetAccountOrganizations()[0].GetOrganization(), ao.GetOrganization())
}

// TestE2E_GetAccount_NoOrgClaim pins the non-MCP token shape: no
// `organization` claim → active_organization unset, but the identity
// fields are still populated. The caller is memberless, which also
// proves whoami is on the membership-exempt allowlist (a
// mid-bootstrap caller must be able to learn who they are).
func TestE2E_GetAccount_NoOrgClaim(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	auth := newClaimsAuthService()
	h := newIamHarness(t, grpcharness.WithAuth(auth))
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{
		UID:         "loner",
		Email:       "loner@example.test",
		DisplayName: "Lone Ranger",
	})
	auth.set(caller.UID, &authn.Identity{
		UID:   caller.UID,
		Email: "loner@example.test",
		Claims: map[string]any{
			"name": "Lone Ranger",
			// No `organization` claim — the non-MCP web/electron token.
		},
	})
	h.SetCaller(caller)

	resp, err := iamClient.GetAccount(context.Background(), &iampb.GetAccountRequest{
		Name: "accounts/me",
	})
	require.NoError(t, err, "whoami is membership-exempt; a memberless caller must succeed")
	assert.Equal(t, caller.UID, resp.GetSubject())
	assert.Equal(t, "loner@example.test", resp.GetEmail())
	assert.Equal(t, "Lone Ranger", resp.GetDisplayName())
	assert.Nil(t, resp.GetActiveOrganization(), "no organization claim → active_organization unset")
}

// TestE2E_GetAccount_OrgClaimNotAMember pins the fail-closed edge: the
// token pins an org the caller is NOT a member of. active_organization
// stays unset — the resolution is scoped to the caller's own
// memberships, so a mismatched claim can never surface an org the
// caller has no binding on.
func TestE2E_GetAccount_OrgClaimNotAMember(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	auth := newClaimsAuthService()
	h := newIamHarness(t, grpcharness.WithAuth(auth))
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	auth.set(caller.UID, &authn.Identity{
		UID:   caller.UID,
		Email: caller.Email,
		Claims: map[string]any{
			"name":         caller.DisplayName,
			"organization": "ghost", // an org the caller has no membership in
		},
	})
	h.SetCaller(caller)
	createOrg(t, orgClient, "real-org", "Real Org")

	resp, err := iamClient.GetAccount(context.Background(), &iampb.GetAccountRequest{
		Name: "accounts/me",
	})
	require.NoError(t, err)
	assert.Nil(t, resp.GetActiveOrganization(),
		"token pinned to a non-member org resolves to unset, not the caller's other org")
}

// TestE2E_GetAccount_RejectsNonMeName pins the singleton enforcement:
// name must be the literal "accounts/me". Mirrors the same guard on
// ListAccountOrganizations and DeleteAccount.
func TestE2E_GetAccount_RejectsNonMeName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "caller"})
	h.SetCaller(caller)

	_, err := iamClient.GetAccount(context.Background(), &iampb.GetAccountRequest{
		Name: "accounts/someone-else",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"only accounts/me is accepted; cross-account addressing is rejected")
}
