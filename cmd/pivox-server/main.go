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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"github.com/dashkan/pivox-server/internal/config"
	"github.com/dashkan/pivox-server/internal/crypto"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/firebase"
	"github.com/dashkan/pivox-server/internal/iam"
	"github.com/dashkan/pivox-server/internal/lro"
	"github.com/dashkan/pivox-server/internal/server"
	"github.com/dashkan/pivox-server/internal/service/apikeys"
	"github.com/dashkan/pivox-server/internal/service/operations"
	"github.com/dashkan/pivox-server/internal/service/organizations"
	"github.com/dashkan/pivox-server/internal/service/projects"
	"github.com/dashkan/pivox-server/internal/service/storage"
	"github.com/dashkan/pivox-server/internal/service/tags"

	agentv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/agent/v1"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
	storagev1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/storage/v1"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "pivox-server",
		Short:   "Pivox control plane server",
		Version: version,
		RunE:    serve,
	}

	f := rootCmd.Flags()
	f.String("database-url", envOrDefault("DATABASE_URL", "postgres://localhost:5432/pivox?sslmode=disable"), "PostgreSQL connection URL")
	f.String("grpc-port", envOrDefault("GRPC_PORT", ":50051"), "gRPC listen address")
	f.String("rest-port", envOrDefault("REST_PORT", ":8080"), "REST gateway listen address")
	f.String("debug-port", envOrDefault("DEBUG_PORT", ":9090"), "Debug/health listen address")
	f.String("log-level", envOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	f.String("gcp-project-id", envOrDefault("GOOGLE_CLOUD_PROJECT_ID", ""), "Google Cloud project ID")
	f.String("gcp-service-account-key", envOrDefault("GOOGLE_CLOUD_SERVICE_ACCOUNT_KEY", ""), "Google Cloud service account key (inline JSON)")
	f.String("gcp-service-account-file", envOrDefault("GOOGLE_CLOUD_SERVICE_ACCOUNT_FILE", ""), "Google Cloud service account key file path")

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

func must(s string, _ error) string { return s }

func serve(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	cfg := &config.Config{
		DatabaseURL: must(f.GetString("database-url")),
		GRPCPort:    must(f.GetString("grpc-port")),
		RESTPort:    must(f.GetString("rest-port")),
		DebugPort:   must(f.GetString("debug-port")),
		LogLevel:    must(f.GetString("log-level")),
		GoogleCloud: config.GoogleCloudConfig{
			ProjectID:          must(f.GetString("gcp-project-id")),
			ServiceAccountKey:  must(f.GetString("gcp-service-account-key")),
			ServiceAccountFile: must(f.GetString("gcp-service-account-file")),
		},
		SyncAuth: loadSyncAuthConfig(cmd),
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

	// Database
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
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
	lroManager := lro.NewManager(pool, queries, logger)
	iamHelper := iam.NewHelper(queries)

	// Recover any pending operations from previous run
	if err := lroManager.RecoverPending(ctx); err != nil {
		logger.Error("failed to recover pending operations", "error", err)
	}

	// Start the operation reaper
	reaper := lro.NewReaper(queries, 5*time.Minute, logger)
	go func() {
		if err := reaper.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("reaper stopped", "error", err)
		}
	}()

	// Firebase
	authSvc, err := firebase.NewAuthService(ctx, cfg.GoogleCloud)
	if err != nil {
		return fmt.Errorf("initialize Firebase auth: %w", err)
	}

	// gRPC server
	validator, err := protovalidate.New()
	if err != nil {
		return fmt.Errorf("create validator: %w", err)
	}
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			server.AuthInterceptor(authSvc),
			server.FieldMaskAwareValidationInterceptor(validator),
		),
		grpc.ChainStreamInterceptor(
			server.AuthStreamInterceptor(authSvc),
		),
	)

	// Register all services
	longrunningpb.RegisterOperationsServer(grpcServer, operations.NewOperationsServer(lroManager))
	apiv1.RegisterProjectsServer(grpcServer, projects.NewProjectsServer(pool, queries, iamHelper))
	apiv1.RegisterOrganizationsServer(grpcServer, organizations.NewOrganizationsServer(pool, queries, iamHelper, authSvc))
	apiv1.RegisterTagKeysServer(grpcServer, tags.NewTagKeysServer(pool, queries, iamHelper))
	apiv1.RegisterTagValuesServer(grpcServer, tags.NewTagValuesServer(pool, queries, iamHelper))
	apiv1.RegisterTagBindingsServer(grpcServer, tags.NewTagBindingsServer(pool, queries))
	apiv1.RegisterApiKeysServer(grpcServer, apikeys.NewApiKeysServer(pool, queries))

	// Storage services
	storagev1.RegisterStorageGatewaysServer(grpcServer, storage.NewStorageGatewaysServer(pool, queries, enc))
	storagev1.RegisterAgentsServer(grpcServer, storage.NewAgentsServer(queries))
	storagev1.RegisterEndpointsServer(grpcServer, storage.NewEndpointsServer(pool, queries, enc))

	// Agent bidi streaming service (agents authenticate via registration token, not Firebase)
	agentv1.RegisterAgentServiceServer(grpcServer, storage.NewAgentServiceServer(pool, queries, logger))

	reflection.Register(grpcServer)

	// Start gRPC listener
	grpcLis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("listen on gRPC port %s: %w", cfg.GRPCPort, err)
	}

	go func() {
		logger.Info("gRPC server listening", "addr", cfg.GRPCPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			logger.Error("gRPC server stopped", "error", err)
		}
	}()

	// REST gateway
	gwMux := runtime.NewServeMux()
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	grpcEndpoint := fmt.Sprintf("localhost%s", cfg.GRPCPort)

	for _, reg := range []func(context.Context, *runtime.ServeMux, string, []grpc.DialOption) error{
		apiv1.RegisterProjectsHandlerFromEndpoint,
		apiv1.RegisterOrganizationsHandlerFromEndpoint,
		apiv1.RegisterTagKeysHandlerFromEndpoint,
		apiv1.RegisterTagValuesHandlerFromEndpoint,
		apiv1.RegisterTagBindingsHandlerFromEndpoint,
		apiv1.RegisterApiKeysHandlerFromEndpoint,
		storagev1.RegisterStorageGatewaysHandlerFromEndpoint,
		storagev1.RegisterAgentsHandlerFromEndpoint,
		storagev1.RegisterEndpointsHandlerFromEndpoint,
	} {
		if err := reg(ctx, gwMux, grpcEndpoint, dialOpts); err != nil {
			return fmt.Errorf("register REST gateway: %w", err)
		}
	}

	// HTTP mux: internal hooks + gRPC gateway (fallback)
	httpMux := http.NewServeMux()
	hooks, err := server.NewInternalHooks(queries, cfg.SyncAuth, logger, authSvc)
	if err != nil {
		return fmt.Errorf("initialize internal hooks: %w", err)
	}
	hooks.Register(httpMux)
	httpMux.Handle("/", gwMux)

	restServer := &http.Server{
		Addr:    cfg.RESTPort,
		Handler: httpMux,
	}
	go func() {
		logger.Info("REST gateway listening", "addr", cfg.RESTPort)
		if err := restServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST gateway stopped", "error", err)
		}
	}()

	// Debug server (health/readiness)
	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	debugMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "not ready")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ready")
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

	logger.Info("server stopped")
	return nil
}
