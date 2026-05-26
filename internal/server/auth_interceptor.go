package server

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/authn"
)

// pivoxUserIDKey is the context key for the caller's per-Pivox
// `identities.id` UUID. Populated by the auth interceptor from the
// `pivox_user_id` Firebase ID-token custom claim (browser/native
// clients) or the `actor_uid` claim of an SA-signed JWT (SSR server
// acting on behalf of a user — see CompositeAuthService in
// composite_auth.go).
//
// All membership tables (`org_members.principal_id`,
// `space_members.principal_id`, `group_members.user_id`) reference
// this UUID — it's the universal user identifier across the API.
type pivoxUserIDKey struct{}

// Canonical Unauthenticated messages. Centralized so both the unary
// and stream interceptors return identical errors and so any future
// reword happens in one place.
const (
	errMissingMetadata    = "missing metadata"
	errMissingAuthHeader  = "missing authorization header"
	errInvalidAuthFormat  = "invalid authorization format"
	errInvalidOrExpiredID = "invalid or expired token"
)

// PivoxUserID extracts the verified Pivox user UUID
// (`identities.id`) from the context. Returns the UUID and
// true when the auth interceptor extracted a `pivox_user_id` claim
// from the verified ID token.
func PivoxUserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(pivoxUserIDKey{}).(uuid.UUID)
	return id, ok
}

// WithPivoxUserID returns a new context with the given UUID set as
// if the auth interceptor had extracted it from a verified token's
// `pivox_user_id` claim. Intended for tests.
func WithPivoxUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, pivoxUserIDKey{}, id)
}

// MustPivoxUserID extracts the verified Pivox user UUID from the
// context. Panics if the context does not contain one — only call
// from handlers behind the auth interceptor, which rejects any
// token without a `pivox_user_id` claim with Unauthenticated, so
// by the time a handler runs the claim is guaranteed present.
func MustPivoxUserID(ctx context.Context) uuid.UUID {
	id, ok := PivoxUserID(ctx)
	if !ok {
		panic("server: MustPivoxUserID called without pivox_user_id claim on context")
	}
	return id
}

// authenticateBearer is the transport-agnostic core of bearer
// authentication. Caller passes the bearer header value already
// extracted from its transport (gRPC metadata, HTTP Authorization
// header). Returns the context augmented with pivoxUserIDKey, or
// an apierr.Unauthenticated error.
//
// The interceptor doesn't know or care which kind of token this is
// (Firebase ID token, SA-signed SSR JWT, anything else we add) —
// it asks `auth.VerifyToken` and trusts whatever Identity comes back.
// Routing across token shapes lives in the authn.Service
// implementation; in production that's CompositeAuthService, which
// inspects the JWT issuer and dispatches accordingly. Tests can pass
// a bare Firebase service or a composite — the interceptor doesn't
// change either way.
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
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	// Every authenticated token must carry pivox_user_id. The
	// Firebase blocking function sets it on Firebase ID tokens; the
	// SSR composite path sets it explicitly when it mints an
	// Identity from a verified SA-signed JWT. Reject here with the
	// same Unauthenticated message used for verification failures
	// so clients can't probe to distinguish "missing claim" from
	// "bad signature" — both should trigger a token-refresh path.
	claim, ok := identity.Claims["pivox_user_id"].(string)
	if !ok || claim == "" {
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	uid, parseErr := uuid.Parse(claim)
	if parseErr != nil {
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	ctx = context.WithValue(ctx, pivoxUserIDKey{}, uid)
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
// wires CompositeAuthService (Firebase + SSR-SA-signed); tests can
// pass either.
//
// Scope: this interceptor is registered on the public gRPC server only.
// Service-to-service traffic (e.g. AgentService) lives on a separate
// gRPC server with its own interceptor chain — see cmd/pivox-cloud/main.go
// and server.AgentAuthStreamInterceptor.
//
// Reflection and health checks are handled by gRPC itself.
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
