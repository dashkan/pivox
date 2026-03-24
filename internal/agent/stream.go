package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

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

// NewStream creates a Stream wrapper around the given bidi gRPC stream.
// The caller must start ReceiveLoop in a separate goroutine before calling
// any request/response method (e.g. Handshake).
func NewStream(stream agentv1.AgentService_ConnectClient, timeout time.Duration, sessions *SessionStore, endpoints *EndpointStore, denied *DeniedPatterns, logger *slog.Logger) *Stream {
	return &Stream{
		stream:    stream,
		pending:   make(map[string]chan *agentv1.ControlMessage),
		timeout:   timeout,
		sessions:  sessions,
		endpoints: endpoints,
		denied:    denied,
		logger:    logger,
	}
}

// Handshake sends a Handshake message and waits for the corresponding
// HandshakeAck from the control plane. It uses roundTrip for correlation.
func (s *Stream) Handshake(ctx context.Context, h *agentv1.Handshake) (*agentv1.HandshakeAck, error) {
	msg := &agentv1.AgentMessage{
		Message: &agentv1.AgentMessage_Handshake{Handshake: h},
	}

	resp, err := s.roundTrip(ctx, msg)
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
	return s.send(&agentv1.AgentMessage{
		Message: &agentv1.AgentMessage_Heartbeat{Heartbeat: h},
	})
}

// SendTelemetry sends a fire-and-forget telemetry report to the control plane.
func (s *Stream) SendTelemetry(ctx context.Context, t *agentv1.Telemetry) error {
	return s.send(&agentv1.AgentMessage{
		Message: &agentv1.AgentMessage_Telemetry{Telemetry: t},
	})
}

// SendEndpointHealth sends a fire-and-forget endpoint health report.
func (s *Stream) SendEndpointHealth(ctx context.Context, eh *agentv1.EndpointHealth) error {
	return s.send(&agentv1.AgentMessage{
		Message: &agentv1.AgentMessage_EndpointHealth{EndpointHealth: eh},
	})
}

// SendUpgradeStatus sends a fire-and-forget upgrade status report.
func (s *Stream) SendUpgradeStatus(ctx context.Context, us *agentv1.UpgradeStatus) error {
	return s.send(&agentv1.AgentMessage{
		Message: &agentv1.AgentMessage_UpgradeStatus{UpgradeStatus: us},
	})
}

// roundTrip sends a message with a generated correlation id, waits for the
// matching response from the receive loop, and returns it. If the context
// deadline or the stream timeout is exceeded, the pending entry is cleaned
// up and an error is returned.
func (s *Stream) roundTrip(ctx context.Context, msg *agentv1.AgentMessage) (*agentv1.ControlMessage, error) {
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

	if err := s.stream.Send(msg); err != nil {
		return nil, fmt.Errorf("send: %w", err)
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
		s.handleServerMessage(resp)
	}
}

// handleServerMessage processes server-initiated control messages. For now
// it logs them; handler callbacks can be added later.
func (s *Stream) handleServerMessage(msg *agentv1.ControlMessage) {
	switch m := msg.GetMessage().(type) {
	case *agentv1.ControlMessage_ConfigUpdate:
		update := m.ConfigUpdate
		if err := s.endpoints.Update(update.GetEndpoints()); err != nil {
			s.logger.Error("failed to apply config update", "error", err)
		} else {
			s.logger.Info("applied config update", "endpoints", len(update.GetEndpoints()))
		}
		if patterns := update.GetDeniedPatterns(); patterns != nil {
			s.denied.Update(patterns)
			s.logger.Info("updated denied patterns", "count", len(patterns))
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
		s.sessions.Grant(grant.Token, grant.Patterns, grant.Expiry.AsTime())
		s.logger.Info("session granted", "token", grant.Token[:8]+"...", "patterns", len(grant.Patterns))
	case *agentv1.ControlMessage_SessionRevoke:
		s.sessions.Revoke(m.SessionRevoke.Token)
		s.logger.Info("session revoked", "token", m.SessionRevoke.Token[:8]+"...")
	case *agentv1.ControlMessage_ServerHeartbeat:
		s.logger.Debug("received server heartbeat")
	default:
		s.logger.Warn("received unknown server message", "type", fmt.Sprintf("%T", m))
	}
}
