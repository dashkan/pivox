package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
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
		if membershipExemptMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		uid, ok := AuthenticatedUID(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing authenticated caller")
		}
		account, err := queries.GetAccountByFirebaseUID(ctx, uid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "lookup account: %v", err)
		}
		memberships, err := queries.ListUsersByAccount(ctx, account.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "lookup memberships: %v", err)
		}
		if len(memberships) == 0 {
			return nil, status.Error(codes.PermissionDenied,
				"caller has no organization membership; create or accept an invitation to an organization first")
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
		if membershipExemptMethods[info.FullMethod] {
			return handler(srv, ss)
		}
		ctx := ss.Context()
		uid, ok := AuthenticatedUID(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "missing authenticated caller")
		}
		account, err := queries.GetAccountByFirebaseUID(ctx, uid)
		if err != nil {
			return status.Errorf(codes.Internal, "lookup account: %v", err)
		}
		memberships, err := queries.ListUsersByAccount(ctx, account.ID)
		if err != nil {
			return status.Errorf(codes.Internal, "lookup memberships: %v", err)
		}
		if len(memberships) == 0 {
			return status.Error(codes.PermissionDenied,
				"caller has no organization membership; create or accept an invitation to an organization first")
		}
		return handler(srv, ss)
	}
}
