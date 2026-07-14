package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyJWKS serves a JWKS that fails its first `failures` requests with 503
// before succeeding — the shape of a Keycloak (or the tunnel in front of it)
// that is not up yet when the API boots.
type flakyJWKS struct {
	issuer  string
	jwksURL string
	// fetches counts every request that reached the server, so a test can
	// assert exactly how many attempts NewVerifier made.
	fetches *atomic.Int64
}

func newFlakyJWKS(t *testing.T, failures int64) *flakyJWKS {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	var fetches atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/pivox/protocol/openid-connect/certs", func(w http.ResponseWriter, _ *http.Request) {
		if fetches.Add(1) <= failures {
			// 503 is what an unreachable origin behind a proxy actually returns.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		jwks := map[string]any{"keys": []map[string]string{{
			"kid": "kc-key-1",
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}}
		w.Header().Set("Content-Type", "application/jwk-set+json")
		require.NoError(t, json.NewEncoder(w).Encode(jwks))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &flakyJWKS{
		issuer:  srv.URL + "/realms/pivox",
		jwksURL: srv.URL + "/realms/pivox/protocol/openid-connect/certs",
		fetches: &fetches,
	}
}

// retryCfg is a Config with a tiny backoff so the tests stay fast.
func retryCfg(h *flakyJWKS, attempts int) Config {
	return Config{
		Issuer:            h.issuer,
		JWKSURL:           h.jwksURL,
		Audience:          "pivox",
		JWKSFetchAttempts: attempts,
		JWKSFetchBackoff:  time.Millisecond,
	}
}

func TestNewVerifier_JWKSFetchRetry(t *testing.T) {
	t.Parallel()

	t.Run("does not retry when the first fetch succeeds", func(t *testing.T) {
		t.Parallel()
		h := newFlakyJWKS(t, 0)

		v, err := NewVerifier(context.Background(), retryCfg(h, 5))

		require.NoError(t, err)
		assert.NotNil(t, v)
		assert.Equal(t, int64(1), h.fetches.Load(), "should not retry a successful fetch")
	})

	t.Run("retries a transiently unavailable IdP and succeeds", func(t *testing.T) {
		t.Parallel()
		// The real scenario: the API boots before Keycloak/the tunnel is up.
		h := newFlakyJWKS(t, 2)

		v, err := NewVerifier(context.Background(), retryCfg(h, 5))

		require.NoError(t, err, "should recover once the IdP comes up")
		assert.NotNil(t, v)
		assert.Equal(t, int64(3), h.fetches.Load(), "two failures then a success")
	})

	t.Run("fails after exhausting the attempt budget", func(t *testing.T) {
		t.Parallel()
		// Never recovers — must FAIL, not hang and not come up unable to verify.
		h := newFlakyJWKS(t, 1_000)

		v, err := NewVerifier(context.Background(), retryCfg(h, 4))

		require.Error(t, err)
		assert.Nil(t, v)
		assert.ErrorContains(t, err, "4 attempts", "error should say how hard it tried")
		assert.Equal(t, int64(4), h.fetches.Load(), "exactly the attempt budget, no more")
	})

	t.Run("aborts promptly when the context is cancelled mid-backoff", func(t *testing.T) {
		t.Parallel()
		h := newFlakyJWKS(t, 1_000)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already cancelled: shutdown raced startup

		cfg := retryCfg(h, 100)
		cfg.JWKSFetchBackoff = time.Hour // would hang forever if ctx were ignored

		start := time.Now()
		v, err := NewVerifier(ctx, cfg)

		require.Error(t, err)
		assert.Nil(t, v)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(start), 5*time.Second, "must not wait out the backoff")
	})
}

func TestResolveJWKSFetchRetry(t *testing.T) {
	t.Parallel()

	t.Run("applies defaults when unset or nonsensical", func(t *testing.T) {
		t.Parallel()
		// An unset Config must not mean "never retry" or "retry forever".
		assert.Equal(t, defaultJWKSFetchAttempts, resolveJWKSFetchAttempts(0))
		assert.Equal(t, defaultJWKSFetchAttempts, resolveJWKSFetchAttempts(-3))
		assert.Equal(t, defaultJWKSFetchBackoff, resolveJWKSFetchBackoff(0))
		assert.Equal(t, defaultJWKSFetchBackoff, resolveJWKSFetchBackoff(-time.Second))
	})

	t.Run("honours explicit values", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 1, resolveJWKSFetchAttempts(1), "1 attempt = no retry, a legitimate choice")
		assert.Equal(t, 9, resolveJWKSFetchAttempts(9))
		assert.Equal(t, 250*time.Millisecond, resolveJWKSFetchBackoff(250*time.Millisecond))
	})
}
