package server

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// Canonical gateway returned by the mock querier when the token matches.
// Tests assert that the authenticated handler observes this exact gateway
// in its context, proving the interceptor injected the validated record.
var testAgentGateway = db.StorageGateway{
	ID:                uuid.MustParse("0192a000-0010-7000-8000-000000100010"),
	Name:              "local",
	RegistrationToken: "valid-token",
}

// streamCtx returns a grpc.ServerStream whose Context() returns ctx.
// Mirrors the mockServerStream pattern from auth_interceptor_test.go.
func streamCtx(ctx context.Context) grpc.ServerStream {
	return &mockServerStream{ctx: ctx}
}

func streamHandler(called *bool, capturedGateway **db.StorageGateway) grpc.StreamHandler {
	return func(_ any, ss grpc.ServerStream) error {
		*called = true
		if g, ok := AuthenticatedGateway(ss.Context()); ok {
			*capturedGateway = &g
		}
		return nil
	}
}

// --- AuthenticatedGateway helper ---

func TestAuthenticatedGateway_Present(t *testing.T) {
	ctx := context.WithValue(context.Background(), agentGatewayKey{}, testAgentGateway)
	g, ok := AuthenticatedGateway(ctx)
	assert.True(t, ok)
	assert.Equal(t, testAgentGateway.ID, g.ID)
}

func TestAuthenticatedGateway_Missing(t *testing.T) {
	g, ok := AuthenticatedGateway(context.Background())
	assert.False(t, ok)
	assert.Equal(t, db.StorageGateway{}, g)
}

// --- Missing / malformed credentials ---

func TestAgentAuthInterceptor_NoMetadata(t *testing.T) {
	q := new(mocks.MockQuerier)
	interceptor := AgentAuthStreamInterceptor(q)

	called := false
	var captured *db.StorageGateway
	err := interceptor(
		nil,
		streamCtx(context.Background()), // no incoming metadata at all
		&grpc.StreamServerInfo{FullMethod: "/pivox.agent.v1.AgentService/Connect"},
		streamHandler(&called, &captured),
	)

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, called, "handler must not be invoked when metadata is missing")
}

func TestAgentAuthInterceptor_MissingTokenHeader(t *testing.T) {
	q := new(mocks.MockQuerier)
	interceptor := AgentAuthStreamInterceptor(q)

	// Has metadata but no x-pivox-agent-token entry.
	md := metadata.New(map[string]string{"some-other-key": "value"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	called := false
	var captured *db.StorageGateway
	err := interceptor(
		nil,
		streamCtx(ctx),
		&grpc.StreamServerInfo{FullMethod: "/pivox.agent.v1.AgentService/Connect"},
		streamHandler(&called, &captured),
	)

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Contains(t, err.Error(), "missing agent registration token")
	assert.False(t, called)
}

func TestAgentAuthInterceptor_EmptyToken(t *testing.T) {
	q := new(mocks.MockQuerier)
	interceptor := AgentAuthStreamInterceptor(q)

	md := metadata.New(map[string]string{AgentTokenMetadataKey: ""})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	called := false
	var captured *db.StorageGateway
	err := interceptor(
		nil,
		streamCtx(ctx),
		&grpc.StreamServerInfo{FullMethod: "/pivox.agent.v1.AgentService/Connect"},
		streamHandler(&called, &captured),
	)

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, called)
}

// --- Token validation ---

func TestAgentAuthInterceptor_InvalidToken(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetStorageGatewayByToken", mock.Anything, "bad-token").
		Return(db.StorageGateway{}, pgx.ErrNoRows)
	interceptor := AgentAuthStreamInterceptor(q)

	md := metadata.New(map[string]string{AgentTokenMetadataKey: "bad-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	called := false
	var captured *db.StorageGateway
	err := interceptor(
		nil,
		streamCtx(ctx),
		&grpc.StreamServerInfo{FullMethod: "/pivox.agent.v1.AgentService/Connect"},
		streamHandler(&called, &captured),
	)

	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Contains(t, err.Error(), "invalid agent registration token")
	assert.False(t, called)
	q.AssertExpectations(t)
}

func TestAgentAuthInterceptor_DBError(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetStorageGatewayByToken", mock.Anything, "any-token").
		Return(db.StorageGateway{}, errors.New("connection refused"))
	interceptor := AgentAuthStreamInterceptor(q)

	md := metadata.New(map[string]string{AgentTokenMetadataKey: "any-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	called := false
	var captured *db.StorageGateway
	err := interceptor(
		nil,
		streamCtx(ctx),
		&grpc.StreamServerInfo{FullMethod: "/pivox.agent.v1.AgentService/Connect"},
		streamHandler(&called, &captured),
	)

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.False(t, called)
	q.AssertExpectations(t)
}

// --- Happy path ---

func TestAgentAuthInterceptor_ValidToken(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetStorageGatewayByToken", mock.Anything, "valid-token").
		Return(testAgentGateway, nil)
	interceptor := AgentAuthStreamInterceptor(q)

	md := metadata.New(map[string]string{AgentTokenMetadataKey: "valid-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	called := false
	var captured *db.StorageGateway
	err := interceptor(
		nil,
		streamCtx(ctx),
		&grpc.StreamServerInfo{FullMethod: "/pivox.agent.v1.AgentService/Connect"},
		streamHandler(&called, &captured),
	)

	require.NoError(t, err)
	assert.True(t, called, "handler must be invoked on valid token")
	require.NotNil(t, captured, "gateway must be injected into context")
	assert.Equal(t, testAgentGateway.ID, captured.ID)
	q.AssertExpectations(t)
}
