package organizations

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
)

// org-scope Member handlers. Member CRUD lives here (rather than on
// a shared `Iam` service) because the ≥1-owner boundary check and
// the org-only TransferOwnership semantic make these org-shaped
// operations. Space-scope Member handlers live on the Spaces service
// and operate on `space_members` instead.
//
// The Member proto type is shared (`pivox.iam.v1.Member`); only the
// URL pattern + DB table dispatch differs per scope.

// orgMemberPath captures the parsed pieces of an org-scope Member
// resource name: `organizations/{org}/members/{member}` where
// `{member}` is `user-{uuid}` or `group-{uuid}`.
type orgMemberPath struct {
	orgSlug       string
	principalKind db.PrincipalKind
	principalID   uuid.UUID
}

// parseOrgMemberName parses `organizations/{org}/members/{member}`.
// The URL pattern at the gRPC layer already constrains the shape, but
// we re-parse defensively in case grpc-gateway passes through a
// malformed name.
func parseOrgMemberName(name string) (orgMemberPath, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "members" || parts[1] == "" || parts[3] == "" {
		return orgMemberPath{}, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member name %q: expected organizations/{org}/members/{member}", name)))
	}
	kind, id, err := permission.ParseMemberSegment(parts[3])
	if err != nil {
		return orgMemberPath{}, err
	}
	return orgMemberPath{orgSlug: parts[1], principalKind: kind, principalID: id}, nil
}

// parseOrgMemberParent parses `organizations/{org}` (the parent for
// org-scope Member listing/creation). Returns the org slug.
func parseOrgMemberParent(parent string) (string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/{org}", parent)))
	}
	return parts[1], nil
}

// GetMember resolves an org-scope Member by resource name. Reads from
// the `org_members` table; space-scope members live on the Spaces
// service.
func (s *OrganizationsServer) GetMember(ctx context.Context, req *iampb.GetMemberRequest) (*iampb.Member, error) {
	path, err := parseOrgMemberName(req.GetName())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, path.orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	row, err := s.queries.GetOrgMember(ctx, db.GetOrgMemberParams{
		OrgID:         org.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	return convert.OrgMemberRowToProto(row, path.orgSlug), nil
}

// ListMembers returns all org-scope Members. v1 returns up to 1000
// rows (the SQL LIMIT) without pagination — system-role member
// counts in normal orgs are far below that ceiling.
func (s *OrganizationsServer) ListMembers(ctx context.Context, req *iampb.ListMembersRequest) (*iampb.ListMembersResponse, error) {
	orgSlug, err := parseOrgMemberParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}
	rows, err := s.queries.ListOrgMembers(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list org members failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("list members")
	}
	out := make([]*iampb.Member, len(rows))
	for i, r := range rows {
		out[i] = convert.OrgMemberToProto(r, orgSlug)
	}
	return &iampb.ListMembersResponse{Members: out}, nil
}

// CreateMember binds a principal (user or group) to a role at org
// scope. The principal must already exist in this org and the role
// must be a system role (v1).
//
// Tx-wrapped: principal-existence check + insert run in one
// transaction so a concurrent principal soft-delete cannot race in
// between and create a dead binding.
func (s *OrganizationsServer) CreateMember(ctx context.Context, req *iampb.CreateMemberRequest) (*iampb.Member, error) {
	orgSlug, err := parseOrgMemberParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	mem := req.GetMember()
	if mem == nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("member", "member is required"))
	}

	principalKind, principalID, err := principalFromMember(mem, orgSlug)
	if err != nil {
		return nil, err
	}
	if req.GetMemberId() != "" {
		expected := fmt.Sprintf("%s-%s", principalKind, principalID)
		if req.GetMemberId() != expected {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("member_id",
				fmt.Sprintf("member_id %q does not match principal (expected %q)", req.GetMemberId(), expected)))
		}
	}

	roleSlug, err := parseRoleRef(mem.GetRole(), orgSlug)
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}
	role, err := s.queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: org.ID,
		Name:  roleSlug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Role", mem.GetRole())
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	if err := verifyPrincipalInOrg(ctx, qtx, org.ID, principalKind, principalID); err != nil {
		return nil, err
	}

	row, err := qtx.CreateOrgMember(ctx, db.CreateOrgMemberParams{
		ID:            uuid.New(),
		OrgID:         org.ID,
		RoleID:        role.ID,
		PrincipalKind: principalKind,
		PrincipalID:   principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetParent())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("commit transaction")
	}

	return buildOrgMemberProto(orgSlug, role.Name, principalKind, principalID,
		row.ID, row.Etag, row.CreateTime, row.UpdateTime), nil
}

// UpdateMember mutates the role of an existing org-scope Member.
// Only `role` is mutable. Refuses to demote the last owner (would
// leave the org ownerless).
//
// Tx-wrapped: the count-owners check + role mutation run in one
// transaction so concurrent admin actions cannot race the boundary
// to zero owners.
func (s *OrganizationsServer) UpdateMember(ctx context.Context, req *iampb.UpdateMemberRequest) (*iampb.Member, error) {
	mem := req.GetMember()
	if mem == nil || mem.GetName() == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("member.name", "member.name is required"))
	}
	if err := validateRoleOnlyMask(req.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	path, err := parseOrgMemberName(mem.GetName())
	if err != nil {
		return nil, err
	}
	roleSlug, err := parseRoleRef(mem.GetRole(), path.orgSlug)
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, path.orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", mem.GetName())
	}
	newRole, err := s.queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: org.ID,
		Name:  roleSlug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Role", mem.GetRole())
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	current, err := qtx.GetOrgMember(ctx, db.GetOrgMemberParams{
		OrgID:         org.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", mem.GetName())
	}
	if current.RoleName == permission.RoleOwner && newRole.Name != permission.RoleOwner {
		count, err := qtx.CountOwnersByOrg(ctx, org.ID)
		if err != nil {
			return nil, apierr.Internal("count owners")
		}
		if count <= 1 {
			return nil, apierr.FailedPrecondition("cannot demote the last owner; promote another member to owner first")
		}
	}

	row, err := qtx.UpdateOrgMemberRole(ctx, db.UpdateOrgMemberRoleParams{
		OrgID:         org.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
		RoleID:        newRole.ID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", mem.GetName())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("commit transaction")
	}

	return buildOrgMemberProto(path.orgSlug, newRole.Name, path.principalKind, path.principalID,
		row.ID, row.Etag, row.CreateTime, row.UpdateTime), nil
}

// DeleteMember removes an org-scope Member binding. Refuses to
// delete the last owner (same boundary as UpdateMember).
func (s *OrganizationsServer) DeleteMember(ctx context.Context, req *iampb.DeleteMemberRequest) (*emptypb.Empty, error) {
	path, err := parseOrgMemberName(req.GetName())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, path.orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	current, err := qtx.GetOrgMember(ctx, db.GetOrgMemberParams{
		OrgID:         org.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	if current.RoleName == permission.RoleOwner {
		count, err := qtx.CountOwnersByOrg(ctx, org.ID)
		if err != nil {
			return nil, apierr.Internal("count owners")
		}
		if count <= 1 {
			return nil, apierr.FailedPrecondition("cannot remove the last owner; promote another member to owner first")
		}
	}

	n, err := qtx.DeleteOrgMember(ctx, db.DeleteOrgMemberParams{
		OrgID:         org.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.Internal("delete org member")
	}
	if n == 0 {
		// Lost a race against another deleter — treat as already-gone.
		return nil, apierr.NotFound("Member", req.GetName())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("commit transaction")
	}
	return &emptypb.Empty{}, nil
}

// principalFromMember pulls (kind, id) out of the Member proto's
// `principal` oneof. The principal resource ref must address the
// same org as the parent — caller passes parentOrgSlug for the check.
func principalFromMember(mem *iampb.Member, parentOrgSlug string) (db.PrincipalKind, uuid.UUID, error) {
	switch p := mem.GetPrincipal().(type) {
	case *iampb.Member_User:
		id, err := parsePrincipalRef(p.User, parentOrgSlug, "users")
		if err != nil {
			return "", uuid.Nil, err
		}
		return db.PrincipalKindUser, id, nil
	case *iampb.Member_Group:
		id, err := parsePrincipalRef(p.Group, parentOrgSlug, "groups")
		if err != nil {
			return "", uuid.Nil, err
		}
		return db.PrincipalKindGroup, id, nil
	default:
		return "", uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("member.principal",
			"exactly one of `user` or `group` must be set"))
	}
}

// parsePrincipalRef validates a principal resource string of the form
// `organizations/{org}/{collection}/{uuid}` and confirms the org
// matches the parent.
func parsePrincipalRef(ref, parentOrgSlug, collection string) (uuid.UUID, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != collection || parts[1] == "" || parts[3] == "" {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("member.principal",
			fmt.Sprintf("invalid principal ref %q: expected organizations/{org}/%s/{uuid}", ref, collection)))
	}
	if parts[1] != parentOrgSlug {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("member.principal",
			fmt.Sprintf("principal org %q does not match parent org %q", parts[1], parentOrgSlug)))
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("member.principal",
			fmt.Sprintf("invalid uuid in principal ref %q: %v", ref, err)))
	}
	return id, nil
}

// parseRoleRef extracts the role's stable name slug from
// `organizations/{org}/roles/{role}` and verifies the org matches.
func parseRoleRef(ref, parentOrgSlug string) (string, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "roles" || parts[1] == "" || parts[3] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("member.role",
			fmt.Sprintf("invalid role ref %q: expected organizations/{org}/roles/{role}", ref)))
	}
	if parts[1] != parentOrgSlug {
		return "", apierr.InvalidArgument(apierr.FieldViolation("member.role",
			fmt.Sprintf("role org %q does not match parent org %q", parts[1], parentOrgSlug)))
	}
	return parts[3], nil
}

// verifyPrincipalInOrg confirms the principal exists in this org so
// we don't insert dead bindings (org_members.principal_id is NOT a
// DB FK — it's polymorphic). Runs against the caller's qtx so the
// check + subsequent insert are atomic.
func verifyPrincipalInOrg(ctx context.Context, qtx db.Querier, orgID uuid.UUID, kind db.PrincipalKind, id uuid.UUID) error {
	switch kind {
	case db.PrincipalKindUser:
		if _, err := qtx.GetUserByID(ctx, db.GetUserByIDParams{ID: id, OrgID: orgID}); err != nil {
			if isNotFound(err) {
				return apierr.NotFound("User", id.String())
			}
			return apierr.Internal("verify user principal")
		}
	case db.PrincipalKindGroup:
		if _, err := qtx.GetGroupByID(ctx, db.GetGroupByIDParams{ID: id, OrgID: orgID}); err != nil {
			if isNotFound(err) {
				return apierr.NotFound("Group", id.String())
			}
			return apierr.Internal("verify group principal")
		}
	}
	return nil
}

// validateRoleOnlyMask rejects update_mask paths other than "role".
// Allows nil/empty (treats as "update all mutable fields", which is
// just role today).
func validateRoleOnlyMask(paths []string) error {
	for _, p := range paths {
		if p != "role" {
			return apierr.InvalidArgument(apierr.FieldViolation("update_mask",
				fmt.Sprintf("only `role` is mutable; got %q", p)))
		}
	}
	return nil
}

// buildOrgMemberProto constructs the Member wire shape from values
// already in hand at the call site — avoiding a follow-up
// GetOrgMember round-trip after a write.
func buildOrgMemberProto(orgSlug, roleName string, kind db.PrincipalKind, principalID, memberID uuid.UUID, etag string, createTime, updateTime time.Time) *iampb.Member {
	return convert.OrgMemberRowToProto(db.GetOrgMemberRow{
		ID:            memberID,
		PrincipalKind: kind,
		PrincipalID:   principalID,
		RoleName:      roleName,
		Etag:          etag,
		CreateTime:    createTime,
		UpdateTime:    updateTime,
	}, orgSlug)
}

// isNotFound returns true if err is pgx.ErrNoRows. Defined here as
// a convenience so handler call sites stay readable.
func isNotFound(err error) bool {
	return err == pgx.ErrNoRows
}
