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
	"context"
	"strings"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/dashboard/system"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// scopeKind tags the parsed shape of a Dashboards parent or name
// path so handlers branch off a single parse instead of repeating
// shape checks.
type scopeKind int

const (
	// scopeMalformed indicates the path doesn't match either the
	// org-scoped or space-scoped pattern.
	scopeMalformed scopeKind = iota
	// scopeOrg is the org-scoped pattern: the dashboard's parent
	// is an organization. SYSTEM_MANAGED dashboards live here.
	scopeOrg
	// scopeSpace is the space-scoped pattern: the dashboard's
	// parent is a space. USER_MANAGED dashboards live here
	// (handled in Phase 4b).
	scopeSpace
)

// parseParent classifies a ListDashboards parent path. It accepts:
//
//   - `organizations/{org}` → scopeOrg, orgSlug=org
//   - `organizations/{org}/spaces/{space}` → scopeSpace,
//     orgSlug=org, spaceSlug=space
//
// Any other shape returns scopeMalformed. Empty slugs are rejected.
func parseParent(parent string) (kind scopeKind, orgSlug, spaceSlug string) {
	parts := strings.Split(parent, "/")
	switch {
	case len(parts) == 2 && parts[0] == "organizations" && parts[1] != "":
		return scopeOrg, parts[1], ""
	case len(parts) == 4 && parts[0] == "organizations" && parts[1] != "" &&
		parts[2] == "spaces" && parts[3] != "":
		return scopeSpace, parts[1], parts[3]
	default:
		return scopeMalformed, "", ""
	}
}

// parseDashboardName classifies a Dashboard resource name. It
// accepts:
//
//   - `organizations/{org}/dashboards/{id}` → scopeOrg
//   - `organizations/{org}/spaces/{space}/dashboards/{id}` →
//     scopeSpace
//
// Any other shape returns scopeMalformed. Empty slugs / ID are
// rejected.
func parseDashboardName(name string) (kind scopeKind, orgSlug, spaceSlug, id string) {
	parts := strings.Split(name, "/")
	switch {
	case len(parts) == 4 && parts[0] == "organizations" && parts[1] != "" &&
		parts[2] == "dashboards" && parts[3] != "":
		return scopeOrg, parts[1], "", parts[3]
	case len(parts) == 6 && parts[0] == "organizations" && parts[1] != "" &&
		parts[2] == "spaces" && parts[3] != "" &&
		parts[4] == "dashboards" && parts[5] != "":
		return scopeSpace, parts[1], parts[3], parts[5]
	default:
		return scopeMalformed, "", "", ""
	}
}

// ListDashboards lists dashboards under a parent. At org parent the
// response is the system-curated catalog; at space parent the
// response is the customer-owned dashboards (Phase 4b). The
// permission interceptor has already enforced dashboards.read.
func (s *Server) ListDashboards(ctx context.Context, req *apiv1.ListDashboardsRequest) (*apiv1.ListDashboardsResponse, error) {
	kind, orgSlug, _ := parseParent(req.GetParent())
	switch kind {
	case scopeOrg:
		return listOrgDashboards(orgSlug), nil
	case scopeSpace:
		return nil, apierr.Unimplemented(
			"space-scoped ListDashboards lands in Phase 4b (USER_MANAGED CRUD)")
	default:
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"expected organizations/{org} or organizations/{org}/spaces/{space}"))
	}
}

// listOrgDashboards builds a fresh Dashboard for every catalog
// entry. The Build closure deep-clones its template Widget per
// call (see internal/dashboard/system/library) so the entries in
// the response don't share state with each other or with the
// registered template.
//
// Pagination is not yet wired — the catalog is small (≤ 10 entries
// for the foreseeable future) and ListDashboardsRequest's
// page_size / page_token apply naturally to space-scoped DB-backed
// listings, not to the static org catalog. If a future system
// catalog grows past one page's worth, slice the catalog here and
// emit a next_page_token; until then, returning the full catalog
// in one response is the right v1 behavior.
func listOrgDashboards(orgSlug string) *apiv1.ListDashboardsResponse {
	entries := system.All()
	out := &apiv1.ListDashboardsResponse{
		Dashboards: make([]*apiv1.Dashboard, 0, len(entries)),
	}
	for _, e := range entries {
		out.Dashboards = append(out.Dashboards, e.Build(orgSlug))
	}
	return out
}

// GetDashboard returns one dashboard by name. At org-scoped names
// the dispatch resolves through the system catalog; at space-scoped
// names the dispatch falls through to Phase 4b's DB read.
func (s *Server) GetDashboard(ctx context.Context, req *apiv1.GetDashboardRequest) (*apiv1.Dashboard, error) {
	kind, orgSlug, _, id := parseDashboardName(req.GetName())
	switch kind {
	case scopeOrg:
		return getOrgDashboard(orgSlug, id, req.GetName())
	case scopeSpace:
		return nil, apierr.Unimplemented(
			"space-scoped GetDashboard lands in Phase 4b (USER_MANAGED CRUD)")
	default:
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected organizations/{org}/dashboards/{id} or organizations/{org}/spaces/{space}/dashboards/{id}"))
	}
}

func getOrgDashboard(orgSlug, id, fullName string) (*apiv1.Dashboard, error) {
	e, ok := system.Get(id)
	if !ok {
		return nil, apierr.NotFound("Dashboard", fullName)
	}
	return e.Build(orgSlug), nil
}

// CreateDashboard / UpdateDashboard / DeleteDashboard land in
// Phase 4b alongside the dashboards table, sqlc queries, the
// SYSTEM_MANAGED mutation guard, and the FieldMask reject for
// management_mode on Update. Phase 4a stubs them so the gRPC
// surface is complete (gateway routing works, permission
// interceptor wires correctly) without yet supporting writes.

func (s *Server) CreateDashboard(ctx context.Context, req *apiv1.CreateDashboardRequest) (*apiv1.Dashboard, error) {
	return nil, apierr.Unimplemented(
		"CreateDashboard lands in Phase 4b (space-scoped USER_MANAGED CRUD)")
}

func (s *Server) UpdateDashboard(ctx context.Context, req *apiv1.UpdateDashboardRequest) (*apiv1.Dashboard, error) {
	return nil, apierr.Unimplemented(
		"UpdateDashboard lands in Phase 4b (space-scoped USER_MANAGED CRUD)")
}

func (s *Server) DeleteDashboard(ctx context.Context, req *apiv1.DeleteDashboardRequest) (*apiv1.Dashboard, error) {
	return nil, apierr.Unimplemented(
		"DeleteDashboard lands in Phase 4b (space-scoped USER_MANAGED CRUD)")
}
