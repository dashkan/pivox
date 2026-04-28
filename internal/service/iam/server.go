// Package iam implements the gRPC `Iam` service: members, groups,
// roles, and permission resolution. v1 ships system-roles only;
// custom-role CRUD is deferred (returns Unimplemented per phase
// roadmap). Permission gating itself lives in `internal/permission`
// — this package's handlers are the wire interface.
package iam

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
)

// CallerResolver extracts the authenticated caller's
// firebase_identity.id from the request context. Production wires
// this to a small adapter that reads the Firebase UID off the auth
// interceptor's context, looks it up in firebase_identities, and
// returns the row id.
//
// Returns a non-nil error if the caller can't be resolved — handlers
// surface that error directly. The production resolver returns
// Unauthenticated for "no auth context" and Internal for DB faults;
// tests inject either a plain id+nil for happy path or a
// pre-formed error for sad path.
type CallerResolver func(ctx context.Context) (uuid.UUID, error)

// IamServer implements the wire-level Iam service. v1 surface:
// permission catalog reads, role reads, member CRUD (3b2/3b3),
// group CRUD (3d). Anything not yet implemented falls through to
// UnimplementedIamServer and returns Unimplemented.
type IamServer struct {
	iampb.UnimplementedIamServer
	queries  db.Querier
	resolver *permission.Resolver
	caller   CallerResolver
}

// NewIamServer constructs the server. `resolver` is allowed to be
// nil only at construction-time for tests that don't exercise
// TestIamPermissions; production must always pass a real one.
func NewIamServer(queries db.Querier, resolver *permission.Resolver, caller CallerResolver) *IamServer {
	if resolver == nil {
		// Allows handler-level tests to skip wiring the resolver.
		// TestIamPermissions tests construct it inline against the
		// same MockQuerier so the call paths exercise the real
		// resolver logic.
		resolver = permission.NewResolver(queries)
	}
	return &IamServer{
		queries:  queries,
		resolver: resolver,
		caller:   caller,
	}
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

// TestIamPermissions returns the subset of `req.permissions` the
// caller is allowed against `req.resource`. Used by UI clients for
// permission-gated affordance rendering ("which buttons should I
// enable?") with one round-trip instead of N HasPermission calls.
//
// Returns Unauthenticated if the caller has no auth context.
// Returns the empty set (and OK) if the caller has no role bindings
// or the resource resolves to nothing — the caller treats that as
// "no permissions granted" and the UI greys out everything.
func (s *IamServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	identity, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	target, err := parseResourceTarget(req.GetResource())
	if err != nil {
		return nil, err
	}
	allowed, err := s.resolver.TestPermissions(ctx, identity, target, req.GetPermissions())
	if err != nil {
		slog.ErrorContext(ctx, "iam: resolve test permissions failed", "resource", req.GetResource(), "error", err)
		return nil, apierr.Internal("resolve permissions")
	}
	return &iampb.TestIamPermissionsResponse{Permissions: allowed}, nil
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

// parseResourceTarget extracts a permission.Target from a resource
// path. v1 supports two shapes: `organizations/{org}` (org scope)
// and `organizations/{org}/spaces/{space}` (space scope). The
// trailing segment is parsed as a UUID — TestIamPermissions callers
// pass resolved IDs, not slugs (the org/space already exists by the
// time the UI is asking "what can I do here?").
func parseResourceTarget(resource string) (permission.Target, error) {
	parts := strings.Split(resource, "/")
	switch {
	case len(parts) == 2 && parts[0] == "organizations" && parts[1] != "":
		id, err := uuid.Parse(parts[1])
		if err != nil {
			return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
				fmt.Sprintf("invalid org id in resource %q: %v", resource, err)))
		}
		return permission.OrgTarget(id), nil
	case len(parts) == 4 && parts[0] == "organizations" && parts[2] == "spaces" && parts[1] != "" && parts[3] != "":
		id, err := uuid.Parse(parts[3])
		if err != nil {
			return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
				fmt.Sprintf("invalid space id in resource %q: %v", resource, err)))
		}
		return permission.SpaceTarget(id), nil
	default:
		return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
			fmt.Sprintf("invalid resource %q: expected organizations/{org} or organizations/{org}/spaces/{space}", resource)))
	}
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

// silence "imported and not used" if the package ever loses the
// errors.Is path. Keeps the import list explicit during
// development.
var _ = errors.Is
var _ = pgx.ErrNoRows
