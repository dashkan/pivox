package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"buf.build/go/protovalidate"
	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	sloghttp "github.com/samber/slog-http"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"
	"riverqueue.com/riverui"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/config"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/oidc"
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
	"github.com/dashkan/pivox/internal/telemetry"
	"github.com/dashkan/pivox/internal/telemetry/rivertrace"

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
	f.String("grpc-port", envOrDefault("PIVOX_GRPC_PORT", ":50051"), "Public gRPC listen address (OIDC/Keycloak-authenticated)")
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
	f.Bool("enable-reflection", envOrBool("PIVOX_ENABLE_REFLECTION", false), "Register gRPC server reflection for dev tooling (grpcurl, buf curl). OFF by default — never enable in production; it exposes the full API surface to unauthenticated callers.")
	f.Duration("storage-session-max-ttl", envOrDuration("PIVOX_STORAGE_SESSION_MAX_TTL", 8*time.Hour), "Cap on CreateStorageSession TTL; caller-requested values above this are silently clamped")
	f.String("storage-session-cookie-domain", envOrDefault("PIVOX_STORAGE_SESSION_COOKIE_DOMAIN", ""), "Domain attribute for the storage-session Set-Cookie header (e.g. \".pivox.app\"). Empty omits Domain= so the cookie scopes to the response origin only — right default for self-hosted; SaaS deployments configure per-tenant subdomain.")
	f.String("ollama-url", envOrDefault("PIVOX_OLLAMA_URL", "http://localhost:11434"), "Ollama API base URL")
	f.String("ollama-model", envOrDefault("PIVOX_OLLAMA_MODEL", "qwen3-vl"), "Ollama model to use for AI chat")

	// OIDC resource-server verification (Keycloak). The backend validates Bearer
	// access tokens against the issuer's JWKS; client_id/secret live in the BFF,
	// not here. Keycloak is the sole auth provider — the issuer is required.
	f.String("oidc-issuer", envOrDefault("PIVOX_OIDC_ISSUER", ""), "OIDC issuer URL whose access tokens the backend accepts (e.g. https://host/realms/pivox); required")
	f.String("oidc-audience", envOrDefault("PIVOX_OIDC_AUDIENCE", ""), "Audience the access token's aud must contain (Keycloak audience-mapper value)")
	f.Bool("disable-oidc-audience-validation", envOrBool("PIVOX_DISABLE_OIDC_AUDIENCE_VALIDATION", false), "Opt out of OIDC audience validation (fail-closed otherwise)")
	f.Duration("oidc-jwks-refresh-interval", envOrDuration("PIVOX_OIDC_JWKS_REFRESH_INTERVAL", 5*time.Minute), "How often to background-refresh the issuer's JWKS (0 = fetch once at startup, never refresh)")

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

func envOrBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}

func must(s string, _ error) string { return s }

func serve(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	enableReflection, _ := f.GetBool("enable-reflection")
	disableOIDCAud, _ := f.GetBool("disable-oidc-audience-validation")
	jwksRefreshInterval, _ := f.GetDuration("oidc-jwks-refresh-interval")
	cfg := &config.Config{
		DatabaseURL:      must(f.GetString("database-url")),
		GRPCPort:         must(f.GetString("grpc-port")),
		ServiceGRPCPort:  must(f.GetString("service-grpc-port")),
		RESTPort:         must(f.GetString("rest-port")),
		DebugPort:        must(f.GetString("debug-port")),
		LogLevel:         must(f.GetString("log-level")),
		EnableReflection: enableReflection,
		OIDC: config.OIDCConfig{
			Issuer:                    must(f.GetString("oidc-issuer")),
			Audience:                  must(f.GetString("oidc-audience")),
			DisableAudienceValidation: disableOIDCAud,
			JWKSRefreshInterval:       jwksRefreshInterval,
		},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Logger + OpenTelemetry (traces + metrics + logs) in one bootstrap. OTel
	// is a no-op unless an OTLP endpoint is configured (the Aspire AppHost
	// injects it); the logger always writes JSON to stdout.
	logger, otelShutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "pivox-cloud",
		LogLevel:    cfg.LogLevel,
	})
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.Warn("telemetry shutdown", "error", err)
		}
	}()

	// Database. db.NewPool wires the otelpgx query tracer + pgvector
	// per-connection type registration (required to decode `vector` columns
	// like assets.embedding) in one place shared with the worker + test harness.
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
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
	//   3. All LRO handlers enqueue their jobs through this Client.
	// Constructed without Workers + without Start so it's a
	// query/insert-only handle. Migrations are owned by pivox-worker;
	// pivox-cloud assumes the river schema exists.
	riverDriver := riverpgxv5.New(pool)
	riverClient, err := river.NewClient(riverDriver, &river.Config{
		Logger: logger,
		Schema: "river",
		// otelriver emits river.insert_many spans + metrics; rivertrace
		// (outer) injects the enqueuing request's trace context into job
		// metadata so the worker's river.work joins this trace. The ordering
		// is load-bearing, so the slice is built in one place.
		Middleware: rivertrace.Middlewares(),
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

	// Keycloak (OIDC) is the sole auth provider. The backend is a pure
	// resource server: it validates Bearer access tokens against the
	// realm's JWKS. The token's `sub` IS the Pivox identity id, so the
	// verifier directly satisfies authn.Service — no provider-routing
	// wrapper. The verifier's JWKS load is lazy/tolerant, so startup is
	// NOT coupled to Keycloak readiness — keys are fetched on the first
	// token (when a user logs in through the edge).
	if cfg.OIDC.Issuer == "" {
		return fmt.Errorf("PIVOX_OIDC_ISSUER is required (Keycloak is the sole auth provider)")
	}
	oidcVerifier, err := oidc.NewVerifier(ctx, oidc.Config{
		Issuer:                    cfg.OIDC.Issuer,
		JWKSURL:                   strings.TrimRight(cfg.OIDC.Issuer, "/") + "/protocol/openid-connect/certs",
		Audience:                  cfg.OIDC.Audience,
		DisableAudienceValidation: cfg.OIDC.DisableAudienceValidation,
		JWKSRefreshInterval:       cfg.OIDC.JWKSRefreshInterval,
	})
	if err != nil {
		return fmt.Errorf("initialize OIDC verifier: %w", err)
	}
	// authChainSvc is what the gRPC AuthInterceptor / HTTP RequireAuth see.
	var authChainSvc authn.Service = oidcVerifier
	logger.Info("OIDC auth enabled",
		"issuer", cfg.OIDC.Issuer,
		"audience", cfg.OIDC.Audience,
		"audience_validation", !cfg.OIDC.DisableAudienceValidation,
	)
	if cfg.OIDC.DisableAudienceValidation {
		logger.Warn("OIDC audience validation DISABLED — any token this realm signs (including ID tokens minted for other clients) will be accepted; set PIVOX_OIDC_AUDIENCE to re-enable")
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
	auditResolver := audit.NewResolver(audit.Config{Queries: queries})
	permissionInterceptor := server.PermissionInterceptor(
		server.GeneratedRegistry, server.GeneratedExempt,
		queries, permResolver,
	)
	permissionStreamInterceptor := server.PermissionStreamInterceptor(
		server.GeneratedRegistry, server.GeneratedExempt,
		queries, permResolver,
	)

	grpcServer := grpc.NewServer(
		// OTel: server span per RPC + trace-context extraction from gRPC
		// metadata (no-op when export is disabled).
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		// Logging is FIRST so it sees every RPC including the ones
		// auth/validate reject — those would otherwise fail silently
		// from the operator's perspective.
		// The auth/authz interceptors are gated by method prefix (see
		// server.Gated*Interceptor): Auth runs for pivox.* + LRO;
		// Membership, Permission and Validate run for pivox.* only.
		// Everything else (server reflection, health, other
		// infrastructure) bypasses the chain entirely. Logging is NOT
		// gated — it observes every RPC. LRO (google.longrunning.
		// Operations) is therefore authenticated but skips membership/
		// permission: an operation is caller-scoped and we don't annotate
		// the vendored google proto.
		grpc.ChainUnaryInterceptor(
			server.LoggingUnaryInterceptor(logger),
			server.GatedUnaryInterceptor(server.IsPivoxOrLRO, server.AuthInterceptor(authChainSvc)),
			// Membership check runs after Auth so the caller's UID is
			// in context, and before Permission/Validate so we don't
			// leak field shape errors to memberless callers.
			// Allowlisted methods (CreateOrganization,
			// ListOrganizations, AcceptInvitation, GetInvitation)
			// bypass — see server/membership_interceptor.go.
			server.GatedUnaryInterceptor(server.IsPivox, server.MembershipRequiredInterceptor(queries)),
			server.GatedUnaryInterceptor(server.IsPivox, permissionInterceptor),
			server.GatedUnaryInterceptor(server.IsPivox, server.FieldMaskAwareValidationInterceptor(validator)),
		),
		grpc.ChainStreamInterceptor(
			server.LoggingStreamInterceptor(logger),
			server.GatedStreamInterceptor(server.IsPivoxOrLRO, server.AuthStreamInterceptor(authChainSvc)),
			server.GatedStreamInterceptor(server.IsPivox, server.MembershipRequiredStreamInterceptor(queries)),
			server.GatedStreamInterceptor(server.IsPivox, permissionStreamInterceptor),
			// Stream validator parallels the unary chain's
			// FieldMaskAwareValidationInterceptor so CEL rules and
			// `string.in`-style constraints on streaming RPC
			// requests don't silently no-op. Without this,
			// AiChat.StreamGenerateContent's CEL rules on
			// MessagePart (text-needs-text, file-needs-url,
			// tool-needs-id) and InputMessage.role
			// (in: [user, assistant, system, tool]) fire only on
			// unary callers and let malformed streaming requests
			// reach the handler.
			server.GatedStreamInterceptor(server.IsPivox, server.ValidateStreamInterceptor(validator)),
		),
	)

	// Register all services. permResolver is reused here by service
	// handlers (TestIamPermissions, etc.). Caller identity comes from
	// the verified token's `sub` and is read directly
	// Operations authorizes each call against the op's scope
	// (space_id → spaces.read, org_id → organizations.read, else
	// created_by) via the resolver, and trims ListOperations to the
	// caller's visible scopes in one query.
	longrunningpb.RegisterOperationsServer(grpcServer, operations.NewOperationsServer(operations.Config{
		LRO:      lroManager,
		Queries:  queries,
		Resolver: permResolver,
	}))

	apiv1.RegisterSpacesServer(grpcServer, spaces.NewSpacesServer(spaces.Config{
		Pool:          pool,
		Queries:       queries,
		Codec:         appCodec,
		Resolver:      permResolver,
		AuditResolver: auditResolver,
		LROManager:    lroManager,
	}))
	apiv1.RegisterOrganizationsServer(grpcServer, organizations.NewOrganizationsServer(organizations.Config{
		Pool:          pool,
		Queries:       queries,
		Codec:         appCodec,
		Resolver:      permResolver,
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
		Pool: pool, Queries: queries,
		LROManager: lroManager, AuditResolver: auditResolver,
	}))

	// Storage services
	connMgr := agentstream.NewConnectionManager()
	storageSessionMaxTTL, _ := f.GetDuration("storage-session-max-ttl")
	storageSessionCookieDomain, _ := f.GetString("storage-session-cookie-domain")
	// Session JWT signing key. Threaded into BOTH the controller's
	// CreateStorageSession handler (for minting) AND the
	// AgentService's HandshakeAck builder (for distribution to
	// agents that validate the JWTs the controller mints). The
	// two MUST be the same value; main.go is the single source of
	// truth. Production key loading via KMS is tracked in #24.
	storageSessionSigningKey := []byte("pivox-dev-session-signing-key-do-not-use-in-prod")
	storagev1.RegisterStorageGatewaysServer(grpcServer, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
		Queries: queries, Encryptor: enc, Conns: connMgr, AuditResolver: auditResolver,
		MaxSessionTTL:     storageSessionMaxTTL,
		CookieDomain:      storageSessionCookieDomain,
		SessionSigningKey: storageSessionSigningKey,
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

	// Server reflection is registered only when explicitly enabled
	// (PIVOX_ENABLE_REFLECTION / --enable-reflection, off by default).
	// It exposes the full API surface to unauthenticated callers — the
	// AuthInterceptor exempts reflection methods — so it's a dev-only
	// convenience for grpcurl / buf curl. Production leaves it unset and
	// the edge proxy blocks the reflection route (defense in depth).
	if cfg.EnableReflection {
		reflection.Register(grpcServer)
		logger.Warn("gRPC server reflection ENABLED — dev only; exposes the API surface to unauthenticated callers")
	}

	// Start gRPC listener (public surface)
	grpcLis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("listen on gRPC port %s: %w", cfg.GRPCPort, err)
	}

	// In-process bufconn listener for self-dialing clients (the REST
	// gateway translation layer and the SSE handler that wraps the
	// AiChat streaming RPC). The same grpc.Server serves both
	// listeners — interceptors run identically on either transport,
	// so AuthInterceptor still validates OIDC bearer tokens on
	// in-process calls. Avoids TCP loopback for self-dial without
	// changing the auth boundary. External gRPC clients keep
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
	// public chain (OIDC auth + membership) never has to special-case
	// agent traffic, and operators can apply different network policy
	// (firewall, mTLS termination, separate ingress) to internal-only RPCs
	// without restructuring the proto surface.
	//
	// Not exposed via grpc-gateway — REST is for public clients only.
	// ----------------------------------------------------------------------
	// No otelgrpc StatsHandler here: this server hosts only the long-lived
	// AgentService.Connect bidi stream, where a per-RPC span would stay open
	// for the entire connection (hours) — never exporting while healthy and
	// rooting all in-stream work under one unbounded span. The handler
	// instead opens a short span per stream message via streamtrace, which
	// also carries trace context across the stream boundary.
	serviceGRPCServer := grpc.NewServer(
		grpc.ChainStreamInterceptor(
			server.LoggingStreamInterceptor(logger),
			server.AgentAuthStreamInterceptor(queries),
		),
	)
	agentv1.RegisterAgentServiceServer(serviceGRPCServer, storage.NewAgentServiceServer(storage.AgentServiceConfig{
		Pool: pool, Queries: queries, Logger: logger, Conns: connMgr,
		// Same signing key the StorageGatewaysServer uses to mint
		// session JWTs. Stamped into HandshakeAck.session_signing_key
		// so connected agents validate against the same value. If
		// this drifts from storageSessionSigningKey above, every
		// storage request 401s.
		SessionSigningKey: storageSessionSigningKey,
	}))
	if cfg.EnableReflection {
		reflection.Register(serviceGRPCServer)
	}

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
		iamv1.RegisterIamHandlerFromEndpoint,
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

	// HTTP mux: gRPC gateway (fallback) + River UI. Auth flows
	// (sign-in, federation, identity sync) live in the Keycloak BFF
	// (web/start) and the keycloak-events sync path — the backend is a
	// pure resource server.
	httpMux := http.NewServeMux()

	// River UI — admin web UI for inspecting/cancelling/retrying
	// background jobs in the river schema. Prefix "/river" must
	// match the mount path "/river/".
	//
	// **Pre-prod posture: NO AUTH.** Mounted without auth wrapping
	// so an operator can click around in a browser without
	// OIDC-token plumbing. This is a deliberate dev choice —
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
	// gets OIDC bearer verification + ctx augmentation before the
	// handler runs. grpc-gateway-translated routes also pass through
	// this middleware; they pay a redundant verification (gateway
	// forwards the bearer to the gRPC backend, where AuthInterceptor
	// re-verifies) but OIDC verify is local + JWKS-cached, so the
	// cost is ~1ms and worth the "set and forget" simplicity.
	authMW := server.RequireAuth(authChainSvc, logger)
	httpMux.Handle("/", authMW(gwMux))

	// SSE bypasses HTTP auth on purpose. The handler is a thin proxy
	// to AiChat.StreamGenerateContent over the in-process bufconn
	// dial; the gRPC AuthInterceptor on that call validates the
	// bearer token forwarded as gRPC metadata. Wrapping with HTTP
	// auth would double-verify.
	//
	// Path matches the AIP gRPC route — same parent as every other
	// conversation RPC — so client-side useChat hits
	// /v1/organizations/{org}/users/{user}:streamGenerateContent
	// directly. Go 1.22 stdlib mux can't match `{user}:verb` as a
	// single segment (no mixed literal+wildcard segments), so the
	// pattern captures the full `<user>:<verb>` as one path value
	// (`userVerb`). The dispatcher below routes `:streamGenerateContent`
	// to the SSE handler and forwards every other verb (e.g.
	// `:generateContent` unary) back through the auth-wrapped
	// gateway. Without this fallback the parametric pattern would
	// shadow gateway routes on the same parent.
	gatewayWithAuth := authMW(gwMux)
	streamVerbSuffix := ":" + aichat.SSEStreamVerb()
	httpMux.HandleFunc(
		"POST /v1/organizations/{org}/users/{userVerb}",
		func(w http.ResponseWriter, r *http.Request) {
			userVerb := r.PathValue("userVerb")
			// `len > len(suffix)` rejects the degenerate case where
			// `userVerb` is exactly ":streamGenerateContent" (empty
			// user slug). The SSE handler's parsePathOrgUser also
			// catches this as a 400, but rejecting at the dispatcher
			// avoids spinning up the SSE path for input that can
			// never produce a valid stream.
			if !strings.HasSuffix(userVerb, streamVerbSuffix) || len(userVerb) <= len(streamVerbSuffix) {
				gatewayWithAuth.ServeHTTP(w, r)
				return
			}
			sseHandler.ServeHTTP(w, r)
		},
	)

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
		Addr: cfg.RESTPort,
		// otelhttp is the OUTERMOST wrapper so it extracts the incoming
		// W3C traceparent (set by the web apps' fetch instrumentation)
		// and opens the server span before request logging runs — making
		// the browser→BE→gRPC→pgx trace a single connected trace.
		Handler: otelhttp.NewHandler(requestLogMW(httpMux), "pivox-cloud"),
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

	// Shut down the LRO manager — stop the LISTEN goroutine and let it
	// release its pool conn — before pool.Close runs in the outer
	// defer. GracefulStop above ensures no new RPCs land (NewLro also
	// self-rejects post-Shutdown).
	if err := lroManager.Shutdown(shutdownCtx); err != nil {
		logger.Warn("lro shutdown drain timed out", "error", err)
	}

	logger.Info("server stopped")
	return nil
}
