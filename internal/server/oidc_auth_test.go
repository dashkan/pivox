package server

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/authn"
)

const testKCIssuer = "https://kc.example.com/realms/pivox"

// recordingBase is a stub wrapped authn.Service that records whether its
// VerifyToken was reached. Embeds authn.Service (nil) — only VerifyToken is
// exercised here.
type recordingBase struct {
	authn.Service
	hit bool
}

func (r *recordingBase) VerifyToken(context.Context, string) (*authn.Identity, error) {
	r.hit = true
	return &authn.Identity{UID: "firebase-uid", Claims: map[string]any{"pivox_user_id": "fb"}}, nil
}

type recordingOIDC struct{ hit bool }

func (r *recordingOIDC) VerifyToken(context.Context, string) (*authn.Identity, error) {
	r.hit = true
	return &authn.Identity{UID: "kc-sub"}, nil
}

// failingOIDC simulates a KC-issuer token that fails verification.
type failingOIDC struct{}

func (failingOIDC) VerifyToken(context.Context, string) (*authn.Identity, error) {
	return nil, errors.New("kc verify failed")
}

func signWithIssuer(t *testing.T, iss string) string {
	t.Helper()
	// Unverified routing only reads `iss`, so any signature works.
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": iss}).
		SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return signed
}

func TestOIDCAuthService_Routing(t *testing.T) {
	t.Parallel()

	t.Run("routes the configured issuer to the OIDC verifier", func(t *testing.T) {
		t.Parallel()
		base, oidcVer := &recordingBase{}, &recordingOIDC{}
		svc := NewOIDCAuthService(base, oidcVer, testKCIssuer)

		id, err := svc.VerifyToken(context.Background(), signWithIssuer(t, testKCIssuer))
		require.NoError(t, err)
		assert.True(t, oidcVer.hit, "OIDC verifier should handle the KC issuer")
		assert.False(t, base.hit, "wrapped service should not be reached")
		assert.Equal(t, "kc-sub", id.UID)
	})

	t.Run("routes a Firebase issuer to the wrapped service", func(t *testing.T) {
		t.Parallel()
		base, oidcVer := &recordingBase{}, &recordingOIDC{}
		svc := NewOIDCAuthService(base, oidcVer, testKCIssuer)

		_, err := svc.VerifyToken(context.Background(), signWithIssuer(t, "https://securetoken.google.com/proj"))
		require.NoError(t, err)
		assert.True(t, base.hit)
		assert.False(t, oidcVer.hit)
	})

	t.Run("does NOT fall through to base when OIDC verification fails", func(t *testing.T) {
		t.Parallel()
		base := &recordingBase{}
		svc := NewOIDCAuthService(base, failingOIDC{}, testKCIssuer)

		_, err := svc.VerifyToken(context.Background(), signWithIssuer(t, testKCIssuer))
		assert.Error(t, err)
		assert.False(t, base.hit, "a failed KC verification must not get a second chance at Firebase (auth-bypass guard)")
	})

	t.Run("routes a non-JWT bearer to the wrapped service", func(t *testing.T) {
		t.Parallel()
		base, oidcVer := &recordingBase{}, &recordingOIDC{}
		svc := NewOIDCAuthService(base, oidcVer, testKCIssuer)

		_, err := svc.VerifyToken(context.Background(), "not-a-jwt")
		require.NoError(t, err)
		assert.True(t, base.hit)
		assert.False(t, oidcVer.hit)
	})

	t.Run("returns base unchanged when OIDC is not configured", func(t *testing.T) {
		t.Parallel()
		base := &recordingBase{}
		_, wrapped := NewOIDCAuthService(base, nil, "").(*oidcAuthService)
		assert.False(t, wrapped, "nil verifier should leave the chain unwrapped")
		_, wrapped = NewOIDCAuthService(base, &recordingOIDC{}, "").(*oidcAuthService)
		assert.False(t, wrapped, "empty issuer should leave the chain unwrapped")
	})
}
