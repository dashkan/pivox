package organizations

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
)

// systemRoles is the canonical list of (name, display_name) tuples
// seeded for every newly-created org. Order is significant only in
// that owner is first — bootstrapOrgRoles relies on iterating this
// slice and capturing the owner role's id for the founder binding.
var systemRoles = []struct {
	name        string
	displayName string
	description string
}{
	{permission.RoleOwner, "Owner", "Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users)."},
	{permission.RoleAdmin, "Admin", "Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content."},
	{permission.RoleEditor, "Editor", "Content management — assets, requests, line items, and AI conversations."},
	{permission.RoleViewer, "Viewer", "Read-only access across the organization."},
}

// bootstrapOrgRoles seeds the 4 system roles for a newly-created org
// and binds the founder to the owner role. All inserts run on the
// caller's qtx so the bootstrap is part of the surrounding
// transaction — a failed seed rolls back the org-create itself.
//
// Caller responsibilities: org and founder user rows must already
// exist in the same transaction (FK targets must resolve at commit
// time). bootstrapOrgRoles itself does not begin or commit.
func bootstrapOrgRoles(ctx context.Context, qtx db.Querier, orgID, founderUserID uuid.UUID, createdBy string) error {
	var ownerRoleID uuid.UUID
	for _, r := range systemRoles {
		id := uuid.New()
		if err := qtx.CreateRole(ctx, db.CreateRoleParams{
			ID:          id,
			OrgID:       orgID,
			Name:        r.name,
			DisplayName: r.displayName,
			Description: r.description,
			IsSystem:    true,
		}); err != nil {
			return fmt.Errorf("seed system role %q: %w", r.name, err)
		}
		if r.name == permission.RoleOwner {
			ownerRoleID = id
		}
	}

	// Bind the founder to the just-created owner role. Using
	// PrincipalKind=user (not group) because group bindings are
	// reserved for Iam.AddGroupMembers in v1.
	if err := qtx.CreateOrgMember(ctx, db.CreateOrgMemberParams{
		ID:            uuid.New(),
		OrgID:         orgID,
		RoleID:        ownerRoleID,
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   founderUserID,
		CreatedBy:     createdBy,
	}); err != nil {
		return fmt.Errorf("bind founder to owner role: %w", err)
	}
	return nil
}
