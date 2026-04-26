package server

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
)

// authContextKey is the context key for the authenticated UID.
type authContextKey struct{}

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

// AuthInterceptor returns a gRPC unary server interceptor that verifies
// Firebase bearer tokens via the provided authn.Service.
//
// Scope: this interceptor is registered on the public gRPC server only.
// Service-to-service traffic (e.g. AgentService) lives on a separate
// gRPC server with its own interceptor chain — see cmd/pivox-cloud/main.go
// and server.AgentAuthStreamInterceptor.
//
// Reflection and health checks are handled by gRPC itself.
//
// The interceptor:
//  1. Extracts the Bearer token from the "authorization" metadata.
//  2. Verifies the token via the auth service.
//  3. Injects the authenticated UID into the context.
func AuthInterceptor(auth authn.Service) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		bearer := authHeaders[0]
		if !strings.HasPrefix(bearer, "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
		}
		idToken := strings.TrimPrefix(bearer, "Bearer ")

		identity, err := auth.VerifyToken(ctx, idToken)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		ctx = context.WithValue(ctx, authContextKey{}, identity.UID)
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor returns a gRPC stream server interceptor that verifies
// bearer tokens. Same logic as AuthInterceptor but for streaming RPCs.
func AuthStreamInterceptor(auth authn.Service) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization header")
		}

		bearer := authHeaders[0]
		if !strings.HasPrefix(bearer, "Bearer ") {
			return status.Error(codes.Unauthenticated, "invalid authorization format")
		}
		idToken := strings.TrimPrefix(bearer, "Bearer ")

		identity, err := auth.VerifyToken(ss.Context(), idToken)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		ctx := context.WithValue(ss.Context(), authContextKey{}, identity.UID)
		wrapped := &wrappedStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrapped)
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
