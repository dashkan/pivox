package organizations

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
)

// TransferOwnership atomically promotes the named user to `owner`
// and demotes the existing sole owner to `admin`, in a single
// transaction so the org never has zero owners during the swap.
//
// Preconditions (FAILED_PRECONDITION otherwise):
//   - target user is already a member of this org and is not the
//     current owner
//   - org has exactly one current owner (multi-owner orgs should use
//     UpdateMember + CreateMember to add/remove owners — no
//     0-owner-window concern there)
//   - current owner is a user (group-as-owner is not transferable
//     via this verb)
func (s *OrganizationsServer) TransferOwnership(ctx context.Context, req *apiv1.TransferOwnershipRequest) (*apiv1.TransferOwnershipResponse, error) {
	orgSlug, err := parseTransferOwnershipName(req.GetName())
	if err != nil {
		return nil, err
	}
	newOwnerID, err := parseTransferNewOwner(req.GetNewOwner(), orgSlug)
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetName())
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, apierr.Internal("begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := db.New(tx)

	// Target must already be an org member; verify and fetch current
	// role (must NOT already be owner). Transfer is user-only — group
	// owners are not transferable via this verb.
	target, err := qtx.GetOrgMemberByUser(ctx, db.GetOrgMemberByUserParams{
		OrgID:  org.ID,
		UserID: convert.PgUUID(newOwnerID),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, apierr.FailedPrecondition("new_owner is not a member of this organization; CreateMember first")
		}
		return nil, apierr.Internal("lookup target member")
	}
	if target.RoleName == permission.RoleOwner {
		return nil, apierr.FailedPrecondition("new_owner is already the owner")
	}

	// Current owners must be exactly 1 user (the to-be-demoted user).
	// Group-owner detection runs FIRST: a (1 group + 1 user) configuration
	// would otherwise route through the multi-owner branch and steer the
	// caller toward UpdateMember-only — without flagging that the group
	// binding is what makes TransferOwnership the wrong verb here.
	owners, err := qtx.ListOrgOwnerMembers(ctx, org.ID)
	if err != nil {
		return nil, apierr.Internal("list owners")
	}
	for _, o := range owners {
		if !o.UserID.Valid {
			return nil, apierr.FailedPrecondition("organization has a group owner; transfer via UpdateMember + CreateMember rather than TransferOwnership")
		}
	}
	switch {
	case len(owners) == 0:
		return nil, apierr.FailedPrecondition("organization has no current owner; restore one before transferring")
	case len(owners) > 1:
		return nil, apierr.FailedPrecondition("organization has multiple owners; use UpdateMember to demote one and promote another")
	}
	prevOwner := owners[0]
	prevOwnerID := uuid.UUID(prevOwner.UserID.Bytes)

	// Resolve owner + admin role IDs in this org.
	ownerRole, err := qtx.GetSystemRole(ctx, db.GetSystemRoleParams{OrgID: org.ID, Name: permission.RoleOwner})
	if err != nil {
		return nil, apierr.Internal("resolve owner role")
	}
	adminRole, err := qtx.GetSystemRole(ctx, db.GetSystemRoleParams{OrgID: org.ID, Name: permission.RoleAdmin})
	if err != nil {
		return nil, apierr.Internal("resolve admin role")
	}

	// Atomic two-row swap. Order matters only insofar as both must
	// commit — if either fails, tx rolls back and ownership is
	// unchanged.
	if _, err := qtx.UpdateOrgUserMemberRole(ctx, db.UpdateOrgUserMemberRoleParams{
		OrgID:  org.ID,
		UserID: convert.PgUUID(prevOwnerID),
		RoleID: adminRole.ID,
	}); err != nil {
		return nil, apierr.Internal("demote current owner")
	}
	if _, err := qtx.UpdateOrgUserMemberRole(ctx, db.UpdateOrgUserMemberRoleParams{
		OrgID:  org.ID,
		UserID: convert.PgUUID(newOwnerID),
		RoleID: ownerRole.ID,
	}); err != nil {
		return nil, apierr.Internal("promote target")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apierr.Internal("commit transaction")
	}

	return &apiv1.TransferOwnershipResponse{
		NewOwner:      fmt.Sprintf("organizations/%s/users/%s", orgSlug, newOwnerID),
		PreviousOwner: fmt.Sprintf("organizations/%s/users/%s", orgSlug, prevOwnerID),
	}, nil
}

// parseTransferOwnershipName extracts the org slug from
// `organizations/{org}`.
func parseTransferOwnershipName(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid name %q: expected organizations/{org}", name)))
	}
	return parts[1], nil
}

// parseTransferNewOwner extracts the user uuid from
// `organizations/{org}/users/{user}` and verifies the org matches.
func parseTransferNewOwner(ref, parentOrgSlug string) (uuid.UUID, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "users" || parts[1] == "" || parts[3] == "" {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("new_owner",
			fmt.Sprintf("invalid new_owner %q: expected organizations/{org}/users/{user}", ref)))
	}
	if parts[1] != parentOrgSlug {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("new_owner",
			fmt.Sprintf("new_owner org %q does not match request name org %q", parts[1], parentOrgSlug)))
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("new_owner",
			fmt.Sprintf("invalid uuid in new_owner %q: %v", ref, err)))
	}
	return id, nil
}
