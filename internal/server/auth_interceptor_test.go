package server

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
)

// claims helper builds the minimal Claims map that the interceptor
// requires — every authenticated request must carry `pivox_user_id`.
func claimsWithPivoxUserID(id uuid.UUID) map[string]any {
	return map[string]any{"pivox_user_id": id.String()}
}

// --- Mock authn.Service ---

type mockAuthService struct {
	mock.Mock
}

func (m *mockAuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authn.Identity), args.Error(1)
}

func (m *mockAuthService) CreateCustomToken(ctx context.Context, uid string) (string, error) {
	args := m.Called(ctx, uid)
	return args.String(0), args.Error(1)
}

func (m *mockAuthService) CreateTenant(ctx context.Context, displayName string) (string, error) {
	args := m.Called(ctx, displayName)
	return args.String(0), args.Error(1)
}

func (m *mockAuthService) DeleteTenant(ctx context.Context, tenantID string) error {
	args := m.Called(ctx, tenantID)
	return args.Error(0)
}

func (m *mockAuthService) DeleteUser(ctx context.Context, uid string) error {
	args := m.Called(ctx, uid)
	return args.Error(0)
}

func (m *mockAuthService) CreateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) UpdateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) DeleteOidcProvider(ctx context.Context, providerID string) error {
	args := m.Called(ctx, providerID)
	return args.Error(0)
}

func (m *mockAuthService) CreateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) UpdateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) DeleteSamlProvider(ctx context.Context, providerID string) error {
	args := m.Called(ctx, providerID)
	return args.Error(0)
}

// --- Mock grpc.ServerStream ---

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

// --- AuthenticatedUID tests ---

func TestAuthenticatedUID_Present(t *testing.T) {
	ctx := context.WithValue(context.Background(), authContextKey{}, "user-123")
	uid, ok := AuthenticatedUID(ctx)

	assert.True(t, ok)
	assert.Equal(t, "user-123", uid)
}

func TestAuthenticatedUID_Missing(t *testing.T) {
	ctx := context.Background()
	uid, ok := AuthenticatedUID(ctx)

	assert.False(t, ok)
	assert.Empty(t, uid)
}

func TestMustAuthenticatedUID_Present(t *testing.T) {
	ctx := context.WithValue(context.Background(), authContextKey{}, "user-456")
	uid := MustAuthenticatedUID(ctx)

	assert.Equal(t, "user-456", uid)
}

func TestMustAuthenticatedUID_Panics(t *testing.T) {
	ctx := context.Background()
	assert.Panics(t, func() {
		MustAuthenticatedUID(ctx)
	})
}

// --- Unary interceptor tests ---

// AgentService no longer participates in the public AuthInterceptor chain
// — it lives on a separate gRPC server with its own AgentAuthStreamInterceptor.
// See cmd/pivox-cloud/main.go.

func TestAuthInterceptor_ValidToken(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer test-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	pivoxUID := uuid.New()
	auth.On("VerifyToken", mock.Anything, "test-token").Return(&authn.Identity{
		UID:    "user-789",
		Email:  "user@example.com",
		Claims: claimsWithPivoxUserID(pivoxUID),
	}, nil)

	var capturedUID string
	var capturedPivoxUID uuid.UUID
	handler := func(ctx context.Context, req any) (any, error) {
		uid, ok := AuthenticatedUID(ctx)
		require.True(t, ok)
		capturedUID = uid
		pid, pok := PivoxUserID(ctx)
		require.True(t, pok)
		capturedPivoxUID = pid
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}

	resp, err := interceptor(ctx, nil, info, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, "user-789", capturedUID)
	assert.Equal(t, pivoxUID, capturedPivoxUID)
	auth.AssertExpectations(t)
}

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)
	ctx := context.Background() // no metadata

	info := &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}

	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "missing metadata")
}

func TestAuthInterceptor_MissingAuthHeader(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"other-header": "value"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}

	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "missing authorization header")
}

func TestAuthInterceptor_InvalidFormat(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Basic dXNlcjpwYXNz"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}

	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "invalid authorization format")
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer bad-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	auth.On("VerifyToken", mock.Anything, "bad-token").Return(nil, fmt.Errorf("invalid token"))

	info := &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}

	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "invalid or expired token")
	auth.AssertExpectations(t)
}

// --- Stream interceptor tests ---

func TestAuthStreamInterceptor_ValidToken(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthStreamInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer stream-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	pivoxUID := uuid.New()
	auth.On("VerifyToken", mock.Anything, "stream-token").Return(&authn.Identity{
		UID:    "stream-user",
		Claims: claimsWithPivoxUserID(pivoxUID),
	}, nil)

	var capturedUID string
	var capturedPivoxUID uuid.UUID
	handler := func(srv any, stream grpc.ServerStream) error {
		uid, ok := AuthenticatedUID(stream.Context())
		require.True(t, ok)
		capturedUID = uid
		pid, pok := PivoxUserID(stream.Context())
		require.True(t, pok)
		capturedPivoxUID = pid
		return nil
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/StreamSpaces",
	}

	ss := &mockServerStream{ctx: ctx}
	err := interceptor(nil, ss, info, handler)

	require.NoError(t, err)
	assert.Equal(t, "stream-user", capturedUID)
	assert.Equal(t, pivoxUID, capturedPivoxUID)
	auth.AssertExpectations(t)
}

// --- pivox_user_id claim handling ---

func TestPivoxUserID_Present(t *testing.T) {
	want := uuid.New()
	ctx := WithPivoxUserID(context.Background(), want)
	got, ok := PivoxUserID(ctx)

	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestPivoxUserID_Missing(t *testing.T) {
	ctx := context.Background()
	_, ok := PivoxUserID(ctx)

	assert.False(t, ok)
}

func TestMustPivoxUserID_Present(t *testing.T) {
	want := uuid.New()
	ctx := WithPivoxUserID(context.Background(), want)

	assert.Equal(t, want, MustPivoxUserID(ctx))
}

func TestMustPivoxUserID_Panics(t *testing.T) {
	assert.Panics(t, func() {
		MustPivoxUserID(context.Background())
	})
}

func TestAuthInterceptor_RejectsMissingPivoxUserID(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer no-claim-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	auth.On("VerifyToken", mock.Anything, "no-claim-token").Return(&authn.Identity{
		UID:    "user-noclaim",
		Claims: map[string]any{},
	}, nil)

	info := &grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.Spaces/GetSpace"}
	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "invalid or expired token")
	auth.AssertExpectations(t)
}

func TestAuthInterceptor_RejectsEmptyPivoxUserID(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer empty-claim-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	auth.On("VerifyToken", mock.Anything, "empty-claim-token").Return(&authn.Identity{
		UID:    "user-empty",
		Claims: map[string]any{"pivox_user_id": ""},
	}, nil)

	info := &grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.Spaces/GetSpace"}
	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	auth.AssertExpectations(t)
}

func TestAuthInterceptor_RejectsNonStringPivoxUserID(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer wrong-type-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// Firebase claims arrive as `any` from JSON decode; a malformed
	// claim value (number, bool, map) must be rejected.
	auth.On("VerifyToken", mock.Anything, "wrong-type-token").Return(&authn.Identity{
		UID:    "user-wrongtype",
		Claims: map[string]any{"pivox_user_id": 12345},
	}, nil)

	info := &grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.Spaces/GetSpace"}
	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	auth.AssertExpectations(t)
}

func TestAuthInterceptor_RejectsInvalidUUIDPivoxUserID(t *testing.T) {
	auth := new(mockAuthService)
	interceptor := AuthInterceptor(auth)

	md := metadata.New(map[string]string{"authorization": "Bearer bad-uuid-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	auth.On("VerifyToken", mock.Anything, "bad-uuid-token").Return(&authn.Identity{
		UID:    "user-baduuid",
		Claims: map[string]any{"pivox_user_id": "not-a-uuid"},
	}, nil)

	info := &grpc.UnaryServerInfo{FullMethod: "/pivox.api.v1.Spaces/GetSpace"}
	_, err := interceptor(ctx, nil, info, nil)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "invalid or expired token")
	auth.AssertExpectations(t)
}
