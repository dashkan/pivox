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
	"fmt"
	"slices"

	"github.com/dashkan/pivox/internal/dashboard/system"
	"github.com/dashkan/pivox/internal/dashboard/templates"
	"github.com/dashkan/pivox/internal/permission"
)

// smokeOrgName is the fixture orgName fed to each catalog Entry's
// Build at validation time. The value itself is opaque — it just
// has to render through the resource-name format check.
const smokeOrgName = "smoke"

// validateRegistries reports the first violation in the templates
// registry or the system catalog, or nil if every entry is
// well-formed.
//
// Server boot calls this before the gRPC server starts accepting
// traffic. A non-nil return is a fatal startup failure: the
// constructor turns it into a panic so operators see a loud crash
// rather than a silently-broken handler.
//
// Violations:
//   - A template's ListPermission is empty.
//   - A template's ListPermission is not present in permission.All.
//   - A template's Widget is nil.
//   - A catalog Entry has an empty ID.
//   - A catalog Entry's Build is nil.
//   - A catalog Entry's Build returns nil.
//   - A catalog Entry's Build returns a Dashboard whose name does
//     not embed the entry's ID at the expected position.
func validateRegistries(tmpls map[string]templates.Template, catalog []system.Entry) error {
	for resourceType, tmpl := range tmpls {
		if err := validateTemplate(resourceType, tmpl); err != nil {
			return err
		}
	}
	for i, entry := range catalog {
		if err := validateEntry(i, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateTemplate(resourceType string, tmpl templates.Template) error {
	if tmpl.Widget == nil {
		return fmt.Errorf("template %q: Widget must not be nil", resourceType)
	}
	if tmpl.ListPermission == "" {
		return fmt.Errorf("template %q: ListPermission must not be empty", resourceType)
	}
	if !slices.Contains(permission.All, tmpl.ListPermission) {
		return fmt.Errorf(
			"template %q: ListPermission %q is not present in permission.All — "+
				"add it to internal/permission/permissions.yaml or fix the template",
			resourceType, tmpl.ListPermission,
		)
	}
	return nil
}

func validateEntry(index int, e system.Entry) error {
	if e.ID == "" {
		return fmt.Errorf("system.Entry[%d]: ID must not be empty", index)
	}
	if e.Build == nil {
		return fmt.Errorf("system.Entry[%d] (id=%q): Build must not be nil", index, e.ID)
	}

	d := e.Build(smokeOrgName)
	if d == nil {
		return fmt.Errorf("system.Entry[%d] (id=%q): Build returned nil Dashboard", index, e.ID)
	}

	// The Library catalog only ships org-scoped dashboards in v1.
	// Build's contract is to embed the entry's ID at the trailing
	// segment so the resource name matches the dashboards.proto
	// pattern `organizations/{org}/dashboards/{dashboard}`.
	want := "organizations/" + smokeOrgName + "/dashboards/" + e.ID
	if got := d.GetName(); got != want {
		return fmt.Errorf(
			"system.Entry[%d] (id=%q): Build produced name %q, want %q",
			index, e.ID, got, want,
		)
	}

	return nil
}
