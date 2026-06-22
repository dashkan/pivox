package storageagent

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// agentTokenMetadataKey must match server-side server.AgentTokenMetadataKey.
// Duplicated as a literal here to avoid importing the cloud server package
// from the agent binary (separate deployable surfaces, separate go modules
// in the long run).
const agentTokenMetadataKey = "x-pivox-agent-token"

const (
	handshakeTimeout  = 10 * time.Second
	heartbeatInterval = 30 * time.Second
)

// Connect dials the control plane at addr, performs the handshake using the
// given registration token, and runs the heartbeat loop until the context is
// cancelled or the stream encounters an error. The caller is responsible for
// reconnection with backoff.
// ConnectConfig holds the dependencies for the agent connection.
type ConnectConfig struct {
	Sessions  *SessionStore
	Endpoints *EndpointStore
	Denied    *DeniedPatterns
	HTTP      *HTTPServer
}

func Connect(ctx context.Context, addr string, useTLS bool, token string, cfg *ConnectConfig, logger *slog.Logger) error {
	var creds grpc.DialOption
	if useTLS {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	} else {
		creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	// No otelgrpc client StatsHandler: the only RPC on this connection is the
	// long-lived Connect bidi stream, where a per-RPC client span would stay
	// open for the whole session (never exporting while healthy). Trace context
	// flows per-message via streamtrace instead (stamped into each AgentMessage
	// / read off each ControlMessage), and each message gets its own span.
	conn, err := grpc.NewClient(addr, creds)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	client := agentv1.NewAgentServiceClient(conn)

	// Attach the registration token to outgoing initial metadata. The cloud
	// server's per-service interceptor validates and resolves the gateway
	// before the Connect handler runs; the token is no longer carried in
	// the Handshake proto message.
	streamCtx := metadata.AppendToOutgoingContext(ctx, agentTokenMetadataKey, token)

	bidi, err := client.Connect(streamCtx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	stream := NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   handshakeTimeout,
		Sessions:  cfg.Sessions,
		Endpoints: cfg.Endpoints,
		Denied:    cfg.Denied,
		Logger:    logger,
	})

	// Start the receive loop in the background. It will return when the
	// stream is closed or errors out.
	recvErr := make(chan error, 1)
	go func() {
		recvErr <- stream.ReceiveLoop(ctx)
	}()

	// Perform the handshake.
	hostname, _ := os.Hostname()

	ack, err := stream.Handshake(ctx, &agentv1.Handshake{
		AgentVersion: version(),
		IpAddress:    "0.0.0.0",
		Hostname:     hostname,
		Os:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	})
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	logger.Info("connected to server", "agent_name", ack.GetAgentName())

	// Apply initial config from handshake.
	if endpoints := ack.GetEndpoints(); len(endpoints) > 0 {
		if err := cfg.Endpoints.Update(ctx, endpoints); err != nil {
			logger.Error("failed to apply initial endpoints", "error", err)
		} else {
			logger.Info("loaded endpoints", "count", len(endpoints))
		}
	}

	if patterns := ack.GetDeniedPatterns(); len(patterns) > 0 {
		if err := cfg.Denied.Update(ctx, patterns); err != nil {
			// Persistence failure on the initial-handshake denied set:
			// log loud, leave whatever was reloaded from disk by
			// LoadFromStore in place. The controller resends the full
			// set on the next ConfigUpdate.
			logger.Error("failed to apply initial denied patterns", "error", err)
		} else {
			logger.Info("loaded denied patterns", "count", len(patterns))
		}
	}

	// Update HTTP server with signing key and CORS from handshake.
	if key := ack.GetSessionSigningKey(); len(key) > 0 {
		cfg.HTTP.SetSigningKey(key)
	}
	if origin := ack.GetCorsOrigin(); origin != "" {
		cfg.HTTP.SetCORSOrigin(origin)
	}

	// Heartbeat loop.
	startTime := time.Now()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-recvErr:
			return fmt.Errorf("stream: %w", err)
		case <-ticker.C:
			uptime := time.Since(startTime)
			if err := stream.SendHeartbeat(ctx, &agentv1.Heartbeat{
				State:  "ready",
				Uptime: durationpb.New(uptime),
			}); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
			logger.Debug("sent heartbeat", "uptime", uptime.Round(time.Second))
		}
	}
}

// version returns the agent binary version. It is set at build time via
// -ldflags in production; defaults to "dev" during development.
func version() string {
	// This could be wired to a build-time variable. For now, return "dev".
	return "dev"
}
