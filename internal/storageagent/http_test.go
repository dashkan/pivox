//go:build dev

package storageagent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSigningKey = []byte("test-secret-key-for-jwt-signing!")

func newTestHTTPServer() *HTTPServer {
	sessions := NewSessionStore()
	cache := NewMemoryCache(100, 1024*1024)
	endpoints := NewEndpointStore(cache)
	denied := NewDeniedPatterns()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHTTPServer(sessions, endpoints, denied, testSigningKey, "https://example.com", logger)
}

// buildJWT constructs a real HS256 JWT with the given claims and signing key.
func buildJWT(claims map[string]interface{}, key []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return sigInput + "." + sig
}

func TestNewHTTPServer(t *testing.T) {
	srv := newTestHTTPServer()
	require.NotNil(t, srv)
	assert.NotNil(t, srv.sessions)
	assert.NotNil(t, srv.endpoints)
	assert.NotNil(t, srv.denied)
	assert.Equal(t, "https://example.com", srv.corsOrigin)
}

func TestServeHTTP_CORSPreflight(t *testing.T) {
	srv := newTestHTTPServer()

	req := httptest.NewRequest(http.MethodOptions, "/media/image.png", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET, PUT, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
}

func TestSetCORSHeaders(t *testing.T) {
	srv := newTestHTTPServer()
	w := httptest.NewRecorder()

	srv.setCORSHeaders(w)

	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET, PUT, OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
}

func TestServeHTTP_DeniedPattern(t *testing.T) {
	srv := newTestHTTPServer()
	srv.denied.Update([]string{"/secret/*"})

	req := httptest.NewRequest(http.MethodGet, "/secret/data", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeHTTP_NoEndpoint(t *testing.T) {
	// With devSkipAuth=true, auth is skipped. Request goes to endpoint routing.
	// Since no endpoints are configured, we get 404.
	srv := newTestHTTPServer()

	req := httptest.NewRequest(http.MethodGet, "/media/image.png", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- JWT validation tests ---

func TestValidateJWT_Valid(t *testing.T) {
	srv := newTestHTTPServer()

	claims := map[string]interface{}{
		"sub":   "user-123",
		"token": "opaque-abc",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
	}
	token := buildJWT(claims, testSigningKey)

	result, err := srv.validateJWT(token)

	require.NoError(t, err)
	assert.Equal(t, "user-123", result["sub"])
	assert.Equal(t, "opaque-abc", result["token"])
}

func TestValidateJWT_Expired(t *testing.T) {
	srv := newTestHTTPServer()

	claims := map[string]interface{}{
		"sub": "user-123",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	}
	token := buildJWT(claims, testSigningKey)

	_, err := srv.validateJWT(token)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "token expired")
}

func TestValidateJWT_BadSignature(t *testing.T) {
	srv := newTestHTTPServer()

	claims := map[string]interface{}{
		"sub": "user-123",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}
	// Sign with a different key.
	token := buildJWT(claims, []byte("wrong-key-for-signing-12345678!"))

	_, err := srv.validateJWT(token)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestValidateJWT_Malformed(t *testing.T) {
	srv := newTestHTTPServer()

	_, err := srv.validateJWT("not-a-jwt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed JWT")
}

func TestValidateJWT_BadPayload(t *testing.T) {
	srv := newTestHTTPServer()

	// Construct a JWT with valid header/signature but invalid base64 in payload.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := "!!!invalid-base64!!!"
	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, testSigningKey)
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := fmt.Sprintf("%s.%s.%s", header, payload, sig)

	_, err := srv.validateJWT(token)

	require.Error(t, err)
	// The error could be from signature check (since the payload has invalid base64,
	// the signature won't match the expected one) or from decode.
	assert.Error(t, err)
}

func TestValidateJWT_MissingExp(t *testing.T) {
	srv := newTestHTTPServer()

	claims := map[string]interface{}{
		"sub": "user-123",
		// no "exp" claim
	}
	token := buildJWT(claims, testSigningKey)

	_, err := srv.validateJWT(token)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or invalid exp claim")
}

func TestSetSigningKey(t *testing.T) {
	srv := newTestHTTPServer()
	newKey := []byte("new-signing-key-1234567890!!!!!!")
	srv.SetSigningKey(newKey)

	// Build a JWT with the new key and verify it works.
	claims := map[string]interface{}{
		"sub": "user",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}
	token := buildJWT(claims, newKey)

	result, err := srv.validateJWT(token)
	require.NoError(t, err)
	assert.Equal(t, "user", result["sub"])
}

func TestSetCORSOrigin(t *testing.T) {
	srv := newTestHTTPServer()
	srv.SetCORSOrigin("https://new-origin.com")

	w := httptest.NewRecorder()
	srv.setCORSHeaders(w)
	assert.Equal(t, "https://new-origin.com", w.Header().Get("Access-Control-Allow-Origin"))
}
