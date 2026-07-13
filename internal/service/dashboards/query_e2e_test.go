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
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	db "github.com/dashkan/pivox/internal/db/generated"
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

// ============================================================================
// sqlc query shape (Phase 6c)
// ============================================================================

// TestE2E_ListAssetsByOrg_PopulatesVersionAndEndpointColumns pins the
// shape of ListAssetsByOrg's row after the Phase 6c LATERAL latest-
// version + storage_endpoints LEFT JOIN. The synthesizer's URL
// composer (lands in 6c.2) reads `LatestVersionNumber`,
// `LatestVersionMimeType`, and `EndpointSlug` from this row; this test
// verifies they're populated correctly across the three relevant
// branches:
//
//   - asset with v1 + endpoint  → all three populated.
//   - asset with v1, no endpoint → version columns populated,
//     EndpointSlug pgtype.Text Valid=false.
//   - asset with no version yet  → COALESCE sentinels (0, "") on the
//     version columns; EndpointSlug Valid=true if endpoint is bound.
//
// Why the COALESCE sentinels rather than pgtype.Int4 / pgtype.Text:
// sqlc v1.31 mistypes LEFT-JOIN-derived nullable columns as NOT NULL
// (see internal/db/queries/assets.sql for details). The sentinels
// (0 for version_number, "" for mime_type) are the workaround the
// synthesizer's composer reads as "no version exists."
func TestE2E_ListAssetsByOrg_PopulatesVersionAndEndpointColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-row-shape")
	ctx := context.Background()

	// Seed a gateway + endpoint for the "with endpoint" cases.
	gwID := uuid.New()
	endpointID := uuid.New()
	const endpointSlug = "primary"
	_, err := h.Pool.Exec(ctx, `
		INSERT INTO storage_gateways (id, org_id, name, display_name, registration_token, hostname, state)
		VALUES ($1, $2, 'gw-test', 'Test Gateway', 'tok-test', 'gw-test.storage.pivox.app', 'ACTIVE')`,
		gwID, fx.owner.Row.ID,
	)
	require.NoError(t, err)
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO storage_endpoints (id, gateway_id, name, display_name, configuration, cache_enabled, cache_max_size_gb, cache_eviction)
		VALUES ($1, $2, $3, 'Primary', '{"type":"s3"}'::jsonb, false, 0, 'LRU')`,
		endpointID, gwID, endpointSlug,
	)
	require.NoError(t, err)

	parent := "organizations/" + fx.owner.Slug + "/spaces/" + fx.alphaSpace.Slug

	// Case A: asset + v1 + endpoint → all three populated.
	withEndpointName := createPlaceholderAsset(t, h.Conn(), parent, "Asset With Endpoint")
	withEndpointSlug := withEndpointName[strings.LastIndex(withEndpointName, "/")+1:]
	var withEndpointID uuid.UUID
	require.NoError(t, h.Pool.QueryRow(ctx, `
		UPDATE assets
		   SET endpoint_id = $1, media_type = 'IMAGE'::asset_media_type, content_type = 'image/png', state = 'ACTIVE'
		 WHERE space_id = $2 AND name = $3 RETURNING id`,
		endpointID, fx.alphaSpace.Row.ID, withEndpointSlug,
	).Scan(&withEndpointID))
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO asset_versions (asset_id, version_number, mime_type, storage_key, size_bytes)
		VALUES ($1, 1, 'image/png', 'meridian-broad/alpha/assets/x/v1/original.png', 0)`,
		withEndpointID,
	)
	require.NoError(t, err)

	// Case B: asset + v1 + NO endpoint → EndpointSlug Valid=false.
	noEndpointName := createPlaceholderAsset(t, h.Conn(), parent, "Asset No Endpoint")
	noEndpointSlug := noEndpointName[strings.LastIndex(noEndpointName, "/")+1:]
	var noEndpointID uuid.UUID
	require.NoError(t, h.Pool.QueryRow(ctx, `
		UPDATE assets
		   SET media_type = 'IMAGE'::asset_media_type, content_type = 'image/jpeg', state = 'ACTIVE'
		 WHERE space_id = $1 AND name = $2 RETURNING id`,
		fx.alphaSpace.Row.ID, noEndpointSlug,
	).Scan(&noEndpointID))
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO asset_versions (asset_id, version_number, mime_type, storage_key, size_bytes)
		VALUES ($1, 1, 'image/jpeg', 'meridian-broad/alpha/assets/y/v1/original.jpg', 0)`,
		noEndpointID,
	)
	require.NoError(t, err)

	// Case C: asset PLACEHOLDER, no v1 row → version sentinels (0, "").
	noVersionName := createPlaceholderAsset(t, h.Conn(), parent, "Asset No Version")
	noVersionSlug := noVersionName[strings.LastIndex(noVersionName, "/")+1:]
	var noVersionID uuid.UUID
	require.NoError(t, h.Pool.QueryRow(ctx, `
		UPDATE assets SET endpoint_id = $1 WHERE space_id = $2 AND name = $3 RETURNING id`,
		endpointID, fx.alphaSpace.Row.ID, noVersionSlug,
	).Scan(&noVersionID))

	rows, err := h.Queries.ListAssetsByOrg(ctx, db.ListAssetsByOrgParams{
		OrgID:  fx.owner.Row.ID,
		Limit:  100,
		Offset: 0,
	})
	require.NoError(t, err)

	byID := map[uuid.UUID]db.ListAssetsByOrgRow{}
	for _, r := range rows {
		byID[r.Asset.ID] = r
	}

	// Case A.
	rA, ok := byID[withEndpointID]
	require.True(t, ok, "Case A asset must surface in ListAssetsByOrg")
	assert.Equal(t, int32(1), rA.LatestVersionNumber, "Case A: version_number from latest asset_versions row")
	assert.Equal(t, "image/png", rA.LatestVersionMimeType, "Case A: mime_type from latest version")
	assert.Equal(t, pgtype.Text{String: endpointSlug, Valid: true}, rA.EndpointSlug, "Case A: endpoint_slug populated via JOIN")

	// Case B.
	rB, ok := byID[noEndpointID]
	require.True(t, ok)
	assert.Equal(t, int32(1), rB.LatestVersionNumber)
	assert.Equal(t, "image/jpeg", rB.LatestVersionMimeType)
	assert.False(t, rB.EndpointSlug.Valid, "Case B: endpoint_slug NULL when assets.endpoint_id is NULL")

	// Case C.
	rC, ok := byID[noVersionID]
	require.True(t, ok)
	assert.Equal(t, int32(0), rC.LatestVersionNumber, "Case C: COALESCE sentinel 0 when no asset_versions row exists")
	assert.Equal(t, "", rC.LatestVersionMimeType, "Case C: COALESCE sentinel '' when no asset_versions row exists")
	assert.True(t, rC.EndpointSlug.Valid, "Case C: endpoint_slug populated even with no version (independent JOIN)")
}

// ============================================================================
// thumbnail_url synthesis (Phase 6c)
// ============================================================================

// promoteToIngestedAsset takes a placeholder asset (created via the
// real CreateAsset RPC) and DB-promotes it to a fully-ingested asset
// bound to a storage endpoint, with a v1 asset_versions row. Used by
// the thumbnail-URL e2e test as a stand-in for the InitiateUpload +
// rendition-pipeline flow that hasn't shipped yet (#83 + the rendition
// pipeline land separately).
//
// Returns the asset's UUID. The storage_key written to asset_versions
// follows the locked Phase 6c layout: `{org}/{space}/assets/{id}/v1/
// original.{ext}` per docs/assets.md:430-435 + the {org}/{space}/
// prefix from #83.
func promoteToIngestedAsset(
	t *testing.T,
	h *grpcharness.Harness,
	spaceID uuid.UUID,
	orgSlug, spaceSlug, assetSlug string,
	endpointID uuid.UUID,
	mediaType, contentType, ext string,
) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var assetID uuid.UUID
	err := h.Pool.QueryRow(ctx, `
		UPDATE assets
		   SET endpoint_id = $1,
		       media_type = $2::asset_media_type,
		       content_type = $3,
		       state = 'ACTIVE'
		 WHERE space_id = $4 AND name = $5
		 RETURNING id`,
		endpointID, mediaType, contentType, spaceID, assetSlug,
	).Scan(&assetID)
	require.NoError(t, err, "promote asset to ingested state")

	storageKey := fmt.Sprintf("%s/%s/assets/%s/v1/original.%s",
		orgSlug, spaceSlug, assetID, ext)
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO asset_versions (asset_id, version_number, mime_type, storage_key, size_bytes)
		VALUES ($1, 1, $2, $3, 0)`,
		assetID, contentType, storageKey,
	)
	require.NoError(t, err, "seed v1 asset_versions row")
	return assetID
}

// seedThumbnailGateway inserts a storage_gateway + storage_endpoint
// pair directly via SQL. Returns (gatewayHostname, endpointSlug,
// endpointID). Hostname matches the dev seed flip
// (10_storage_gateways.sql) so the e2e assertion shape parallels what
// the dev Library would produce against rustfs through the tunnel →
// ingress /files/ → agent. Inline rather than via a fixture because
// the thumbnail-URL test is the only consumer; promote to
// internal/testutil/fixtures when a second consumer arrives.
func seedThumbnailGateway(
	t *testing.T,
	h *grpcharness.Harness,
	orgID uuid.UUID,
) (gwHostname, endpointSlug string, endpointID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	gwID := uuid.New()
	endpointID = uuid.New()
	gwHostname = "pivox.example"
	endpointSlug = "primary"

	_, err := h.Pool.Exec(ctx, `
		INSERT INTO storage_gateways (id, org_id, name, display_name, registration_token, hostname, state)
		VALUES ($1, $2, 'gw-test', 'Test Gateway', 'tok-test', $3, 'ACTIVE')`,
		gwID, orgID, gwHostname,
	)
	require.NoError(t, err, "seed storage_gateway")

	_, err = h.Pool.Exec(ctx, `
		INSERT INTO storage_endpoints (id, gateway_id, name, display_name, configuration, cache_enabled, cache_max_size_gb, cache_eviction)
		VALUES ($1, $2, $3, 'Primary', '{"type":"s3"}'::jsonb, false, 0, 'LRU')`,
		endpointID, gwID, endpointSlug,
	)
	require.NoError(t, err, "seed storage_endpoint")
	return gwHostname, endpointSlug, endpointID
}

// lastSegment extracts the trailing path segment from an AIP resource
// name. CreateAsset returns "organizations/{org}/spaces/{space}/assets/
// {asset}"; the asset-row's `name` column is the leaf slug only.
func lastSegment(name string) string {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return name
	}
	return name[idx+1:]
}

// TestE2E_QueryDashboardData_ThumbnailURL_Synthesis exercises the
// Phase 6c.2 thumbnail-URL composer end-to-end through
// QueryDashboardData. Output shape:
//
//	https://{gw.hostname}/files/{endpointSlug}/{org}/{space}/assets/{id}/v{n}/thumb_md.webp
//
// Three rows in one org-scope query so per-row branching covers:
//
//   - IMAGE asset in alpha space → URL containing alpha's slug.
//   - IMAGE asset in beta space  → URL containing beta's slug
//     (verifies the org-scope synthesizer passes r.SpaceSlug per-row,
//     not a single fixed space slug — pins the load-bearing
//     `assetRowToStruct(viewFromOrgRow(r), orgSlug, r.SpaceSlug)`
//     call site against future refactors).
//   - VIDEO asset → empty thumbnail_url (renderer's
//     IconConfigResolver chain falls through to iconField).
//
// The /files/ prefix is permanent — in dev the ingress routes it to the
// storage agent, in prod the gateway's edge proxy serves it at the
// public hostname. Same prefix in both, different host (DECISION 1).
func TestE2E_QueryDashboardData_ThumbnailURL_Synthesis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newQueryHarness(t)
	fx := seedQueryFixture(t, h, "qd-thumb")

	gwHostname, endpointSlug, endpointID := seedThumbnailGateway(t, h, fx.owner.Row.ID)

	// IMAGE asset in alpha space → URL with alpha's space slug.
	parentAlpha := "organizations/" + fx.owner.Slug + "/spaces/" + fx.alphaSpace.Slug
	imgAlphaName := createPlaceholderAsset(t, h.Conn(), parentAlpha, "Image Alpha")
	imgAlphaID := promoteToIngestedAsset(t, h,
		fx.alphaSpace.Row.ID, fx.owner.Slug, fx.alphaSpace.Slug,
		lastSegment(imgAlphaName), endpointID,
		"IMAGE", "image/png", "png",
	)

	// IMAGE asset in beta space → URL with BETA's slug. If
	// queryAssetsAtOrg ever stops passing r.SpaceSlug per-row and
	// switches to a fixed slug, this assertion fires red.
	parentBeta := "organizations/" + fx.owner.Slug + "/spaces/" + fx.betaSpace.Slug
	imgBetaName := createPlaceholderAsset(t, h.Conn(), parentBeta, "Image Beta")
	imgBetaID := promoteToIngestedAsset(t, h,
		fx.betaSpace.Row.ID, fx.owner.Slug, fx.betaSpace.Slug,
		lastSegment(imgBetaName), endpointID,
		"IMAGE", "image/jpeg", "jpg",
	)

	// VIDEO asset in alpha → empty (non-image media type).
	vidName := createPlaceholderAsset(t, h.Conn(), parentAlpha, "Video Asset")
	_ = promoteToIngestedAsset(t, h,
		fx.alphaSpace.Row.ID, fx.owner.Slug, fx.alphaSpace.Slug,
		lastSegment(vidName), endpointID,
		"VIDEO", "video/mp4", "mp4",
	)

	client := apiv1.NewDashboardsClient(h.Conn())
	resp, err := client.QueryDashboardData(context.Background(), &apiv1.QueryDashboardDataRequest{
		Parent: "organizations/" + fx.owner.Slug,
		Query:  &apiv1.ResourceQuery{ResourceType: "pivox.assets/Asset"},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRows(), 3)

	urlByDisplay := map[string]string{}
	for _, row := range resp.GetRows() {
		fields := row.GetFields()
		dn := fields["display_name"].GetStringValue()
		urlByDisplay[dn] = fields["thumbnail_url"].GetStringValue()
	}

	// Alpha row carries an alpha-stamped URL.
	expectedAlpha := fmt.Sprintf(
		"https://%s/files/%s/%s/%s/assets/%s/v1/thumb_md.webp",
		gwHostname, endpointSlug, fx.owner.Slug, fx.alphaSpace.Slug, imgAlphaID,
	)
	assert.Equal(t, expectedAlpha, urlByDisplay["Image Alpha"],
		"IMAGE asset in alpha must carry alpha-stamped URL")

	// Beta row carries a beta-stamped URL — proves per-row SpaceSlug
	// propagation through the org-scope synthesizer.
	expectedBeta := fmt.Sprintf(
		"https://%s/files/%s/%s/%s/assets/%s/v1/thumb_md.webp",
		gwHostname, endpointSlug, fx.owner.Slug, fx.betaSpace.Slug, imgBetaID,
	)
	assert.Equal(t, expectedBeta, urlByDisplay["Image Beta"],
		"IMAGE asset in beta must carry beta-stamped URL (per-row SpaceSlug)")

	// VIDEO row falls through to the iconField path.
	assert.Equal(t, "", urlByDisplay["Video Asset"],
		"non-image asset must leave thumbnail_url empty")
}
