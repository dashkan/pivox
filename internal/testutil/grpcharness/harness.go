//go:build dev

package grpcharness

import (
	"context"
	"net"
	"sync"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil"
)

// Harness is the canonical end-to-end test scaffold. It owns a
// Postgres testcontainer, an in-memory gRPC server with the full
// production interceptor chain, and a gRPC client whose outgoing
// calls auto-authenticate as the current Caller.
type Harness struct {
	// Direct DB access for seed helpers and assertions that bypass
	// the gRPC layer (e.g., setting up an org without going through
	// CreateOrganization).
	Pool      *pgxpool.Pool
	Queries   *db.Queries
	Encryptor crypto.Encryptor

	// LROManager is shared with the test gRPC server so async LROs
	// run end-to-end. Tests can also use it directly to wait on or
	// cancel operations.
	LROManager *lro.Manager

	// Auth is the authn.Service the test gRPC server's
	// AuthInterceptor calls into. Tests that need to assert specific
	// auth side-effects (e.g., DeleteOidcProvider was invoked) can
	// override it via WithAuth(...).
	Auth authn.Service

	server   *grpc.Server
	listener *bufconn.Listener
	conn     *grpc.ClientConn

	callerMu sync.RWMutex
	caller   *Caller
}

// Option customizes harness construction.
type Option func(*config)

type config struct {
	registerServices func(*Harness, *grpc.Server)
	auth             authn.Service
}

// WithServices registers gRPC services on the test server. The
// callback receives the harness so it can pass Pool, Queries,
// LROManager, etc., to service constructors. Required for any test
// that exercises the gRPC surface (most of them).
func WithServices(fn func(*Harness, *grpc.Server)) Option {
	return func(c *config) { c.registerServices = fn }
}

// WithAuth overrides the harness's default test authn.Service.
// Useful for tests that assert specific calls on the Service
// interface (e.g., that DeleteOidcProvider was invoked during
// SSO config rotation).
func WithAuth(a authn.Service) Option {
	return func(c *config) { c.auth = a }
}

// New starts the harness: Postgres container + in-memory gRPC
// server with the production interceptor chain + connected gRPC
// client. Cleanup is registered via t.Cleanup so the caller does
// not have to defer anything.
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()

	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.auth == nil {
		cfg.auth = testAuthService{}
	}

	pool, queries, dbCleanup := testutil.SetupTestDB(t)
	t.Cleanup(dbCleanup)

	enc, err := crypto.NewEncryptor()
	require.NoError(t, err)

	lroManager := lro.NewManager(queries, testLogger())

	h := &Harness{
		Pool:       pool,
		Queries:    queries,
		Encryptor:  enc,
		LROManager: lroManager,
		Auth:       cfg.auth,
	}

	permResolver := permission.NewResolver(queries)
	callerIdentity := server.NewCallerIdentityResolver(queries)
	permissionInterceptor := server.PermissionInterceptor(
		server.GeneratedRegistry, server.GeneratedExempt,
		queries, permResolver, callerIdentity,
	)
	permissionStreamInterceptor := server.PermissionStreamInterceptor(
		server.GeneratedRegistry, server.GeneratedExempt,
		queries, permResolver, callerIdentity,
	)
	validator, err := protovalidate.New()
	require.NoError(t, err)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			server.AuthInterceptor(h.Auth),
			server.MembershipRequiredInterceptor(queries),
			permissionInterceptor,
			server.FieldMaskAwareValidationInterceptor(validator),
		),
		grpc.ChainStreamInterceptor(
			server.AuthStreamInterceptor(h.Auth),
			server.MembershipRequiredStreamInterceptor(queries),
			permissionStreamInterceptor,
		),
	)

	if cfg.registerServices != nil {
		cfg.registerServices(h, grpcServer)
	}

	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)
	go func() { _ = grpcServer.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(h.attachCallerUnary),
		grpc.WithStreamInterceptor(h.attachCallerStream),
	)
	require.NoError(t, err)

	h.server = grpcServer
	h.listener = lis
	h.conn = conn

	t.Cleanup(func() {
		_ = conn.Close()
		grpcServer.GracefulStop()
	})

	return h
}

// Conn returns the gRPC client connection. Pass to a generated
// client constructor (e.g. apiv1.NewOrganizationsClient(h.Conn())).
func (h *Harness) Conn() *grpc.ClientConn { return h.conn }

// SetCaller swaps the identity that subsequent gRPC calls
// authenticate as. Pass nil to send unauthenticated requests
// (useful for testing the AuthInterceptor's reject path).
func (h *Harness) SetCaller(c *Caller) {
	h.callerMu.Lock()
	h.caller = c
	h.callerMu.Unlock()
}

// CurrentCaller returns the active caller, or nil when unset.
func (h *Harness) CurrentCaller() *Caller {
	h.callerMu.RLock()
	defer h.callerMu.RUnlock()
	return h.caller
}

func (h *Harness) attachCallerUnary(
	ctx context.Context,
	method string,
	req, reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	ctx = h.attachCallerMetadata(ctx)
	return invoker(ctx, method, req, reply, cc, opts...)
}

func (h *Harness) attachCallerStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	ctx = h.attachCallerMetadata(ctx)
	return streamer(ctx, desc, cc, method, opts...)
}

func (h *Harness) attachCallerMetadata(ctx context.Context) context.Context {
	c := h.CurrentCaller()
	if c == nil {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.UID)
}
