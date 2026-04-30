package spaces

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// space-scope Member handlers. Companion to the org-scope Member
// handlers on the Organizations service. Same wire shape (`Member`
// proto from iam/v1 is shared); different DB table (`space_members`)
// and no ≥1-owner boundary check.
//
// IMPORTANT: ListMembers here returns ONLY direct space-level
// bindings. Org-level inheritance (an org-admin acting at space
// scope) is resolved at runtime by the permission resolver and does
// NOT surface as a Member resource at the space scope — that would
// conflate authorization (resolver) with the canonical role-binding
// catalog (this list).

type spaceMemberPath struct {
	orgSlug       string
	spaceSlug     string
	principalKind db.PrincipalKind
	principalID   uuid.UUID
}

// parseSpaceMemberName parses
// `organizations/{org}/spaces/{space}/members/{member}` where
// `{member}` is `user-{uuid}` or `group-{uuid}`.
func parseSpaceMemberName(name string) (spaceMemberPath, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "spaces" || parts[4] != "members" ||
		parts[1] == "" || parts[3] == "" || parts[5] == "" {
		return spaceMemberPath{}, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member name %q: expected organizations/{org}/spaces/{space}/members/{member}", name)))
	}
	kind, id, err := permission.ParseMemberSegment(parts[5])
	if err != nil {
		return spaceMemberPath{}, err
	}
	return spaceMemberPath{
		orgSlug:       parts[1],
		spaceSlug:     parts[3],
		principalKind: kind,
		principalID:   id,
	}, nil
}

// parseSpaceMemberParent parses
// `organizations/{org}/spaces/{space}` (the parent for space-scope
// Member listing/creation). Returns (orgSlug, spaceSlug).
func parseSpaceMemberParent(parent string) (orgSlug, spaceSlug string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" || parts[1] == "" || parts[3] == "" {
		return "", "", apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/{org}/spaces/{space}", parent)))
	}
	return parts[1], parts[3], nil
}

// GetMember resolves a space-scope Member by resource name. Reads
// from the `space_members` table only — does NOT include org-level
// inheritance (use TestIamPermissions for the unioned view of
// effective roles).
func (s *SpacesServer) GetMember(ctx context.Context, req *iampb.GetMemberRequest) (*iampb.Member, error) {
	path, err := parseSpaceMemberName(req.GetName())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if path.orgSlug != resolvedOrg.Slug || path.spaceSlug != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"slugs in member path do not match resolved scope"))
	}
	row, err := s.queries.GetSpaceMember(ctx, db.GetSpaceMemberParams{
		SpaceID:       resolvedSpace.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	return convert.SpaceMemberRowToProto(row, path.orgSlug, path.spaceSlug, nil), nil
}

// ListMembers returns space-scope Members. Direct bindings only —
// see GetMember doc comment for the inheritance caveat.
func (s *SpacesServer) ListMembers(ctx context.Context, req *iampb.ListMembersRequest) (*iampb.ListMembersResponse, error) {
	parentOrgSlug, parentSpaceSlug, err := parseSpaceMemberParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if parentOrgSlug != resolvedOrg.Slug || parentSpaceSlug != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"slugs in parent do not match resolved scope"))
	}
	pageSize, offset, err := parseMembersPaging(req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListSpaceMembers(ctx, db.ListSpaceMembersParams{
		SpaceID: resolvedSpace.ID,
		Offset:  offset,
		Limit:   int64(pageSize) + 1,
	})
	if err != nil {
		slog.ErrorContext(ctx, "list space members failed", "space_id", resolvedSpace.ID, "error", err)
		return nil, apierr.Internal("list members")
	}
	hasMore := len(rows) > int(pageSize)
	if hasMore {
		rows = rows[:pageSize]
	}
	out := make([]*iampb.Member, len(rows))
	for i, r := range rows {
		out[i] = convert.SpaceMemberToProto(r, resolvedOrg.Slug, resolvedSpace.Slug, nil)
	}
	resp := &iampb.ListMembersResponse{Members: out}
	if hasMore {
		resp.NextPageToken = encodeMembersPageToken(offset + int64(pageSize))
	}
	return resp, nil
}

const (
	defaultMembersPageSize = 50
	maxMembersPageSize     = 500
)

// parseMembersPaging mirrors the org-side helper; kept as a sibling
// here rather than a shared package because the two services don't
// share an internal helpers module yet and the function is small
// enough that duplication is cheaper than introducing one.
func parseMembersPaging(reqPageSize int32, pageToken string) (pageSize int32, offset int64, err error) {
	pageSize = reqPageSize
	if pageSize <= 0 {
		pageSize = defaultMembersPageSize
	}
	if pageSize > maxMembersPageSize {
		pageSize = maxMembersPageSize
	}
	if pageToken == "" {
		return pageSize, 0, nil
	}
	off, parseErr := strconv.ParseInt(pageToken, 10, 64)
	if parseErr != nil || off < 0 {
		return 0, 0, apierr.InvalidArgument(apierr.FieldViolation("page_token",
			"page_token is not a valid cursor"))
	}
	return pageSize, off, nil
}

func encodeMembersPageToken(off int64) string { return strconv.FormatInt(off, 10) }

// CreateMember binds a principal (user or group) to a role at space
// scope. The principal must already exist in the org that owns this
// space; the role must be a system role (v1).
//
// Tx-wrapped: principal-existence check + insert run in one
// transaction so a concurrent principal soft-delete cannot race
// between them and produce a dead binding.
//
// No ≥1-owner boundary at space scope — spaces don't have a sole-
// owner invariant; the inherited org-admin path means a space is
// never operationally ownerless even if no direct space-owner
// binding exists.
func (s *SpacesServer) CreateMember(ctx context.Context, req *iampb.CreateMemberRequest) (*iampb.Member, error) {
	parentOrgSlug, parentSpaceSlug, err := parseSpaceMemberParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if parentOrgSlug != resolvedOrg.Slug || parentSpaceSlug != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			"slugs in parent do not match resolved scope"))
	}
	orgSlug := resolvedOrg.Slug
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

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}

	// Tx-wrapped: role lookup + principal-existence check + insert run
	// atomically. Mirrors the org-scope CreateMember pattern.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	role, err := qtx.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: resolvedOrg.ID,
		Name:  roleSlug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Role", mem.GetRole())
	}

	if err := verifyPrincipalInOrg(ctx, qtx, resolvedOrg.ID, principalKind, principalID); err != nil {
		return nil, err
	}

	row, err := qtx.CreateSpaceMember(ctx, db.CreateSpaceMemberParams{
		ID:            uuid.New(),
		SpaceID:       resolvedSpace.ID,
		RoleID:        role.ID,
		PrincipalKind: principalKind,
		PrincipalID:   principalID,
		CreatedBy:     convert.PgUUID(caller),
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetParent())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("commit transaction")
	}

	return convert.SpaceMemberRowToProto(db.GetSpaceMemberRow{
		ID:            row.ID,
		SpaceID:       resolvedSpace.ID,
		RoleID:        role.ID,
		PrincipalKind: principalKind,
		PrincipalID:   principalID,
		RoleName:      role.Name,
		Etag:          row.Etag,
		CreateTime:    row.CreateTime,
		UpdateTime:    row.UpdateTime,
	}, orgSlug, resolvedSpace.Slug, nil), nil
}

// UpdateMember mutates the role of an existing space-scope Member.
// Only `role` is mutable. No boundary check — spaces have no
// ≥1-owner invariant.
func (s *SpacesServer) UpdateMember(ctx context.Context, req *iampb.UpdateMemberRequest) (*iampb.Member, error) {
	mem := req.GetMember()
	if mem == nil || mem.GetName() == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("member.name", "member.name is required"))
	}
	if err := validateRoleOnlyMask(req.GetUpdateMask().GetPaths()); err != nil {
		return nil, err
	}
	path, err := parseSpaceMemberName(mem.GetName())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if path.orgSlug != resolvedOrg.Slug || path.spaceSlug != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("member.name",
			"slugs in member path do not match resolved scope"))
	}
	roleSlug, err := parseRoleRef(mem.GetRole(), path.orgSlug)
	if err != nil {
		return nil, err
	}

	// Tx-wrapped: role lookup + binding mutation run atomically so a
	// concurrent role rename (or v2 custom-role delete) cannot race in
	// between and produce a binding that points at a different role
	// than the caller asked for. Mirrors the org-scope UpdateMember,
	// minus the ≥1-owner boundary check (spaces have no sole-owner
	// invariant — inherited org-admin keeps a space reachable even
	// without a direct space-owner binding).
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	newRole, err := qtx.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: resolvedOrg.ID,
		Name:  roleSlug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Role", mem.GetRole())
	}
	row, err := qtx.UpdateSpaceMemberRole(ctx, db.UpdateSpaceMemberRoleParams{
		SpaceID:       resolvedSpace.ID,
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

	return convert.SpaceMemberRowToProto(db.GetSpaceMemberRow{
		ID:            row.ID,
		SpaceID:       resolvedSpace.ID,
		RoleID:        newRole.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
		RoleName:      newRole.Name,
		Etag:          row.Etag,
		CreateTime:    row.CreateTime,
		UpdateTime:    row.UpdateTime,
	}, path.orgSlug, path.spaceSlug, nil), nil
}

// principalFromMember pulls (kind, id) out of the Member proto's
// `principal` oneof. The principal resource ref must address the
// same org as the parent.
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

// verifyPrincipalInOrg confirms the principal exists so we don't
// insert dead bindings (space_members.principal_id is NOT a DB FK
// — it's polymorphic by principal_kind). Post-Phase-7 the user
// check is "firebase_identity row exists"; the per-org `users` row
// was dropped. orgID is preserved on the signature but only used
// for groups (which remain org-scoped).
func verifyPrincipalInOrg(ctx context.Context, qtx db.Querier, orgID uuid.UUID, kind db.PrincipalKind, id uuid.UUID) error {
	switch kind {
	case db.PrincipalKindUser:
		if _, err := qtx.GetIdentityForMember(ctx, id); err != nil {
			return apierr.HandleResourceError(err, "User", id.String())
		}
	case db.PrincipalKindGroup:
		if _, err := qtx.GetGroupByID(ctx, db.GetGroupByIDParams{ID: id, OrgID: orgID}); err != nil {
			return apierr.HandleResourceError(err, "Group", id.String())
		}
	}
	return nil
}

// validateRoleOnlyMask rejects update_mask paths other than "role".
func validateRoleOnlyMask(paths []string) error {
	for _, p := range paths {
		if p != "role" {
			return apierr.InvalidArgument(apierr.FieldViolation("update_mask",
				fmt.Sprintf("only `role` is mutable; got %q", p)))
		}
	}
	return nil
}

// DeleteMember removes a space-scope Member binding. No boundary
// check.
func (s *SpacesServer) DeleteMember(ctx context.Context, req *iampb.DeleteMemberRequest) (*emptypb.Empty, error) {
	path, err := parseSpaceMemberName(req.GetName())
	if err != nil {
		return nil, err
	}
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	resolvedSpace := server.MustResolvedSpaceFromContext(ctx)
	if path.orgSlug != resolvedOrg.Slug || path.spaceSlug != resolvedSpace.Slug {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"slugs in member path do not match resolved scope"))
	}
	n, err := s.queries.DeleteSpaceMember(ctx, db.DeleteSpaceMemberParams{
		SpaceID:       resolvedSpace.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.Internal("delete space member")
	}
	if n == 0 {
		return nil, apierr.NotFound("Member", req.GetName())
	}
	return &emptypb.Empty{}, nil
}
