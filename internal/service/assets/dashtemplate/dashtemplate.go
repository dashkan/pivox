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

// Package dashtemplate registers the default dashboard widget for
// pivox.assets/Asset into the templates registry. It is a leaf
// sibling of the assets gRPC service (not the service itself) so
// the dashboard layer can pull in the registration without dragging
// in the full AssetsServer dependency graph (apierr, audit, db,
// lro, …). System dashboards (e.g. internal/dashboard/system/library)
// blank-import this package; the heavy AssetsServer is wired
// separately in the binary.
package dashtemplate

import (
	"github.com/dashkan/pivox/internal/dashboard/templates"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// ResourceType is the canonical AIP resource type for an asset,
// declared at api/proto/pivox/assets/v1/asset.proto:191. Exported
// so callers (system dashboards, tests) can reference the same
// constant rather than restating the literal.
const ResourceType = "pivox.assets/Asset"

// init registers the default Widget template for assets so the
// "add widget" picker can hand back a fully-populated CollectionWidget
// the moment a customer adds an Asset library to a dashboard.
//
// Defaults reflect the locked design:
//
//   - DisplayMode = CARD; supported_modes = {CARD, TABLE} so the
//     customer can toggle. LIST is not available.
//   - Default columns: display_name, media_type, state, size_bytes,
//     create_time. media_type / state / create_time are filterable;
//     display_name / size_bytes / create_time are sortable.
//   - Row actions: open_detail (push detail sheet), share, archive
//     (with confirmation).
//   - IconConfig: per-row thumbnail_url (set by QueryDashboardData
//     in Phase 5 from the asset's storage gateway), then per-row
//     icon (numeric Icon value derived from content_type), then a
//     static ICON_DOCUMENT fallback.
//   - EmptyState: prompts the user to upload assets.
func init() {
	templates.Register(ResourceType, templates.Template{
		Widget:         buildAssetWidget(),
		ListPermission: permission.AssetsAssetsRead,
	})
}

func buildAssetWidget() *apiv1.Widget {
	return &apiv1.Widget{
		Title: "Asset Library",
		Content: &apiv1.Widget_Collection{
			Collection: &apiv1.CollectionWidget{
				DataSource: &apiv1.DataSource{
					Source: &apiv1.DataSource_ResourceQuery{
						ResourceQuery: &apiv1.ResourceQuery{
							ResourceType: ResourceType,
						},
					},
				},
				DisplayMode: apiv1.CollectionWidget_CARD,
				SupportedModes: []apiv1.CollectionWidget_DisplayMode{
					apiv1.CollectionWidget_CARD,
					apiv1.CollectionWidget_TABLE,
				},
				Columns: []*apiv1.Column{
					{
						Field:       "display_name",
						DisplayName: "Name",
						Visible:     true,
						Sortable:    true,
						Filterable:  true,
					},
					{
						Field:       "media_type",
						DisplayName: "Type",
						Visible:     true,
						Filterable:  true,
					},
					{
						Field:       "state",
						DisplayName: "Status",
						Visible:     true,
						Filterable:  true,
					},
					{
						Field:       "size_bytes",
						DisplayName: "Size",
						Visible:     true,
						Sortable:    true,
					},
					{
						Field:       "create_time",
						DisplayName: "Added",
						Visible:     true,
						Sortable:    true,
						Filterable:  true,
					},
				},
				RowActions: []*apiv1.RowAction{
					{
						Key:   "open_detail",
						Label: "Open",
						Icon:  apiv1.Icon_ICON_INFO,
					},
					{
						Key:   "share",
						Label: "Share",
						Icon:  apiv1.Icon_ICON_SHARE,
					},
					{
						Key:                  "archive",
						Label:                "Archive",
						Icon:                 apiv1.Icon_ICON_TRASH,
						RequiresConfirmation: true,
					},
				},
				IconConfig: &apiv1.IconConfig{
					SourceField:  "thumbnail_url",
					IconField:    "icon",
					FallbackIcon: apiv1.Icon_ICON_DOCUMENT,
					Size:         apiv1.IconConfig_MEDIUM,
				},
				EmptyState: &apiv1.EmptyState{
					Title:    "No assets yet",
					Subtitle: "Upload or import files to see them here.",
					Icon:     apiv1.Icon_ICON_PHOTO,
					PrimaryAction: &apiv1.RowAction{
						Key:   "upload_assets",
						Label: "Upload",
						Icon:  apiv1.Icon_ICON_UPLOAD,
					},
				},
			},
		},
	}
}
