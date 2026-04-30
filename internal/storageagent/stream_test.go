package storageagent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockBidiStream implements grpc.BidiStreamingClient[AgentMessage, ControlMessage]
// for testing the Stream type.
type mockBidiStream struct {
	mu       sync.Mutex
	sent     []*agentv1.AgentMessage
	recvCh   chan *agentv1.ControlMessage
	recvErr  error
	sendErr  error
	closed   bool
	closedCh chan struct{}
}

func newMockBidiStream() *mockBidiStream {
	return &mockBidiStream{
		recvCh:   make(chan *agentv1.ControlMessage, 100),
		closedCh: make(chan struct{}),
	}
}

func (m *mockBidiStream) Send(msg *agentv1.AgentMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockBidiStream) Recv() (*agentv1.ControlMessage, error) {
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	msg, ok := <-m.recvCh
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (m *mockBidiStream) CloseSend() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	close(m.closedCh)
	return nil
}

// Header, Trailer, Context, RecvMsg, SendMsg satisfy the grpc.ClientStream interface.
func (m *mockBidiStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockBidiStream) Trailer() metadata.MD         { return nil }
func (m *mockBidiStream) Context() context.Context     { return context.Background() }
func (m *mockBidiStream) RecvMsg(any) error            { return nil }
func (m *mockBidiStream) SendMsg(any) error            { return nil }

func (m *mockBidiStream) sentMessages() []*agentv1.AgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*agentv1.AgentMessage, len(m.sent))
	copy(out, m.sent)
	return out
}

func newTestStream(bidi *mockBidiStream) *Stream {
	return NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   2 * time.Second,
		Sessions:  NewSessionStore(),
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    NewDeniedPatterns(),
		Logger:    slog.Default(),
	})
}

// ---------------------------------------------------------------------------
// SendHeartbeat
// ---------------------------------------------------------------------------

func TestSendHeartbeat_Success(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)
	ctx := context.Background()

	hb := &agentv1.Heartbeat{State: "ready"}
	err := s.SendHeartbeat(ctx, hb)
	require.NoError(t, err)

	msgs := bidi.sentMessages()
	require.Len(t, msgs, 1)
	assert.Empty(t, msgs[0].Id, "fire-and-forget should not set an id")
	assert.Equal(t, "ready", msgs[0].GetHeartbeat().GetState())
}

func TestSendHeartbeat_StreamError(t *testing.T) {
	bidi := newMockBidiStream()
	bidi.sendErr = fmt.Errorf("connection lost")
	s := newTestStream(bidi)

	err := s.SendHeartbeat(context.Background(), &agentv1.Heartbeat{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send")
}

// ---------------------------------------------------------------------------
// SendTelemetry
// ---------------------------------------------------------------------------

func TestSendTelemetry_Success(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	err := s.SendTelemetry(context.Background(), &agentv1.Telemetry{})
	require.NoError(t, err)

	msgs := bidi.sentMessages()
	require.Len(t, msgs, 1)
	assert.NotNil(t, msgs[0].GetTelemetry())
}

// ---------------------------------------------------------------------------
// SendEndpointHealth
// ---------------------------------------------------------------------------

func TestSendEndpointHealth_Success(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	err := s.SendEndpointHealth(context.Background(), &agentv1.EndpointHealth{})
	require.NoError(t, err)

	msgs := bidi.sentMessages()
	require.Len(t, msgs, 1)
	assert.NotNil(t, msgs[0].GetEndpointHealth())
}

// ---------------------------------------------------------------------------
// SendUpgradeStatus
// ---------------------------------------------------------------------------

func TestSendUpgradeStatus_Success(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	err := s.SendUpgradeStatus(context.Background(), &agentv1.UpgradeStatus{})
	require.NoError(t, err)

	msgs := bidi.sentMessages()
	require.Len(t, msgs, 1)
	assert.NotNil(t, msgs[0].GetUpgradeStatus())
}

// ---------------------------------------------------------------------------
// Handshake (roundTrip)
// ---------------------------------------------------------------------------

func TestHandshake_Success(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)
	ctx := context.Background()

	// Start ReceiveLoop in background.
	go func() {
		_ = s.ReceiveLoop(ctx)
	}()

	// Simulate server responding to the handshake in a goroutine.
	go func() {
		// Wait for the handshake message to be sent.
		for {
			msgs := bidi.sentMessages()
			if len(msgs) > 0 && msgs[0].Id != "" {
				bidi.recvCh <- &agentv1.ControlMessage{
					Id: msgs[0].Id,
					Message: &agentv1.ControlMessage_HandshakeAck{
						HandshakeAck: &agentv1.HandshakeAck{
							AgentName: "agents/test-agent",
						},
					},
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	ack, err := s.Handshake(ctx, &agentv1.Handshake{
		AgentVersion: "dev",
	})

	require.NoError(t, err)
	assert.Equal(t, "agents/test-agent", ack.GetAgentName())

	// Verify the handshake message had a correlation ID.
	msgs := bidi.sentMessages()
	require.NotEmpty(t, msgs)
	assert.NotEmpty(t, msgs[0].Id)
}

func TestHandshake_Timeout(t *testing.T) {
	bidi := newMockBidiStream()
	// Use a very short timeout to trigger expiry.
	s := NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   50 * time.Millisecond,
		Sessions:  NewSessionStore(),
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    NewDeniedPatterns(),
		Logger:    slog.Default(),
	})

	// Start ReceiveLoop but never send a response.
	go func() {
		_ = s.ReceiveLoop(context.Background())
	}()

	_, err := s.Handshake(context.Background(), &agentv1.Handshake{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestHandshake_WrongResponseType(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)
	ctx := context.Background()

	go func() {
		_ = s.ReceiveLoop(ctx)
	}()

	// Respond with a non-HandshakeAck message.
	go func() {
		for {
			msgs := bidi.sentMessages()
			if len(msgs) > 0 && msgs[0].Id != "" {
				bidi.recvCh <- &agentv1.ControlMessage{
					Id: msgs[0].Id,
					Message: &agentv1.ControlMessage_ServerHeartbeat{
						ServerHeartbeat: &agentv1.ServerHeartbeat{},
					},
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, err := s.Handshake(ctx, &agentv1.Handshake{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected HandshakeAck")
}

func TestHandshake_SendError(t *testing.T) {
	bidi := newMockBidiStream()
	bidi.sendErr = fmt.Errorf("broken pipe")
	s := newTestStream(bidi)

	_, err := s.Handshake(context.Background(), &agentv1.Handshake{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send")
}

// ---------------------------------------------------------------------------
// ReceiveLoop
// ---------------------------------------------------------------------------

func TestReceiveLoop_StreamClosed(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Close the receive channel to simulate stream end.
	close(bidi.recvCh)

	err := s.ReceiveLoop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receive")
}

func TestReceiveLoop_ClosePendingChannels(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Manually add a pending channel.
	ch := make(chan *agentv1.ControlMessage, 1)
	s.mu.Lock()
	s.pending["test-id"] = ch
	s.mu.Unlock()

	// Close the stream.
	close(bidi.recvCh)
	_ = s.ReceiveLoop(context.Background())

	// The pending channel should have been closed.
	_, ok := <-ch
	assert.False(t, ok, "pending channel should be closed when stream ends")
}

// ---------------------------------------------------------------------------
// handleServerMessage
// ---------------------------------------------------------------------------

func TestHandleServerMessage_SessionGrant(t *testing.T) {
	bidi := newMockBidiStream()
	sessions := NewSessionStore()
	s := NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   time.Second,
		Sessions:  sessions,
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    NewDeniedPatterns(),
		Logger:    slog.Default(),
	})

	expiry := time.Now().Add(time.Hour)
	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_SessionGrant{
			SessionGrant: &agentv1.SessionGrant{
				Token:    "abcdefghijklmnop",
				Patterns: []string{"/media/*"},
				Expiry:   timestamppb.New(expiry),
			},
		},
	})

	assert.True(t, sessions.Authorize("abcdefghijklmnop", "/media/file.mp4"))
}

func TestHandleServerMessage_SessionRevoke(t *testing.T) {
	bidi := newMockBidiStream()
	sessions := NewSessionStore()
	sessions.Grant("revoke-me-token1", []string{"/data/*"}, time.Now().Add(time.Hour))
	s := NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   time.Second,
		Sessions:  sessions,
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    NewDeniedPatterns(),
		Logger:    slog.Default(),
	})

	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_SessionRevoke{
			SessionRevoke: &agentv1.SessionRevoke{
				Token: "revoke-me-token1",
			},
		},
	})

	assert.False(t, sessions.Authorize("revoke-me-token1", "/data/file.csv"))
}

func TestHandleServerMessage_ConfigUpdate_DeniedPatterns(t *testing.T) {
	bidi := newMockBidiStream()
	denied := NewDeniedPatterns()
	s := NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   time.Second,
		Sessions:  NewSessionStore(),
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    denied,
		Logger:    slog.Default(),
	})

	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_ConfigUpdate{
			ConfigUpdate: &agentv1.ConfigUpdate{
				DeniedPatterns: []string{"*.tmp", "secret.*"},
			},
		},
	})

	assert.True(t, denied.IsDenied("file.tmp"))
	assert.True(t, denied.IsDenied("secret.txt"))
	assert.False(t, denied.IsDenied("readme.md"))
}

func TestHandleServerMessage_ConfigUpdate_Endpoints(t *testing.T) {
	bidi := newMockBidiStream()
	endpoints := NewEndpointStore(NewMemoryCache(10, 1024))
	s := NewStream(StreamConfig{
		Stream:    bidi,
		Timeout:   time.Second,
		Sessions:  NewSessionStore(),
		Endpoints: endpoints,
		Denied:    NewDeniedPatterns(),
		Logger:    slog.Default(),
	})

	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_ConfigUpdate{
			ConfigUpdate: &agentv1.ConfigUpdate{
				Endpoints: []*agentv1.EndpointConfig{
					{
						Name: "organizations/acme/storageGateways/gw1/endpoints/media",
						Configuration: &agentv1.EndpointConfig_Filesystem{
							Filesystem: &agentv1.FileSystemEndpointConfig{
								Path: "/tmp/test-media",
							},
						},
					},
				},
			},
		},
	})

	endpoints.mu.RLock()
	_, exists := endpoints.endpoints["media"]
	endpoints.mu.RUnlock()
	assert.True(t, exists, "endpoint should be registered after config update")
}

func TestHandleServerMessage_UnknownType(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Should not panic on unknown message type.
	s.handleServerMessage(&agentv1.ControlMessage{})
}

func TestHandleServerMessage_DrainRequest(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Should not panic — just logs.
	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_DrainRequest{
			DrainRequest: &agentv1.DrainRequest{Reason: "maintenance"},
		},
	})
}

func TestHandleServerMessage_CertDelivery(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Should not panic — just logs.
	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_CertDelivery{
			CertDelivery: &agentv1.CertDelivery{},
		},
	})
}

func TestHandleServerMessage_UpgradeRequest(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Should not panic — just logs.
	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_UpgradeRequest{
			UpgradeRequest: &agentv1.UpgradeRequest{
				TargetVersion: "1.2.3",
			},
		},
	})
}

func TestHandleServerMessage_ServerHeartbeat(t *testing.T) {
	bidi := newMockBidiStream()
	s := newTestStream(bidi)

	// Should not panic — just logs.
	s.handleServerMessage(&agentv1.ControlMessage{
		Message: &agentv1.ControlMessage_ServerHeartbeat{
			ServerHeartbeat: &agentv1.ServerHeartbeat{},
		},
	})
}
