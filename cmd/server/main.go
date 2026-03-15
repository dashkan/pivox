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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"github.com/pivoxai/pivox/internal/config"
	db "github.com/pivoxai/pivox/internal/db/generated"
	"github.com/pivoxai/pivox/internal/iam"
	"github.com/pivoxai/pivox/internal/lro"
	"github.com/pivoxai/pivox/internal/server"

	apiv1 "github.com/pivoxai/pivox/internal/pkg/gen/pivox/api/v1"
)

func main() {
	cfg := config.Load()

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
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	queries := db.New(pool)

	// Shared services
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

	// gRPC server
	validator, err := protovalidate.New()
	if err != nil {
		logger.Error("failed to create validator", "error", err)
		os.Exit(1)
	}
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(server.FieldMaskAwareValidationInterceptor(validator)),
	)

	// Register all services
	longrunningpb.RegisterOperationsServer(grpcServer, server.NewOperationsServer(lroManager))
	apiv1.RegisterProjectsServer(grpcServer, server.NewProjectsServer(pool, queries, iamHelper))
	apiv1.RegisterOrganizationsServer(grpcServer, server.NewOrganizationsServer(pool, queries, iamHelper))
	apiv1.RegisterTagKeysServer(grpcServer, server.NewTagKeysServer(pool, queries, iamHelper))
	apiv1.RegisterTagValuesServer(grpcServer, server.NewTagValuesServer(pool, queries, iamHelper))
	apiv1.RegisterTagBindingsServer(grpcServer, server.NewTagBindingsServer(pool, queries))
	apiv1.RegisterApiKeysServer(grpcServer, server.NewApiKeysServer(pool, queries))

	reflection.Register(grpcServer)

	// Start gRPC listener
	grpcLis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		logger.Error("failed to listen on gRPC port", "port", cfg.GRPCPort, "error", err)
		os.Exit(1)
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
	} {
		if err := reg(ctx, gwMux, grpcEndpoint, dialOpts); err != nil {
			logger.Error("failed to register REST gateway", "error", err)
			os.Exit(1)
		}
	}

	// HTTP mux: internal hooks + gRPC gateway (fallback)
	httpMux := http.NewServeMux()
	hooks := server.NewInternalHooks(queries, cfg.InternalSecret, logger)
	hooks.Register(httpMux)
	httpMux.Handle("/", gwMux) // gRPC gateway handles everything else

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
}
