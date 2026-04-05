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

func (s *mockConnectStream) Context() context.Context      { return s.ctx }
func (s *mockConnectStream) SetHeader(metadata.MD) error   { return nil }
func (s *mockConnectStream) SendHeader(metadata.MD) error  { return nil }
func (s *mockConnectStream) SetTrailer(metadata.MD)        {}
func (s *mockConnectStream) SendMsg(any) error             { return nil }
func (s *mockConnectStream) RecvMsg(any) error             { return nil }

// ---------------------------------------------------------------------------
// Connect — InvalidFirstMessage
// ---------------------------------------------------------------------------

func TestConnect_InvalidFirstMessage(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	stream := &mockConnectStream{
		ctx: context.Background(),
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
// Connect — InvalidToken
// ---------------------------------------------------------------------------

func TestConnect_InvalidToken(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := NewAgentServiceServer(mockQ, slog.Default(), conns)

	mockQ.On("GetStorageGatewayByToken", mock.Anything, "bad-token").
		Return(db.StorageGateway{}, pgx.ErrNoRows)

	stream := &mockConnectStream{
		ctx: context.Background(),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-1",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						RegistrationToken: "bad-token",
						IpAddress:         "10.0.0.1",
					},
				},
			},
		},
	}

	err := srv.Connect(stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	mockQ.AssertExpectations(t)
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
	mockQ.On("GetStorageGatewayByToken", mock.Anything, "valid-token").Return(gateway, nil)
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
		ctx: context.Background(),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						RegistrationToken: "valid-token",
						IpAddress:         "10.0.0.5",
						Hostname:          "agent-host",
						AgentVersion:      "1.0.0",
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

	mockQ.On("GetStorageGatewayByToken", mock.Anything, "new-token").Return(gateway, nil)
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
		ctx: context.Background(),
		recvQueue: []*agentv1.AgentMessage{
			{
				Id: "msg-hs",
				Message: &agentv1.AgentMessage_Handshake{
					Handshake: &agentv1.Handshake{
						RegistrationToken: "new-token",
						IpAddress:         "10.0.0.10",
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
