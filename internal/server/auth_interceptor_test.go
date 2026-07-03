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

// A verified Keycloak access token carries the caller's identity id as its
// `sub`, surfaced as Identity.UID. The interceptor parses it and resolves the
// caller onto the context. (sub == identities.id — the whole point of the
// Firebase->Keycloak cutover; there is no custom claim.)
func TestAuthInterceptor_ValidToken(t *testing.T) {
	auth := authnmock.NewMockService(t)
	sub := uuid.New()
	expectVerifyToken(auth, "kc-token", &authn.Identity{
		UID:    sub.String(),
		Email:  "kc@example.com",
		Claims: map[string]any{"iss": "https://kc/realms/pivox"},
	})

	md := metadata.New(map[string]string{"authorization": "Bearer kc-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var captured uuid.UUID
	handler := func(ctx context.Context, req any) (any, error) {
		var ok bool
		captured, ok = UserID(ctx)
		require.True(t, ok)
		return "ok", nil
	}

	resp, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, sub, captured)
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
	sub := uuid.New()
	expectVerifyToken(auth, "stream-token", &authn.Identity{
		UID: sub.String(),
	})

	md := metadata.New(map[string]string{"authorization": "Bearer stream-token"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var captured uuid.UUID
	handler := func(srv any, stream grpc.ServerStream) error {
		var ok bool
		captured, ok = UserID(stream.Context())
		require.True(t, ok)
		return nil
	}

	err := AuthStreamInterceptor(auth)(nil, &mockServerStream{ctx: ctx}, &grpc.StreamServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/StreamSpaces",
	}, handler)

	require.NoError(t, err)
	assert.Equal(t, sub, captured)
}

// ---------------------------------------------------------------------------
// Identity resolution from the token `sub`
// ---------------------------------------------------------------------------

func TestUserID_Present(t *testing.T) {
	want := uuid.New()
	ctx := WithUserID(context.Background(), want)
	got, ok := UserID(ctx)

	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestUserID_Missing(t *testing.T) {
	_, ok := UserID(context.Background())
	assert.False(t, ok)
}

func TestMustUserID_Present(t *testing.T) {
	want := uuid.New()
	ctx := WithUserID(context.Background(), want)
	assert.Equal(t, want, MustUserID(ctx))
}

func TestMustUserID_Panics(t *testing.T) {
	assert.Panics(t, func() {
		MustUserID(context.Background())
	})
}

// TestAuthInterceptor_UnresolvableSub covers the identity-resolution reject
// branches: the token verifies, but its `sub` (Identity.UID) isn't a usable
// identity id. Both an empty sub and a non-UUID sub must produce Unauthenticated
// (indistinguishable from a bad signature, so clients just refresh).
func TestAuthInterceptor_UnresolvableSub(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		token string
		sub   string
	}{
		{"empty sub", "empty-sub-token", ""},
		{"non-uuid sub", "bad-uuid-token", "not-a-uuid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			auth := authnmock.NewMockService(t)
			expectVerifyToken(auth, tc.token, &authn.Identity{UID: tc.sub})

			md := metadata.New(map[string]string{"authorization": "Bearer " + tc.token})
			ctx := metadata.NewIncomingContext(context.Background(), md)

			_, err := AuthInterceptor(auth)(ctx, nil, &grpc.UnaryServerInfo{
				FullMethod: "/pivox.api.v1.Spaces/GetSpace",
			}, nil)

			requireAuthn(t, err, "")
		})
	}
}
