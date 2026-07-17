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
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	"github.com/dashkan/pivox/internal/dashboard/system"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
)

// dashboardSlugPattern is the AIP-style slug regex enforced for
// client-supplied dashboard_id values. Mirrors the
// `^[a-z][a-z0-9-]{3,19}$` pattern used by org / space slugs;
// 4-20 characters, lowercase + digits + hyphens, must start with
// a letter.
var dashboardSlugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{3,19}$`)

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
	// parent is a space. USER_MANAGED dashboards live here.
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

// spaceParentName reconstructs the parent resource name from the
// resolved-context slugs. Used as the `parentName` argument to
// convert.DashboardToProto.
func spaceParentName(orgSlug, spaceSlug string) string {
	return "organizations/" + orgSlug + "/spaces/" + spaceSlug
}

// ListDashboards lists dashboards under a parent. At org parent the
// response is the system-curated catalog; at space parent the
// response is the customer-owned dashboards from the dashboards
// table. The permission interceptor has already enforced
// dashboards.read.
func (s *Server) ListDashboards(ctx context.Context, req *apiv1.ListDashboardsRequest) (*apiv1.ListDashboardsResponse, error) {
	kind, _, _ := parseParent(req.GetParent())
	switch kind {
	case scopeOrg:
		// orgSlug is recovered from the parsed parent so the system
		// catalog's Build closures can embed it in the dashboard
		// name. Re-parse rather than trust the resolved-context
		// (which carries the canonical slug for org parents only when
		// the membership interceptor ran the org branch — for
		// dashboards the interceptor uses the auto-discriminating
		// ScopeFromPath so org context is set for org parents).
		_, orgSlug, _ := parseParent(req.GetParent())
		return listOrgDashboards(orgSlug), nil
	case scopeSpace:
		return s.listSpaceDashboards(ctx, req)
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
// listings, not to the static org catalog.
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

// listSpaceDashboards reads space-scoped USER_MANAGED dashboards out of the
// dashboards table via the dynamic AIP-160 filtered + AIP-132 sorted +
// compound-cursor keyset engine (filter.BuildListQuery), mirroring
// ListConnectors / ListSecrets. The interceptor-resolved space is the
// NON-NEGOTIABLE base scope (space_id = $), ANDed as the base of the query; the
// request's filter/order_by layer ON TOP and can only narrow. Every value
// (space id, filter operands, cursor values, page size) is bound as a $N
// parameter — column/direction come only from DashboardFilter's whitelist. See
// docs/aip-list-transpiler-procedure.md.
func (s *Server) listSpaceDashboards(ctx context.Context, req *apiv1.ListDashboardsRequest) (*apiv1.ListDashboardsResponse, error) {
	resolved := server.MustResolvedSpaceFromContext(ctx)
	rf := filter.DashboardFilter()
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	// Resolve order_by against the sortable whitelist (default: createTime
	// desc). The plan also tells the cursor codec whether the sort value is a
	// timestamp.
	plan, err := filter.PlanOrderBy(rf, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}

	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	// Base scope: the interceptor-resolved space. The AIP filter is layered on
	// top — it can only narrow within the space.
	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     []filter.Predicate{{SQL: "space_id = %s", Arg: resolved.ID}},
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter, e.g.
		// an unknown field) — surface it as InvalidArgument on "filter".
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list dashboards")
	}
	rows, err := filter.ScanDashboards(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list dashboards")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row (see the connectors comment for
	// the off-by-one this closes).
	rows, nextPageToken, err := filter.Paginate(rows, int(pageSize), func(last db.Dashboard) (string, error) {
		return filter.EncodeCursor(s.codec, plan, dashboardSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	orgSlug, spaceSlug := orgSpaceSlugsFromContext(ctx)
	parent := spaceParentName(orgSlug, spaceSlug)
	out := &apiv1.ListDashboardsResponse{
		Dashboards:    make([]*apiv1.Dashboard, 0, len(rows)),
		NextPageToken: nextPageToken,
	}
	for _, row := range rows {
		d, err := convert.DashboardToProto(row, parent, nil)
		if err != nil {
			return nil, apierr.Internal(err, "dashboard payload corrupt")
		}
		out.Dashboards = append(out.Dashboards, d)
	}
	return out, nil
}

// dashboardSortValue renders the primary order_by column's value for the given
// row as the string the compound page token carries. Timestamps use
// RFC3339Nano so filter.DecodeCursor can parse them back to an exact
// time.Time. For the default id ordering (plan.Field == "") the value is unused
// (EncodeCursor emits the id-only token), so "" is returned. Mirrors
// connectorSortValue.
func dashboardSortValue(plan filter.OrderByPlan, r db.Dashboard) string {
	switch plan.Field {
	case "displayName":
		return r.DisplayName
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

// GetDashboard returns one dashboard by name. At org-scoped names
// the dispatch resolves through the system catalog; at space-scoped
// names it reads from the dashboards table.
func (s *Server) GetDashboard(ctx context.Context, req *apiv1.GetDashboardRequest) (*apiv1.Dashboard, error) {
	kind, orgSlug, spaceSlug, id := parseDashboardName(req.GetName())
	switch kind {
	case scopeOrg:
		return getOrgDashboard(orgSlug, id, req.GetName())
	case scopeSpace:
		return s.getSpaceDashboard(ctx, orgSlug, spaceSlug, id, req.GetName())
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

func (s *Server) getSpaceDashboard(ctx context.Context, orgSlug, spaceSlug, id, fullName string) (*apiv1.Dashboard, error) {
	resolved := server.MustResolvedSpaceFromContext(ctx)
	row, err := s.queries.GetDashboardByName(ctx, db.GetDashboardByNameParams{
		SpaceID: resolved.ID,
		Name:    id,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Dashboard", fullName)
	}
	return convert.DashboardToProto(row, spaceParentName(orgSlug, spaceSlug), nil)
}

// CreateDashboard creates a USER_MANAGED dashboard in a space.
// SYSTEM_MANAGED is server-curated and cannot be created via this
// RPC — the request's management_mode is OUTPUT_ONLY and the
// handler always inserts USER_MANAGED.
//
// dashboard_id is required (the proto marks it OPTIONAL with a
// "server generates one if missing" doc, but server-side ID
// generation is deferred — empty IDs are rejected with a clear
// InvalidArgument message rather than silently auto-named).
func (s *Server) CreateDashboard(ctx context.Context, req *apiv1.CreateDashboardRequest) (*apiv1.Dashboard, error) {
	parentKind, orgSlug, spaceSlug := parseParent(req.GetParent())
	if parentKind != scopeSpace {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"CreateDashboard requires a space-scoped parent: organizations/{org}/spaces/{space}"))
	}

	dashboardID := req.GetDashboardId()
	if dashboardID == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("dashboard_id",
			"dashboard_id is required; server-side auto-generation is not yet implemented"))
	}
	if !dashboardSlugPattern.MatchString(dashboardID) {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("dashboard_id",
			"must match "+dashboardSlugPattern.String()))
	}

	if req.GetDashboard() == nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("dashboard",
			"dashboard is required"))
	}
	if req.GetDashboard().GetDisplayName() == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("dashboard.display_name",
			"display_name is required"))
	}

	resolved := server.MustResolvedSpaceFromContext(ctx)
	callerID := server.MustUserID(ctx)

	// Build a clean Dashboard for marshaling: strip OUTPUT_ONLY
	// fields the server owns. management_mode is forced to
	// USER_MANAGED — the proto says OUTPUT_ONLY, so any value the
	// client supplied is silently discarded per AIP convention.
	clean := &apiv1.Dashboard{
		DisplayName:    req.GetDashboard().GetDisplayName(),
		Description:    req.GetDashboard().GetDescription(),
		Layout:         req.GetDashboard().GetLayout(),
		Variables:      req.GetDashboard().GetVariables(),
		Annotations:    req.GetDashboard().GetAnnotations(),
		ManagementMode: apiv1.Dashboard_USER_MANAGED,
	}
	payload, err := convert.DashboardPayload(clean)
	if err != nil {
		return nil, apierr.Internal(err, "dashboard marshal")
	}

	// validate_only runs the INSERT against real constraints and rolls it
	// back, so a would-fail request (e.g. duplicate slug) returns the same
	// error a live one would while persisting nothing.
	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Dashboard, error) {
		return qtx.CreateDashboard(ctx, db.CreateDashboardParams{
			SpaceID:        resolved.ID,
			Name:           dashboardID,
			DisplayName:    clean.GetDisplayName(),
			Description:    clean.GetDescription(),
			ManagementMode: "USER_MANAGED",
			Payload:        payload,
			CreatedBy:      convert.PgUUID(callerID),
		})
	})
	if err != nil {
		// HandleResourceError maps SQLSTATE 23505 (unique violation) →
		// AlreadyExists, ErrNoRows → NotFound, FK violations →
		// NotFound. The (org, space) FKs are pre-validated by the
		// membership interceptor; the only path that reaches us is
		// the dashboard slug clash.
		return nil, apierr.HandleResourceError(err, "Dashboard",
			spaceParentName(orgSlug, spaceSlug)+"/dashboards/"+dashboardID)
	}

	return convert.DashboardToProto(row, spaceParentName(orgSlug, spaceSlug), nil)
}

// UpdateDashboard applies a partial update to a space-scoped
// USER_MANAGED dashboard. The handler enforces three guards in
// order:
//
//  1. FieldMask cannot name `management_mode` (it's OUTPUT_ONLY;
//     attempting to mask-write it returns InvalidArgument).
//  2. The target row's existing management_mode must be
//     USER_MANAGED — SYSTEM_MANAGED rows reject mutation with
//     FailedPrecondition regardless of which URL the caller hit.
//  3. The supplied etag must match the row's current etag —
//     stale etags map to Aborted.
//
// Org-scoped names (`organizations/{org}/dashboards/{id}`) are
// rejected with FailedPrecondition because the catalog is
// SYSTEM_MANAGED and not editable.
func (s *Server) UpdateDashboard(ctx context.Context, req *apiv1.UpdateDashboardRequest) (*apiv1.Dashboard, error) {
	if req.GetDashboard() == nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("dashboard",
			"dashboard is required"))
	}

	// Guard 1: FieldMask must not name management_mode.
	for _, p := range req.GetUpdateMask().GetPaths() {
		if p == "management_mode" {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("update_mask",
				"path \"management_mode\" is OUTPUT_ONLY and cannot be updated"))
		}
	}

	kind, orgSlug, spaceSlug, id := parseDashboardName(req.GetDashboard().GetName())
	switch kind {
	case scopeOrg:
		// Catalog dashboards are SYSTEM_MANAGED — same rejection
		// shape as a SYSTEM_MANAGED row in the table.
		return nil, apierr.FailedPrecondition(
			"Dashboard " + req.GetDashboard().GetName() +
				" is SYSTEM_MANAGED and cannot be updated")
	case scopeSpace:
		// Fall through.
	default:
		return nil, apierr.InvalidArgument(apierr.FieldViolation("dashboard.name",
			"expected organizations/{org}/spaces/{space}/dashboards/{id}"))
	}

	resolved := server.MustResolvedSpaceFromContext(ctx)
	callerID := server.MustUserID(ctx)

	// validate_only runs the whole update tx (row lock, guards, UPDATE)
	// against real state and rolls it back, so a would-fail request (e.g.
	// etag mismatch, SYSTEM_MANAGED) returns the same error a live one would
	// while persisting nothing.
	updated, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Dashboard, error) {
		existing, err := qtx.GetDashboardByNameForUpdate(ctx, db.GetDashboardByNameForUpdateParams{
			SpaceID: resolved.ID,
			Name:    id,
		})
		if err != nil {
			return db.Dashboard{}, apierr.HandleResourceError(err, "Dashboard", req.GetDashboard().GetName())
		}
		// Guard 2: existing row must be USER_MANAGED. This fires
		// before the etag check because "this resource never
		// supports Update" is more actionable than "you forgot
		// etag" — the customer can't fix the latter without
		// learning the former.
		if existing.ManagementMode == "SYSTEM_MANAGED" {
			return db.Dashboard{}, apierr.FailedPrecondition(
				"Dashboard " + req.GetDashboard().GetName() +
					" is SYSTEM_MANAGED and cannot be updated")
		}
		// Guard 3: etag is required for optimistic concurrency. Per
		// AIP-154 omitting it isn't "skip the check" — it's a
		// malformed Update request. Two clients both omitting etag
		// would otherwise race with last-write-wins and no error.
		if req.GetDashboard().GetEtag() == "" {
			return db.Dashboard{}, apierr.InvalidArgument(apierr.FieldViolation("dashboard.etag",
				"etag is required for Update — fetch the dashboard first and pass its etag back"))
		}

		// Build the post-update Dashboard for re-marshaling. v1's
		// FieldMask coverage is "all-or-nothing": the request
		// replaces display_name, description, layout, variables,
		// annotations from the supplied dashboard. `pickString`
		// preserves the prior value when the request leaves a
		// string empty so a partial Update doesn't blank the
		// untouched field.
		//
		// TODO: when per-path FieldMask handling lands, drop
		// pickString — empty string in a masked path means clear,
		// and unmasked paths fall through to the existing value.
		merged := &apiv1.Dashboard{
			DisplayName:    pickString(req.GetDashboard().GetDisplayName(), existing.DisplayName),
			Description:    pickString(req.GetDashboard().GetDescription(), existing.Description),
			Layout:         req.GetDashboard().GetLayout(),
			Variables:      req.GetDashboard().GetVariables(),
			Annotations:    req.GetDashboard().GetAnnotations(),
			ManagementMode: apiv1.Dashboard_USER_MANAGED,
		}
		payload, err := convert.DashboardPayload(merged)
		if err != nil {
			return db.Dashboard{}, apierr.Internal(err, "dashboard marshal")
		}

		row, err := qtx.UpdateDashboardByName(ctx, db.UpdateDashboardByNameParams{
			SpaceID:     resolved.ID,
			Name:        id,
			DisplayName: merged.GetDisplayName(),
			Description: merged.GetDescription(),
			Payload:     payload,
			UpdatedBy:   convert.PgUUID(callerID),
			Etag:        req.GetDashboard().GetEtag(),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Etag mismatch: row exists (we held its lock above)
				// but the etag in the WHERE didn't match.
				return db.Dashboard{}, apierr.Aborted("Dashboard",
					req.GetDashboard().GetName(),
					"etag mismatch — re-fetch the dashboard and retry")
			}
			return db.Dashboard{}, apierr.HandleResourceError(err, "Dashboard", req.GetDashboard().GetName())
		}
		return row, nil
	})
	if err != nil {
		return nil, err
	}

	return convert.DashboardToProto(updated, spaceParentName(orgSlug, spaceSlug), nil)
}

// DeleteDashboard soft-deletes a space-scoped USER_MANAGED
// dashboard. SYSTEM_MANAGED dashboards (including catalog entries
// hit via the org-scoped name pattern) reject with
// FailedPrecondition.
func (s *Server) DeleteDashboard(ctx context.Context, req *apiv1.DeleteDashboardRequest) (*apiv1.Dashboard, error) {
	kind, orgSlug, spaceSlug, id := parseDashboardName(req.GetName())
	switch kind {
	case scopeOrg:
		return nil, apierr.FailedPrecondition(
			"Dashboard " + req.GetName() +
				" is SYSTEM_MANAGED and cannot be deleted")
	case scopeSpace:
		// Fall through.
	default:
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected organizations/{org}/spaces/{space}/dashboards/{id}"))
	}

	resolved := server.MustResolvedSpaceFromContext(ctx)
	callerID := server.MustUserID(ctx)

	// validate_only runs the whole delete tx (row lock, guard, soft-delete)
	// against real state and rolls it back, so a would-fail request returns
	// the same error a live one would while persisting nothing.
	deleted, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Dashboard, error) {
		existing, err := qtx.GetDashboardByNameForUpdate(ctx, db.GetDashboardByNameForUpdateParams{
			SpaceID: resolved.ID,
			Name:    id,
		})
		if err != nil {
			return db.Dashboard{}, apierr.HandleResourceError(err, "Dashboard", req.GetName())
		}
		if existing.ManagementMode == "SYSTEM_MANAGED" {
			return db.Dashboard{}, apierr.FailedPrecondition(
				"Dashboard " + req.GetName() +
					" is SYSTEM_MANAGED and cannot be deleted")
		}
		row, err := qtx.SoftDeleteDashboardByName(ctx, db.SoftDeleteDashboardByNameParams{
			SpaceID:   resolved.ID,
			Name:      id,
			DeletedBy: convert.PgUUID(callerID),
		})
		if err != nil {
			return db.Dashboard{}, apierr.HandleResourceError(err, "Dashboard", req.GetName())
		}
		return row, nil
	})
	if err != nil {
		return nil, err
	}

	return convert.DashboardToProto(deleted, spaceParentName(orgSlug, spaceSlug), nil)
}

// orgSpaceSlugsFromContext recovers (orgSlug, spaceSlug) from the
// resolved-space context populated by the membership interceptor.
// Slug values are taken from the resolved-context structs (not the
// row blobs) so they round-trip the canonical-slug form rather
// than any possibly-stale historical value.
func orgSpaceSlugsFromContext(ctx context.Context) (string, string) {
	space := server.MustResolvedSpaceFromContext(ctx)
	// Org slug comes from the resolved org in the same context.
	org, ok := server.ResolvedOrgFromContext(ctx)
	if !ok {
		// The space-resolution path also populates the org in the
		// same context (see permission_interceptor.go); a missing
		// org here would be a wiring bug, not a runtime concern.
		panic("dashboards: ResolvedSpaceFromContext set but ResolvedOrgFromContext missing")
	}
	return org.Slug, space.Slug
}

// pickString returns `next` when non-empty, otherwise `prev`. Used
// in UpdateDashboard's merge logic so an empty field in the request
// preserves the existing value rather than zeroing it.
//
// TODO: when per-path FieldMask handling lands, delete this helper —
// empty string in a masked path means clear, and unmasked paths fall
// through to the existing row value. See the merge block in
// UpdateDashboard for the full context.
func pickString(next, prev string) string {
	if next != "" {
		return next
	}
	return prev
}
