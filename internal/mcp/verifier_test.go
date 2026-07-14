package mcp_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/mcp"
	"github.com/dashkan/pivox/internal/testutil/authnmock"
)

// TestNewTokenVerifier covers the adapter from authn.Service (the OIDC verifier,
// pinned to the MCP resource audience) to the SDK's auth.TokenInfo: it maps the
// SDK-facing fields through, stashes the full verified identity under
// ExtraIdentity for the whoami handler, and on any verification failure returns
// an error that unwraps to auth.ErrInvalidToken (the SDK's 401 sentinel) without
// leaking the underlying reason to the caller.
func TestNewTokenVerifier(t *testing.T) {
	t.Parallel()

	const uid = "0192a000-0000-7000-8000-000000000001"

	tests := []struct {
		name       string
		identity   *authn.Identity
		verifyErr  error
		wantErrIs  error
		wantUID    string
		wantScopes []string
	}{
		{
			name: "valid token maps identity and claims to token info",
			identity: &authn.Identity{
				UID:   uid,
				Email: "user@example.com",
				Claims: map[string]any{
					"sub":          uid,
					"scope":        "openid mcp:tools organization",
					"organization": []any{"pacific-coast-net"},
				},
			},
			wantUID:    uid,
			wantScopes: []string{"openid", "mcp:tools", "organization"},
		},
		{
			name:      "verification failure returns ErrInvalidToken",
			verifyErr: errors.New(`oidc: token audience does not include "https://pivox.example/mcp"`),
			wantErrIs: auth.ErrInvalidToken,
		},
		{
			name: "identity with no organization claim still stashed",
			identity: &authn.Identity{
				UID:    uid,
				Claims: map[string]any{"sub": uid, "scope": "mcp:tools"},
			},
			wantUID:    uid,
			wantScopes: []string{"mcp:tools"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := authnmock.NewMockService(t)
			svc.EXPECT().VerifyToken(mock.Anything, "tok").Return(tt.identity, tt.verifyErr)

			verify := mcp.NewTokenVerifier(svc)
			info, err := verify(context.Background(), "tok", httptest.NewRequest("POST", "/mcp", nil))

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Nil(t, info)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, tt.wantUID, info.UserID)
			if tt.wantScopes != nil {
				assert.Equal(t, tt.wantScopes, info.Scopes)
			}
			// The full verified identity is stashed for iam.BuildAccount (the
			// whoami resolution), keyed by ExtraIdentity.
			gotIdentity, ok := info.Extra[mcp.ExtraIdentity].(*authn.Identity)
			require.True(t, ok, "verified identity must be stashed under ExtraIdentity")
			assert.Same(t, tt.identity, gotIdentity)
		})
	}
}

// TestNewTokenVerifier_NilServicePanics documents the fail-loud constructor
// contract (a nil dependency is a boot-time programmer error, not a runtime 401).
func TestNewTokenVerifier_NilServicePanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { mcp.NewTokenVerifier(nil) })
}
