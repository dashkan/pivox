package server

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// Canonical messages. Centralized so the unary and stream interceptors
// can't drift, and so the wording is one-edit-away.
const (
	errMissingAuthenticatedCaller = "missing authenticated caller"

	// memberlessRecoveryMessage is returned when the caller has zero
	// memberships. Routes the caller through the bootstrap allowlist
	// (CreateOrganization / AcceptInvitation) so they can acquire
	// their first membership.
	memberlessRecoveryMessage = "caller has no organization membership; create or accept an invitation to an organization first"
)

// membershipExemptMethods is the bootstrap allowlist — RPCs that an
// authenticated caller with zero org memberships is permitted to call.
// All methods reaching this interceptor have already passed the
// AuthInterceptor; service-to-service surfaces (AgentService) live on a
// separate gRPC server and never reach this chain.
//
// Adding to this list is a security-sensitive change. Each entry must
// be safe to invoke without org context, and ideally must be on the
// path a freshly-registered or invited account uses to *acquire* its
// first membership. Anything outside this list returns
// `PermissionDenied` for memberless callers.
//
//   - `CreateOrganization` — the founder path: caller creates an org
//     and is auto-added as the owner in the same transaction.
//   - `ListOrganizations` — the post-signin "which orgs am I in?"
//     query. Memberless callers see an empty list, which the native
//     client uses to detect the zero-membership state and route to
//     the org-creation screen.
//   - `AcceptInvitation` — the invite path: invitee is added to the
//     inviting org in the same transaction.
//   - `GetInvitation` — lets an invitee inspect the invitation
//     pointing at their email before accepting it.
//   - `Iam.ListAccountOrganizations` — the same bootstrap shape as
//     `Organizations.ListOrganizations`, but for the slim
//     account-scoped view used by the web org-gate. Gating "do I
//     have membership?" on prior membership is chicken-and-egg; the
//     query is hard-scoped to the caller's own bindings so there is
//     no over-disclosure vector beyond what authn already grants.
var membershipExemptMethods = map[string]bool{
	"/pivox.api.v1.Organizations/CreateOrganization": true,
	"/pivox.api.v1.Organizations/ListOrganizations":  true,
	"/pivox.api.v1.Organizations/AcceptInvitation":   true,
	"/pivox.api.v1.Organizations/GetInvitation":      true,
	"/pivox.iam.v1.Iam/ListAccountOrganizations":     true,
	// DeleteAccount targets accounts/me, the caller's own account —
	// no org scope. A user stuck in a half-bootstrapped state (identity
	// exists, no org memberships) must still be able to
	// delete their account; gating this on membership would lock
	// them out of recovery.
	"/pivox.iam.v1.Iam/DeleteAccount": true,
}

// requireMembership is the shared body for both unary and stream
// membership interceptors. Returns nil if the caller has at least one
// membership (or if the method is on the bootstrap allowlist), or an
// apierr.* error otherwise. Single source of truth so unary and
// stream chains can't drift.
func requireMembership(ctx context.Context, queries db.Querier, fullMethod string) error {
	if membershipExemptMethods[fullMethod] {
		return nil
	}
	identityID, ok := UserID(ctx)
	if !ok {
		// AuthInterceptor rejects any token it can't resolve to an
		// identity id before this interceptor runs, so reaching here without
		// the UUID means the interceptor chain is misconfigured (e.g.,
		// membership interceptor wired without auth in front of it).
		// Return Unauthenticated for caller-facing consistency.
		return apierr.Unauthenticated(errMissingAuthenticatedCaller)
	}
	// Membership = at least one org the caller's identity is a direct
	// member of (post-Phase-7 unification: no per-org `users` row;
	// membership is `org_members.principal_id` = identities.id).
	// `ListOrganizationsForIdentity` is the canonical query — same one
	// ListOrganizations uses, so the gate and the read RPC see the
	// same set. If the identity row was hard-deleted after the JWT was
	// minted, the join returns empty and the caller is treated as
	// memberless — correct semantics, no separate identity lookup
	// needed.
	orgs, err := queries.ListOrganizationsForIdentity(ctx, convert.PgUUID(identityID))
	if err != nil {
		slog.ErrorContext(ctx, "membership: lookup memberships failed", "identity_id", identityID, "error", err)
		return apierr.Internal(err, "lookup memberships")
	}
	if len(orgs) == 0 {
		return apierr.PermissionDenied(memberlessRecoveryMessage)
	}
	return nil
}

// MembershipRequiredInterceptor returns a gRPC unary server interceptor
// that enforces the system-wide invariant: every authenticated caller
// has at least one org membership before any RPC outside the bootstrap
// allowlist is dispatched.
//
// Why this exists: registration is non-atomic across Keycloak and
// Pivox (KC user creation + identity-sync can land and then Pivox
// CreateOrganization can fail or never get called — KC owns
// registration, so we can't bundle them server-side). Without this
// interceptor, the half-registered "account exists, no membership"
// state would let callers touch resources they have no claim to. With
// it, those callers can only reach allowlisted methods, which exist
// specifically to recover them into a membership.
//
// Must run AFTER `AuthInterceptor` so the auth context is populated.
func MembershipRequiredInterceptor(queries db.Querier) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if err := requireMembership(ctx, queries, info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// MembershipRequiredStreamInterceptor is the streaming variant of
// `MembershipRequiredInterceptor`. Same semantics, same allowlist.
func MembershipRequiredStreamInterceptor(queries db.Querier) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := requireMembership(ss.Context(), queries, info.FullMethod); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}
