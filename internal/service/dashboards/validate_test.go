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

package dashboards

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/dashboard/system"
	"github.com/dashkan/pivox/internal/dashboard/templates"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// goodTemplate produces a template that passes validation: a real
// permission ID from the catalog, plus a non-nil Widget.
func goodTemplate() templates.Template {
	return templates.Template{
		Widget:         &apiv1.Widget{Id: "stub"},
		ListPermission: permission.AssetsAssetsRead,
	}
}

// goodEntry produces a catalog Entry whose Build returns a
// well-formed Dashboard for any orgName.
func goodEntry(id string) system.Entry {
	return system.Entry{
		ID: id,
		Build: func(orgName string) *apiv1.Dashboard {
			return &apiv1.Dashboard{
				Name: "organizations/" + orgName + "/dashboards/" + id,
			}
		},
	}
}

func TestValidateRegistries_Clean(t *testing.T) {
	tmpls := map[string]templates.Template{
		"pivox.assets/Asset": goodTemplate(),
	}
	catalog := []system.Entry{goodEntry("library")}

	err := validateRegistries(tmpls, catalog)
	assert.NoError(t, err)
}

func TestValidateRegistries_TemplatePermissionMissing(t *testing.T) {
	tmpls := map[string]templates.Template{
		"x.foo/Bar": {
			Widget:         &apiv1.Widget{},
			ListPermission: "", // empty — wiring bug
		},
	}

	err := validateRegistries(tmpls, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x.foo/Bar")
	assert.Contains(t, err.Error(), "ListPermission")
}

func TestValidateRegistries_TemplatePermissionNotInCatalog(t *testing.T) {
	tmpls := map[string]templates.Template{
		"x.foo/Bar": {
			Widget:         &apiv1.Widget{},
			ListPermission: "totally.fictional.permission",
		},
	}

	err := validateRegistries(tmpls, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "totally.fictional.permission")
	assert.Contains(t, err.Error(), "permission.All")
}

func TestValidateRegistries_TemplateWidgetNil(t *testing.T) {
	tmpls := map[string]templates.Template{
		"x.foo/Bar": {
			Widget:         nil, // wiring bug
			ListPermission: permission.AssetsAssetsRead,
		},
	}

	err := validateRegistries(tmpls, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x.foo/Bar")
	assert.Contains(t, err.Error(), "Widget")
}

func TestValidateRegistries_CatalogEntryBuildNil(t *testing.T) {
	catalog := []system.Entry{
		{ID: "broken", Build: nil},
	}

	err := validateRegistries(nil, catalog)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken")
	assert.Contains(t, err.Error(), "Build")
}

func TestValidateRegistries_CatalogEntryBuildReturnsNil(t *testing.T) {
	catalog := []system.Entry{
		{
			ID:    "broken",
			Build: func(string) *apiv1.Dashboard { return nil },
		},
	}

	err := validateRegistries(nil, catalog)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken")
}

func TestValidateRegistries_CatalogEntryWrongNameFormat(t *testing.T) {
	catalog := []system.Entry{
		{
			ID: "library",
			Build: func(orgName string) *apiv1.Dashboard {
				// Forgets to embed the ID; produces a name that
				// doesn't match the resource pattern.
				return &apiv1.Dashboard{
					Name: "organizations/" + orgName + "/dashboards/wrong",
				}
			},
		},
	}

	err := validateRegistries(nil, catalog)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library")
	assert.Contains(t, err.Error(), "name")
}

func TestValidateRegistries_CatalogEntryEmptyID(t *testing.T) {
	catalog := []system.Entry{
		{
			ID:    "",
			Build: goodEntry("anything").Build,
		},
	}

	err := validateRegistries(nil, catalog)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ID")
}

// Sanity: validateRegistries on the production registries (asset
// template + library catalog) returns nil. This catches a wiring
// bug at the package-init boundary even before the gRPC server
// constructor touches it.
func TestValidateRegistries_ProductionWiringIsClean(t *testing.T) {
	err := validateRegistries(templates.All(), system.All())
	require.NoError(t, err,
		"production templates + catalog must validate clean — "+
			"any failure here surfaces a wiring regression")
}
