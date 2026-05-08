package agentstream

import (
	"errors"
	"sync"
	"testing"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

// mockStream implements agentv1.AgentService_ConnectServer
// (grpc.BidiStreamingServer[AgentMessage, ControlMessage]).
type mockStream struct {
	grpc.ServerStream
	sent []*agentv1.ControlMessage
	mu   sync.Mutex
	err  error // if set, Send returns this error
}

func (m *mockStream) Send(msg *agentv1.ControlMessage) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	return nil
}

func (m *mockStream) Recv() (*agentv1.AgentMessage, error) {
	return nil, nil
}

func (m *mockStream) sentMessages() []*agentv1.ControlMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*agentv1.ControlMessage, len(m.sent))
	copy(out, m.sent)
	return out
}

func TestRegisterAndUnregister(t *testing.T) {
	mgr := NewConnectionManager()

	agentID := uuid.New()
	gatewayID := uuid.New()
	stream := &mockStream{}

	conn := &AgentConnection{
		AgentID:   agentID,
		GatewayID: gatewayID,
		Stream:    stream,
	}

	// Register the agent.
	mgr.Register(conn)

	// Verify agent is tracked by sending a message to its gateway.
	msg := &agentv1.ControlMessage{Id: "test-1"}
	sent := mgr.SendToGateway(gatewayID, msg)
	assert.Equal(t, 1, sent, "expected 1 agent to receive message after register")

	// Unregister the agent.
	mgr.Unregister(agentID)

	// Verify agent is removed.
	sent = mgr.SendToGateway(gatewayID, msg)
	assert.Equal(t, 0, sent, "expected 0 agents after unregister")
}

func TestSendToGateway(t *testing.T) {
	mgr := NewConnectionManager()

	gw1 := uuid.New()
	gw2 := uuid.New()

	stream1 := &mockStream{}
	stream2 := &mockStream{}
	stream3 := &mockStream{}

	// Two agents on gw1, one on gw2.
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: gw1, Stream: stream1})
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: gw1, Stream: stream2})
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: gw2, Stream: stream3})

	msg := &agentv1.ControlMessage{Id: "gw-msg"}

	// Send to gw1 only.
	sent := mgr.SendToGateway(gw1, msg)
	assert.Equal(t, 2, sent, "expected 2 agents on gw1")

	// stream3 (gw2) should have received nothing.
	assert.Empty(t, stream3.sentMessages(), "gw2 stream should not receive gw1 messages")

	// Each gw1 stream should have received exactly 1 message.
	assert.Len(t, stream1.sentMessages(), 1)
	assert.Len(t, stream2.sentMessages(), 1)
}

func TestSendToGateway_ErrorStream(t *testing.T) {
	mgr := NewConnectionManager()

	gw := uuid.New()

	healthy := &mockStream{}
	broken := &mockStream{err: errors.New("stream broken")}

	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: gw, Stream: healthy})
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: gw, Stream: broken})

	msg := &agentv1.ControlMessage{Id: "err-msg"}
	sent := mgr.SendToGateway(gw, msg)

	// Only the healthy stream counts as sent.
	assert.Equal(t, 1, sent, "broken stream should not count as sent")
	assert.Len(t, healthy.sentMessages(), 1)
}

// TestSendToOrg verifies that SendToOrg routes a ControlMessage only
// to agents whose AgentConnection.OrgID matches, isolating cross-org
// session grants. Load-bearing for #27 phase 3 — replaces the
// previously-broadcast cross-org SessionGrant in CreateStorageSession.
func TestSendToOrg(t *testing.T) {
	mgr := NewConnectionManager()

	orgA := uuid.New()
	orgB := uuid.New()

	// Two agents in orgA (across two gateways), one agent in orgB.
	streamA1 := &mockStream{}
	streamA2 := &mockStream{}
	streamB := &mockStream{}

	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: uuid.New(), OrgID: orgA, Stream: streamA1})
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: uuid.New(), OrgID: orgA, Stream: streamA2})
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: uuid.New(), OrgID: orgB, Stream: streamB})

	msg := &agentv1.ControlMessage{Id: "org-a-grant"}
	sent := mgr.SendToOrg(orgA, msg)
	assert.Equal(t, 2, sent, "exactly the two orgA agents must have received the message")

	assert.Len(t, streamA1.sentMessages(), 1)
	assert.Len(t, streamA2.sentMessages(), 1)
	assert.Empty(t, streamB.sentMessages(),
		"orgB agent must NOT receive an orgA-scoped message — cross-org leakage closed")
}

func TestSendToOrg_NoAgentsForOrg(t *testing.T) {
	mgr := NewConnectionManager()
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: uuid.New(), OrgID: uuid.New(), Stream: &mockStream{}})

	sent := mgr.SendToOrg(uuid.New(), &agentv1.ControlMessage{Id: "nobody-in-this-org"})
	assert.Equal(t, 0, sent, "no agents in target org → 0 sent (not an error)")
}

func TestSendToOrg_ErrorStream(t *testing.T) {
	mgr := NewConnectionManager()
	org := uuid.New()

	healthy := &mockStream{}
	broken := &mockStream{err: errors.New("stream broken")}

	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: uuid.New(), OrgID: org, Stream: healthy})
	mgr.Register(&AgentConnection{AgentID: uuid.New(), GatewayID: uuid.New(), OrgID: org, Stream: broken})

	sent := mgr.SendToOrg(org, &agentv1.ControlMessage{Id: "err-msg"})
	assert.Equal(t, 1, sent, "broken stream should not count as sent")
	assert.Len(t, healthy.sentMessages(), 1)
}

func TestConcurrentAccess(t *testing.T) {
	mgr := NewConnectionManager()

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	gw := uuid.New()

	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for i := range opsPerGoroutine {
				agentID := uuid.New()
				stream := &mockStream{}
				conn := &AgentConnection{
					AgentID:   agentID,
					GatewayID: gw,
					Stream:    stream,
				}

				mgr.Register(conn)

				// Interleave sends.
				if i%3 == 0 {
					mgr.SendToGateway(gw, &agentv1.ControlMessage{Id: "concurrent"})
				}
				if i%5 == 0 {
					mgr.SendToOrg(uuid.New(), &agentv1.ControlMessage{Id: "org-scoped"})
				}

				// Unregister half to exercise deletion under contention.
				if i%2 == 0 {
					mgr.Unregister(agentID)
				}
			}
		}(g)
	}

	wg.Wait()

	// If we get here without a race detector complaint, the test passes.
	// Verify the manager is in a consistent state: SendToGateway should
	// not panic against the post-test state.
	sent := mgr.SendToGateway(gw, &agentv1.ControlMessage{Id: "final"})
	assert.GreaterOrEqual(t, sent, 0, "sent count should be non-negative")
}
