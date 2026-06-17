package permission

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// Target is what a permission check is asked against — an org, or a
// space (which transitively inherits role grants from its parent
// org). One of OrgID / SpaceID is set; never both, never neither.
type Target struct {
	OrgID   uuid.UUID
	SpaceID uuid.UUID
}

// OrgTarget returns a target scoped to an organization.
func OrgTarget(orgID uuid.UUID) Target {
	return Target{OrgID: orgID}
}

// SpaceTarget returns a target scoped to a space. Resolution unions
// direct space-level bindings with the parent org's bindings (locked
// IAM decision #1: org → space role inheritance).
func SpaceTarget(spaceID uuid.UUID) Target {
	return Target{SpaceID: spaceID}
}

// Resolver answers "does this Firebase identity have this permission
// against this target scope?" by resolving the identity's effective
// permissions through the `role_permissions` table — joining
// org_members / space_members / group_members against role_permissions
// (the catalog-driven grant data, keyed by role_id, so system and
// custom roles resolve identically).
//
// Resolver does NOT verify the identity exists, that the caller is
// authenticated, or anything else upstream — those are the auth
// interceptor's job. This layer only resolves *authorization* given
// an already-known identity.
type Resolver struct {
	queries db.Querier
}

// NewResolver returns a Resolver backed by the given Querier.
func NewResolver(queries db.Querier) *Resolver {
	return &Resolver{queries: queries}
}

// HasPermission reports whether `identity` has `permission` against
// `target`. Returns (false, nil) on legitimate deny and (false, err)
// on DB faults — callers must treat these as different (a fault
// should surface as Internal, not silently 403).
//
// For OrgTarget: checks the union of direct user bindings and
// group-derived bindings in `org_members` for the org.
//
// For SpaceTarget: checks the union of (a) direct space-level
// bindings in `space_members`, and (b) parent-org-level bindings
// inherited via `org_members`. Either path can grant — locked
// inheritance decision #1.
func (r *Resolver) HasPermission(ctx context.Context, identity uuid.UUID, target Target, permission string) (bool, error) {
	perms, err := r.effectivePermissions(ctx, identity, target)
	if err != nil {
		return false, err
	}
	_, ok := perms[permission]
	return ok, nil
}

// effectivePermissions returns the set of catalog permission_id strings
// the identity holds at the target scope, resolved through role_permissions.
func (r *Resolver) effectivePermissions(ctx context.Context, identity uuid.UUID, target Target) (map[string]struct{}, error) {
	var list []string
	var err error
	switch {
	case target.OrgID != uuid.Nil && target.SpaceID == uuid.Nil:
		list, err = r.queries.EffectiveOrgPermissions(ctx, db.EffectiveOrgPermissionsParams{
			OrgID:      target.OrgID,
			IdentityID: convert.PgUUID(identity),
		})
	case target.SpaceID != uuid.Nil && target.OrgID == uuid.Nil:
		list, err = r.queries.EffectiveSpacePermissions(ctx, db.EffectiveSpacePermissionsParams{
			SpaceID:    target.SpaceID,
			IdentityID: convert.PgUUID(identity),
		})
	default:
		return nil, fmt.Errorf("invalid target: exactly one of OrgID/SpaceID must be set")
	}
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(list))
	for _, p := range list {
		set[p] = struct{}{}
	}
	return set, nil
}

// TestPermissions returns the subset of `permissions` the identity is
// allowed against `target`. Powers the `Iam.TestIamPermissions` RPC
// (UI-side gating: "which buttons should I show enabled?") with one
// effective-permission lookup per call rather than N HasPermission calls.
func (r *Resolver) TestPermissions(ctx context.Context, identity uuid.UUID, target Target, permissions []string) ([]string, error) {
	perms, err := r.effectivePermissions(ctx, identity, target)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(permissions))
	for _, p := range permissions {
		if _, ok := perms[p]; ok {
			allowed = append(allowed, p)
		}
	}
	return allowed, nil
}
