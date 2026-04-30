package convert

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
)

// PermissionToProto converts a DB permission row to its wire shape.
// Permissions are global (no parent), so the resource name is just
// `permissions/{permission_id}`.
func PermissionToProto(p db.Permission) *iampb.Permission {
	return &iampb.Permission{
		Name:        fmt.Sprintf("permissions/%s", p.PermissionID),
		DisplayName: p.DisplayName,
		Description: p.Description,
	}
}

// OrgMemberToProto converts a joined org_members row (member columns
// plus the joined role.name) to the unified Member proto. The proto's
// resource name encodes the principal as `user-{uuid}` or
// `group-{uuid}` — uniqueness-by-construction means at most one
// Member per (scope, principal). Caller passes the org slug; the
// converter does NOT re-resolve it. `actors` is the pre-resolved
// Actor map; pass nil to skip Actor inflation.
func OrgMemberToProto(m db.ListOrgMembersRow, orgSlug string, actors map[uuid.UUID]*typespb.Actor) *iampb.Member {
	pb := &iampb.Member{
		Name:       fmt.Sprintf("organizations/%s/members/%s-%s", orgSlug, m.PrincipalKind, m.PrincipalID),
		Role:       fmt.Sprintf("organizations/%s/roles/%s", orgSlug, m.RoleName),
		Etag:       m.Etag,
		CreatedBy:  actorOrNil(actors, m.CreatedBy),
		CreateTime: timestamppb.New(m.CreateTime),
		UpdatedBy:  actorOrNil(actors, m.UpdatedBy),
		UpdateTime: timestamppb.New(m.UpdateTime),
	}
	switch m.PrincipalKind {
	case db.PrincipalKindUser:
		pb.Principal = &iampb.Member_User{
			User: fmt.Sprintf("organizations/%s/users/%s", orgSlug, m.PrincipalID),
		}
	case db.PrincipalKindGroup:
		pb.Principal = &iampb.Member_Group{
			Group: fmt.Sprintf("organizations/%s/groups/%s", orgSlug, m.PrincipalID),
		}
	}
	return pb
}

// OrgMemberRowToProto is the GetOrgMember equivalent of
// OrgMemberToProto; sqlc generates separate row types for :one vs
// :many even when the SELECT clauses match, so we provide both
// converters as thin wrappers.
func OrgMemberRowToProto(m db.GetOrgMemberRow, orgSlug string, actors map[uuid.UUID]*typespb.Actor) *iampb.Member {
	return OrgMemberToProto(db.ListOrgMembersRow(m), orgSlug, actors)
}

// SpaceMemberToProto converts a joined space_members row to the
// unified Member proto at space scope. The resource name nests under
// the space: `organizations/{org}/spaces/{space}/members/{member}`.
// Caller passes both org and space slugs. `actors` is the pre-resolved
// Actor map.
func SpaceMemberToProto(m db.ListSpaceMembersRow, orgSlug, spaceSlug string, actors map[uuid.UUID]*typespb.Actor) *iampb.Member {
	pb := &iampb.Member{
		Name:       fmt.Sprintf("organizations/%s/spaces/%s/members/%s-%s", orgSlug, spaceSlug, m.PrincipalKind, m.PrincipalID),
		Role:       fmt.Sprintf("organizations/%s/roles/%s", orgSlug, m.RoleName),
		Etag:       m.Etag,
		CreatedBy:  actorOrNil(actors, m.CreatedBy),
		CreateTime: timestamppb.New(m.CreateTime),
		UpdatedBy:  actorOrNil(actors, m.UpdatedBy),
		UpdateTime: timestamppb.New(m.UpdateTime),
	}
	switch m.PrincipalKind {
	case db.PrincipalKindUser:
		pb.Principal = &iampb.Member_User{
			User: fmt.Sprintf("organizations/%s/users/%s", orgSlug, m.PrincipalID),
		}
	case db.PrincipalKindGroup:
		pb.Principal = &iampb.Member_Group{
			Group: fmt.Sprintf("organizations/%s/groups/%s", orgSlug, m.PrincipalID),
		}
	}
	return pb
}

// SpaceMemberRowToProto mirrors OrgMemberRowToProto for space scope.
func SpaceMemberRowToProto(m db.GetSpaceMemberRow, orgSlug, spaceSlug string, actors map[uuid.UUID]*typespb.Actor) *iampb.Member {
	return SpaceMemberToProto(db.ListSpaceMembersRow(m), orgSlug, spaceSlug, actors)
}

// RoleToProto converts a DB role row to its wire shape. `orgName` is
// the organization slug — the proto's resource name format is
// `organizations/{org}/roles/{role}` and we already have the slug at
// the call site (it's how we resolved the org id in the first place).
//
// `permissions` is the list of permission_id strings the role grants;
// callers resolve this from the role_permissions join table OR (for
// system roles in v1) from the static permission matrix. Passed in
// rather than re-queried so the converter stays pure.
func RoleToProto(r db.Role, orgName string, permissions []string) *iampb.Role {
	pb := &iampb.Role{
		Name:        fmt.Sprintf("organizations/%s/roles/%s", orgName, r.Name),
		DisplayName: r.DisplayName,
		Description: r.Description,
		Permissions: permissions,
		System:      r.IsSystem,
		Etag:        r.Etag,
		CreateTime:  timestamppb.New(r.CreateTime),
		UpdateTime:  timestamppb.New(r.UpdateTime),
	}
	return pb
}
