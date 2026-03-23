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

	agentv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/agent/v1"
)

const (
	handshakeTimeout  = 10 * time.Second
	heartbeatInterval = 30 * time.Second
)

// Connect dials the control plane at addr, performs the handshake using the
// given registration token, and runs the heartbeat loop until the context is
// cancelled or the stream encounters an error. The caller is responsible for
// reconnection with backoff.
func Connect(ctx context.Context, addr string, token string, logger *slog.Logger) error {
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

	stream := NewStream(bidi, handshakeTimeout, logger)

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
