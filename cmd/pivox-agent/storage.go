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

	agent "github.com/dashkan/pivox/internal/storageagent"
	"github.com/dashkan/pivox/internal/telemetry"
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
	f.String("state-dir", envOrDefault("PIVOX_STATE_DIR", "/var/lib/pivox/state"), "Agent state directory (sessions, denied patterns, endpoints). Persisted across restarts; do NOT colocate with --cache-dir or include in cache cleanup.")
	f.Int("cache-size", envOrDefaultInt("PIVOX_CACHE_SIZE", 0), "Disk cache size in GB (0 = auto-detect, 80% of available disk)")
	f.Int("memcache-max-items", envOrDefaultInt("PIVOX_MEMCACHE_MAX_ITEMS", 0), "In-memory cache: max items (0=default 100, hard max 100000)")
	f.Int("memcache-max-item-mb", envOrDefaultInt("PIVOX_MEMCACHE_MAX_ITEM_MB", 0), "In-memory cache: max size of a single item in MB (0=default 8, hard max 64)")
	f.Int("port", envOrDefaultInt("PIVOX_PORT", defaultPort), "HTTPS listen port")
	f.String("bind", envOrDefault("PIVOX_BIND", "0.0.0.0"), "Bind address")
	addControlPlaneFlag(f)
	f.String("role", envOrDefault("PIVOX_ROLE", "both"), "Agent role: both, serve, worker")
	f.String("log-level", envOrDefault("PIVOX_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	f.Bool("plaintext", envOrDefault("PIVOX_PLAINTEXT", "false") == "true", "Use plaintext (no TLS) for the control plane gRPC connection")

	// NOTE: token is required but NOT via cmd.MarkFlagRequired — that only
	// checks the command line and ignores the PIVOX_TOKEN env default, which
	// would break env-driven setups (direnv, Aspire). Validated at runtime
	// below so either --token or PIVOX_TOKEN satisfies it.

	return cmd
}

func runStorage(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()

	token, _ := f.GetString("token")
	if token == "" {
		return fmt.Errorf("token is required: set --token or PIVOX_TOKEN (the storage gateway registration token)")
	}
	cacheDir, _ := f.GetString("cache-dir")
	stateDir, _ := f.GetString("state-dir")
	cacheSize, _ := f.GetInt("cache-size")
	port, _ := f.GetInt("port")
	bind, _ := f.GetString("bind")
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
		"state_dir", stateDir,
		"cache_size_gb", cacheSize,
	)

	// Construct the in-memory blob cache used by S3-backed endpoints.
	// Done before OpenAgentState because OpenAgentState passes it
	// through to the constructed EndpointStore (which needs the cache
	// at construction time, not just at first request).
	memcacheMaxItems, _ := f.GetInt("memcache-max-items")
	memcacheMaxItemMB, _ := f.GetInt("memcache-max-item-mb")
	memcacheMaxItemBytes := memcacheMaxItemMB * 1024 * 1024 // 0 → constructor uses default (8 MB)
	cache := agent.NewMemoryCache(memcacheMaxItems, memcacheMaxItemBytes)

	// Open the agent's local state DB (sessions, denied patterns, and
	// endpoints) and reload any persisted state before the HTTP
	// listener starts.
	//
	// Boot uses a bounded, NOT signal-cancellable, context. A SIGTERM
	// during boot must not cancel a LoadFromStore mid-iteration —
	// that would surface as a partial load (some rows in memory, the
	// rest swallowed by the slog.Error path) which looks identical to
	// corruption. Boot is "complete or fail," not "cancellable." 30s
	// is generous against the busy_timeout (5s) and the realistic
	// row count.
	//
	// Failure mode: log-and-continue. OpenAgentState falls back to
	// in-memory-only stores on any state-dir / DB / load error,
	// having logged at slog.Error. The controller is the source of
	// truth and re-delivers active sessions, denied patterns, and
	// endpoints on the reconnect handshake — refusing to start would
	// be strictly worse for availability than serving with empty
	// initial state.
	bootCtx, bootCancel := context.WithTimeout(context.Background(), 30*time.Second)
	state := agent.OpenAgentState(bootCtx, agent.OpenAgentStateConfig{
		StateDir: stateDir,
		Cache:    cache,
		Logger:   logger,
	})
	bootCancel()
	defer func() {
		// Store.Close is nil-safe (see persist.go) — the explicit
		// guard here is belt-and-suspenders, not strictly required.
		if err := state.Store.Close(); err != nil {
			logger.Warn("close agent state DB", "error", err)
		}
	}()

	// Operational context — cancellable on SIGINT/SIGTERM. Used for
	// the cleanup goroutine, HTTP listener, and the control-plane
	// reconnect loop. Boot has already completed by this point.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// OpenTelemetry (traces + metrics). No-op unless an OTLP endpoint is
	// configured in the environment (the Aspire AppHost injects it).
	otelShutdown, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "pivox-agent",
		Logger:      logger,
	})
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.Warn("telemetry shutdown", "error", err)
		}
	}()

	go state.Sessions.StartCleanup(ctx, 1*time.Minute)

	// Start the HTTP file server alongside the bidi connection.
	httpServer := agent.NewHTTPServer(agent.Config{
		Sessions:   state.Sessions,
		Endpoints:  state.Endpoints,
		Denied:     state.Denied,
		CORSOrigin: "*",
		Logger:     logger,
	})

	go func() {
		addr := fmt.Sprintf("%s:%d", bind, port)
		logger.Info("HTTP server listening", "addr", addr)
		if err := httpServer.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server stopped", "error", err)
		}
	}()

	connectCfg := &agent.ConnectConfig{
		Sessions:  state.Sessions,
		Endpoints: state.Endpoints,
		Denied:    state.Denied,
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
