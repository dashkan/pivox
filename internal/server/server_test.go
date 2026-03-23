package server

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/iam"
	"github.com/dashkan/pivox-server/internal/lro"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox-server/internal/service/apikeys"
	"github.com/dashkan/pivox-server/internal/service/organizations"
	"github.com/dashkan/pivox-server/internal/service/projects"
	"github.com/dashkan/pivox-server/internal/service/tags"
)

const bufSize = 1024 * 1024

// Seed data organization UUID (meridian-broadcasting from seed.sql).
const seedOrgID = "0192a000-0001-7000-8000-000000000001"

// setupTestServer creates an in-memory gRPC server with all services registered
// and returns a client connection. It skips the test if DATABASE_URL is not set.
func setupTestServer(t *testing.T) *grpc.ClientConn {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	require.NoError(t, pool.Ping(ctx))

	queries := db.New(pool)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	lroManager := lro.NewManager(pool, queries, logger)
	iamHelper := iam.NewHelper(queries)

	// Suppress unused variable warnings for services that still need LRO.
	_ = lroManager

	lis := bufconn.Listen(bufSize)
	validator, err := protovalidate.New()
	require.NoError(t, err)
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(FieldMaskAwareValidationInterceptor(validator)),
	)
	t.Cleanup(func() { grpcServer.Stop() })

	// Register all servers.
	apiv1.RegisterProjectsServer(grpcServer, projects.NewProjectsServer(pool, queries, iamHelper))
	apiv1.RegisterOrganizationsServer(grpcServer, organizations.NewOrganizationsServer(pool, queries, iamHelper, nil))
	apiv1.RegisterTagKeysServer(grpcServer, tags.NewTagKeysServer(pool, queries, iamHelper))
	apiv1.RegisterTagValuesServer(grpcServer, tags.NewTagValuesServer(pool, queries, iamHelper))
	apiv1.RegisterTagBindingsServer(grpcServer, tags.NewTagBindingsServer(pool, queries))
	apiv1.RegisterApiKeysServer(grpcServer, apikeys.NewApiKeysServer(pool, queries))

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			// Server was stopped; this is expected during cleanup.
		}
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return conn
}

// TODO: Rewrite integration tests to match new proto API (no LRO, removed
// fields/services). The old tests referenced Folders, TagHolds, LRO-wrapped
// responses, and fields like Uid/Parent/ShortName that have been removed.
