package server

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/authn"
)

// userIDKey is the context key for the caller's `identities.id` UUID,
// populated by the auth interceptor from the `sub` of a verified Keycloak
// access token (sub == identities.id — see oidc_auth.go).
//
// All membership tables (`org_members.principal_id`,
// `space_members.principal_id`, `group_members.user_id`) reference
// this UUID — it's the universal user identifier across the API.
type userIDKey struct{}

// Canonical Unauthenticated messages. Centralized so both the unary
// and stream interceptors return identical errors and so any future
// reword happens in one place.
const (
	errMissingMetadata    = "missing metadata"
	errMissingAuthHeader  = "missing authorization header"
	errInvalidAuthFormat  = "invalid authorization format"
	errInvalidOrExpiredID = "invalid or expired token"
)

// UserID extracts the verified caller's `identities.id` UUID from the
// context. Returns the UUID and true when the auth interceptor resolved
// the caller from the `sub` of a verified Keycloak token.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return id, ok
}

// WithUserID returns a new context with the given UUID set as if the auth
// interceptor had extracted it from a verified token's `sub`. Intended for
// tests.
func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

// MustUserID extracts the verified caller's `identities.id` UUID from the
// context. Panics if the context does not contain one — only call from
// handlers behind the auth interceptor, which rejects any token it can't
// resolve to an identity id with Unauthenticated, so by the time a
// handler runs the id is guaranteed present.
func MustUserID(ctx context.Context) uuid.UUID {
	id, ok := UserID(ctx)
	if !ok {
		panic("server: MustUserID called without an identity on context")
	}
	return id
}

// authenticateBearer is the transport-agnostic core of bearer
// authentication. Caller passes the bearer header value already
// extracted from its transport (gRPC metadata, HTTP Authorization
// header). Returns the context augmented with userIDKey, or
// an apierr.Unauthenticated error.
//
// The interceptor doesn't know or care which kind of token this is
// (Firebase ID token, Keycloak access token, anything else we add) —
// it asks `auth.VerifyToken` and trusts whatever Identity comes back.
// Routing across token shapes lives in the authn.Service
// implementation; in production that's the OIDC wrapper (oidcAuthService),
// which inspects the JWT issuer and dispatches Keycloak tokens to the
// OIDC verifier and everything else to the wrapped Firebase service.
// Tests can pass a bare Firebase service or the wrapper — the
// interceptor doesn't change either way.
//
// This is the single source of truth for "bearer auth" across the
// codebase — gRPC unary/stream interceptors and the HTTP middleware
// in http_auth.go all converge here so they cannot drift.
func authenticateBearer(ctx context.Context, auth authn.Service, bearerHeader string) (context.Context, error) {
	if bearerHeader == "" {
		return nil, apierr.Unauthenticated(errMissingAuthHeader)
	}
	if !strings.HasPrefix(bearerHeader, "Bearer ") {
		return nil, apierr.Unauthenticated(errInvalidAuthFormat)
	}
	idToken := strings.TrimPrefix(bearerHeader, "Bearer ")

	identity, err := auth.VerifyToken(ctx, idToken)
	if err != nil {
		// Keep the detail server-side only (client gets the generic message —
		// no info leak). Logged at Debug so a misconfigured audience or an
		// unreachable JWKS during the Keycloak cutover leaves a breadcrumb when
		// the operator raises the log level, instead of a silent "every KC user
		// gets invalid token".
		slog.DebugContext(ctx, "bearer token verification failed", "error", err)
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	// Resolve the caller's identity id. For a Keycloak access token the `sub`
	// IS the identity id (== identities.id), surfaced as identity.UID. Reject
	// with the same Unauthenticated message used for verification failures so
	// clients can't distinguish "missing id" from "bad signature" — both should
	// trigger a token refresh.
	idStr := identity.UID
	if idStr == "" {
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	uid, parseErr := uuid.Parse(idStr)
	if parseErr != nil {
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	ctx = context.WithValue(ctx, userIDKey{}, uid)
	return ctx, nil
}

// authenticate is the gRPC adapter for authenticateBearer. Pulls the
// bearer header out of incoming gRPC metadata and delegates to the
// transport-agnostic core.
func authenticate(ctx context.Context, auth authn.Service) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated(errMissingMetadata)
	}
	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return nil, apierr.Unauthenticated(errMissingAuthHeader)
	}
	return authenticateBearer(ctx, auth, authHeaders[0])
}

// AuthInterceptor returns a gRPC unary server interceptor that
// verifies bearer tokens via the provided authn.Service. The service
// is responsible for any token-type routing internally — production
// wires the OIDC wrapper (Keycloak + Firebase); tests can pass either.
//
// Scope: this interceptor is registered on the public gRPC server only.
// Service-to-service traffic (e.g. AgentService) lives on a separate
// gRPC server with its own interceptor chain — see cmd/pivox-cloud/main.go
// and server.AgentAuthStreamInterceptor.
//
// It runs unconditionally here; which methods it actually applies to is
// decided one level up by GatedUnaryInterceptor in main.go (pivox.* +
// LRO). Infrastructure methods (reflection, health) bypass it there.
func AuthInterceptor(auth authn.Service) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		ctx, err := authenticate(ctx, auth)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor returns a gRPC stream server interceptor that verifies
// bearer tokens. Same logic as AuthInterceptor but for streaming RPCs.
func AuthStreamInterceptor(auth authn.Service) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx, err := authenticate(ss.Context(), auth)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

// wrappedStream overrides Context() to return the authenticated context.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}
