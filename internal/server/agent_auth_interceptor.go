package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// AgentTokenMetadataKey is the gRPC metadata header that carries an agent's
// registration token on every AgentService stream. Validated by
// AgentAuthStreamInterceptor before the handler runs.
//
// Lowercased deliberately — gRPC metadata keys are case-insensitive on the
// wire but Go's metadata package normalizes to lowercase, so any other
// casing here would silently miss header lookups in tests.
const AgentTokenMetadataKey = "x-pivox-agent-token"

type agentGatewayKey struct{}

// AuthenticatedGateway extracts the registration-token-validated gateway
// from a context populated by AgentAuthStreamInterceptor. Use this in
// AgentService handlers instead of re-querying by token.
func AuthenticatedGateway(ctx context.Context) (db.StorageGateway, bool) {
	g, ok := ctx.Value(agentGatewayKey{}).(db.StorageGateway)
	return g, ok
}

// WithAuthenticatedGateway returns a context with the given gateway set,
// for tests and seams that bypass the interceptor. Production code should
// always reach handlers through the interceptor; this is the test analogue
// of what AgentAuthStreamInterceptor does after a successful token lookup.
func WithAuthenticatedGateway(ctx context.Context, gateway db.StorageGateway) context.Context {
	return context.WithValue(ctx, agentGatewayKey{}, gateway)
}

// AgentAuthStreamInterceptor validates the registration token in the initial
// gRPC metadata of every incoming stream and injects the resolved gateway
// into the stream's context for the handler to use. Streams without a valid
// token are rejected at the gRPC layer with codes.Unauthenticated so the
// handler is never reached.
//
// Scope: this interceptor is registered on the service-to-service gRPC
// server only (cmd/pivox-cloud/main.go). The public gRPC server has a
// separate chain that enforces Firebase bearer auth. Network-level
// isolation between the two surfaces means this interceptor doesn't need
// to filter methods — every stream that reaches it must authenticate.
func AgentAuthStreamInterceptor(queries db.Querier) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		tokens := md.Get(AgentTokenMetadataKey)
		if len(tokens) == 0 || tokens[0] == "" {
			return status.Error(codes.Unauthenticated, "missing agent registration token")
		}

		gateway, err := queries.GetStorageGatewayByToken(ss.Context(), tokens[0])
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.Unauthenticated, "invalid agent registration token")
			}
			return status.Errorf(codes.Internal, "lookup agent gateway: %v", err)
		}

		ctx := context.WithValue(ss.Context(), agentGatewayKey{}, gateway)
		return handler(srv, &serverStreamWithContext{ServerStream: ss, ctx: ctx})
	}
}

// serverStreamWithContext overrides Context() so the handler observes the
// derived context with the validated gateway value. grpc.ServerStream
// doesn't expose a way to replace the underlying context directly.
type serverStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *serverStreamWithContext) Context() context.Context { return s.ctx }
