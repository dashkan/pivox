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

package assets_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/dashboard/templates"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"

	// The asset package's init() registers the Asset dashboard
	// template. The blank import pulls it in even though this test
	// file does not reference any exported symbol from the package.
	_ "github.com/dashkan/pivox/internal/service/assets"
)

// assetResourceType is the canonical AIP resource type for assets.
// Source: api/proto/pivox/assets/v1/asset.proto:191.
const assetResourceType = "pivox.assets/Asset"

func TestAssetDashboardTemplate_Registered(t *testing.T) {
	tmpl, ok := templates.Get(assetResourceType)
	require.True(t, ok, "Asset template must be registered at init() under %q", assetResourceType)

	require.NotNil(t, tmpl.Widget, "Template.Widget must be populated")
	require.NotNil(t, tmpl.Widget.GetCollection(),
		"Asset template must use a CollectionWidget (TABLE/CARD/LIST modes), not a Statistic/Chart/etc.")
}

func TestAssetDashboardTemplate_ListPermissionIsCanonical(t *testing.T) {
	tmpl, ok := templates.Get(assetResourceType)
	require.True(t, ok)

	require.Equal(t, permission.AssetsAssetsRead, tmpl.ListPermission,
		"ListPermission must use the catalog constant, not a free-form string")
	require.True(t,
		slices.Contains(permission.All, tmpl.ListPermission),
		"ListPermission %q must be present in permission.All — server boot will reject otherwise",
		tmpl.ListPermission,
	)
}

func TestAssetDashboardTemplate_DisplayModesMatchLockedDesign(t *testing.T) {
	tmpl, _ := templates.Get(assetResourceType)
	coll := tmpl.Widget.GetCollection()
	require.NotNil(t, coll)

	assert.Equal(t, apiv1.CollectionWidget_CARD, coll.GetDisplayMode(),
		"locked design: Asset library defaults to CARD")

	supported := coll.GetSupportedModes()
	require.NotEmpty(t, supported, "supported_modes must be non-empty so the customer can toggle")
	assert.Contains(t, supported, apiv1.CollectionWidget_TABLE)
	assert.Contains(t, supported, apiv1.CollectionWidget_CARD)
	assert.NotContains(t, supported, apiv1.CollectionWidget_DISPLAY_MODE_UNSPECIFIED,
		"UNSPECIFIED must never appear in supported_modes")
}

func TestAssetDashboardTemplate_DataSourceTargetsAsset(t *testing.T) {
	tmpl, _ := templates.Get(assetResourceType)
	coll := tmpl.Widget.GetCollection()

	rq := coll.GetDataSource().GetResourceQuery()
	require.NotNil(t, rq, "Asset library must use a ResourceQuery data source")
	assert.Equal(t, assetResourceType, rq.GetResourceType(),
		"ResourceQuery.resource_type must match the registry key")
}

func TestAssetDashboardTemplate_RequiredColumnsPresent(t *testing.T) {
	tmpl, _ := templates.Get(assetResourceType)
	coll := tmpl.Widget.GetCollection()

	require.NotEmpty(t, coll.GetColumns(), "default columns must be defined")

	fields := map[string]*apiv1.Column{}
	for _, c := range coll.GetColumns() {
		assert.NotEmpty(t, c.GetField(), "Column.field is required")
		fields[c.GetField()] = c
	}

	for _, required := range []string{"display_name", "media_type", "create_time"} {
		assert.Contains(t, fields, required, "expected default column %q", required)
	}
}

func TestAssetDashboardTemplate_RowActionKeysAreSnakeCase(t *testing.T) {
	tmpl, _ := templates.Get(assetResourceType)
	coll := tmpl.Widget.GetCollection()

	require.NotEmpty(t, coll.GetRowActions(), "default row actions must be defined")

	keys := map[string]bool{}
	for _, a := range coll.GetRowActions() {
		key := a.GetKey()
		assert.Regexp(t, `^[a-z][a-z0-9_]*$`, key,
			"RowAction.key must match the snake-case pattern enforced by buf.validate")
		keys[key] = true
	}
	assert.True(t, keys["open_detail"], "expected an open_detail row action")
}

func TestAssetDashboardTemplate_IconConfigPopulated(t *testing.T) {
	tmpl, _ := templates.Get(assetResourceType)
	coll := tmpl.Widget.GetCollection()

	icon := coll.GetIconConfig()
	require.NotNil(t, icon, "IconConfig must be populated")

	assert.NotEmpty(t, icon.GetSourceField(),
		"source_field names the row column carrying the thumbnail URL synthesized by QueryDashboardData (Phase 5)")
	assert.NotEmpty(t, icon.GetIconField(),
		"icon_field names the row column carrying the numeric Icon enum value")
	assert.NotEqual(t, apiv1.Icon_ICON_UNSPECIFIED, icon.GetFallbackIcon(),
		"fallback_icon must resolve to a real Icon when every other derivation path is empty")
}

func TestAssetDashboardTemplate_EmptyStatePopulated(t *testing.T) {
	tmpl, _ := templates.Get(assetResourceType)
	coll := tmpl.Widget.GetCollection()

	es := coll.GetEmptyState()
	require.NotNil(t, es, "EmptyState must be populated to avoid a blank widget on first use")

	assert.NotEmpty(t, es.GetTitle())

	pa := es.GetPrimaryAction()
	require.NotNil(t, pa, "EmptyState.primary_action drives the call-to-action")
	assert.Regexp(t, `^[a-z][a-z0-9_]*$`, pa.GetKey(),
		"primary_action.key must match the snake-case pattern (it is a RowAction)")
}
