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

// Package library provides the org-level "Asset Library" system
// dashboard. Build composes a SYSTEM_MANAGED Dashboard whose sole
// widget is the Asset CollectionWidget registered by the assets
// package's init() into the templates registry.
//
// The library package exports its ID and Build directly rather
// than registering through an init() hook. This is intentional:
// internal/dashboard/system/catalog.go owns the canonical list of
// system dashboards and imports each entry's package by name. That
// keeps the catalog explicit, readable, and stably ordered without
// relying on Go's init() ordering rules.
package library

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/dashboard/templates"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"

	// Side-effect import: the dashtemplate package's init()
	// registers the Asset template that Build composes. The leaf
	// dashtemplate package depends only on `templates`,
	// `permission`, and the generated proto types — NOT on the
	// heavy AssetsServer (apierr, audit, db, lro, …) — so the
	// dashboard graph stays light. The full AssetsServer is wired
	// separately in the binary.
	_ "github.com/dashkan/pivox/internal/service/assets/dashtemplate"
)

// ID is the trailing segment of the Library dashboard's resource
// name: `organizations/{org}/dashboards/library`.
const ID = "library"

// assetResourceType anchors the template lookup. Source:
// api/proto/pivox/assets/v1/asset.proto:191.
const assetResourceType = "pivox.assets/Asset"

// Build returns the Library Dashboard for orgName, fully populated
// and SYSTEM_MANAGED. Each call deep-clones the Asset Widget out of
// the templates registry so per-org Dashboards never share Widget
// pointers — mutation through the returned Dashboard cannot
// corrupt the registered template or another org's Dashboard.
//
// orgName is the org's stable slug (per resource-naming convention
// in internal/AGENTS.md); the caller upstream of Build is
// responsible for resolving any UUID/slug translation.
//
// Build panics if the Asset template is missing from the registry.
// At runtime the assets package's init() guarantees registration
// before any handler can call Build, so a missing template is a
// build/wiring bug — fail loud at the call site rather than ship
// a partial Dashboard.
func Build(orgName string) *apiv1.Dashboard {
	tmpl, ok := templates.Get(assetResourceType)
	if !ok {
		panic(fmt.Sprintf(
			"dashboard/system/library: template for %q is not registered — "+
				"is internal/service/assets imported into the binary?",
			assetResourceType,
		))
	}

	widget := proto.Clone(tmpl.Widget).(*apiv1.Widget)

	return &apiv1.Dashboard{
		Name:           "organizations/" + orgName + "/dashboards/" + ID,
		DisplayName:    "Library",
		Description:    "Browse every asset in the organization across all spaces.",
		ManagementMode: apiv1.Dashboard_SYSTEM_MANAGED,
		Layout: &apiv1.Dashboard_GridLayout{
			GridLayout: &apiv1.GridLayout{
				Columns: 12,
				Tiles: []*apiv1.Tile{
					{
						XPos:   0,
						YPos:   0,
						Width:  12,
						Height: 8,
						Widget: widget,
					},
				},
			},
		},
	}
}
