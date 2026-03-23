package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dashkan/pivox-server/internal/agent"
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
	f.Int("cache-size", 0, "Cache size in GB (0 = auto-detect, 80% of available disk)")
	f.Int("port", 443, "HTTPS listen port")
	f.String("bind", envOrDefault("PIVOX_BIND", "0.0.0.0"), "Bind address")
	addControlPlaneFlag(f)
	f.Bool("telemetry", true, "Enable telemetry reporting to Pivox Cloud")
	f.String("log-level", envOrDefault("PIVOX_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")

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
		"server", controlPlaneAddr,
		"bind", fmt.Sprintf("%s:%d", bind, port),
		"cache_dir", cacheDir,
		"cache_size_gb", cacheSize,
		"telemetry", telemetry,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Connect to control plane with reconnect loop.
	for {
		logger.Info("connecting to server", "addr", controlPlaneAddr)
		err := agent.Connect(ctx, controlPlaneAddr, token, logger)
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
