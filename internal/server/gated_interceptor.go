package server

import (
	"context"
	"strings"

	"google.golang.org/grpc"
)

// Method-prefix predicates deciding which RPCs the auth/authz chain
// applies to. gRPC interceptors are server-global — they fire for every
// registered method, including infrastructure services we don't own
// (server reflection, health). Rather than teaching each interceptor to
// special-case those, the chain wraps the auth/authz interceptors with
// Gated*Interceptor below so they run ONLY for the methods a predicate
// selects; everything else passes straight to the handler.
const (
	pivoxMethodPrefix = "/pivox."

	// lroMethodPrefix is the AIP long-running-operations surface
	// (google.longrunning.Operations). It's authenticated but NOT
	// membership- or permission-gated: an operation is caller-scoped, so
	// a memberless caller must still be able to poll their own op (e.g.
	// DeleteAccount), and gating it on permission would mean annotating a
	// vendored google proto — which we deliberately don't. So auth uses
	// IsPivoxOrLRO; membership and permission use IsPivox.
	lroMethodPrefix = "/google.longrunning.Operations/"
)

// IsPivox reports whether a gRPC FullMethod belongs to a first-party
// pivox.* service — the methods that run the full auth + membership +
// permission chain.
func IsPivox(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, pivoxMethodPrefix)
}

// IsPivoxOrLRO additionally matches the LRO surface, which is
// authenticated but skips membership/permission (see lroMethodPrefix).
func IsPivoxOrLRO(fullMethod string) bool {
	return IsPivox(fullMethod) || strings.HasPrefix(fullMethod, lroMethodPrefix)
}

// GatedUnaryInterceptor wraps a unary interceptor so it runs only for
// methods the predicate selects; non-matching methods (reflection,
// health, other infrastructure) pass straight to the handler untouched.
func GatedUnaryInterceptor(predicate func(string) bool, inner grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !predicate(info.FullMethod) {
			return handler(ctx, req)
		}
		return inner(ctx, req, info, handler)
	}
}

// GatedStreamInterceptor is the streaming counterpart of
// GatedUnaryInterceptor.
func GatedStreamInterceptor(predicate func(string) bool, inner grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !predicate(info.FullMethod) {
			return handler(srv, ss)
		}
		return inner(srv, ss, info, handler)
	}
}
