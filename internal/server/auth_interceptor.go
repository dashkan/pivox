package server

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/pivoxai/pivox/internal/firebase"
)

// authContextKey is the context key for the authenticated Firebase UID.
type authContextKey struct{}

// AuthenticatedUID extracts the verified Firebase UID from the context.
// Returns the UID and true if present, or an empty string and false if the
// request was not authenticated (e.g., a public endpoint).
func AuthenticatedUID(ctx context.Context) (string, bool) {
	uid, ok := ctx.Value(authContextKey{}).(string)
	return uid, ok
}

// MustAuthenticatedUID extracts the verified Firebase UID from the context.
// Panics if the context does not contain an authenticated UID — only call
// this from handlers that are known to be behind the auth interceptor.
func MustAuthenticatedUID(ctx context.Context) string {
	uid, ok := AuthenticatedUID(ctx)
	if !ok {
		panic("server: MustAuthenticatedUID called without authenticated context")
	}
	return uid
}

// publicMethods lists gRPC full method names that skip authentication.
// Reflection and health checks are handled separately by gRPC itself.
var publicMethods = map[string]bool{
	// Add unauthenticated endpoints here as needed, e.g.:
	// "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo": true,
	// "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": true,
}

// AuthInterceptor returns a gRPC unary server interceptor that verifies
// Firebase ID tokens from the "authorization" metadata header.
//
// The interceptor:
//  1. Skips methods listed in publicMethods.
//  2. Extracts the Bearer token from the "authorization" metadata.
//  3. Verifies the token via Firebase Auth.
//  4. Injects the authenticated UID into the context.
func AuthInterceptor(auth *firebase.AuthService) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if publicMethods[info.FullMethod] {
			return handler(ctx, req)
		}

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

		token, err := auth.VerifyIDToken(ctx, idToken)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		ctx = context.WithValue(ctx, authContextKey{}, token.UID)
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor returns a gRPC stream server interceptor that verifies
// Firebase ID tokens. Same logic as AuthInterceptor but for streaming RPCs.
func AuthStreamInterceptor(auth *firebase.AuthService) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if publicMethods[info.FullMethod] {
			return handler(srv, ss)
		}

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

		token, err := auth.VerifyIDToken(ss.Context(), idToken)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		ctx := context.WithValue(ss.Context(), authContextKey{}, token.UID)
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
