package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jwksHarness stands up an httptest server that serves a JWKS for one
// RSA key at a Keycloak-style certs path, plus a `sign` helper that
// mints JWTs with the same key. This exercises the real
// signature-verification path without a live Keycloak.
type jwksHarness struct {
	t       *testing.T
	key     *rsa.PrivateKey
	kid     string
	issuer  string
	jwksURL string
}

func newJWKSHarness(t *testing.T) *jwksHarness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const kid = "kc-key-1"

	mux := http.NewServeMux()
	mux.HandleFunc("/realms/pivox/protocol/openid-connect/certs", func(w http.ResponseWriter, _ *http.Request) {
		jwks := map[string]any{"keys": []map[string]string{{
			"kid": kid,
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

	return &jwksHarness{
		t:       t,
		key:     key,
		kid:     kid,
		issuer:  srv.URL + "/realms/pivox",
		jwksURL: srv.URL + "/realms/pivox/protocol/openid-connect/certs",
	}
}

func (h *jwksHarness) sign(claims jwt.MapClaims) string {
	h.t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = h.kid
	signed, err := tok.SignedString(h.key)
	require.NoError(h.t, err)
	return signed
}

// validClaims is a well-formed Keycloak access-token claim set: the
// `sub` is a UUID (which IS the Pivox identity id), plus standard
// profile claims and a future `exp`.
func validClaims(h *jwksHarness, sub string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            h.issuer,
		"sub":            sub,
		"aud":            "pivox",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"email":          "ashkan@acme.com",
		"email_verified": true,
		"name":           "Ashkan Daie",
	}
}

func newTestVerifier(t *testing.T, h *jwksHarness, audience string) *Verifier {
	t.Helper()
	v, err := NewVerifier(context.Background(), Config{
		Issuer:   h.issuer,
		JWKSURL:  h.jwksURL,
		Audience: audience,
	})
	require.NoError(t, err)
	return v
}

func TestVerifier_VerifyToken(t *testing.T) {
	t.Parallel()

	t.Run("valid token yields identity with sub as UID", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		sub := uuid.NewString()

		id, err := v.VerifyToken(context.Background(), h.sign(validClaims(h, sub)))
		require.NoError(t, err)
		is.Equal(sub, id.UID) // sub IS the Pivox identity id
		is.Equal("ashkan@acme.com", id.Email)
		is.Equal(true, id.Claims["email_verified"])
		is.Equal("Ashkan Daie", id.Claims["name"])
	})

	t.Run("rejects a token from a different issuer", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, uuid.NewString())
		claims["iss"] = "https://evil.example.com/realms/pivox"

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.Error(t, err)
	})

	t.Run("rejects an expired token", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, uuid.NewString())
		claims["exp"] = time.Now().Add(-time.Minute).Unix()

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.Error(t, err)
	})

	t.Run("rejects a token with no exp", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, uuid.NewString())
		delete(claims, "exp") // WithExpirationRequired must reject perpetual tokens.

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.Error(t, err)
	})

	t.Run("rejects a token signed by an unknown key", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		// Sign with a different key than the one in the JWKS.
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, validClaims(h, uuid.NewString()))
		tok.Header["kid"] = h.kid
		signed, err := tok.SignedString(other)
		require.NoError(t, err)

		_, err = v.VerifyToken(context.Background(), signed)
		assert.Error(t, err)
	})

	t.Run("rejects a token with the wrong audience", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, uuid.NewString())
		claims["aud"] = "some-other-client"

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.Error(t, err)
	})

	t.Run("DisableAudienceValidation skips the check", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v, err := NewVerifier(context.Background(), Config{
			Issuer:                    h.issuer,
			JWKSURL:                   h.jwksURL,
			DisableAudienceValidation: true,
		})
		require.NoError(t, err)
		claims := validClaims(h, uuid.NewString())
		claims["aud"] = "anything"

		_, err = v.VerifyToken(context.Background(), h.sign(claims))
		assert.NoError(t, err)
	})

	t.Run("accepts a multi-valued aud containing the audience", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, uuid.NewString())
		// Keycloak access tokens carry an array aud, e.g. ["account","pivox"].
		claims["aud"] = []string{"account", "pivox"}

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.NoError(t, err)
	})

	t.Run("rejects a multi-valued aud with no accepted value", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, uuid.NewString())
		claims["aud"] = []string{"account", "some-other-client"}

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.Error(t, err)
	})

	t.Run("rejects a missing sub", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		claims := validClaims(h, "")
		delete(claims, "sub")

		_, err := v.VerifyToken(context.Background(), h.sign(claims))
		assert.Error(t, err)
	})

	t.Run("rejects a sub that is not a UUID", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")

		_, err := v.VerifyToken(context.Background(), h.sign(validClaims(h, "not-a-uuid")))
		assert.Error(t, err)
	})
}

func TestNewVerifier_AudiencePolicy(t *testing.T) {
	t.Parallel()

	t.Run("errors when audience validation is on but no audience is set", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		// Fail closed: omitting Audience without DisableAudienceValidation is a
		// config error, so the aud check can never be skipped accidentally.
		_, err := NewVerifier(context.Background(), Config{
			Issuer:  h.issuer,
			JWKSURL: h.jwksURL,
		})
		assert.Error(t, err)
	})
}

func TestVerifier_RejectsAlgConfusion(t *testing.T) {
	t.Parallel()

	t.Run("rejects HS256 signed with the RSA public key", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		// The classic algorithm-confusion attack: sign HS256 using the verifier's
		// PUBLIC key bytes as the HMAC secret. Without algorithm pinning, keyfunc
		// would hand the RSA public key to an HMAC verifier and accept it.
		// WithValidMethods must reject the HS256 token first.
		pub, err := x509.MarshalPKIXPublicKey(&h.key.PublicKey)
		require.NoError(t, err)
		pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims(h, uuid.NewString()))
		tok.Header["kid"] = h.kid
		signed, err := tok.SignedString(pubPEM)
		require.NoError(t, err)

		_, err = v.VerifyToken(context.Background(), signed)
		require.Error(t, err)
		// Assert the rejection came from algorithm pinning (the parser's
		// valid-methods gate: "signing method ... is invalid"), NOT merely the
		// downstream HMAC key-type backstop ("HMAC verify expects []byte"). This
		// makes the test fail if jwt.WithValidMethods is ever dropped — without
		// it, keyfunc hands the RSA key to the HMAC verifier and the token is
		// still rejected, but by the wrong control.
		assert.ErrorContains(t, err, "signing method")
	})

	t.Run("rejects an unsigned (alg=none) token", func(t *testing.T) {
		t.Parallel()
		h := newJWKSHarness(t)
		v := newTestVerifier(t, h, "pivox")
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims(h, uuid.NewString()))
		tok.Header["kid"] = h.kid
		signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)

		_, err = v.VerifyToken(context.Background(), signed)
		require.Error(t, err)
		// Same control as above: rejected at the valid-methods gate, not by the
		// none-safety backstop — fails if WithValidMethods is dropped.
		assert.ErrorContains(t, err, "signing method")
	})
}
