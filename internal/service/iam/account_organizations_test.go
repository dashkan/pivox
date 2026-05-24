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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestE2E_ListAccountOrganizations_NoMemberships pins the bootstrap
// signal: a freshly-registered caller with no org_members rows gets
// an empty list, which the web org-gate uses to route to the
// create-org screen. Critically: the RPC succeeds (not
// PermissionDenied) — this is what the membership-exempt allowlist
// is for.
func TestE2E_ListAccountOrganizations_NoMemberships(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "memberless"})
	h.SetCaller(caller)

	resp, err := iamClient.ListAccountOrganizations(context.Background(), &iampb.ListAccountOrganizationsRequest{
		Parent: "accounts/me",
	})
	require.NoError(t, err, "memberless caller is on the exempt allowlist; RPC must succeed")
	assert.Empty(t, resp.GetAccountOrganizations(), "memberless caller has no orgs to list")
}

// TestE2E_ListAccountOrganizations_SingleOrgOwner pins the
// happy-path bootstrap: caller created an org via
// CreateOrganization (which auto-binds them as owner). ListAccount-
// Organizations returns exactly that one org with role=owner.
func TestE2E_ListAccountOrganizations_SingleOrgOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(caller)
	createOrg(t, orgClient, "founder-org", "Founder Org")

	resp, err := iamClient.ListAccountOrganizations(context.Background(), &iampb.ListAccountOrganizationsRequest{
		Parent: "accounts/me",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetAccountOrganizations(), 1)
	got := resp.GetAccountOrganizations()[0]
	assert.Equal(t, "organizations/founder-org", got.GetOrganization())
	assert.Equal(t, "Founder Org", got.GetDisplayName())
	assert.Equal(t, "owner", got.GetRole(), "creating an org auto-binds the creator as owner")
}

// TestE2E_ListAccountOrganizations_MultipleOrgsMixedRoles pins the
// role-resolution path with multiple bindings: caller is owner of
// one org (via CreateOrganization), admin of a second (via
// SeedMembership). All returned with the correct role.
func TestE2E_ListAccountOrganizations_MultipleOrgsMixedRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())

	// Founder owns "alpha", admin on "beta".
	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	createOrg(t, orgClient, "alpha", "Alpha Inc")

	// A second caller creates beta so founder isn't auto-owner there;
	// then we bind founder as admin via the seed helper.
	other := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "other-founder"})
	h.SetCaller(other)
	createOrg(t, orgClient, "beta", "Beta Inc")
	betaID := h.LookupOrgID(t, "beta")
	h.SeedMembership(t, betaID, founder, grpcharness.RoleAdmin)

	// Switch back to founder and list.
	h.SetCaller(founder)
	resp, err := iamClient.ListAccountOrganizations(context.Background(), &iampb.ListAccountOrganizationsRequest{
		Parent: "accounts/me",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetAccountOrganizations(), 2)

	byOrg := map[string]*iampb.AccountOrganization{}
	for _, o := range resp.GetAccountOrganizations() {
		byOrg[o.GetOrganization()] = o
	}
	require.Contains(t, byOrg, "organizations/alpha")
	require.Contains(t, byOrg, "organizations/beta")
	assert.Equal(t, "owner", byOrg["organizations/alpha"].GetRole())
	assert.Equal(t, "admin", byOrg["organizations/beta"].GetRole())
	assert.Equal(t, "Alpha Inc", byOrg["organizations/alpha"].GetDisplayName())
	assert.Equal(t, "Beta Inc", byOrg["organizations/beta"].GetDisplayName())
}

// TestE2E_ListAccountOrganizations_ExcludesSoftDeleted pins the
// difference from Organizations.ListOrganizations: soft-deleted
// orgs are excluded from the slim account view. The undelete UX
// runs against the full ListOrganizations, not this RPC.
func TestE2E_ListAccountOrganizations_ExcludesSoftDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	iamClient := iampb.NewIamClient(h.Conn())
	ctx := context.Background()

	founder := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(founder)
	createOrg(t, orgClient, "live-org", "Live Org")
	createOrg(t, orgClient, "doomed-org", "Doomed Org")

	// Soft-delete one org. DeleteOrganization is an LRO; wait for done.
	deleteOp, err := orgClient.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/doomed-org",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization")

	resp, err := iamClient.ListAccountOrganizations(ctx, &iampb.ListAccountOrganizationsRequest{
		Parent: "accounts/me",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetAccountOrganizations(), 1, "soft-deleted orgs are excluded from the slim view")
	assert.Equal(t, "organizations/live-org", resp.GetAccountOrganizations()[0].GetOrganization())
}

// TestE2E_ListAccountOrganizations_RejectsNonMeParent pins the
// singleton enforcement: parent must be the literal "accounts/me".
// Mirrors the same shape as DeleteAccount — the caller is implicit
// from the auth context, there's no other account they can address.
func TestE2E_ListAccountOrganizations_RejectsNonMeParent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newIamHarness(t)
	iamClient := iampb.NewIamClient(h.Conn())

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "caller"})
	h.SetCaller(caller)

	_, err := iamClient.ListAccountOrganizations(context.Background(), &iampb.ListAccountOrganizationsRequest{
		Parent: "accounts/someone-else",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"only accounts/me is accepted; cross-account addressing is rejected")
}
