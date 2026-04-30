package server

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// Canonical messages. Centralized so the unary and stream interceptors
// can't drift, and so the wording is one-edit-away.
const (
	errMissingAuthenticatedCaller = "missing authenticated caller"

	// memberlessRecoveryMessage is returned for both "caller has no
	// firebase_identity row yet" (race with the sync-identity webhook
	// on a freshly-Firebase-registered user) AND "caller has an
	// identity but zero memberships". Same code, same message — both
	// states route the caller through the same recovery path (the
	// bootstrap allowlist).
	memberlessRecoveryMessage = "caller has no organization membership; create or accept an invitation to an organization first"
)

// membershipExemptMethods is the bootstrap allowlist — RPCs that an
// authenticated caller with zero org memberships is permitted to call.
// All methods reaching this interceptor have already passed Firebase
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
var membershipExemptMethods = map[string]bool{
	"/pivox.api.v1.Organizations/CreateOrganization": true,
	"/pivox.api.v1.Organizations/ListOrganizations":  true,
	"/pivox.api.v1.Organizations/AcceptInvitation":   true,
	"/pivox.api.v1.Organizations/GetInvitation":      true,
	// DeleteAccount targets the singleton accounts/me — no org
	// scope. A user stuck in a half-bootstrapped state (firebase
	// identity exists, no org memberships) must still be able to
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
	uid, ok := AuthenticatedUID(ctx)
	if !ok {
		return apierr.Unauthenticated(errMissingAuthenticatedCaller)
	}
	identity, err := queries.GetIdentityByFirebaseUID(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.PermissionDenied(memberlessRecoveryMessage)
		}
		slog.ErrorContext(ctx, "membership: lookup firebase identity failed", "uid", uid, "error", err)
		return apierr.Internal("lookup firebase identity")
	}
	// Membership = at least one org the caller's firebase_identity is
	// a direct member of (post-Phase-7 unification: there's no per-org
	// `users` row; membership is `org_members.principal_id` =
	// identities.id). `ListOrganizationsForIdentity`
	// is the canonical query — same one ListOrganizations uses, so
	// the gate and the read RPC see the same set.
	orgs, err := queries.ListOrganizationsForIdentity(ctx, convert.PgUUID(identity.ID))
	if err != nil {
		slog.ErrorContext(ctx, "membership: lookup memberships failed", "identity_id", identity.ID, "error", err)
		return apierr.Internal("lookup memberships")
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
// Why this exists: registration is non-atomic across Firebase Auth and
// Pivox (Firebase create-user can succeed and then Pivox
// CreateOrganization can fail or never get called — the password-
// isolation rule means we can't bundle them server-side). Without this
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
