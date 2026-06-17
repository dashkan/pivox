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
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// IamServer implements the wire-level Iam service. Reads (role,
// permission, user) plus group CRUD live here; anything not yet
// implemented falls through to `UnimplementedIamServer` and returns
// `Unimplemented`.
type IamServer struct {
	iampb.UnimplementedIamServer
	pool       db.TxBeginner
	queries    db.Querier
	auth       authn.Service
	lroManager *lro.Manager
	// audit is non-nil when the resolver wants invalidation
	// callbacks on identity mutations (DeleteAccount soft-delete).
	// Optional — read-only deployments may skip it.
	audit *audit.Resolver
}

// Config is the constructor input for IamServer. The auth + LROManager
// deps are required by DeleteUser (a global LRO that ends with a
// Firebase Auth deletion); read-only handlers (ListPermissions,
// GetRole, ListRoles) ignore them. Unit tests that exercise only
// reads build an IamServer struct literal directly.
type Config struct {
	// Pool is the database pool used for transactional handlers
	// (DeleteUser sole-owner check + cascade; DeleteAccount per-phase
	// read-then-write atomicity). Required.
	Pool db.TxBeginner
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Auth is the authn service. Required.
	Auth authn.Service
	// LROManager drives DeleteUser. Required.
	LROManager *lro.Manager
	// AuditResolver receives Invalidate() calls when identities
	// mutate (DeleteAccount blanks PII + flips is_deleted). Optional;
	// nil disables cache-invalidation, which is fine for read-only
	// deployments and any test that doesn't assert on cache
	// coherence.
	AuditResolver *audit.Resolver
}

// NewIamServer constructs the server from cfg. Panics on a missing
// required field.
func NewIamServer(cfg Config) *IamServer {
	if cfg.Pool == nil {
		panic("iam: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("iam: Config.Queries is required")
	}
	if cfg.Auth == nil {
		panic("iam: Config.Auth is required")
	}
	if cfg.LROManager == nil {
		panic("iam: Config.LROManager is required")
	}
	return &IamServer{
		pool:       cfg.Pool,
		queries:    cfg.Queries,
		auth:       cfg.Auth,
		lroManager: cfg.LROManager,
		audit:      cfg.AuditResolver,
	}
}

// ListAccountOrganizations returns the active organizations the
// authenticated caller has membership in, projected to a slim
// (organization, display_name, role) shape. Distinct from
// `Organizations.ListOrganizations` (which returns the full
// Organization resource and includes soft-deleted orgs for the
// undelete UX). Drives the post-sign-in bootstrap (zero results
// route the client to the create-org screen) and the in-app
// org-picker UI.
//
// `parent` MUST be the literal `accounts/me`; the caller is implicit
// from the authentication context. The membership-exempt interceptor
// allowlist short-circuits the membership check for this method —
// gating "do I have membership?" on prior membership would be
// chicken-and-egg.
//
// Intentionally unpaginated. A single caller's membership list is
// small (1–10 typical, capped at 1000 by the underlying query) —
// pagination would be ceremony with no realistic benefit. The proto
// carries AIP-158 disables explaining the deviation.
func (s *IamServer) ListAccountOrganizations(ctx context.Context, req *iampb.ListAccountOrganizationsRequest) (*iampb.ListAccountOrganizationsResponse, error) {
	if req.GetParent() != "accounts/me" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"expected accounts/me; the caller is implicit from authentication context"))
	}
	identityID := server.MustPivoxUserID(ctx)
	rows, err := s.queries.ListAccountOrganizationsForIdentity(ctx, convert.PgUUID(identityID))
	if err != nil {
		slog.ErrorContext(ctx, "iam: list account organizations failed",
			"identity_id", identityID, "error", err)
		return nil, apierr.Internal("list account organizations")
	}
	out := make([]*iampb.AccountOrganization, len(rows))
	for i, r := range rows {
		out[i] = convert.AccountOrganizationToProto(r)
	}
	return &iampb.ListAccountOrganizationsResponse{AccountOrganizations: out}, nil
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
	perms, err := s.queries.RolePermissionIDs(ctx, role.ID)
	if err != nil {
		return nil, apierr.Internal("load role permissions")
	}
	return convert.RoleToProto(role, orgSlug, perms), nil
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
		perms, err := s.queries.RolePermissionIDs(ctx, r.ID)
		if err != nil {
			slog.ErrorContext(ctx, "iam: load role permissions failed", "role_id", r.ID, "error", err)
			return nil, apierr.Internal("load role permissions")
		}
		out[i] = convert.RoleToProto(r, orgSlug, perms)
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
