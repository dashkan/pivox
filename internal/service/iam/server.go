// Package iam implements the cross-cutting `Iam` gRPC service. v1
// surface (post-redistribution): permission catalog reads, role
// reads (system roles only), user reads, group CRUD, and the
// `DeleteUser` LRO.
//
// Scope-divergent IAM ops (Member CRUD, TransferOwnership,
// TestIamPermissions) live on the scope-owning services
// (`Organizations`, `Spaces`). See locked sub-decision #12 in the
// IAM roadmap for the principle.
package iam

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
)

// IamServer implements the wire-level Iam service. Reads (role,
// permission, user) plus group CRUD live here; anything not yet
// implemented falls through to `UnimplementedIamServer` and returns
// `Unimplemented`.
type IamServer struct {
	iampb.UnimplementedIamServer
	queries db.Querier
}

// NewIamServer constructs the server. No resolver/caller deps —
// permission gating is handled at the interceptor layer; nothing in
// the IamServer's surface needs caller-identity resolution at
// handler granularity.
func NewIamServer(queries db.Querier) *IamServer {
	return &IamServer{queries: queries}
}

// ListPermissions returns the global permission catalog. Permissions
// are static / code-defined in v1; this RPC just echoes the seeded
// rows. The catalog is small (~100 entries) so v1 returns the full
// set in one call without paging — `page_size` and `page_token` on
// the request are accepted but ignored.
func (s *IamServer) ListPermissions(ctx context.Context, _ *iampb.ListPermissionsRequest) (*iampb.ListPermissionsResponse, error) {
	rows, err := s.queries.ListPermissions(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "iam: list permissions failed", "error", err)
		return nil, apierr.Internal("list permissions")
	}
	out := make([]*iampb.Permission, len(rows))
	for i, r := range rows {
		out[i] = convert.PermissionToProto(r)
	}
	return &iampb.ListPermissionsResponse{Permissions: out}, nil
}

// GetRole resolves a role by its `organizations/{org}/roles/{name}`
// path. v1 only resolves system roles; custom roles return
// NotFound.
func (s *IamServer) GetRole(ctx context.Context, req *iampb.GetRoleRequest) (*iampb.Role, error) {
	orgSlug, roleName, err := parseRoleName(req.GetName())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Role", req.GetName())
	}
	role, err := s.queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: org.ID,
		Name:  roleName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Role", req.GetName())
	}
	return convert.RoleToProto(role, orgSlug, permissionsForRole(role.Name)), nil
}

// ListRoles returns all roles in the org. v1 has only the 4 system
// roles per org so paging is unused; future custom roles will fold
// into the same response.
func (s *IamServer) ListRoles(ctx context.Context, req *iampb.ListRolesRequest) (*iampb.ListRolesResponse, error) {
	orgSlug, err := parseOrgParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}
	rows, err := s.queries.ListRolesByOrg(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "iam: list roles failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("list roles")
	}
	out := make([]*iampb.Role, len(rows))
	for i, r := range rows {
		out[i] = convert.RoleToProto(r, orgSlug, permissionsForRole(r.Name))
	}
	return &iampb.ListRolesResponse{Roles: out}, nil
}

// parseRoleName splits `organizations/{org}/roles/{role}` into
// (orgSlug, roleName). Reports InvalidArgument on shape mismatch.
func parseRoleName(name string) (orgSlug, roleName string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "roles" || parts[1] == "" || parts[3] == "" {
		return "", "", apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid role name %q: expected organizations/{org}/roles/{role}", name)))
	}
	return parts[1], parts[3], nil
}

// parseOrgParent extracts the org slug from `organizations/{org}`.
func parseOrgParent(parent string) (string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/{org}", parent)))
	}
	return parts[1], nil
}

// permissionsForRole returns the set of permission_id strings a
// system role grants. For v1 this is the static matrix from
// internal/permission. Once custom roles ship in v2, the resolver
// will additionally read role_permissions for non-system roles —
// this helper stays as the system-role fast path.
func permissionsForRole(roleName string) []string {
	out := make([]string, 0, len(permission.All))
	for _, p := range permission.All {
		if permission.Has(roleName, p) {
			out = append(out, p)
		}
	}
	return out
}
