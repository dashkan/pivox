package storageagent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/dashkan/pivox/internal/telemetry/streamtrace"
)

// setAgentTraceContext stamps a trace-context carrier onto an outgoing
// AgentMessage. proto generates no setter, so streamtrace.Send takes this
// one-liner.
func setAgentTraceContext(m *agentv1.AgentMessage, c map[string]string) { m.TraceContext = c }

// controlMessageName returns a short span-name suffix for an inbound control
// message's oneof variant.
func controlMessageName(msg *agentv1.ControlMessage) string {
	switch msg.GetMessage().(type) {
	case *agentv1.ControlMessage_HandshakeAck:
		return "HandshakeAck"
	case *agentv1.ControlMessage_CertDelivery:
		return "CertDelivery"
	case *agentv1.ControlMessage_DrainRequest:
		return "DrainRequest"
	case *agentv1.ControlMessage_UpgradeRequest:
		return "UpgradeRequest"
	case *agentv1.ControlMessage_ConfigUpdate:
		return "ConfigUpdate"
	case *agentv1.ControlMessage_ServerHeartbeat:
		return "ServerHeartbeat"
	case *agentv1.ControlMessage_SessionGrant:
		return "SessionGrant"
	case *agentv1.ControlMessage_SessionRevoke:
		return "SessionRevoke"
	default:
		return "Unknown"
	}
}

// Stream wraps a bidirectional gRPC stream with typed send methods and
// request/response correlation. Fire-and-forget messages (heartbeat,
// telemetry, endpoint health) are sent without an id. Request/response
// messages (handshake) use a UUID-based correlation id so the caller can
// block until the server responds.
type Stream struct {
	stream    agentv1.AgentService_ConnectClient
	pending   map[string]chan *agentv1.ControlMessage
	mu        sync.Mutex
	timeout   time.Duration
	sessions  *SessionStore
	endpoints *EndpointStore
	denied    *DeniedPatterns
	logger    *slog.Logger
}

// DefaultStreamTimeout is the round-trip timeout used when
// StreamConfig.Timeout is left zero.
const DefaultStreamTimeout = 30 * time.Second

// StreamConfig is the constructor input for Stream. Suffixed to
// avoid colliding with the package-level Config used by HTTPServer.
type StreamConfig struct {
	// Stream is the bidirectional gRPC stream the wrapper drives.
	// Required.
	Stream agentv1.AgentService_ConnectClient
	// Timeout caps a single request/response round-trip. Zero ⇒
	// DefaultStreamTimeout.
	Timeout time.Duration
	// Sessions tracks short-lived agent-issued session tokens granted
	// by the control plane. Required.
	Sessions *SessionStore
	// Endpoints tracks the local endpoint registry pushed by the
	// control plane. Required.
	Endpoints *EndpointStore
	// Denied tracks the deny-list patterns pushed by the control
	// plane. Required.
	Denied *DeniedPatterns
	// Logger is the slog logger used for stream-side audit lines.
	// Required.
	Logger *slog.Logger
}

// NewStream constructs a Stream wrapper from cfg. Panics on a
// missing required field — startup-time programmer error, fail loud
// on boot. The caller must start ReceiveLoop in a separate goroutine
// before calling any request/response method (e.g. Handshake).
func NewStream(cfg StreamConfig) *Stream {
	if cfg.Stream == nil {
		panic("storageagent: StreamConfig.Stream is required")
	}
	if cfg.Sessions == nil {
		panic("storageagent: StreamConfig.Sessions is required")
	}
	if cfg.Endpoints == nil {
		panic("storageagent: StreamConfig.Endpoints is required")
	}
	if cfg.Denied == nil {
		panic("storageagent: StreamConfig.Denied is required")
	}
	if cfg.Logger == nil {
		panic("storageagent: StreamConfig.Logger is required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultStreamTimeout
	}
	return &Stream{
		stream:    cfg.Stream,
		pending:   make(map[string]chan *agentv1.ControlMessage),
		timeout:   timeout,
		sessions:  cfg.Sessions,
		endpoints: cfg.Endpoints,
		denied:    cfg.Denied,
		logger:    cfg.Logger,
	}
}

// Handshake sends a Handshake message and waits for the corresponding
// HandshakeAck from the control plane. It uses roundTrip for correlation.
func (s *Stream) Handshake(ctx context.Context, h *agentv1.Handshake) (*agentv1.HandshakeAck, error) {
	msg := &agentv1.AgentMessage{
		Message: &agentv1.AgentMessage_Handshake{Handshake: h},
	}

	resp, err := s.roundTrip(ctx, "AgentService/Handshake", msg)
	if err != nil {
		return nil, fmt.Errorf("handshake: %w", err)
	}

	ack := resp.GetHandshakeAck()
	if ack == nil {
		return nil, fmt.Errorf("handshake: expected HandshakeAck, got %T", resp.GetMessage())
	}

	return ack, nil
}

// SendHeartbeat sends a fire-and-forget heartbeat to the control plane.
func (s *Stream) SendHeartbeat(ctx context.Context, h *agentv1.Heartbeat) error {
	msg := &agentv1.AgentMessage{Message: &agentv1.AgentMessage_Heartbeat{Heartbeat: h}}
	return streamtrace.Send(ctx, "AgentService/Heartbeat", msg, setAgentTraceContext, s.send)
}

// SendEndpointHealth sends a fire-and-forget endpoint health report.
func (s *Stream) SendEndpointHealth(ctx context.Context, eh *agentv1.EndpointHealth) error {
	msg := &agentv1.AgentMessage{Message: &agentv1.AgentMessage_EndpointHealth{EndpointHealth: eh}}
	return streamtrace.Send(ctx, "AgentService/EndpointHealth", msg, setAgentTraceContext, s.send)
}

// SendUpgradeStatus sends a fire-and-forget upgrade status report.
func (s *Stream) SendUpgradeStatus(ctx context.Context, us *agentv1.UpgradeStatus) error {
	msg := &agentv1.AgentMessage{Message: &agentv1.AgentMessage_UpgradeStatus{UpgradeStatus: us}}
	return streamtrace.Send(ctx, "AgentService/UpgradeStatus", msg, setAgentTraceContext, s.send)
}

// roundTrip sends a message with a generated correlation id, waits for the
// matching response from the receive loop, and returns it. If the context
// deadline or the stream timeout is exceeded, the pending entry is cleaned
// up and an error is returned.
func (s *Stream) roundTrip(ctx context.Context, name string, msg *agentv1.AgentMessage) (*agentv1.ControlMessage, error) {
	id := uuid.New().String()
	msg.Id = id

	ch := make(chan *agentv1.ControlMessage, 1)

	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	// Pending entry is registered before the send so a fast response can't
	// race us. streamtrace.Send opens a producer span + stamps trace context
	// into msg so the control plane's handler joins this trace.
	if err := streamtrace.Send(ctx, name, msg, setAgentTraceContext, s.send); err != nil {
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("stream closed while waiting for response")
		}
		return resp, nil
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("timed out waiting for response (id=%s): %w", id, timeoutCtx.Err())
	}
}

// send writes a fire-and-forget message to the stream. No correlation id is
// set and no response is expected.
func (s *Stream) send(msg *agentv1.AgentMessage) error {
	if err := s.stream.Send(msg); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// ReceiveLoop reads messages from the stream in a loop, routing responses
// to pending request channels by correlation id. Server-initiated messages
// (those with an id not in the pending map, or no id) are logged. When the
// stream returns an error, all pending channels are closed and the function
// returns the error.
func (s *Stream) ReceiveLoop(ctx context.Context) error {
	for {
		resp, err := s.stream.Recv()
		if err != nil {
			s.mu.Lock()
			for id, ch := range s.pending {
				close(ch)
				delete(s.pending, id)
			}
			s.mu.Unlock()
			return fmt.Errorf("receive: %w", err)
		}

		if resp.Id != "" {
			s.mu.Lock()
			ch, ok := s.pending[resp.Id]
			s.mu.Unlock()

			if ok {
				ch <- resp
				continue
			}
		}

		// Server-initiated message (not a response to a pending request).
		s.handleServerMessage(ctx, resp)
	}
}

// handleServerMessage processes server-initiated control messages.
// ctx is the receive-loop context, used to propagate cancellation
// through any persistent writes (Grant/Revoke against the SQLite
// store).
func (s *Stream) handleServerMessage(ctx context.Context, msg *agentv1.ControlMessage) {
	// Isolate handler panics: handleServerMessage runs in the ReceiveLoop
	// goroutine, and a panic here (e.g. a malformed/unexpected control message)
	// would crash the loop WITHOUT running its pending-channel cleanup, leaving
	// roundTrip callers (Handshake) blocked until timeout. Recover so one bad
	// message can't take down the whole stream; the controller can resend.
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("recovered from panic handling server message",
				"type", controlMessageName(msg), "panic", r)
		}
	}()

	// Per-message consumer span continuing the control-plane operation that
	// produced this message (trace context carried in msg.TraceContext). Work
	// done below (SQLite writes via sessions/denied/endpoints) nests under it.
	ctx, span := streamtrace.Receive(ctx, "AgentService/"+controlMessageName(msg), msg.GetTraceContext())
	defer span.End()

	switch m := msg.GetMessage().(type) {
	case *agentv1.ControlMessage_ConfigUpdate:
		update := m.ConfigUpdate
		if err := s.endpoints.Update(ctx, update.GetEndpoints()); err != nil {
			s.logger.Error("failed to apply config update", "error", err)
		} else {
			s.logger.Info("applied config update", "endpoints", len(update.GetEndpoints()))
		}
		if patterns := update.GetDeniedPatterns(); patterns != nil {
			if err := s.denied.Update(ctx, patterns); err != nil {
				// Persistence failure: log and continue with the
				// existing in-memory set. The controller will resend
				// the full set on the next ConfigUpdate.
				s.logger.Error("failed to apply denied patterns update", "error", err)
			} else {
				s.logger.Info("updated denied patterns", "count", len(patterns))
			}
		}
	case *agentv1.ControlMessage_DrainRequest:
		s.logger.Info("received drain request",
			"reason", m.DrainRequest.GetReason(),
		)
	case *agentv1.ControlMessage_CertDelivery:
		s.logger.Info("received certificate delivery")
	case *agentv1.ControlMessage_UpgradeRequest:
		s.logger.Info("received upgrade request",
			"command", m.UpgradeRequest.GetCommand().String(),
			"target_version", m.UpgradeRequest.GetTargetVersion(),
		)
	case *agentv1.ControlMessage_SessionGrant:
		grant := m.SessionGrant
		if err := s.sessions.Grant(ctx, grant.Token, grant.Patterns, grant.Expiry.AsTime()); err != nil {
			// Persistence (or future controller-mandated atomic check)
			// failed. Drop the grant rather than leaving in-memory and
			// disk diverged; the controller can re-deliver.
			//
			// TODO: when SessionGrantAck (or equivalent) is added to
			// the control protocol, NACK back to the controller here
			// so it knows the agent rejected the grant rather than
			// silently logging.
			s.logger.Error("failed to apply session grant",
				"token", tokenPrefix(grant.Token), "error", err)
			break
		}
		s.logger.Info("session granted", "token", tokenPrefix(grant.Token), "patterns", len(grant.Patterns))
	case *agentv1.ControlMessage_SessionRevoke:
		if err := s.sessions.Revoke(ctx, m.SessionRevoke.Token); err != nil {
			s.logger.Error("failed to apply session revoke",
				"token", tokenPrefix(m.SessionRevoke.Token), "error", err)
			break
		}
		s.logger.Info("session revoked", "token", tokenPrefix(m.SessionRevoke.Token))
	case *agentv1.ControlMessage_ServerHeartbeat:
		s.logger.Debug("received server heartbeat")
	default:
		s.logger.Warn("received unknown server message", "type", fmt.Sprintf("%T", m))
	}
}

// tokenPrefix returns the first up-to-8 bytes of token followed by
// "...". Bounded by len(token) so a short or empty token doesn't panic
// the slice. Used for log lines where the full opaque token must not
// be emitted but a debugging prefix is useful.
func tokenPrefix(token string) string {
	const n = 8
	if len(token) <= n {
		return token + "..."
	}
	return token[:n] + "..."
}
