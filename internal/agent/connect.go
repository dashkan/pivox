package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

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

func Connect(ctx context.Context, addr string, token string, cfg *ConnectConfig, logger *slog.Logger) error {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)

	bidi, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	stream := NewStream(bidi, handshakeTimeout, cfg.Sessions, cfg.Endpoints, cfg.Denied, logger)

	// Start the receive loop in the background. It will return when the
	// stream is closed or errors out.
	recvErr := make(chan error, 1)
	go func() {
		recvErr <- stream.ReceiveLoop(ctx)
	}()

	// Perform the handshake.
	hostname, _ := os.Hostname()

	ack, err := stream.Handshake(ctx, &agentv1.Handshake{
		RegistrationToken: token,
		AgentVersion:      version(),
		IpAddress:         "0.0.0.0",
		Hostname:          hostname,
		Os:                runtime.GOOS,
		Arch:              runtime.GOARCH,
	})
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	logger.Info("connected to server", "agent_name", ack.GetAgentName())

	// Apply initial config from handshake.
	if endpoints := ack.GetEndpoints(); len(endpoints) > 0 {
		if err := cfg.Endpoints.Update(endpoints); err != nil {
			logger.Error("failed to apply initial endpoints", "error", err)
		} else {
			logger.Info("loaded endpoints", "count", len(endpoints))
		}
	}

	if patterns := ack.GetDeniedPatterns(); len(patterns) > 0 {
		cfg.Denied.Update(patterns)
		logger.Info("loaded denied patterns", "count", len(patterns))
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
