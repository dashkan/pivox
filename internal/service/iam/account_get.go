// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package iam

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// claimDisplayName is the OIDC token claim carrying the caller's
// human-readable name. Keycloak populates it from the user's profile.
const claimDisplayName = "name"

// GetAccount returns the authenticated caller's account — the
// `accounts/me` whoami. Every field is sourced from the verified
// access token, not a Pivox-side profile replica: `subject` is the
// token's `sub` (== identities.id), `email` and `display_name` come
// from the `email` and `name` claims, and `active_organization` is
// derived from the `organization` claim.
//
// `active_organization` is the single org the token is scoped to. It
// is resolved against the caller's OWN memberships via the exact same
// query + role computation as `ListAccountOrganizations`, so its
// `role` and `display_name` match that endpoint byte-for-byte. It is
// left unset when the token carries no `organization` claim (the
// non-MCP web/electron tokens, where the claim is gated behind the
// `organization` scope) or when the claimed org is not one the caller
// is an active member of — a mismatched claim can never surface an org
// the caller has no binding on.
//
// `name` MUST be the literal `accounts/me`; the caller is implicit
// from the authentication context. Membership-exempt, like
// `ListAccountOrganizations` and `DeleteAccount`: a mid-bootstrap
// caller with no memberships must still be able to learn who they are.
func (s *IamServer) GetAccount(ctx context.Context, req *iampb.GetAccountRequest) (*iampb.Account, error) {
	if req.GetName() != "accounts/me" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected accounts/me; the caller is implicit from authentication context"))
	}

	identity := server.MustIdentity(ctx)
	return BuildAccount(ctx, s.queries, identity)
}

// BuildAccount assembles the `accounts/me` whoami for a verified
// identity: `subject` is the token's `sub` (== identities.id), `email`
// and `display_name` come from the token claims, and
// `active_organization` is derived from the `organization` claim
// against the caller's own memberships (nil when the token carries no
// org claim or the claimed org is not one the caller is an active
// member of).
//
// It is the shared core behind the Iam.GetAccount RPC and the MCP
// whoami tool/resource, so both surfaces return byte-identical account
// shapes with no second copy of the resolution logic. Callers that
// don't have the identity on context (the MCP surface) pass it
// explicitly; the RPC reads it via server.MustIdentity.
func BuildAccount(ctx context.Context, q db.Querier, identity *authn.Identity) (*iampb.Account, error) {
	displayName, _ := identity.Claims[claimDisplayName].(string)
	account := &iampb.Account{
		Name:        "accounts/me",
		Subject:     identity.UID,
		Email:       identity.Email,
		DisplayName: displayName,
	}
	activeOrg, err := resolveActiveOrganization(ctx, q, identity)
	if err != nil {
		return nil, err
	}
	account.ActiveOrganization = activeOrg
	return account, nil
}

// resolveActiveOrganization derives the caller's active organization
// from the token's `organization` claim. Returns nil (unset) when the
// token carries no org claim or the claimed org is not among the
// caller's active memberships.
//
// It reuses `ListAccountOrganizationsForIdentity` — the same query
// (and therefore the same highest-precedence role computation)
// `ListAccountOrganizations` runs — then filters to the claimed org.
// This guarantees the reported role is identical to the list
// endpoint's, with no second copy of the role logic to drift.
func resolveActiveOrganization(ctx context.Context, q db.Querier, identity *authn.Identity) (*iampb.AccountOrganization, error) {
	orgs := authn.OrganizationsFromClaims(identity.Claims)
	if len(orgs) == 0 {
		return nil, nil
	}
	// Keycloak's `organization` ANY scope yields a single-element array;
	// the token is pinned to exactly one active org.
	alias := orgs[0]

	// identity.UID is the token's `sub`, which the OIDC verifier already
	// validated as a UUID (== identities.id); parse defensively so a
	// malformed identity fails closed rather than panicking.
	identityID, err := uuid.Parse(identity.UID)
	if err != nil {
		slog.ErrorContext(ctx, "iam: get account: identity UID is not a uuid",
			"uid", identity.UID, "error", err)
		return nil, apierr.Internal(err, "resolve active organization")
	}
	rows, err := q.ListAccountOrganizationsForIdentity(ctx, convert.PgUUID(identityID))
	if err != nil {
		slog.ErrorContext(ctx, "iam: get account: list memberships failed",
			"identity_id", identityID, "error", err)
		return nil, apierr.Internal(err, "resolve active organization")
	}
	// INVARIANT: the Keycloak `organization` alias equals the Pivox org slug.
	// Identity + org provisioning flows KC→Kafka→Pivox, which keeps the KC org
	// alias and the Pivox slug in lockstep. If that ever diverges, a real member
	// with a valid org-scoped token resolves to unset here (fail-closed, safe
	// direction) — whoami would silently report no active org. Guard the sync
	// path against alias/slug drift rather than loosening this match.
	for _, r := range rows {
		if r.Slug == alias {
			return convert.AccountOrganizationToProto(r), nil
		}
	}
	// Token pinned to an org the caller is not an active member of.
	// Fail closed: leave active_organization unset rather than error —
	// the identity fields are still valid whoami output.
	slog.DebugContext(ctx, "iam: get account: token org claim has no matching membership",
		"identity_id", identityID, "org_alias", alias)
	return nil, nil
}
