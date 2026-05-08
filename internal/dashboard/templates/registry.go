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

// Package templates holds per-resource Widget templates that drive
// the system-curated dashboard catalog. Each resource that can
// surface in a dashboard registers a Template at init() time. The
// central registry is consumed by:
//
//   - The "add widget" picker, which calls Get(resource_type) to
//     retrieve a fully-populated default Widget.
//   - Server boot, which iterates All() and validates that every
//     Template's ListPermission is a real permission ID before the
//     gRPC server starts accepting traffic.
//
// Registration is package-init-only by design — the registry is a
// plain map with no lock. Calling Register concurrently with Get or
// All is a programming error and is not supported. Re-registration
// of the same resource_type panics; we catch the bug at init()
// rather than risk last-wins drift at runtime.
package templates

import (
	"fmt"
	"maps"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// Template describes how a resource type renders inside a
// CollectionWidget by default, plus the IAM permission a caller
// must hold to list rows of this type.
type Template struct {
	// Widget is the fully-populated default Widget for this resource
	// — display_mode, supported_modes, columns, row_actions,
	// icon_config, empty_state — set the way the customer would see
	// it the moment the resource is added to a dashboard.
	Widget *apiv1.Widget

	// ListPermission is the permission ID that gates listing rows
	// of this resource (e.g. "assets.assets.read"). The registry
	// itself does not validate this; server boot does, against the
	// permission catalog defined in internal/permission.
	ListPermission string
}

// registry is the package-init-only catalog. Package-level state
// is safe because Register is only called from init() functions in
// resource-handler packages; reads (Get / All) happen after every
// init() has completed.
var registry = map[string]Template{}

// Register installs a template for resourceType. Calling Register
// twice with the same resourceType panics — re-registration is a
// programming error, caught loudly at init() rather than producing
// last-wins drift at runtime.
func Register(resourceType string, t Template) {
	if _, exists := registry[resourceType]; exists {
		panic(fmt.Sprintf(
			"dashboard/templates: duplicate registration for resource_type %q",
			resourceType,
		))
	}
	registry[resourceType] = t
}

// Get returns the template for resourceType plus an "ok" flag,
// matching the standard Go map-lookup shape. The caller handles
// the missing case (typically a "no template for this resource"
// error in the picker UI).
func Get(resourceType string) (Template, bool) {
	t, ok := registry[resourceType]
	return t, ok
}

// All returns every registered template, indexed by resource_type.
// The returned map is a defensive copy — adding or deleting keys
// in the result does not affect the registry — but the copy is
// shallow: every Template still carries its original `*Widget`
// pointer. Callers that intend to mutate the Widget MUST take
// their own deep copy (e.g. via `proto.Clone`); the read-mostly
// callers (server-boot validation, picker UI) iterate without
// mutating, so a shallow copy is the right cost trade.
//
// Iteration order is not guaranteed; callers that need stable
// ordering must sort by key.
func All() map[string]Template {
	out := make(map[string]Template, len(registry))
	maps.Copy(out, registry)
	return out
}
