package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dashkan/pivox/internal/agent"
)

func storageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Run the storage gateway agent (S3 reverse proxy + cache)",
		Long: `Starts the storage gateway agent which acts as an S3 reverse proxy
with caching. The agent connects to Pivox Cloud via a persistent bidi
gRPC connection for configuration, TLS certificate delivery, and
upgrade orchestration.

The agent serves HTTPS on the local network, allowing browsers and
Electron to access storage assets directly without proxying through
the cloud.`,
		RunE: runStorage,
	}

	f := cmd.Flags()
	f.String("token", envOrDefault("PIVOX_TOKEN", ""), "Registration token from the storage gateway")
	f.String("cache-dir", envOrDefault("PIVOX_CACHE_DIR", "/var/lib/pivox/cache"), "Cache directory path")
	f.Int("cache-size", envOrDefaultInt("PIVOX_CACHE_SIZE", 0), "Cache size in GB (0 = auto-detect, 80% of available disk)")
	f.Int("port", envOrDefaultInt("PIVOX_PORT", defaultPort), "HTTPS listen port")
	f.String("bind", envOrDefault("PIVOX_BIND", "0.0.0.0"), "Bind address")
	addControlPlaneFlag(f)
	f.Bool("telemetry", envOrDefault("PIVOX_TELEMETRY", "true") == "true", "Enable telemetry reporting to Pivox Cloud")
	f.String("role", envOrDefault("PIVOX_ROLE", "both"), "Agent role: both, serve, worker")
	f.String("log-level", envOrDefault("PIVOX_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	f.Bool("plaintext", envOrDefault("PIVOX_PLAINTEXT", "false") == "true", "Use plaintext (no TLS) for the control plane gRPC connection")

	_ = cmd.MarkFlagRequired("token")

	return cmd
}

func runStorage(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()

	token, _ := f.GetString("token")
	cacheDir, _ := f.GetString("cache-dir")
	cacheSize, _ := f.GetInt("cache-size")
	port, _ := f.GetInt("port")
	bind, _ := f.GetString("bind")
	telemetry, _ := f.GetBool("telemetry")
	logLevel, _ := f.GetString("log-level")
	plaintext, _ := f.GetBool("plaintext")

	var level slog.Level
	switch logLevel {
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

	logger.Info("starting storage agent",
		"server", cloudHost,
		"bind", fmt.Sprintf("%s:%d", bind, port),
		"cache_dir", cacheDir,
		"cache_size_gb", cacheSize,
		"telemetry", telemetry,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create stores.
	sessions := agent.NewSessionStore()
	go sessions.StartCleanup(ctx, 1*time.Minute)

	cache := agent.NewMemoryCache(0, 0) // defaults: 1000 entries, 256MB
	endpoints := agent.NewEndpointStore(cache)
	denied := agent.NewDeniedPatterns()

	// Start the HTTP file server alongside the bidi connection.
	httpServer := agent.NewHTTPServer(sessions, endpoints, denied, nil, "*", logger)

	go func() {
		addr := fmt.Sprintf("%s:%d", bind, port)
		logger.Info("HTTP server listening", "addr", addr)
		if err := httpServer.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server stopped", "error", err)
		}
	}()

	connectCfg := &agent.ConnectConfig{
		Sessions:  sessions,
		Endpoints: endpoints,
		Denied:    denied,
		HTTP:      httpServer,
	}

	// Connect to control plane with reconnect loop.
	for {
		logger.Info("connecting to server", "addr", cloudHost)
		err := agent.Connect(ctx, cloudHost, !plaintext, token, connectCfg, logger)
		if ctx.Err() != nil {
			logger.Info("storage agent shutting down...")
			return nil
		}
		logger.Error("disconnected from server", "error", err)

		// Back off before reconnecting.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}
