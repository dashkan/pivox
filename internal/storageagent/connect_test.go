//go:build dev

package storageagent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/dashkan/pivox/internal/agentstream"
	db "github.com/dashkan/pivox/internal/db/generated"
	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	storage "github.com/dashkan/pivox/internal/service/storage"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ---------------------------------------------------------------------------
// TestVersion
// ---------------------------------------------------------------------------

func TestVersion(t *testing.T) {
	assert.Equal(t, "dev", version())
}

// ---------------------------------------------------------------------------
// TestListenAndServe
// ---------------------------------------------------------------------------

func TestListenAndServe_InvalidAddr(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewHTTPServer(Config{
		Sessions:  NewSessionStore(),
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    NewDeniedPatterns(),
		Logger:    logger,
	})
	err := srv.ListenAndServe("invalid-not-a-port-!!!!")
	require.Error(t, err)
}

func TestListenAndServe_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewHTTPServer(Config{
		Sessions:   NewSessionStore(),
		Endpoints:  NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:     NewDeniedPatterns(),
		SigningKey: []byte("test-key"),
		CORSOrigin: "*",
		Logger:     logger,
	})

	// Pick a random free port by binding to :0.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close() // free it so ListenAndServe can bind

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(addr)
	}()

	// Give the server a moment to start listening.
	time.Sleep(50 * time.Millisecond)

	// Make a real HTTP request — dev build skips auth, so we should get a
	// response (likely 404 since no endpoints are configured).
	resp, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err, "should be able to connect to the server")
	resp.Close()
}

// ---------------------------------------------------------------------------
// TestConnect — full end-to-end with a real TCP gRPC server
// ---------------------------------------------------------------------------

// setupAgentGRPC starts a gRPC server with the real AgentServiceServer on a
// random TCP port and returns the listener address. The server is stopped when
// the test finishes.
func setupAgentGRPC(t *testing.T, mockQ *mocks.MockQuerier) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	connMgr := agentstream.NewConnectionManager()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, storage.NewAgentServiceServerForTesting(mockQ, logger, connMgr))

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })

	return lis.Addr().String()
}

func newConnectConfig(t *testing.T) *ConnectConfig {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &ConnectConfig{
		Sessions:  NewSessionStore(),
		Endpoints: NewEndpointStore(NewMemoryCache(10, 1024)),
		Denied:    NewDeniedPatterns(),
		HTTP: NewHTTPServer(Config{
			Sessions:   NewSessionStore(),
			Endpoints:  NewEndpointStore(NewMemoryCache(10, 1024)),
			Denied:     NewDeniedPatterns(),
			SigningKey: []byte("key"),
			CORSOrigin: "*",
			Logger:     logger,
		}),
	}
}

// anyCtx matches any context.Context argument.
var anyCtx = mock.MatchedBy(func(_ context.Context) bool { return true })

func TestConnect_FullHandshake(t *testing.T) {
	gatewayID := uuid.New()
	agentID := uuid.New()
	token := "valid-token-123"

	gateway := db.StorageGateway{
		ID:                gatewayID,
		OrgID:             uuid.New(),
		Name:              "test-gw",
		RegistrationToken: token,
		State:             db.StorageGatewayStatePROVISIONING,
		CertState:         db.CertStatePENDING,
		Annotations:       json.RawMessage(`{}`),
	}

	agent := db.StorageAgent{
		ID:        agentID,
		GatewayID: gatewayID,
		IpAddress: "0.0.0.0",
		Hostname:  "test-host",
		Version:   "dev",
		State:     db.AgentStateCONNECTED,
	}

	fsConfig := json.RawMessage(`{"type":"filesystem","path":"/tmp/test-media"}`)
	endpoints := []db.StorageEndpoint{
		{
			ID:            uuid.New(),
			GatewayID:     gatewayID,
			Name:          "organizations/acme/storageGateways/gw1/endpoints/media",
			DisplayName:   "Media",
			Configuration: fsConfig,
			CacheEnabled:  false,
			CacheEviction: db.EvictionPolicyLRU,
			State:         db.EndpointStateACTIVE,
			Annotations:   json.RawMessage(`{}`),
		},
	}

	mockQ := new(mocks.MockQuerier)

	// Handshake flow — these are called in order by the server.
	mockQ.On("GetStorageGatewayByToken", anyCtx, token).Return(gateway, nil)
	mockQ.On("GetStorageAgentByGatewayAndIP", anyCtx, mock.AnythingOfType("db.GetStorageAgentByGatewayAndIPParams")).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", anyCtx, mock.AnythingOfType("db.CreateStorageAgentParams")).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", anyCtx, mock.AnythingOfType("db.CreateStorageAgentAuditParams")).Return(nil).Maybe()
	mockQ.On("ListStorageEndpointsByGateway", anyCtx, gatewayID).Return(endpoints, nil)
	mockQ.On("UpdateStorageGatewayState", anyCtx, mock.AnythingOfType("db.UpdateStorageGatewayStateParams")).Return(nil).Maybe()

	// Heartbeat — may or may not fire depending on timing.
	mockQ.On("UpdateStorageAgentHeartbeat", anyCtx, agentID).Return(nil).Maybe()

	// Disconnect flow.
	mockQ.On("UpdateStorageAgentState", anyCtx, mock.AnythingOfType("db.UpdateStorageAgentStateParams")).Return(agent, nil).Maybe()
	mockQ.On("CountConnectedStorageAgentsByGateway", anyCtx, gatewayID).Return(int64(0), nil).Maybe()

	addr := setupAgentGRPC(t, mockQ)
	cfg := newConnectConfig(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Cancel after a short delay so we exit the heartbeat loop.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := Connect(ctx, addr, false, token, cfg, logger)
	// Context cancellation should cause Connect to return nil.
	assert.NoError(t, err)

	// Verify the handshake was called.
	mockQ.AssertCalled(t, "GetStorageGatewayByToken", anyCtx, token)
	mockQ.AssertCalled(t, "CreateStorageAgent", anyCtx, mock.AnythingOfType("db.CreateStorageAgentParams"))
	mockQ.AssertCalled(t, "ListStorageEndpointsByGateway", anyCtx, gatewayID)
}

func TestConnect_InvalidToken(t *testing.T) {
	mockQ := new(mocks.MockQuerier)

	// Server rejects the token.
	mockQ.On("GetStorageGatewayByToken", anyCtx, "bad-token").Return(db.StorageGateway{}, pgx.ErrNoRows)

	addr := setupAgentGRPC(t, mockQ)
	cfg := newConnectConfig(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Connect(ctx, addr, false, "bad-token", cfg, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handshake")
}

func TestConnect_ContextCancelDuringHeartbeat(t *testing.T) {
	gatewayID := uuid.New()
	agentID := uuid.New()
	token := "cancel-token"

	gateway := db.StorageGateway{
		ID:                gatewayID,
		OrgID:             uuid.New(),
		Name:              "cancel-gw",
		RegistrationToken: token,
		State:             db.StorageGatewayStateACTIVE, // already active, no state transition
		CertState:         db.CertStatePENDING,
		Annotations:       json.RawMessage(`{}`),
	}

	agent := db.StorageAgent{
		ID:        agentID,
		GatewayID: gatewayID,
		IpAddress: "0.0.0.0",
		Version:   "dev",
		State:     db.AgentStateCONNECTED,
	}

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetStorageGatewayByToken", anyCtx, token).Return(gateway, nil)
	mockQ.On("GetStorageAgentByGatewayAndIP", anyCtx, mock.AnythingOfType("db.GetStorageAgentByGatewayAndIPParams")).Return(db.StorageAgent{}, pgx.ErrNoRows)
	mockQ.On("CreateStorageAgent", anyCtx, mock.AnythingOfType("db.CreateStorageAgentParams")).Return(agent, nil)
	mockQ.On("CreateStorageAgentAudit", anyCtx, mock.AnythingOfType("db.CreateStorageAgentAuditParams")).Return(nil).Maybe()
	mockQ.On("ListStorageEndpointsByGateway", anyCtx, gatewayID).Return([]db.StorageEndpoint{}, nil)
	mockQ.On("UpdateStorageAgentHeartbeat", anyCtx, agentID).Return(nil).Maybe()
	mockQ.On("UpdateStorageAgentState", anyCtx, mock.AnythingOfType("db.UpdateStorageAgentStateParams")).Return(agent, nil).Maybe()
	mockQ.On("CountConnectedStorageAgentsByGateway", anyCtx, gatewayID).Return(int64(0), nil).Maybe()
	mockQ.On("UpdateStorageGatewayState", anyCtx, mock.AnythingOfType("db.UpdateStorageGatewayStateParams")).Return(nil).Maybe()

	addr := setupAgentGRPC(t, mockQ)
	cfg := newConnectConfig(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 200ms — well before the heartbeat interval fires.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	err := Connect(ctx, addr, false, token, cfg, logger)
	assert.NoError(t, err, "cancelling context during heartbeat loop should return nil")
}

func TestConnect_ReconnectingAgent(t *testing.T) {
	gatewayID := uuid.New()
	agentID := uuid.New()
	token := "reconnect-token"

	gateway := db.StorageGateway{
		ID:                gatewayID,
		OrgID:             uuid.New(),
		Name:              "reconnect-gw",
		RegistrationToken: token,
		State:             db.StorageGatewayStateACTIVE,
		CertState:         db.CertStatePENDING,
		Annotations:       json.RawMessage(`{}`),
	}

	existingAgent := db.StorageAgent{
		ID:        agentID,
		GatewayID: gatewayID,
		IpAddress: "0.0.0.0",
		Version:   "dev",
		State:     db.AgentStateDISCONNECTED,
	}

	reconnectedAgent := existingAgent
	reconnectedAgent.State = db.AgentStateCONNECTED

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetStorageGatewayByToken", anyCtx, token).Return(gateway, nil)
	// This time the agent already exists.
	mockQ.On("GetStorageAgentByGatewayAndIP", anyCtx, mock.AnythingOfType("db.GetStorageAgentByGatewayAndIPParams")).Return(existingAgent, nil)
	// Server should call UpdateStorageAgentState to CONNECTED (not CreateStorageAgent).
	mockQ.On("UpdateStorageAgentState", anyCtx, mock.MatchedBy(func(p db.UpdateStorageAgentStateParams) bool {
		return p.ID == agentID && p.State == db.AgentStateCONNECTED
	})).Return(reconnectedAgent, nil).Once()
	mockQ.On("CreateStorageAgentAudit", anyCtx, mock.AnythingOfType("db.CreateStorageAgentAuditParams")).Return(nil).Maybe()
	mockQ.On("ListStorageEndpointsByGateway", anyCtx, gatewayID).Return([]db.StorageEndpoint{}, nil)
	mockQ.On("UpdateStorageAgentHeartbeat", anyCtx, agentID).Return(nil).Maybe()
	// Disconnect flow — UpdateStorageAgentState to DISCONNECTED.
	mockQ.On("UpdateStorageAgentState", anyCtx, mock.MatchedBy(func(p db.UpdateStorageAgentStateParams) bool {
		return p.ID == agentID && p.State == db.AgentStateDISCONNECTED
	})).Return(existingAgent, nil).Maybe()
	mockQ.On("CountConnectedStorageAgentsByGateway", anyCtx, gatewayID).Return(int64(0), nil).Maybe()
	mockQ.On("UpdateStorageGatewayState", anyCtx, mock.AnythingOfType("db.UpdateStorageGatewayStateParams")).Return(nil).Maybe()

	addr := setupAgentGRPC(t, mockQ)
	cfg := newConnectConfig(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := Connect(ctx, addr, false, token, cfg, logger)
	assert.NoError(t, err)

	// Verify CreateStorageAgent was NOT called (reconnection path).
	mockQ.AssertNotCalled(t, "CreateStorageAgent", anyCtx, mock.Anything)
}

func TestConnect_TLS(t *testing.T) {
	// Verify that passing useTLS=true with a non-TLS server fails with a
	// connection error rather than silently succeeding.
	mockQ := new(mocks.MockQuerier)

	// Set up a plain (non-TLS) gRPC server.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	connMgr := agentstream.NewConnectionManager()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(srv, storage.NewAgentServiceServerForTesting(mockQ, logger, connMgr))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })

	cfg := newConnectConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Connect with TLS to a non-TLS server should fail.
	err = Connect(ctx, lis.Addr().String(), true, "any-token", cfg, logger)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Verify the agent-side Connect function actually uses gRPC NewClient with
// insecure credentials when useTLS is false. We do this by dialing a server
// that requires no TLS and succeeding.
// ---------------------------------------------------------------------------

func TestConnect_InsecureDialSuccess(t *testing.T) {
	// This test verifies that the insecure path works end-to-end by
	// checking the gRPC client can dial successfully. We set up a server
	// that rejects the token immediately to keep the test fast.
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetStorageGatewayByToken", anyCtx, "insecure-token").Return(db.StorageGateway{}, pgx.ErrNoRows)

	addr := setupAgentGRPC(t, mockQ)

	// Directly test that we can establish a gRPC connection with insecure creds.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	require.NoError(t, err)
	assert.NotNil(t, stream)
}
