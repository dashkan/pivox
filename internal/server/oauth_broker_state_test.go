package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAppKey = "test-app-key-padded-to-thirty-two-chars"

func TestSignOAuthState_RoundTrip(t *testing.T) {
	tok, _, err := signOAuthState(testAppKey, "pivox://auth-complete", "github")
	require.NoError(t, err)

	got, err := verifyOAuthState(testAppKey, tok)
	require.NoError(t, err)
	assert.Equal(t, "pivox://auth-complete", got.R)
	assert.Equal(t, "github", got.P)
	assert.NotEmpty(t, got.N, "nonce must be populated")
	assert.True(t, time.Now().Unix()-got.T < 5, "issued-at must be recent")
}

func TestVerifyOAuthState_RejectsMalformed(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"single segment", "abc"},
		{"three segments", "a.b.c"},
		{"non-base64 body", "!!!.zzz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifyOAuthState(testAppKey, tt.token)
			require.Error(t, err)
		})
	}
}

func TestVerifyOAuthState_RejectsTamperedSignature(t *testing.T) {
	tok, _, err := signOAuthState(testAppKey, "pivox://x", "github")
	require.NoError(t, err)

	parts := strings.Split(tok, ".")
	require.Len(t, parts, 2)
	// Flip one character in the signature.
	tampered := parts[0] + "." + flipChar(parts[1])
	_, err = verifyOAuthState(testAppKey, tampered)
	require.Error(t, err)
}

func TestVerifyOAuthState_RejectsTamperedBody(t *testing.T) {
	tok, _, err := signOAuthState(testAppKey, "pivox://x", "github")
	require.NoError(t, err)

	parts := strings.Split(tok, ".")
	require.Len(t, parts, 2)
	// Flip one character in the body — even if the same length, the
	// HMAC won't match.
	tampered := flipChar(parts[0]) + "." + parts[1]
	_, err = verifyOAuthState(testAppKey, tampered)
	require.Error(t, err)
}

func TestVerifyOAuthState_RejectsWrongKey(t *testing.T) {
	tok, _, err := signOAuthState(testAppKey, "pivox://x", "github")
	require.NoError(t, err)
	otherKey := "different-key-padded-to-thirty-two-chars!!"
	_, err = verifyOAuthState(otherKey, tok)
	require.Error(t, err)
}

func TestVerifyOAuthState_RejectsExpired(t *testing.T) {
	// Forge a token with an issued-at past the TTL window via the
	// test-only signOAuthStateAt seam. signOAuthState always uses
	// time.Now(), so production callers can't accidentally produce
	// a back-dated token.
	expiredAt := time.Now().Add(-time.Duration(oauthStateMaxAgeSecs+5) * time.Second)
	tok, _, err := signOAuthStateAt(testAppKey, "pivox://x", "github", expiredAt)
	require.NoError(t, err)

	_, err = verifyOAuthState(testAppKey, tok)
	require.Error(t, err, "expired tokens must fail verification")
}

func TestVerifyOAuthState_RejectsFutureDated(t *testing.T) {
	// Future-dated tokens are also rejected (the verify check is
	// `age < 0 || age > MaxAgeSeconds`). Guards against clock-skew
	// attacks where an attacker forges a token timestamped in the
	// future to extend its effective lifetime.
	futureAt := time.Now().Add(1 * time.Hour)
	tok, _, err := signOAuthStateAt(testAppKey, "pivox://x", "github", futureAt)
	require.NoError(t, err)

	_, err = verifyOAuthState(testAppKey, tok)
	require.Error(t, err, "future-dated tokens must fail verification")
}

func TestSignOAuthState_RejectsShortKey(t *testing.T) {
	_, _, err := signOAuthState("short", "pivox://x", "github")
	require.Error(t, err)
}

// flipChar flips the first character of a base64url string into a
// different valid one — ensures we get a different valid-shape
// string rather than a shape-failure that the parser catches before
// HMAC verification runs.
func flipChar(s string) string {
	if len(s) == 0 {
		return s
	}
	first := s[0]
	var replacement byte
	if first == 'A' {
		replacement = 'B'
	} else {
		replacement = 'A'
	}
	return string(replacement) + s[1:]
}
