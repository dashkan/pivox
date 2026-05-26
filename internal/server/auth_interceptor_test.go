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
	"github.com/dashkan/pivox/internal/testutil/authnmock"
)

// claimsWithPivoxUserID builds the minimal Claims map that the
// AuthInterceptor requires — every authenticated request must
// carry pivox_user_id.
func claimsWithPivoxUserID(id uuid.UUID) map[string]any {
	return map[string]any{"pivox_user_id": id.String()}
}

// mockServerStream is a minimal grpc.ServerStream stand-in: the
// stream interceptor only ever reads Context() to thread auth
// claims, so embedding the real interface and overriding that one
// method gives us all the surface we need without hand-rolling
// the rest.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

// expectVerifyToken is the canonical "this token resolves to this
// identity" expectation. Keeping the call shape in one place stops
// each test from re-typing the mock.Anything / argument list.
func expectVerifyToken(auth *authnmock.MockService, token string, id *authn.Identity) {
	auth.EXPECT().VerifyToken(mock.Anything, token).Return(id, nil)
}

// expectVerifyTokenError is the failure variant of expectVerifyToken.
func expectVerifyTokenError(auth *authnmock.MockService, token string, err error) {
	auth.EXPECT().VerifyToken(mock.Anything, token).Return(nil, err)
}

// requireAuthn asserts that err carries an Unauthenticated status
// and that its message contains substr. Centralizes the three-line
// dance every reject-path test was repeating.
func requireAuthn(t *testing.T, err error, substr string) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "want status error, got %T", err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	if substr != "" {
		assert.Contains(t, st.Message(), substr)
	}
}

// ---------------------------------------------------------------------------
// Unary AuthInterceptor
//
// AgentService no longer participates in the public AuthInterceptor chain —
// it lives on a separate gRPC server with its own AgentAuthStreamInterceptor
// (see cmd/pivox-cloud/main.go).
// ---------------------------------------------------------------------------

func TestAuthInterceptor_ValidToken(t *testing.T) {
	auth := authnmock.NewMockService(t)
	pivoxUID := uuid.New()
	expectVerifyToken(auth, "test-token", &authn.Identity{
		UID:    "user-789",
		Email:  "user@example.com",
		Claims: claimsWithPivoxUserID(pivoxUID),
	})

	md := metadata.New(map[string]string{"authorization": "Bearer test-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedPivoxUID uuid.UUID
	handler := func(ctx context.Context, req any) (any, error) {
		var ok bool
		capturedPivoxUID, ok = PivoxUserID(ctx)
		require.True(t, ok)
		return "ok", nil
	}

	resp, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, pivoxUID, capturedPivoxUID)
}

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	auth := authnmock.NewMockService(t)

	_, err := AuthInterceptor(auth)(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, nil)

	requireAuthn(t, err, "missing metadata")
}

func TestAuthInterceptor_MissingAuthHeader(t *testing.T) {
	auth := authnmock.NewMockService(t)

	md := metadata.New(map[string]string{"other-header": "value"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, nil)

	requireAuthn(t, err, "missing authorization header")
}

func TestAuthInterceptor_InvalidFormat(t *testing.T) {
	auth := authnmock.NewMockService(t)

	md := metadata.New(map[string]string{"authorization": "Basic dXNlcjpwYXNz"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, nil)

	requireAuthn(t, err, "invalid authorization format")
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	auth := authnmock.NewMockService(t)
	expectVerifyTokenError(auth, "bad-token", fmt.Errorf("invalid token"))

	md := metadata.New(map[string]string{"authorization": "Bearer bad-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, nil)

	requireAuthn(t, err, "invalid or expired token")
}

// ---------------------------------------------------------------------------
// Stream AuthInterceptor
// ---------------------------------------------------------------------------

func TestAuthStreamInterceptor_ValidToken(t *testing.T) {
	auth := authnmock.NewMockService(t)
	pivoxUID := uuid.New()
	expectVerifyToken(auth, "stream-token", &authn.Identity{
		UID:    "stream-user",
		Claims: claimsWithPivoxUserID(pivoxUID),
	})

	md := metadata.New(map[string]string{"authorization": "Bearer stream-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedPivoxUID uuid.UUID
	handler := func(srv any, stream grpc.ServerStream) error {
		var ok bool
		capturedPivoxUID, ok = PivoxUserID(stream.Context())
		require.True(t, ok)
		return nil
	}

	err := AuthStreamInterceptor(auth)(nil, &mockServerStream{ctx: ctx}, &grpc.StreamServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/StreamSpaces",
	}, handler)

	require.NoError(t, err)
	assert.Equal(t, pivoxUID, capturedPivoxUID)
}

// ---------------------------------------------------------------------------
// pivox_user_id claim handling
// ---------------------------------------------------------------------------

func TestPivoxUserID_Present(t *testing.T) {
	want := uuid.New()
	ctx := WithPivoxUserID(context.Background(), want)
	got, ok := PivoxUserID(ctx)

	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestPivoxUserID_Missing(t *testing.T) {
	_, ok := PivoxUserID(context.Background())
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

// TestAuthInterceptor_PivoxUserIDClaim covers every malformed-claim
// branch through the same table: missing key, empty string, wrong
// type, malformed UUID. They all need to produce Unauthenticated;
// the table prevents the per-case copy/paste that the previous
// shape encouraged.
func TestAuthInterceptor_PivoxUserIDClaim(t *testing.T) {
	t.Parallel()

	// Firebase claims arrive from JSON decode so the value type can
	// be anything; the interceptor must reject every shape that isn't
	// a parseable UUID string.
	cases := []struct {
		name   string
		token  string
		claims map[string]any
	}{
		{"missing", "no-claim-token", map[string]any{}},
		{"empty string", "empty-claim-token", map[string]any{"pivox_user_id": ""}},
		{"non-string", "wrong-type-token", map[string]any{"pivox_user_id": 12345}},
		{"invalid uuid", "bad-uuid-token", map[string]any{"pivox_user_id": "not-a-uuid"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			auth := authnmock.NewMockService(t)
			expectVerifyToken(auth, tc.token, &authn.Identity{
				UID:    "claim-test-user",
				Claims: tc.claims,
			})

			md := metadata.New(map[string]string{"authorization": "Bearer " + tc.token})
			ctx := metadata.NewIncomingContext(context.Background(), md)

			_, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
				FullMethod: "/pivox.api.v1.Spaces/GetSpace",
			}, nil)

			requireAuthn(t, err, "")
		})
	}
}
