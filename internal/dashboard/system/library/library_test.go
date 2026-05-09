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

package library_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/dashboard/system/library"
	"github.com/dashkan/pivox/internal/dashboard/templates"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

const (
	// orgName fixture for Build calls. The Library.Build contract
	// is "produce a Dashboard whose name embeds this org slug" —
	// the value itself is opaque to Build.
	testOrgName = "acme"

	// assetResourceType matches asset.proto:191 — anchor for the
	// composition assertion.
	assetResourceType = "pivox.assets/Asset"
)

func TestLibrary_ID(t *testing.T) {
	assert.Equal(t, "library", library.ID,
		"the catalog uses library.ID as the trailing dashboard segment in resource names")
}

func TestLibrary_BuildResourceName(t *testing.T) {
	d := library.Build(testOrgName)
	require.NotNil(t, d)

	assert.Equal(t,
		"organizations/"+testOrgName+"/dashboards/"+library.ID,
		d.GetName(),
		"Build must populate the org-scoped resource name in full",
	)
}

func TestLibrary_BuildIsSystemManaged(t *testing.T) {
	d := library.Build(testOrgName)
	assert.Equal(t, apiv1.Dashboard_SYSTEM_MANAGED, d.GetManagementMode(),
		"library is server-curated; mutations are rejected by Phase 4 handler")
}

func TestLibrary_BuildHasRequiredDisplayName(t *testing.T) {
	d := library.Build(testOrgName)
	// Per dashboards.proto:84-91, display_name is REQUIRED with
	// min_len=1 and max_len=128.
	displayName := d.GetDisplayName()
	require.NotEmpty(t, displayName, "Dashboard.display_name is REQUIRED")
	assert.LessOrEqual(t, len(displayName), 128, "display_name must respect proto max_len=128")
}

func TestLibrary_BuildContainsAssetCollectionWidget(t *testing.T) {
	d := library.Build(testOrgName)

	grid := d.GetGridLayout()
	require.NotNil(t, grid, "library uses a GridLayout in v1")
	require.Len(t, grid.GetTiles(), 1, "library has exactly one widget in v1")

	tile := grid.GetTiles()[0]
	assert.GreaterOrEqual(t, tile.GetWidth(), int32(1),
		"Tile.width must be ≥1 per widgets.proto buf.validate")
	assert.GreaterOrEqual(t, tile.GetHeight(), int32(1),
		"Tile.height must be ≥1 per widgets.proto buf.validate")

	coll := tile.GetWidget().GetCollection()
	require.NotNil(t, coll, "library widget is a CollectionWidget composed from the Asset template")

	rq := coll.GetDataSource().GetResourceQuery()
	require.NotNil(t, rq, "ResourceQuery must be populated so Phase 5 QueryDashboardData can dispatch")
	assert.Equal(t, assetResourceType, rq.GetResourceType())
}

func TestLibrary_BuildClonesTemplatePerCall(t *testing.T) {
	d1 := library.Build(testOrgName)
	d2 := library.Build("widgetco")

	// Different per-org Builds must not share Widget pointers —
	// otherwise mutation through one Dashboard (or marshalling
	// pipeline) corrupts the other.
	w1 := d1.GetGridLayout().GetTiles()[0].GetWidget()
	w2 := d2.GetGridLayout().GetTiles()[0].GetWidget()
	require.NotSame(t, w1, w2,
		"Build must deep-clone the template's Widget per call; "+
			"shared pointers across orgs are a corruption vector")

	// Resource name embeds the orgName, confirming the rest of
	// the Dashboard is also per-call.
	assert.NotEqual(t, d1.GetName(), d2.GetName())
}

func TestLibrary_BuildDoesNotMutateRegisteredTemplate(t *testing.T) {
	tmpl, ok := templates.Get(assetResourceType)
	require.True(t, ok)
	require.NotNil(t, tmpl.Widget)

	// Snapshot the registered Widget BEFORE Build runs.
	before := proto.Clone(tmpl.Widget).(*apiv1.Widget)

	// Build twice with different org slugs — both must succeed.
	require.NotNil(t, library.Build(testOrgName))
	require.NotNil(t, library.Build("another-org"))

	// Re-fetch the registered Widget: it must be byte-equal to the
	// pre-Build snapshot. If Build had mutated the shared template
	// (e.g. forgotten to proto.Clone before tweaking the Widget),
	// this assertion would catch it.
	tmplAfter, _ := templates.Get(assetResourceType)
	require.True(t, proto.Equal(before, tmplAfter.Widget),
		"Build must not mutate the registered Widget — even via shared sub-message pointers")
}
