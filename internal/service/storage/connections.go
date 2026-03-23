package storage

import (
	"sync"

	agentv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/agent/v1"
	"github.com/google/uuid"
)

// AgentConnection represents a connected agent's bidi stream.
type AgentConnection struct {
	AgentID   uuid.UUID
	GatewayID uuid.UUID
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
// Returns the number of agents the message was sent to.
func (m *ConnectionManager) SendToGateway(gatewayID uuid.UUID, msg *agentv1.ControlMessage) int {
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

// SendToAll sends a ControlMessage to all connected agents.
func (m *ConnectionManager) SendToAll(msg *agentv1.ControlMessage) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sent := 0
	for _, conn := range m.conns {
		if err := conn.Stream.Send(msg); err == nil {
			sent++
		}
	}
	return sent
}
