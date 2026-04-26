package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/agentstream"
	db "github.com/dashkan/pivox/internal/db/generated"
	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ---------------------------------------------------------------------------
// parseEndpointConfig
// ---------------------------------------------------------------------------

func TestParseEndpointConfig_S3(t *testing.T) {
	ep := db.StorageEndpoint{
		Name:           "ep-s3",
		Configuration:  json.RawMessage(`{"type":"s3","endpoint_uri":"https://s3.us-east-1.amazonaws.com","bucket":"my-bucket","region":"us-east-1","access_key":{"access_key_id":"AKIA...","secret_access_key":"secret"}}`),
		CacheEnabled:   true,
		CacheMaxSizeGb: 100,
		CacheEviction:  db.EvictionPolicyLRU,
		CacheTtlHours:  48,
	}

	cfg, err := parseEndpointConfig(ep)
	require.NoError(t, err)
	assert.Equal(t, "ep-s3", cfg.GetName())

	s3 := cfg.GetS3()
	require.NotNil(t, s3)
	assert.Equal(t, "https://s3.us-east-1.amazonaws.com", s3.GetEndpointUri())
	assert.Equal(t, "my-bucket", s3.GetBucket())
	assert.Equal(t, "us-east-1", s3.GetRegion())
	assert.Equal(t, "AKIA...", s3.GetAccessKeyId())
	assert.Equal(t, "secret", s3.GetSecretAccessKey())

	assert.True(t, cfg.GetCacheConfig().GetEnabled())
	assert.Equal(t, int32(100), cfg.GetCacheConfig().GetMaxSizeGb())
	assert.Equal(t, "LRU", cfg.GetCacheConfig().GetEvictionPolicy())
	assert.Equal(t, int32(48), cfg.GetCacheConfig().GetTtlHours())
}

func TestParseEndpointConfig_S3_NoAccessKey(t *testing.T) {
	ep := db.StorageEndpoint{
		Name:          "ep-s3-no-key",
		Configuration: json.RawMessage(`{"type":"s3","endpoint_uri":"https://s3.amazonaws.com","bucket":"b","region":"us-west-2"}`),
		CacheEviction: db.EvictionPolicyLRU,
	}

	cfg, err := parseEndpointConfig(ep)
	require.NoError(t, err)

	s3 := cfg.GetS3()
	require.NotNil(t, s3)
	assert.Empty(t, s3.GetAccessKeyId())
	assert.Empty(t, s3.GetSecretAccessKey())
}

func TestParseEndpointConfig_Filesystem(t *testing.T) {
	ep := db.StorageEndpoint{
		Name:          "ep-fs",
		Configuration: json.RawMessage(`{"type":"filesystem","path":"/mnt/storage"}`),
		CacheEviction: db.EvictionPolicyLRU,
	}

	cfg, err := parseEndpointConfig(ep)
	require.NoError(t, err)
	assert.Equal(t, "ep-fs", cfg.GetName())

	fs := cfg.GetFilesystem()
	require.NotNil(t, fs)
	assert.Equal(t, "/mnt/storage", fs.GetPath())
}

func TestParseEndpointConfig_UnknownType(t *testing.T) {
	ep := db.StorageEndpoint{
		Name:          "ep-unknown",
		Configuration: json.RawMessage(`{"type":"azure_blob","container":"c"}`),
		CacheEviction: db.EvictionPolicyLRU,
	}

	_, err := parseEndpointConfig(ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown endpoint type")
}

func TestParseEndpointConfig_InvalidJSON(t *testing.T) {
	ep := db.StorageEndpoint{
		Name:          "ep-bad",
		Configuration: json.RawMessage(`not valid json`),
		CacheEviction: db.EvictionPolicyLRU,
	}

	_, err := parseEndpointConfig(ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal configuration")
}

// ---------------------------------------------------------------------------
// buildEndpointConfigs
// ---------------------------------------------------------------------------

func TestBuildEndpointConfigs_Success(t *testing.T) {
	endpoints := []db.StorageEndpoint{
		{
			Name:          "ep-1",
			Configuration: json.RawMessage(`{"type":"filesystem","path":"/a"}`),
			CacheEviction: db.EvictionPolicyLRU,
		},
		{
			Name:          "ep-2",
			Configuration: json.RawMessage(`{"type":"s3","endpoint_uri":"https://s3.amazonaws.com","bucket":"b","region":"r"}`),
			CacheEviction: db.EvictionPolicyLRU,
		},
	}

	configs, err := buildEndpointConfigs(endpoints)
	require.NoError(t, err)
	assert.Len(t, configs, 2)
	assert.Equal(t, "ep-1", configs[0].GetName())
	assert.Equal(t, "ep-2", configs[1].GetName())
}

func TestBuildEndpointConfigs_Empty(t *testing.T) {
	configs, err := buildEndpointConfigs(nil)
	require.NoError(t, err)
	assert.Empty(t, configs)
}

func TestBuildEndpointConfigs_Error(t *testing.T) {
	endpoints := []db.StorageEndpoint{
		{
			Name:          "ep-bad",
			Configuration: json.RawMessage(`{"type":"unknown"}`),
			CacheEviction: db.EvictionPolicyLRU,
		},
	}

	_, err := buildEndpointConfigs(endpoints)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ep-bad")
}

// ---------------------------------------------------------------------------
// endpointConfigJSON helpers
// ---------------------------------------------------------------------------

func TestEndpointConfigJSON_AccessKeyMethods(t *testing.T) {
	t.Run("with_access_key", func(t *testing.T) {
		c := endpointConfigJSON{
			AccessKey: &s3AccessKeyJSON{
				AccessKeyID:     "AK123",
				SecretAccessKey: "secret456",
			},
		}
		assert.Equal(t, "AK123", c.accessKeyID())
		assert.Equal(t, "secret456", c.secretAccessKey())
	})

	t.Run("nil_access_key", func(t *testing.T) {
		c := endpointConfigJSON{}
		assert.Empty(t, c.accessKeyID())
		assert.Empty(t, c.secretAccessKey())
	})
}

// ---------------------------------------------------------------------------
// NewAgentServiceServer
// ---------------------------------------------------------------------------

func TestNewAgentServiceServer(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	logger := slog.Default()
	conns := agentstream.NewConnectionManager()

	srv := NewAgentServiceServer(mockQ, logger, conns)
	require.NotNil(t, srv)
	assert.Equal(t, mockQ, srv.queries)
	assert.Equal(t, logger, srv.logger)
	assert.Equal(t, conns, srv.conns)
}

// ---------------------------------------------------------------------------
// mockConnectStream implements agentv1.AgentService_ConnectServer for testing.
// ---------------------------------------------------------------------------

type mockConnectStream struct {
	ctx       context.Context
	recvQueue []*agentv1.AgentMessage
	recvIdx   int
	sent      []*agentv1.ControlMessage
}

func (s *mockConnectStream) Send(msg *agentv1.ControlMessage) error {
	s.sent = append(s.sent, msg)
	return nil
}

func (s *mockConnectStream) Recv() (*agentv1.AgentMessage, error) {
	if s.recvIdx >= len(s.recvQueue) {
		return nil, io.EOF
	}
	msg := s.recvQueue[s.recvIdx]
	s.recvIdx++
	return msg, nil
}

func (s *mockConnectStream) Context() context.Context     { return s.ctx }
func (s *mockConnectStream) SetHeader(metadata.MD) error  { return nil }
func (s *mockConnectStream) SendHeader(metadata.MD) error { return nil }
func (s *mockConnectStream) SetTrailer(metadata.MD)       {}
func (s *mockConnectStream) SendMsg(any) error            { return nil }
func (s *mockConnectStream) RecvMsg(any) error            { return nil }

// ---------------------------------------------------------------------------
// Connect — InvalidFirstMessage
// ---------------------------------------------------------------------------

func TestConnect_InvalidFirstMessage(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gateway := db.StorageGateway{ID: uuid.New(), Name: "gw-bad-first"}

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-1",
				Message: &agentv1.AgentMessage_Heartbeat{
					Heartbeat: &agentv1.Heartbeat{},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "first message must be handshake")
}

// ---------------------------------------------------------------------------
// Connect — Handshake + Heartbeat + Disconnect
// ---------------------------------------------------------------------------

func TestConnect_HandshakeAndHeartbeat(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentID2 := uuid.New()

	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-test",
		State: db.StorageGatewayStatePROVISIONING,
	}

	agent := db.StorageAgent{
		ID:        agentID2,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.5",
		State:     db.AgentStateCONNECTED,
	}

	// Handshake flow
	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateACTIVE,
	}).Return(nil)

	// Heartbeat
	mockQ.On("UpdateStorageAgentHeartbeat", mock.Anything, agentID2).Return(nil)

	// Disconnect
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentID2,
		State: db.AgentStateDISCONNECTED,
	}).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress:    "10.0.0.5",
						Hostname:     "agent-host",
						AgentVersion: "1.0.0",
					},
				},
			},
			{
				Id: "msg-hb",
				Message: &agentv1.AgentMessage_Heartbeat{
					Heartbeat: &agentv1.Heartbeat{},
				},
			},
			// EOF ends the stream
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)

	// Verify HandshakeAck was sent.
	require.Len(t, stream.sent, 1)
	ack := stream.sent[0].GetHandshakeAck()
	require.NotNil(t, ack)
	assert.Contains(t, ack.GetAgentName(), "gw-test")

	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — GatewayActivation (PROVISIONING → ACTIVE)
// ---------------------------------------------------------------------------

func TestConnect_GatewayActivation(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentID3 := uuid.New()

	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-new",
		State: db.StorageGatewayStatePROVISIONING,
	}

	agent := db.StorageAgent{
		ID:        agentID3,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.10",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateACTIVE,
	}).Return(nil)

	// Disconnect flow
	mockQ.On("UpdateStorageAgentState", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.10",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)

	// Verify gateway state was updated to ACTIVE.
	mockQ.AssertCalled(t, "UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateACTIVE,
	})
}

// ---------------------------------------------------------------------------
// auditMessage
// ---------------------------------------------------------------------------

func TestAuditMessage_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentID4 := uuid.New()

	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.MatchedBy(func(p db.CreateStorageAgentAuditParams) bool {
		return p.GatewayID == gatewayID && p.Direction == "inbound" && p.MessageType == "heartbeat"
	})).Return(nil)

	msg := &agentv1.AgentMessage{
		Id:      "hb-1",
		Message: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{}},
	}

	// Should not panic or error.
	srv.auditMessage(context.Background(), gatewayID, agentID4, "hb-1", "inbound", "heartbeat", msg)

	mockQ.AssertExpectations(t)
}

func TestAuditMessage_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).
		Return(errors.New("db error"))

	msg := &agentv1.AgentMessage{
		Id:      "hb-2",
		Message: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{}},
	}

	// Should log but not panic.
	srv.auditMessage(context.Background(), uuid.New(), uuid.New(), "hb-2", "inbound", "heartbeat", msg)

	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — handler invoked without an authenticated gateway in context
// ---------------------------------------------------------------------------

// AgentAuthStreamInterceptor is the only path that puts a gateway in context.
// If the handler is somehow reached without one (interceptor misconfigured /
// removed from the chain), it must fail closed rather than panic or leak.
// Token validation errors at the interceptor layer are covered separately
// by TestAgentAuthInterceptor_DBError in the server package.
func TestConnect_NoAuthenticatedGateway(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	stream := &mockConnectStream{
		ctx: context.Background(),
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// ---------------------------------------------------------------------------
// Connect — agent lookup DB error (non-ErrNoRows)
// ---------------------------------------------------------------------------

func TestConnect_AgentLookupDBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-test",
		State: db.StorageGatewayStateACTIVE,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).
		Return(db.StorageAgent{}, errors.New("db connection error"))

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-1",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.5",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — create agent DB error
// ---------------------------------------------------------------------------

func TestConnect_CreateAgentDBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-test",
		State: db.StorageGatewayStateACTIVE,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).
		Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).
		Return(db.StorageAgent{}, errors.New("db error"))

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-1",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.5",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — reconnecting agent state update error
// ---------------------------------------------------------------------------

func TestConnect_ReconnectingAgentStateUpdateError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	existingAgentID := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-test",
		State: db.StorageGatewayStateACTIVE,
	}
	existingAgent := db.StorageAgent{
		ID:        existingAgentID,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.5",
		State:     db.AgentStateDISCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(existingAgent, nil)
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    existingAgentID,
		State: db.AgentStateCONNECTED,
	}).Return(db.StorageAgent{}, errors.New("db error"))

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-1",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.5",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — list endpoints error
// ---------------------------------------------------------------------------

func TestConnect_ListEndpointsError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDx := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-test",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDx,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.5",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, errors.New("db error"))

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-1",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.5",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — reconnecting agent full flow
// ---------------------------------------------------------------------------

func TestConnect_ReconnectingAgent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	existingAgentID := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-reconnect",
		State: db.StorageGatewayStateACTIVE, // Already active, no state change on connect
	}
	existingAgent := db.StorageAgent{
		ID:        existingAgentID,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.7",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(existingAgent, nil)
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    existingAgentID,
		State: db.AgentStateCONNECTED,
	}).Return(existingAgent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// Disconnect flow
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    existingAgentID,
		State: db.AgentStateDISCONNECTED,
	}).Return(existingAgent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(1), nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.7",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — receive loop with various message types
// ---------------------------------------------------------------------------

func TestConnect_ReceiveLoop_MessageTypes(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDy := uuid.New()

	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-msg",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDy,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.8",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// Message handling: heartbeat (updates heartbeat), endpoint_health, telemetry, upgrade_status, sync_status
	mockQ.On("UpdateStorageAgentHeartbeat", mock.Anything, agentIDy).Return(nil)

	// Disconnect
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentIDy,
		State: db.AgentStateDISCONNECTED,
	}).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.8",
					},
				},
			},
			{
				Id:      "msg-hb",
				Message: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{}},
			},
			{
				Id: "msg-ep",
				Message: &agentv1.AgentMessage_EndpointHealth{
					EndpointHealth: &agentv1.EndpointHealth{
						EndpointName: "ep-1",
						Reachable:    true,
						LatencyMs:    10,
					},
				},
			},
			{
				Id: "msg-tel",
				Message: &agentv1.AgentMessage_Telemetry{
					Telemetry: &agentv1.Telemetry{},
				},
			},
			{
				Id: "msg-upg",
				Message: &agentv1.AgentMessage_UpgradeStatus{
					UpgradeStatus: &agentv1.UpgradeStatus{
						Version: "2.0.0",
					},
				},
			},
			{
				Id: "msg-sync",
				Message: &agentv1.AgentMessage_SyncStatus{
					SyncStatus: &agentv1.SyncStatus{
						PendingWrites: 5,
						SyncedWrites:  100,
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// mockConnectStreamWithSendError — variant that fails on Send
// ---------------------------------------------------------------------------

type mockConnectStreamWithSendError struct {
	mockConnectStream
	sendErr error
}

func (s *mockConnectStreamWithSendError) Send(_ *agentv1.ControlMessage) error {
	return s.sendErr
}

// ---------------------------------------------------------------------------
// Connect — send handshake ack fails
// ---------------------------------------------------------------------------

func TestConnect_SendAckError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDz := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-send-err",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDz,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.9",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	stream := &mockConnectStreamWithSendError{
		mockConnectStream: mockConnectStream{
			ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
			recvQueue: []*agentv1.AgentMessage{
				{
					Id: "msg-hs",
					Message: &agentv1.AgentMessage_Handshake{
						Handshake: &agentv1.Handshake{
							IpAddress: "10.0.0.9",
						},
					},
				},
			},
		},
		sendErr: errors.New("send failed"),
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// mockConnectStreamWithRecvError — variant that emits a non-EOF recv error
// after the handshake
// ---------------------------------------------------------------------------

type mockConnectStreamWithRecvError struct {
	ctx     context.Context
	sent    []*agentv1.ControlMessage
	hs      *agentv1.AgentMessage
	recvIdx int
	recvErr error
}

func (s *mockConnectStreamWithRecvError) Send(msg *agentv1.ControlMessage) error {
	s.sent = append(s.sent, msg)
	return nil
}

func (s *mockConnectStreamWithRecvError) Recv() (*agentv1.AgentMessage, error) {
	if s.recvIdx == 0 {
		s.recvIdx++
		return s.hs, nil
	}
	return nil, s.recvErr
}

func (s *mockConnectStreamWithRecvError) Context() context.Context     { return s.ctx }
func (s *mockConnectStreamWithRecvError) SetHeader(metadata.MD) error  { return nil }
func (s *mockConnectStreamWithRecvError) SendHeader(metadata.MD) error { return nil }
func (s *mockConnectStreamWithRecvError) SetTrailer(metadata.MD)       {}
func (s *mockConnectStreamWithRecvError) SendMsg(any) error            { return nil }
func (s *mockConnectStreamWithRecvError) RecvMsg(any) error            { return nil }

// ---------------------------------------------------------------------------
// Connect — non-EOF receive error in loop
// ---------------------------------------------------------------------------

func TestConnect_RecvLoopError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDr := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-recv-err",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDr,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.10",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// Disconnect flow
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentIDr,
		State: db.AgentStateDISCONNECTED,
	}).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(nil)

	stream := &mockConnectStreamWithRecvError{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		hs: &agentv1.AgentMessage{
			Id: "msg-hs",
			Message: &agentv1.AgentMessage_Handshake{
				Handshake: &agentv1.Handshake{
					IpAddress: "10.0.0.10",
				},
			},
		},
		recvErr: errors.New("connection reset"),
	}

	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — heartbeat update error (logged, not fatal)
// ---------------------------------------------------------------------------

func TestConnect_HeartbeatUpdateError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDhb := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-hb-err",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDhb,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.11",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// Heartbeat update fails — logged, not fatal
	mockQ.On("UpdateStorageAgentHeartbeat", mock.Anything, agentIDhb).Return(errors.New("db error"))

	// Disconnect flow
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentIDhb,
		State: db.AgentStateDISCONNECTED,
	}).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.11",
					},
				},
			},
			{
				Id:      "msg-hb",
				Message: &agentv1.AgentMessage_Heartbeat{Heartbeat: &agentv1.Heartbeat{}},
			},
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — disconnect state update errors (all logged, not fatal)
// ---------------------------------------------------------------------------

func TestConnect_DisconnectErrors(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDdc := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-dc-err",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDdc,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.12",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// State update to DISCONNECTED fails — logged only
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentIDdc,
		State: db.AgentStateDISCONNECTED,
	}).Return(db.StorageAgent{}, errors.New("db error"))
	// Count also fails — logged only
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), errors.New("db error"))

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.12",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — gateway state OFFLINE update error (logged, not fatal)
// ---------------------------------------------------------------------------

func TestConnect_GatewayOfflineUpdateError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDoc := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-offline-err",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDoc,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.13",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// Disconnect: state update succeeds, count = 0, but offline state update fails
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentIDoc,
		State: db.AgentStateDISCONNECTED,
	}).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(errors.New("db error"))

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.13",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — buildEndpointConfigs error (invalid endpoint config in DB)
// ---------------------------------------------------------------------------

func TestConnect_BuildEndpointConfigsError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDcfg := uuid.New()
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-cfg-err",
		State: db.StorageGatewayStateACTIVE,
	}
	agent := db.StorageAgent{
		ID:        agentIDcfg,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.14",
		State:     db.AgentStateCONNECTED,
	}

	// Endpoint with invalid JSON config in DB — buildEndpointConfigs will fail
	badEndpoint := db.StorageEndpoint{
		Name:          "ep-bad",
		Configuration: json.RawMessage(`{"type":"unknown_type"}`),
		CacheEviction: db.EvictionPolicyLRU,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{badEndpoint}, nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.14",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connect — initial Recv error (before handshake)
// ---------------------------------------------------------------------------

type mockConnectStreamImmediateErr struct {
	ctx     context.Context
	recvErr error
	sent    []*agentv1.ControlMessage
}

func (s *mockConnectStreamImmediateErr) Send(msg *agentv1.ControlMessage) error {
	s.sent = append(s.sent, msg)
	return nil
}
func (s *mockConnectStreamImmediateErr) Recv() (*agentv1.AgentMessage, error) {
	return nil, s.recvErr
}
func (s *mockConnectStreamImmediateErr) Context() context.Context     { return s.ctx }
func (s *mockConnectStreamImmediateErr) SetHeader(metadata.MD) error  { return nil }
func (s *mockConnectStreamImmediateErr) SendHeader(metadata.MD) error { return nil }
func (s *mockConnectStreamImmediateErr) SetTrailer(metadata.MD)       {}
func (s *mockConnectStreamImmediateErr) SendMsg(any) error            { return nil }
func (s *mockConnectStreamImmediateErr) RecvMsg(any) error            { return nil }

func TestConnect_InitialRecvError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gateway := db.StorageGateway{ID: uuid.New(), Name: "gw-recv-init-err"}

	stream := &mockConnectStreamImmediateErr{
		ctx:     server.WithAuthenticatedGateway(context.Background(), gateway),
		recvErr: errors.New("transport error"),
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "failed to receive first message")
}

// ---------------------------------------------------------------------------
// Connect — PROVISIONING→ACTIVE state update error (logged, not fatal)
// ---------------------------------------------------------------------------

func TestConnect_GatewayActivationError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	gatewayID := uuid.New()
	agentIDact := uuid.New()
	// Gateway starts in PROVISIONING state
	gateway := db.StorageGateway{
		ID:    gatewayID,
		Name:  "gw-act-err",
		State: db.StorageGatewayStatePROVISIONING,
	}
	agent := db.StorageAgent{
		ID:        agentIDact,
		GatewayID: gatewayID,
		IpAddress: "10.0.0.15",
		State:     db.AgentStateCONNECTED,
	}

	mockQ.On("GetStorageAgentByGatewayAndIP", mock.Anything, mock.Anything).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", mock.Anything, mock.Anything).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", mock.Anything, mock.Anything).Return(nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gatewayID).Return([]db.StorageEndpoint{}, nil)

	// ACTIVE state update fails — logged only
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateACTIVE,
	}).Return(errors.New("db error"))

	// Disconnect flow
	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentIDact,
		State: db.AgentStateDISCONNECTED,
	}).Return(agent, nil)
	mockQ.On("CountConnectedStorageAgentsByGateway", mock.Anything, gatewayID).Return(int64(0), nil)
	mockQ.On("UpdateStorageGatewayState", mock.Anything, db.UpdateStorageGatewayStateParams{
		ID:    gatewayID,
		State: db.StorageGatewayStateOFFLINE,
	}).Return(nil)

	stream := &mockConnectStream{
		ctx: server.WithAuthenticatedGateway(context.Background(), gateway),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						IpAddress: "10.0.0.15",
					},
				},
			},
		},
	}

	// Should not error — activation failure is logged only
	err := srv.Connect(stream)
	require.NoError(t, err)
	mockQ.AssertExpectations(t)
}
