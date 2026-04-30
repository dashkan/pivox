package permission

import (
	"context"
	"fmt"

	"github.com/google/uuid"

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
// against this target scope?" by joining org_members / space_members /
// group_members against the static role-permission matrix.
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
	roles, err := r.effectiveRoles(ctx, identity, target)
	if err != nil {
		return false, err
	}
	for _, role := range roles {
		if Has(role, permission) {
			return true, nil
		}
	}
	return false, nil
}

// effectiveRoles returns the deduplicated set of system-role names
// the identity holds at the target scope. Used internally by
// HasPermission and exposed via TestPermissions for the
// `Iam.TestIamPermissions` RPC handler.
func (r *Resolver) effectiveRoles(ctx context.Context, identity uuid.UUID, target Target) ([]string, error) {
	switch {
	case target.OrgID != uuid.Nil && target.SpaceID == uuid.Nil:
		return r.queries.GetEffectiveOrgRoles(ctx, db.GetEffectiveOrgRolesParams{
			OrgID:              target.OrgID,
			IdentityID: identity,
		})

	case target.SpaceID != uuid.Nil && target.OrgID == uuid.Nil:
		// Resolve the space's parent org first (cheap, single-row
		// lookup) so we can inherit org-level role bindings. A
		// space's parent org is immutable post-create
		// (`spaces.org_id NOT NULL` and there's no UPDATE that
		// touches it), so this could be cached — leaving as a
		// single query for v1 simplicity.
		//
		// TODO(perf): collapse parent-org + space-roles + org-roles
		// into a single SQL with a CTE once we have profiling data.
		// Every authenticated space-scoped RPC currently costs three
		// round-trips; sqlc + the local pool keep it cheap, but it's
		// the obvious win when permission-check latency surfaces.
		parentOrg, err := r.queries.GetSpaceParentOrg(ctx, target.SpaceID)
		if err != nil {
			return nil, fmt.Errorf("resolve parent org for space %s: %w", target.SpaceID, err)
		}
		spaceRoles, err := r.queries.GetEffectiveSpaceRoles(ctx, db.GetEffectiveSpaceRolesParams{
			SpaceID:            target.SpaceID,
			IdentityID: identity,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve space roles: %w", err)
		}
		orgRoles, err := r.queries.GetEffectiveOrgRoles(ctx, db.GetEffectiveOrgRolesParams{
			OrgID:              parentOrg,
			IdentityID: identity,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve org roles for parent org: %w", err)
		}
		return dedupe(append(spaceRoles, orgRoles...)), nil

	default:
		return nil, fmt.Errorf("invalid target: exactly one of OrgID/SpaceID must be set")
	}
}

// TestPermissions returns the subset of `permissions` the identity is
// allowed against `target`. Powers the `Iam.TestIamPermissions` RPC
// (UI-side gating: "which buttons should I show enabled?") with one
// effective-role lookup per call rather than N HasPermission calls.
func (r *Resolver) TestPermissions(ctx context.Context, identity uuid.UUID, target Target, permissions []string) ([]string, error) {
	roles, err := r.effectiveRoles(ctx, identity, target)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(permissions))
	for _, p := range permissions {
		for _, role := range roles {
			if Has(role, p) {
				allowed = append(allowed, p)
				break
			}
		}
	}
	return allowed, nil
}

// dedupe returns a fresh slice with duplicate strings removed,
// preserving first-occurrence order. Always returns a new slice
// (never aliases the input) so callers can mutate either side
// without surprise.
func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
