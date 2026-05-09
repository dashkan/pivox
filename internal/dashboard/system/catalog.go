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

// Package system holds the catalog of org-level SYSTEM_MANAGED
// dashboards. Each entry is a (ID, Build) pair: the ID is the
// trailing segment of the dashboard's resource name, and Build
// produces the populated Dashboard for a given org slug.
//
// The catalog is an explicit slice declared in this file, not an
// init()-time registry. This is a deliberate departure from
// internal/dashboard/templates: that package's registrations live
// in many resource packages and must be discovered at boot time;
// the system catalog is small, owned in one place, and benefits
// from a stable, source-readable ordering. Adding a new system
// dashboard is a one-line edit here.
//
// Phase 4's Dashboards.ListDashboards handler iterates All() at
// org parent and emits a Dashboard for every entry (subject to
// permission filtering); Get(id) drives Dashboards.GetDashboard
// at org parent.
package system

import (
	"slices"

	"github.com/dashkan/pivox/internal/dashboard/system/library"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// Entry is one system dashboard in the catalog.
type Entry struct {
	// ID is the trailing segment of the dashboard's resource name
	// (e.g. "library" → organizations/{org}/dashboards/library).
	// IDs are stable wire identifiers and MUST NOT be renamed once
	// shipped.
	ID string

	// Build returns the populated Dashboard for orgName. orgName is
	// the org's stable slug; Build is responsible for embedding it
	// in Dashboard.name and (where relevant) in any per-widget
	// ResourceQuery scope.
	Build func(orgName string) *apiv1.Dashboard
}

// catalog is the canonical, ordered list of system dashboards. The
// order here drives ListDashboards response order at org parent —
// keep entries in the order operators expect to see them in the
// dashboard switcher UI.
var catalog = []Entry{
	{ID: library.ID, Build: library.Build},
	// Future system dashboards land here in their intended display
	// order. Examples (not yet built): activity, members.
}

// All returns the catalog as a defensive copy — caller mutations
// to the returned slice do not affect the underlying catalog. The
// Entry values themselves are not deep-cloned (Entry.Build is a
// function pointer, sharing it is the intended semantics).
func All() []Entry {
	return slices.Clone(catalog)
}

// Get returns the catalog entry with the given id plus an "ok"
// flag. The caller handles the missing case (typically NotFound at
// the gRPC handler layer).
func Get(id string) (Entry, bool) {
	for _, e := range catalog {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}
