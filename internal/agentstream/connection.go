package agentstream

import (
	"context"
	"log/slog"
	"sync"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/dashkan/pivox/internal/telemetry/streamtrace"
	"github.com/google/uuid"
)

// AgentConnection represents a connected agent's bidi stream.
//
// OrgID is the organization that owns the gateway this agent serves.
// Populated at registration time (the handshake handler resolves the
// gateway → org via the DB before calling Register), so SendToOrg can
// scope routing without a per-message DB lookup. Zero-value (uuid.Nil)
// is semantically "unscoped"; SendToOrg(uuid.Nil, ...) returns 0
// rather than matching unscoped registrations — registrations with no
// org are a programmer error and shouldn't accidentally receive
// org-scoped messages.
type AgentConnection struct {
	AgentID   uuid.UUID
	GatewayID uuid.UUID
	OrgID     uuid.UUID
	Stream    agentv1.AgentService_ConnectServer
}

// ConnectionManager tracks active agent bidi connections.
type ConnectionManager struct {
	mu    sync.RWMutex
	conns map[uuid.UUID]*AgentConnection // key: agent ID
}

// NewConnectionManager creates a new ConnectionManager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		conns: make(map[uuid.UUID]*AgentConnection),
	}
}

// Register adds a connected agent.
func (m *ConnectionManager) Register(conn *AgentConnection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conns[conn.AgentID] = conn
}

// Unregister removes a disconnected agent.
func (m *ConnectionManager) Unregister(agentID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.conns, agentID)
}

// SendToGateway sends a ControlMessage to all agents connected to the given gateway.
// Returns the number of agents the message was sent to. ctx carries the trace
// context stamped into the message (see SendToOrg).
func (m *ConnectionManager) SendToGateway(ctx context.Context, gatewayID uuid.UUID, msg *agentv1.ControlMessage) int {
	injectTraceContext(ctx, msg)

	m.mu.RLock()
	defer m.mu.RUnlock()

	sent := 0
	for _, conn := range m.conns {
		if conn.GatewayID == gatewayID {
			if err := conn.Stream.Send(msg); err == nil {
				sent++
			}
		}
	}
	return sent
}

// SendToOrg sends a ControlMessage to every agent whose
// AgentConnection.OrgID matches the given org. Returns the number
// of agents the message was successfully sent to.
//
// orgID == uuid.Nil is treated as a programmer error and logged at
// WARN — the call returns 0 without scanning any connections so a
// bug at the call site (e.g. a request resolver returning Nil
// instead of the resolved org ID) doesn't accidentally match
// connections registered with a zero OrgID. Silent-zero would hide
// the bug; loud-WARN makes it observable.
//
// Lock-then-Send shape: matching connections are snapshotted under
// RLock and Sent OUTSIDE the lock. This matters because SendToOrg
// runs in the synchronous CreateStorageSession request path
// (#27 phase 3) — a wedged agent stream could otherwise stall a
// concurrent Register/Unregister behind the held RLock. The other
// helpers on this type (SendToGateway) keep the legacy hold-while-
// sending shape because they have no production callers in
// synchronous request paths today.
//
// Used by Cloud Controller's CreateStorageSession (#27 phase 3) to
// scope SessionGrant routing to the target organization, replacing
// the cross-org SendToAll broadcast that was the original gap
// motivating #27.
func (m *ConnectionManager) SendToOrg(ctx context.Context, orgID uuid.UUID, msg *agentv1.ControlMessage) int {
	if orgID == uuid.Nil {
		slog.Warn("agentstream: SendToOrg called with uuid.Nil orgID; skipping (programmer error)")
		return 0
	}

	injectTraceContext(ctx, msg)

	// Snapshot under RLock, send outside.
	m.mu.RLock()
	streams := make([]agentv1.AgentService_ConnectServer, 0)
	for _, conn := range m.conns {
		if conn.OrgID == orgID {
			streams = append(streams, conn.Stream)
		}
	}
	m.mu.RUnlock()

	sent := 0
	for _, s := range streams {
		if err := s.Send(msg); err == nil {
			sent++
		}
	}
	return sent
}

// injectTraceContext stamps the active trace context from ctx into the
// control message once, before fan-out, so every recipient agent's handler
// continues this trace. Set once (read-only during the subsequent sends) — no
// per-recipient mutation, so fanning one message to N streams is race-free.
// No-op when there's no active span (tracing disabled). Centralizing it here
// means every send path is traced; callers can't forget to inject.
func injectTraceContext(ctx context.Context, msg *agentv1.ControlMessage) {
	if tc := streamtrace.Inject(ctx); tc != nil {
		msg.TraceContext = tc
	}
}
