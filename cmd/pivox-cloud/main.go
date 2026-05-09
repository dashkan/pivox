package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"buf.build/go/protovalidate"
	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	sloghttp "github.com/samber/slog-http"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"
	"riverqueue.com/riverui"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/config"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/firebase"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/apikeys"
	"github.com/dashkan/pivox/internal/service/dashboards"
	"github.com/dashkan/pivox/internal/service/iam"
	"github.com/dashkan/pivox/internal/service/operations"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/service/spaces"
	"github.com/dashkan/pivox/internal/service/storage"
	"github.com/dashkan/pivox/internal/service/tags"

	"github.com/dashkan/pivox/internal/service/aichat"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/service/aichat/tools"
	"github.com/dashkan/pivox/internal/service/assets"
	"github.com/dashkan/pivox/internal/service/requests"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	iamv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "pivox-cloud",
		Short:   "Pivox control plane server",
		Version: version,
		RunE:    serve,
	}

	f := rootCmd.Flags()
	f.String("database-url", envOrDefault("PIVOX_DATABASE_URL", "postgres://localhost:5432/pivox?sslmode=disable"), "PostgreSQL connection URL")
	f.String("grpc-port", envOrDefault("PIVOX_GRPC_PORT", ":50051"), "Public gRPC listen address (Firebase-authenticated)")
	// Service-to-service surface defaults to loopback. Production binds via
	// nginx (configs/nginx.conf maps /pivox.agent.v1.AgentService/ to this
	// port), so external reach is opt-in by either changing this flag or
	// terminating at a reverse proxy. Token validation in
	// AgentAuthStreamInterceptor still gates direct connections — defense
	// in depth — but defaulting to 0.0.0.0 contradicts the design intent
	// of "internal-only listener" and would require every operator to
	// remember to firewall it.
	f.String("service-grpc-port", envOrDefault("PIVOX_SERVICE_GRPC_PORT", "127.0.0.1:50052"), "Service-to-service gRPC listen address (AgentService et al., registration-token authenticated)")
	f.String("rest-port", envOrDefault("PIVOX_REST_PORT", ":8080"), "REST gateway listen address")
	f.String("debug-port", envOrDefault("PIVOX_DEBUG_PORT", ":9090"), "Debug/health listen address")
	f.String("log-level", envOrDefault("PIVOX_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	// Firebase credentials AND space ID resolve entirely through
	// Google's standard ADC chain (service-account JSON → metadata
	// server → gcloud user identity + quota space). No Pivox-named
	// credential flag — operators set the standard env var.
	f.Duration("delegated-auth-session-ttl", envOrDuration("PIVOX_DELEGATED_AUTH_SESSION_TTL", 5*time.Minute), "How long a delegated auth session code remains valid")
	f.Duration("delegated-auth-poll-interval", envOrDuration("PIVOX_DELEGATED_AUTH_POLL_INTERVAL", 5*time.Second), "Poll interval returned to delegated auth clients")
	f.String("ollama-url", envOrDefault("PIVOX_OLLAMA_URL", "http://localhost:11434"), "Ollama API base URL")
	f.String("ollama-model", envOrDefault("PIVOX_OLLAMA_MODEL", "qwen3-vl"), "Ollama model to use for AI chat")

	// OAuth broker (federated sign-in for native + web). The app
	// key signs the broker's `state` token; base URL is the public
	// origin used to construct the IdP-facing redirect_uri.
	f.String("oauth-broker-base-url", envOrDefault("PIVOX_OAUTH_BROKER_BASE_URL", "https://pivox.ngrok.app"), "Public origin used to build OAuth broker callback URLs")
	f.String("oauth-broker-app-key", envOrDefault("PIVOX_APP_KEY", ""), "HMAC key for OAuth broker state token (≥32 bytes)")
	f.String("github-client-id", envOrDefault("GITHUB_CLIENT_ID", ""), "GitHub OAuth app client ID (broker)")
	f.String("github-client-secret", envOrDefault("GITHUB_CLIENT_SECRET", ""), "GitHub OAuth app client secret (broker)")

	addSyncAuthFlags(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envOrDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}

func must(s string, _ error) string { return s }

func serve(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	sessionTTL, _ := f.GetDuration("delegated-auth-session-ttl")
	pollInterval, _ := f.GetDuration("delegated-auth-poll-interval")
	cfg := &config.Config{
		DatabaseURL:     must(f.GetString("database-url")),
		GRPCPort:        must(f.GetString("grpc-port")),
		ServiceGRPCPort: must(f.GetString("service-grpc-port")),
		RESTPort:        must(f.GetString("rest-port")),
		DebugPort:       must(f.GetString("debug-port")),
		LogLevel:        must(f.GetString("log-level")),
		SyncAuth:        loadSyncAuthConfig(cmd),
		DelegatedAuth: config.DelegatedAuthConfig{
			SessionTTL:   sessionTTL,
			PollInterval: pollInterval,
		},
		OAuthBroker: config.OAuthBrokerConfig{
			AppKey:             must(f.GetString("oauth-broker-app-key")),
			BaseURL:            must(f.GetString("oauth-broker-base-url")),
			GitHubClientID:     must(f.GetString("github-client-id")),
			GitHubClientSecret: must(f.GetString("github-client-secret")),
		},
	}

	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Database. Register pgvector types per-connection so the
	// scanner can decode `vector` columns (currently `assets.embedding`).
	// Without this, the first read of a row containing a `vector` column
	// fails with "unsupported data type: <nil>" — the test infrastructure
	// (internal/testutil/db.go) sets this up for tests but the
	// production binary never did, so the gap is invisible until a
	// non-test caller reads an asset row.
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse pool config: %w", err)
	}
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	logger.Info("connected to database")

	queries := db.New(pool)

	// Shared services
	enc, err := crypto.NewEncryptor()
	if err != nil {
		return fmt.Errorf("initialize encryptor: %w", err)
	}
	appCodec, err := appkey.NewFromEnv()
	if err != nil {
		return fmt.Errorf("initialize app key: %w", err)
	}
	// Background work (org/space purge, domain verification, LRO
	// reaper, auth-artifact cleanup) lives in the pivox-worker
	// process now. Run pivox-worker alongside pivox-cloud for any
	// non-trivial deployment — see cmd/pivox-worker. River's leader
	// election handles multi-replica coordination across worker
	// replicas; pivox-cloud is pure RPC.
	//
	// pivox-cloud holds a River client for three reasons:
	//   1. lro.Manager.NewLro: enqueues LROs into River atomically
	//      with the operations row insert in a single tx.
	//   2. River UI mounted at /river — the UI needs a Client to
	//      query job state.
	//   3. Future-proofing: as LRO handlers migrate off the legacy
	//      CreateAndRun + runWork path (#69 Phase 5+), they all
	//      flow through this Client.
	// Constructed without Workers + without Start so it's a
	// query/insert-only handle. Migrations are owned by pivox-worker;
	// pivox-cloud assumes the river schema exists.
	riverDriver := riverpgxv5.New(pool)
	riverClient, err := river.NewClient(riverDriver, &river.Config{
		Logger: logger,
		Schema: "river",
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}

	lroManager := lro.NewManager(lro.ManagerConfig{
		Queries: queries,
		Logger:  logger,
		Pool:    pool,
		River:   riverClient,
	})

	// Recover any pending operations from previous run
	if err := lroManager.RecoverPending(ctx); err != nil {
		logger.Error("failed to recover pending operations", "error", err)
	}

	// Firebase
	authSvc, err := firebase.NewAuthService(ctx)
	if err != nil {
		return fmt.Errorf("initialize Firebase auth: %w", err)
	}

	// gRPC server
	validator, err := protovalidate.New()
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}
	// Permission interceptor wiring: a single union registry +
	// exempt set is generated from pivox.permission.v1 method
	// options across every annotated proto. Run order matters —
	// Auth populates the caller UID, Membership cheap-denies
	// memberless callers, Permission resolves slug → uuid + role
	// → permission, Validate runs last so handlers see only
	// permission-checked, well-formed requests.
	permResolver := permission.NewResolver(queries)
	callerIdentity := server.NewCallerIdentityResolver(queries)
	auditResolver := audit.NewResolver(audit.Config{Queries: queries})
	permissionInterceptor := server.PermissionInterceptor(
		server.GeneratedRegistry, server.GeneratedExempt,
		queries, permResolver, callerIdentity,
	)
	permissionStreamInterceptor := server.PermissionStreamInterceptor(
		server.GeneratedRegistry, server.GeneratedExempt,
		queries, permResolver, callerIdentity,
	)

	grpcServer := grpc.NewServer(
		// Logging is FIRST so it sees every RPC including the ones
		// auth/validate reject — those would otherwise fail silently
		// from the operator's perspective.
		grpc.ChainUnaryInterceptor(
			server.LoggingUnaryInterceptor(logger),
			server.AuthInterceptor(authSvc),
			// Membership check runs after Auth so the caller's UID is
			// in context, and before Permission/Validate so we don't
			// leak field shape errors to memberless callers.
			// Allowlisted methods (CreateOrganization,
			// ListOrganizations, AcceptInvitation, GetInvitation)
			// bypass — see server/membership_interceptor.go.
			server.MembershipRequiredInterceptor(queries),
			permissionInterceptor,
			server.FieldMaskAwareValidationInterceptor(validator),
		),
		grpc.ChainStreamInterceptor(
			server.LoggingStreamInterceptor(logger),
			server.AuthStreamInterceptor(authSvc),
			server.MembershipRequiredStreamInterceptor(queries),
			permissionStreamInterceptor,
		),
	)

	// Register all services. permResolver and callerIdentity were
	// constructed above for the permission interceptor and are
	// reused here by service handlers (TestIamPermissions, etc.).
	longrunningpb.RegisterOperationsServer(grpcServer, operations.NewOperationsServer(operations.Config{LRO: lroManager}))

	apiv1.RegisterSpacesServer(grpcServer, spaces.NewSpacesServer(spaces.Config{
		Pool:          pool,
		Queries:       queries,
		Codec:         appCodec,
		Resolver:      permResolver,
		Caller:        callerIdentity,
		AuditResolver: auditResolver,
		LROManager:    lroManager,
	}))
	apiv1.RegisterOrganizationsServer(grpcServer, organizations.NewOrganizationsServer(organizations.Config{
		Pool:          pool,
		Queries:       queries,
		Auth:          authSvc,
		Codec:         appCodec,
		ReadUID:       server.AuthenticatedUID,
		Resolver:      permResolver,
		Caller:        callerIdentity,
		AuditResolver: auditResolver,
		LROManager:    lroManager,
		Encryptor:     enc,
	}))
	apiv1.RegisterTagKeysServer(grpcServer, tags.NewTagKeysServer(tags.TagKeysConfig{
		Pool: pool, Queries: queries, Codec: appCodec, AuditResolver: auditResolver,
	}))
	apiv1.RegisterTagValuesServer(grpcServer, tags.NewTagValuesServer(tags.TagValuesConfig{
		Pool: pool, Queries: queries, Codec: appCodec, AuditResolver: auditResolver,
	}))
	apiv1.RegisterTagBindingsServer(grpcServer, tags.NewTagBindingsServer(tags.TagBindingsConfig{
		Pool: pool, Queries: queries, Codec: appCodec, AuditResolver: auditResolver,
	}))
	apiv1.RegisterApiKeysServer(grpcServer, apikeys.NewApiKeysServer(apikeys.Config{
		Pool: pool, Queries: queries, Codec: appCodec, AuditResolver: auditResolver,
	}))

	// Dashboards: org-scoped SYSTEM_MANAGED reads via the in-memory
	// system catalog (Phase 4a). Space-scoped USER_MANAGED CRUD
	// lands in Phase 4b. NewServer panics on a registry/catalog
	// regression, so this line doubles as a boot-time invariant
	// check.
	apiv1.RegisterDashboardsServer(grpcServer, dashboards.NewServer(dashboards.Config{
		Pool: pool, Queries: queries, AuditResolver: auditResolver,
	}))

	// Iam service: cross-cutting IAM (role reads, permission catalog,
	// user reads, group CRUD, DeleteUser LRO). Scope-divergent IAM
	// ops (Member CRUD, TransferOwnership, TestIamPermissions) live
	// on the scope-owning Organizations / Spaces services above.
	iamv1.RegisterIamServer(grpcServer, iam.NewIamServer(iam.Config{
		Pool: pool, Queries: queries, Auth: authSvc, Caller: callerIdentity,
		LROManager: lroManager, AuditResolver: auditResolver,
	}))

	// Storage services
	connMgr := agentstream.NewConnectionManager()
	storagev1.RegisterStorageGatewaysServer(grpcServer, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
		Queries: queries, Encryptor: enc, Conns: connMgr, AuditResolver: auditResolver,
	}))
	storagev1.RegisterAgentsServer(grpcServer, storage.NewAgentsServer(storage.AgentsConfig{Queries: queries}))
	storagev1.RegisterEndpointsServer(grpcServer, storage.NewEndpointsServer(storage.EndpointsConfig{
		Queries: queries, Encryptor: enc, AuditResolver: auditResolver,
	}))

	// Asset and request services
	assetsv1.RegisterAssetsServer(grpcServer, assets.NewAssetsServer(assets.Config{
		Pool: pool, Queries: queries, AuditResolver: auditResolver,
	}))
	assetsv1.RegisterRequestsServer(grpcServer, requests.NewRequestsServer(requests.Config{
		Pool: pool, Queries: queries, AuditResolver: auditResolver,
	}))

	// AI Chat service
	ollamaURL := must(f.GetString("ollama-url"))
	ollamaModel := must(f.GetString("ollama-model"))
	llm, err := model.NewOllamaAdapter(ollamaURL, ollamaModel)
	if err != nil {
		return fmt.Errorf("initialize Ollama adapter: %w", err)
	}
	toolRegistry := tools.NewRegistry()
	aiChatServer := aichat.NewServer(aichat.Config{
		Pool:          pool,
		Queries:       queries,
		Model:         llm,
		Tools:         toolRegistry,
		Codec:         appCodec,
		Resolver:      permResolver,
		AuditResolver: auditResolver,
		Logger:        logger,
	})
	aiv1.RegisterAiChatServer(grpcServer, aiChatServer)

	reflection.Register(grpcServer)

	// Start gRPC listener (public surface)
	grpcLis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("listen on gRPC port %s: %w", cfg.GRPCPort, err)
	}

	// In-process bufconn listener for self-dialing clients (the REST
	// gateway translation layer and the SSE handler that wraps the
	// AiChat streaming RPC). The same grpc.Server serves both
	// listeners — interceptors run identically on either transport,
	// so AuthInterceptor still validates Firebase bearer tokens on
	// in-process calls. Avoids TCP loopback for self-dial without
	// changing the auth boundary. External clients (Native app) keep
	// using the TCP listener above.
	bufLis := bufconn.Listen(1024 * 1024)
	go func() {
		logger.Info("gRPC server bufconn listener ready (in-process self-dial)")
		if err := grpcServer.Serve(bufLis); err != nil {
			logger.Error("gRPC bufconn server stopped", "error", err)
		}
	}()

	go func() {
		logger.Info("gRPC server listening", "addr", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			logger.Error("gRPC server stopped", "error", err)
		}
	}()

	// ----------------------------------------------------------------------
	// Service-to-service gRPC server (AgentService et al.)
	//
	// Distinct from the public server because the auth model is different:
	// agents present a registration token in initial metadata, validated by
	// AgentAuthStreamInterceptor. Putting it on its own listener means the
	// public chain (Firebase auth + membership) never has to special-case
	// agent traffic, and operators can apply different network policy
	// (firewall, mTLS termination, separate ingress) to internal-only RPCs
	// without restructuring the proto surface.
	//
	// Not exposed via grpc-gateway — REST is for public clients only.
	// ----------------------------------------------------------------------
	serviceGRPCServer := grpc.NewServer(
		grpc.ChainStreamInterceptor(
			server.LoggingStreamInterceptor(logger),
			server.AgentAuthStreamInterceptor(queries),
		),
	)
	agentv1.RegisterAgentServiceServer(serviceGRPCServer, storage.NewAgentServiceServer(storage.AgentServiceConfig{
		Pool: pool, Queries: queries, Logger: logger, Conns: connMgr,
	}))
	reflection.Register(serviceGRPCServer)

	serviceGRPCLis, err := net.Listen("tcp", cfg.ServiceGRPCPort)
	if err != nil {
		return fmt.Errorf("listen on service gRPC port %s: %w", cfg.ServiceGRPCPort, err)
	}
	go func() {
		logger.Info("service gRPC server listening", "addr", cfg.ServiceGRPCPort)
		if err := serviceGRPCServer.Serve(serviceGRPCLis); err != nil {
			logger.Error("service gRPC server stopped", "error", err)
		}
	}()

	// REST gateway
	gwMux := runtime.NewServeMux()

	// In-process clients (gateway translation + SSE) dial the gRPC
	// server via bufconn — bypasses TCP loopback while keeping the
	// gRPC machinery (interceptors, codecs, reflection) intact. The
	// "passthrough:///bufnet" target is opaque to the gRPC name
	// resolver; routing happens through grpc.WithContextDialer.
	bufDialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return bufLis.DialContext(ctx)
	}
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(bufDialer),
	}
	const grpcEndpoint = "passthrough:///bufnet"

	for _, reg := range []func(context.Context, *runtime.ServeMux, string, []grpc.DialOption) error{
		apiv1.RegisterSpacesHandlerFromEndpoint,
		apiv1.RegisterOrganizationsHandlerFromEndpoint,
		apiv1.RegisterTagKeysHandlerFromEndpoint,
		apiv1.RegisterTagValuesHandlerFromEndpoint,
		apiv1.RegisterTagBindingsHandlerFromEndpoint,
		apiv1.RegisterApiKeysHandlerFromEndpoint,
		apiv1.RegisterDashboardsHandlerFromEndpoint,
		storagev1.RegisterStorageGatewaysHandlerFromEndpoint,
		storagev1.RegisterAgentsHandlerFromEndpoint,
		storagev1.RegisterEndpointsHandlerFromEndpoint,
		assetsv1.RegisterAssetsHandlerFromEndpoint,
		assetsv1.RegisterRequestsHandlerFromEndpoint,
		aiv1.RegisterAiChatHandlerFromEndpoint,
	} {
		if err := reg(ctx, gwMux, grpcEndpoint, dialOpts); err != nil {
			return fmt.Errorf("register REST gateway: %w", err)
		}
	}

	// AI Chat HTTP handlers (inline artifact content + SSE stream).
	// The handler shares the gRPC server's resolveConversation +
	// permission resolver so the path-vs-row creator check and
	// `ai.conversations.readAll` audit-bypass match the gRPC RPCs.
	contentHandler := aichat.NewContentHandler(aichat.ContentHandlerConfig{Server: aiChatServer, Logger: logger})
	if err := gwMux.HandlePath(
		"GET",
		"/v1/organizations/{org}/users/{user}/conversations/{conv}/artifacts/{art}/versions/{ver}:content",
		func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
			contentHandler.ServeHTTP(w, r)
		},
	); err != nil {
		return fmt.Errorf("register artifact content handler: %w", err)
	}

	grpcConn, err := grpc.NewClient(grpcEndpoint, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial local gRPC for SSE handler: %w", err)
	}
	sseHandler := aichat.NewSSEHandler(aichat.SSEHandlerConfig{Client: aiv1.NewAiChatClient(grpcConn), Logger: logger})

	// HTTP mux: internal hooks + gRPC gateway (fallback)
	httpMux := http.NewServeMux()
	hooks, err := server.NewInternalHooks(server.InternalHooksConfig{
		Queries:       queries,
		SyncAuth:      cfg.SyncAuth,
		DelegatedAuth: cfg.DelegatedAuth,
		Logger:        logger,
		Auth:          authSvc,
		AuditResolver: auditResolver,
	})
	if err != nil {
		return fmt.Errorf("initialize internal hooks: %w", err)
	}
	hooks.Register(httpMux)

	// OAuth broker for federated sign-in (GitHub, OIDC SSO).
	// Migrated server-side from the TanStack `start` /api/oauth/*
	// routes so auth machinery (DB-backed SsoConfig + KMS-encrypted
	// client_secret) lives next to syncIdentity et al.
	oauthBroker := server.NewOAuthBroker(server.OAuthBrokerConfig{
		Queries:   queries,
		Encryptor: enc,
		Broker:    cfg.OAuthBroker,
		Logger:    logger,
	})
	oauthBroker.Register(httpMux)

	// River UI — admin web UI for inspecting/cancelling/retrying
	// background jobs in the river schema. Prefix "/river" must
	// match the mount path "/river/".
	//
	// **Pre-prod posture: NO AUTH.** Mounted without auth wrapping
	// so an operator can click around in a browser without
	// Firebase-token plumbing. This is a deliberate dev choice —
	// before any production deploy this MUST be gated (HTTP basic
	// auth via env-var creds is the recommended next step; see
	// CLAUDE.md "Pre-prod freedom"). Leaving /river open on a
	// public origin leaks job names, args, and error details.
	riverUIEndpoints := riverui.NewEndpoints(riverClient, nil)
	riverUIHandler, err := riverui.NewHandler(&riverui.HandlerOpts{
		Endpoints: riverUIEndpoints,
		Logger:    logger,
		Prefix:    "/river",
	})
	if err != nil {
		return fmt.Errorf("river UI handler: %w", err)
	}
	if err := riverUIHandler.Start(ctx); err != nil {
		return fmt.Errorf("river UI start: %w", err)
	}
	httpMux.Handle("/river/", riverUIHandler)

	// HTTP auth middleware. Wraps the grpc-gateway mux so every
	// custom HTTP route mounted on gwMux (today: artifact :content)
	// gets Firebase bearer verification + ctx augmentation before the
	// handler runs. grpc-gateway-translated routes also pass through
	// this middleware; they pay a redundant verification (gateway
	// forwards the bearer to the gRPC backend, where AuthInterceptor
	// re-verifies) but Firebase verify is local + key-cached, so the
	// cost is ~1ms and worth the "set and forget" simplicity.
	authMW := server.RequireAuth(authSvc, logger)
	httpMux.Handle("/", authMW(gwMux))

	// SSE bypasses HTTP auth on purpose. The handler is a thin proxy
	// to AiChat.StreamGenerateContent over the in-process bufconn
	// dial; the gRPC AuthInterceptor on that call validates the
	// bearer token forwarded as gRPC metadata. Wrapping with HTTP
	// auth would double-verify. Registered on httpMux directly (not
	// gwMux) so the middleware above does not see it. Method routing
	// uses Go 1.22+ method patterns: GET / etc. fall through to
	// gatedGwMux, returning 405 for the wrong method on this path.
	httpMux.HandleFunc("POST /v1/ai:streamGenerateContent", sseHandler.ServeHTTP)

	// Request logging — slog-native middleware that emits one structured
	// log line per HTTP request at completion (method, path, status,
	// latency, response size, client IP). Auth-class headers
	// (Authorization, Cookie, Set-Cookie, X-Auth-Token, etc.) are
	// redacted by default. Wraps the ENTIRE REST mux so it captures
	// gateway routes + internal hooks + OAuth broker + SSE uniformly.
	// For SSE this fires when the stream closes — a few seconds for
	// normal completions, longer for hung clients (which is itself
	// the diagnostic). Trace-ID/Span-ID propagation is off until we
	// land OpenTelemetry; toggle via WithTraceID/WithSpanID then.
	requestLogMW := sloghttp.NewWithConfig(logger, sloghttp.Config{
		DefaultLevel:     slog.LevelInfo,
		ClientErrorLevel: slog.LevelWarn,
		ServerErrorLevel: slog.LevelError,
		WithRequestID:    true,
		WithClientIP:     true,
	})

	restServer := &http.Server{
		Addr:    cfg.RESTPort,
		Handler: requestLogMW(httpMux),
	}
	go func() {
		logger.Info("REST gateway listening", "addr", cfg.RESTPort)
		if err := restServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST gateway stopped", "error", err)
		}
	}()

	// Debug server (health/readiness)
	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Best-effort write; the client hanging up before we finish
		// is not actionable for a health endpoint.
		_, _ = fmt.Fprintln(w, "ok")
	})
	debugMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "not ready")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ready")
	})
	debugServer := &http.Server{
		Addr:    cfg.DebugPort,
		Handler: debugMux,
	}
	go func() {
		logger.Info("debug server listening", "addr", cfg.DebugPort)
		if err := debugServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("debug server stopped", "error", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	grpcServer.GracefulStop()
	_ = restServer.Shutdown(shutdownCtx)
	_ = debugServer.Shutdown(shutdownCtx)

	// Drain in-flight LRO goroutines before pool.Close runs in the
	// outer defer. GracefulStop above ensures no new RPCs land
	// (CreateAndRun would also self-reject post-Shutdown), but
	// already-running WorkFuncs need their bookkeeping write to
	// complete on a live pool. Anything past the deadline is left for
	// RecoverPending on next start.
	if err := lroManager.Shutdown(shutdownCtx); err != nil {
		logger.Warn("lro shutdown drain timed out", "error", err)
	}

	logger.Info("server stopped")
	return nil
}
