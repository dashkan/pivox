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

package dashboards_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	"github.com/dashkan/pivox/internal/service/assets"
	"github.com/dashkan/pivox/internal/service/dashboards"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// newQueryHarness wires Organizations + Spaces + Assets + Dashboards
// for QueryDashboardData tests. The org / space CRUD and asset
// creation flow through real handlers, so the test exercises the
// production stack the same way an integration consumer would.
func newQueryHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			assetsv1.RegisterAssetsServer(s, assets.NewAssetsServer(assets.Config{
				Pool: h.Pool, Queries: h.Queries,
			}))
			apiv1.RegisterDashboardsServer(s, dashboards.NewServer(dashboards.Config{
				Pool: h.Pool, Queries: h.Queries,
			}))
		}),
	)
}

// queryFixture seeds an org with 2 spaces ("alpha", "beta") so org-
// scope query tests can verify cross-space aggregation, and
// space-scope query tests can verify per-space scoping.
type queryFixture struct {
	owner      grpcharness.OwnedOrg
	alphaSpace grpcharness.OwnedSpace
	betaSpace  grpcharness.OwnedSpace
}

func seedQueryFixture(t *testing.T, h *grpcharness.Harness, slug string) queryFixture {
	t.Helper()
	owner := h.SeedOwnedOrg(t, slug, "Query Test", "owner")
	alpha := h.SeedOwnedSpace(t, owner.Slug, "alpha", "Alpha")
	beta := h.SeedOwnedSpace(t, owner.Slug, "beta", "Beta")
	return queryFixture{owner: owner, alphaSpace: alpha, betaSpace: beta}
}

// createPlaceholderAsset creates an asset in the given space with
// the supplied display_name. The asset lands in PLACEHOLDER state
// (no file content yet) — content_type / media_type stay empty,
// which is the right shape for testing the icon fallback path.
func createPlaceholderAsset(t *testing.T, conn grpc.ClientConnInterface, parent, displayName string) string {
	t.Helper()
	client := assetsv1.NewAssetsClient(conn)
	op, err := client.CreateAsset(context.Background(), &assetsv1.CreateAssetRequest{
		Parent: parent,
		Asset:  &assetsv1.Asset{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	var a assetsv1.Asset
	require.NoError(t, op.GetResponse().UnmarshalTo(&a))
	return a.GetName()
}

// ============================================================================
// Org-scope query
// ============================================================================

func TestE2E_QueryDashboardData_OrgScope_AcrossSpaces(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-org-cross")

	createPlaceholderAsset(t, h.Conn(),
		"organizations/"+fx.owner.Slug+"/spaces/"+fx.alphaSpace.Slug, "Alpha One")
	createPlaceholderAsset(t, h.Conn(),
		"organizations/"+fx.owner.Slug+"/spaces/"+fx.alphaSpace.Slug, "Alpha Two")
	createPlaceholderAsset(t, h.Conn(),
		"organizations/"+fx.owner.Slug+"/spaces/"+fx.betaSpace.Slug, "Beta One")

	client := apiv1.NewDashboardsClient(h.Conn())
	resp, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent: "organizations/" + fx.owner.Slug,
		Query: &apiv1.ResourceQuery{
			ResourceType: "pivox.assets/Asset",
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRows(), 3, "org-scope query must surface assets across all spaces")

	// Each row must carry the full resource name with the right space slug.
	displayNames := map[string]string{}
	for _, row := range resp.GetRows() {
		fields := row.GetFields()
		name := fields["name"].GetStringValue()
		dn := fields["display_name"].GetStringValue()
		require.NotEmpty(t, name)
		require.NotEmpty(t, dn)
		displayNames[dn] = name
	}
	assert.Contains(t, displayNames, "Alpha One")
	assert.Contains(t, displayNames, "Alpha Two")
	assert.Contains(t, displayNames, "Beta One")

	// Spot-check the Alpha One row's name format.
	alphaName := displayNames["Alpha One"]
	assert.Contains(t, alphaName, "/spaces/"+fx.alphaSpace.Slug+"/assets/")
}

func TestE2E_QueryDashboardData_OrgScope_RowFieldsAreSynthesized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-org-fields")

	createPlaceholderAsset(t, h.Conn(),
		"organizations/"+fx.owner.Slug+"/spaces/"+fx.alphaSpace.Slug, "Field Probe")

	client := apiv1.NewDashboardsClient(h.Conn())
	resp, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent: "organizations/" + fx.owner.Slug,
		Query:  &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRows(), 1)

	fields := resp.GetRows()[0].GetFields()
	for _, expected := range []string{
		"name", "display_name", "media_type", "state", "size_bytes",
		"create_time", "icon", "thumbnail_url",
	} {
		assert.Contains(t, fields, expected, "row must include synthesized field %q", expected)
	}

	// Placeholder assets have no media_type → icon falls back to ICON_DOCUMENT (1000).
	icon := int32(fields["icon"].GetNumberValue())
	assert.Equal(t, int32(apiv1.Icon_ICON_DOCUMENT), icon,
		"placeholder asset (no content_type) must fall back to ICON_DOCUMENT")

	// state for a fresh placeholder is PLACEHOLDER per asset.proto:204.
	assert.Equal(t, "PLACEHOLDER", fields["state"].GetStringValue())

	// thumbnail_url is empty in v1 (storage-gateway URL composition
	// lands when the parallel-session storage work merges).
	assert.Equal(t, "", fields["thumbnail_url"].GetStringValue())
}

// ============================================================================
// Space-scope query
// ============================================================================

func TestE2E_QueryDashboardData_SpaceScope_OnlyThatSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-space-only")

	createPlaceholderAsset(t, h.Conn(),
		"organizations/"+fx.owner.Slug+"/spaces/"+fx.alphaSpace.Slug, "Alpha Asset")
	createPlaceholderAsset(t, h.Conn(),
		"organizations/"+fx.owner.Slug+"/spaces/"+fx.betaSpace.Slug, "Beta Asset")

	client := apiv1.NewDashboardsClient(h.Conn())
	resp, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent: "organizations/" + fx.owner.Slug + "/spaces/" + fx.alphaSpace.Slug,
		Query:  &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRows(), 1, "space-scope query must surface only that space's assets")

	dn := resp.GetRows()[0].GetFields()["display_name"].GetStringValue()
	assert.Equal(t, "Alpha Asset", dn)
}

// ============================================================================
// Pagination
// ============================================================================

func TestE2E_QueryDashboardData_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-paginate")

	parent := "organizations/" + fx.owner.Slug + "/spaces/" + fx.alphaSpace.Slug
	for i := range 5 {
		createPlaceholderAsset(t, h.Conn(), parent, fmt.Sprintf("Asset %d", i))
	}

	client := apiv1.NewDashboardsClient(h.Conn())
	first, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent:   parent,
		Query:    &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
		PageSize: 2,
	})
	require.NoError(t, err)
	require.Len(t, first.GetRows(), 2)
	require.NotEmpty(t, first.GetNextPageToken(), "more rows remain — token must be set")

	second, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent:    parent,
		Query:     &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
		PageSize:  2,
		PageToken: first.GetNextPageToken(),
	})
	require.NoError(t, err)
	require.Len(t, second.GetRows(), 2)

	third, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent:    parent,
		Query:     &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
		PageSize:  2,
		PageToken: second.GetNextPageToken(),
	})
	require.NoError(t, err)
	require.Len(t, third.GetRows(), 1)
	assert.Empty(t, third.GetNextPageToken(), "last page must clear the token")
}

func TestE2E_QueryDashboardData_BadPageToken_InvalidArgument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-bad-token")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent:    "organizations/" + fx.owner.Slug + "/spaces/" + fx.alphaSpace.Slug,
		Query:     &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
		PageToken: "not-a-number",
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}

// ============================================================================
// Validation rejections
// ============================================================================

func TestE2E_QueryDashboardData_WrongResourceType_Unimplemented(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-wrong-type")

	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent: "organizations/" + fx.owner.Slug,
		Query:  &apiv1.ResourceQuery{ResourceType: "pivox.iam/Member"},
	})
	requireGRPCCode(t, err, codes.Unimplemented)
}

func TestE2E_QueryDashboardData_MissingQuery_InvalidArgument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-no-query")

	// buf.validate.field.required = true on the request's query field
	// rejects this at the validation interceptor before our handler
	// runs — InvalidArgument is what the customer sees regardless of
	// whether the buf-validate or handler-side check fires.
	client := apiv1.NewDashboardsClient(h.Conn())
	_, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent: "organizations/" + fx.owner.Slug,
	})
	requireGRPCCode(t, err, codes.InvalidArgument)
}
