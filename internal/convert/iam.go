package convert

import (
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
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
