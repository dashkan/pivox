package testutil

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// SetupGRPCServer creates an in-memory gRPC server and returns a client connection.
// registerFn should register service implementations on the server.
func SetupGRPCServer(t *testing.T, registerFn func(s *grpc.Server)) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	registerFn(srv)

	go func() {
		if err := srv.Serve(lis); err != nil {
			// Server stopped; this is expected during cleanup.
		}
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client connection: %v", err)
	}

	t.Cleanup(func() {
		conn.Close()
		srv.GracefulStop()
	})

	return conn
}
