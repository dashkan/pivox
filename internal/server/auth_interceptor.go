package server

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/authn"
)

// authContextKey is the context key for the authenticated UID.
type authContextKey struct{}

// Canonical Unauthenticated messages. Centralized so both the unary
// and stream interceptors return identical errors and so any future
// reword happens in one place.
const (
	errMissingMetadata    = "missing metadata"
	errMissingAuthHeader  = "missing authorization header"
	errInvalidAuthFormat  = "invalid authorization format"
	errInvalidOrExpiredID = "invalid or expired token"
)

// AuthenticatedUID extracts the verified UID from the context.
// Returns the UID and true if present, or an empty string and false if the
// request was not authenticated (e.g., a public endpoint).
func AuthenticatedUID(ctx context.Context) (string, bool) {
	uid, ok := ctx.Value(authContextKey{}).(string)
	return uid, ok
}

// WithAuthenticatedUID returns a new context with the given UID set,
// as if the auth interceptor had verified it. Intended for tests.
func WithAuthenticatedUID(ctx context.Context, uid string) context.Context {
	return context.WithValue(ctx, authContextKey{}, uid)
}

// MustAuthenticatedUID extracts the verified UID from the context.
// Panics if the context does not contain an authenticated UID — only call
// this from handlers that are known to be behind the auth interceptor.
func MustAuthenticatedUID(ctx context.Context) string {
	uid, ok := AuthenticatedUID(ctx)
	if !ok {
		panic("server: MustAuthenticatedUID called without authenticated context")
	}
	return uid
}

// authenticate is the shared body for both unary and stream auth
// interceptors. Returns the augmented context (with the verified UID)
// or an apierr.Unauthenticated error. Single source of truth for
// "Firebase bearer auth" — unary/stream chains can't drift.
func authenticate(ctx context.Context, auth authn.Service) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated(errMissingMetadata)
	}
	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return nil, apierr.Unauthenticated(errMissingAuthHeader)
	}
	bearer := authHeaders[0]
	if !strings.HasPrefix(bearer, "Bearer ") {
		return nil, apierr.Unauthenticated(errInvalidAuthFormat)
	}
	idToken := strings.TrimPrefix(bearer, "Bearer ")

	identity, err := auth.VerifyToken(ctx, idToken)
	if err != nil {
		return nil, apierr.Unauthenticated(errInvalidOrExpiredID)
	}
	return context.WithValue(ctx, authContextKey{}, identity.UID), nil
}

// AuthInterceptor returns a gRPC unary server interceptor that verifies
// Firebase bearer tokens via the provided authn.Service.
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
