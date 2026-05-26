package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/config"
)

// jwksHarness stands up an httptest server that serves a JWKS for a
// single RSA key under the well-known Google service-account path
// shape (`/v1/jwk/<sa-email>`). It also exposes a `sign` helper that
// mints JWTs signed by the same private key. Tests use this to fully
// exercise the verifier — including real signature checking — without
// hitting Google.
type jwksHarness struct {
	t       *testing.T
	key     *rsa.PrivateKey
	saEmail string
	jwksURL string // path on the test server (the verifier reads it)
	kid     string
	server  *httptest.Server
}

func newJWKSHarness(t *testing.T, saEmail string) *jwksHarness {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	kid := "test-key-1"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jwk/"+saEmail, func(w http.ResponseWriter, _ *http.Request) {
		// Minimal JWKS document: one RS256 key with `n` and `e`
		// encoded base64url. Matches the shape Google publishes at
		// /service_accounts/v1/jwk/<sa>.
		nBytes := key.N.Bytes()
		eBytes := big.NewInt(int64(key.E)).Bytes()
		jwks := map[string]any{
			"keys": []map[string]string{{
				"kid": kid,
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			}},
		}
		w.Header().Set("Content-Type", "application/jwk-set+json")
		require.NoError(t, json.NewEncoder(w).Encode(jwks))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &jwksHarness{
		t:       t,
		key:     key,
		saEmail: saEmail,
		jwksURL: srv.URL + "/v1/jwk/" + saEmail,
		kid:     kid,
		server:  srv,
	}
}

// sign produces a signed JWT with the given claims. The `kid` header
// matches the JWKS so keyfunc resolves the right key.
func (h *jwksHarness) sign(claims jwt.MapClaims) string {
	h.t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = h.kid
	signed, err := tok.SignedString(h.key)
	require.NoError(h.t, err)
	return signed
}

// signWith produces a JWT signed by a DIFFERENT key than the JWKS
// publishes. Used to test the signature-rejection path.
func (h *jwksHarness) signWith(claims jwt.MapClaims, otherKey *rsa.PrivateKey) string {
	h.t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = h.kid
	signed, err := tok.SignedString(otherKey)
	require.NoError(h.t, err)
	return signed
}

// defaultClaims returns a "happy path" claims map: valid audience,
// issuer matching the test SA, a future expiry, and a UUID
// actor_uid. Individual tests override fields to exercise rejection
// paths.
func (h *jwksHarness) defaultClaims(audience string, actorID uuid.UUID) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":       h.saEmail,
		"aud":       audience,
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"actor_uid": actorID.String(),
	}
}

// buildVerifier constructs the test-mode verifier pointing at the
// httptest JWKS URL.
func (h *jwksHarness) buildVerifier(audience string) (SsrVerifyFunc, error) {
	return newSsrVerifierFromURLs(
		context.Background(),
		audience,
		[]string{h.saEmail},
		[]string{h.jwksURL},
	)
}

// ---------------------------------------------------------------------------
// NewKeyfuncSsrVerifier — input validation
// ---------------------------------------------------------------------------

func TestNewKeyfuncSsrVerifier_MissingAudience(t *testing.T) {
	t.Parallel()
	_, err := NewKeyfuncSsrVerifier(context.Background(), config.ServiceAccountAuthConfig{
		AllowedServiceAccounts: []string{"ssr@example.com"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Audience is required")
}

func TestNewKeyfuncSsrVerifier_EmptyAllowlist(t *testing.T) {
	t.Parallel()
	_, err := NewKeyfuncSsrVerifier(context.Background(), config.ServiceAccountAuthConfig{
		Audience: "https://api.pivox.app",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one allowed service account")
}

func TestNewKeyfuncSsrVerifier_EmptyEmailInAllowlist(t *testing.T) {
	t.Parallel()
	_, err := NewKeyfuncSsrVerifier(context.Background(), config.ServiceAccountAuthConfig{
		Audience:               "https://api.pivox.app",
		AllowedServiceAccounts: []string{"ssr@example.com", ""},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty service-account email")
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestSsrVerifier_ValidTokenReturnsActorUUID(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	actor := uuid.New()

	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	tok := h.sign(h.defaultClaims(audience, actor))
	got, err := verify(context.Background(), tok)

	require.NoError(t, err)
	assert.Equal(t, actor, got)
}

// ---------------------------------------------------------------------------
// Rejection paths
// ---------------------------------------------------------------------------

func TestSsrVerifier_RejectsWrongAudience(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	verify, err := h.buildVerifier("https://api.pivox.app")
	require.NoError(t, err)

	claims := h.defaultClaims("https://wrong.audience", uuid.New())
	tok := h.sign(claims)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssr: parse/verify")
}

func TestSsrVerifier_RejectsIssuerNotInAllowlist(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	// Token signed by the right key (kid resolves) but claims iss
	// of a different (un-allowlisted) SA. JWKS verification passes
	// (signature is valid against the published key); the explicit
	// iss allowlist check is what rejects.
	claims := h.defaultClaims(audience, uuid.New())
	claims["iss"] = "stranger@example.iam.gserviceaccount.com"
	tok := h.sign(claims)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowlist")
}

func TestSsrVerifier_RejectsMissingActorUID(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	claims := h.defaultClaims(audience, uuid.Nil)
	delete(claims, "actor_uid")
	tok := h.sign(claims)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing actor_uid")
}

func TestSsrVerifier_RejectsMalformedActorUID(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	claims := h.defaultClaims(audience, uuid.Nil)
	claims["actor_uid"] = "not-a-uuid"
	tok := h.sign(claims)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse actor_uid")
}

func TestSsrVerifier_RejectsBadSignature(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	// Sign with a DIFFERENT private key — JWKS lookup by kid will
	// resolve to the published public key, signature verification
	// will fail.
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok := h.signWith(h.defaultClaims(audience, uuid.New()), otherKey)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssr: parse/verify")
}

func TestSsrVerifier_RejectsExpiredToken(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	claims := h.defaultClaims(audience, uuid.New())
	claims["exp"] = time.Now().Add(-time.Minute).Unix()
	tok := h.sign(claims)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssr: parse/verify")
}

func TestSsrVerifier_RejectsTokenMissingExp(t *testing.T) {
	t.Parallel()
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	claims := h.defaultClaims(audience, uuid.New())
	delete(claims, "exp")
	tok := h.sign(claims)

	_, err = verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssr: parse/verify")
}

func TestSsrVerifier_RejectsHS256Token(t *testing.T) {
	t.Parallel()
	// Algorithm-confusion defense: HS256 ("none" / symmetric)
	// tokens must be rejected even if the secret happens to be
	// reachable. The parser is constructed with
	// WithValidMethods([]string{"RS256","ES256"}) for this reason.
	h := newJWKSHarness(t, "ssr@pivox.iam.gserviceaccount.com")
	audience := "https://api.pivox.app"
	verify, err := h.buildVerifier(audience)
	require.NoError(t, err)

	claims := h.defaultClaims(audience, uuid.New())
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := hs.SignedString([]byte("secret-the-attacker-controls"))
	require.NoError(t, err)

	_, err = verify(context.Background(), signed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssr: parse/verify")
}
