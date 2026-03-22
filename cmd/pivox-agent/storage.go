package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
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
	f.String("control-plane", envOrDefault("PIVOX_CONTROL_PLANE", "api.pivox.io:443"), "Control plane gRPC address")
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
	controlPlane, _ := f.GetString("control-plane")
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
		"control_plane", controlPlane,
		"bind", fmt.Sprintf("%s:%d", bind, port),
		"cache_dir", cacheDir,
		"cache_size_gb", cacheSize,
		"telemetry", telemetry,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// TODO: Implement storage agent
	// 1. Dial control plane with registration token
	// 2. Establish bidi gRPC stream (AgentService.Connect)
	// 3. Receive HandshakeAck with TLS cert + endpoint configs
	// 4. Start HTTPS reverse proxy server
	// 5. Start cache manager
	// 6. Send periodic heartbeats
	// 7. Handle control plane messages (config updates, cert renewals, upgrades)
	_ = token

	<-ctx.Done()
	logger.Info("storage agent shutting down...")

	return nil
}
