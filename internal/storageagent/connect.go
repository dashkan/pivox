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

	// OnConnected fires once the handshake has completed AND its payload has been
	// applied — endpoints, denied patterns, and above all the session signing key.
	// That is the first instant the agent can serve anything: without the signing
	// key it cannot validate a single session, so "connected" is the honest
	// readiness boundary, not "process started".
	//
	// OnDisconnected fires when the stream ends, for any reason. A disconnected
	// agent receives no new sessions, so it belongs out of the ready set until it
	// reconnects (Connect is driven by a reconnect loop in cmd/pivox-agent).
	//
	// Both are optional and must be safe to call from this goroutine.
	OnConnected    func()
	OnDisconnected func()
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

	// The session signing key is REQUIRED, not optional. It is the HMAC key every
	// /files/ session JWT is validated against (see http.go), so without it the
	// agent validates nothing: hmac.New(sha256.New, nil) rejects every session.
	//
	// Treat its absence as a failed handshake rather than carrying on. An agent
	// that "connects", reports ready, gets traffic, and then rejects 100% of it is
	// the exact looks-healthy-isn't failure the readiness work exists to kill —
	// and it would be reproduced here. The reconnect loop retries, so a transient
	// cloud-side gap self-heals.
	key := ack.GetSessionSigningKey()
	if len(key) == 0 {
		return fmt.Errorf("handshake: no session signing key — cannot validate sessions")
	}
	cfg.HTTP.SetSigningKey(key)

	if origin := ack.GetCorsOrigin(); origin != "" {
		cfg.HTTP.SetCORSOrigin(origin)
	}

	// Whatever ends this stream — error, cloud restart, context cancel — the agent
	// stops receiving new sessions, so it must leave the ready set. Registered
	// BEFORE OnConnected and independently of it: the two callbacks are documented
	// as separately optional, and nesting this inside `if OnConnected != nil` would
	// silently drop OnDisconnected for a caller that set only that one.
	if cfg.OnDisconnected != nil {
		defer cfg.OnDisconnected()
	}
	// Ready: the handshake landed AND its config — signing key above all — is
	// applied. Announced here rather than at stream-open, because a stream without
	// the key cannot validate a session: the agent would be "connected" and still
	// unable to serve.
	if cfg.OnConnected != nil {
		cfg.OnConnected()
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
