package storageagent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// HTTPServer serves files from storage endpoints with session-based auth.
type HTTPServer struct {
	sessions   *SessionStore
	endpoints  *EndpointStore
	denied     *DeniedPatterns
	signingKey []byte // HMAC key for JWT validation
	corsOrigin string // allowed CORS origin
	logger     *slog.Logger
}

// Config is the constructor input for HTTPServer.
type Config struct {
	// Sessions tracks active session tokens and authorizes paths.
	// Required.
	Sessions *SessionStore
	// Endpoints serves files from configured storage endpoints.
	// Required.
	Endpoints *EndpointStore
	// Denied is the set of soft-deleted patterns blocked from
	// being served. Optional; nil disables the check.
	Denied *DeniedPatterns
	// SigningKey is the HMAC key used to validate session JWTs.
	// Optional; can be set later via SetSigningKey.
	SigningKey []byte
	// CORSOrigin is the allowed CORS origin. Optional; can be
	// set later via SetCORSOrigin.
	CORSOrigin string
	// Logger is the structured logger. Required.
	Logger *slog.Logger
}

// NewHTTPServer constructs the server from cfg. Panics on a missing
// required field.
func NewHTTPServer(cfg Config) *HTTPServer {
	if cfg.Sessions == nil {
		panic("storageagent: Config.Sessions is required")
	}
	if cfg.Endpoints == nil {
		panic("storageagent: Config.Endpoints is required")
	}
	if cfg.Logger == nil {
		panic("storageagent: Config.Logger is required")
	}
	return &HTTPServer{
		sessions:   cfg.Sessions,
		endpoints:  cfg.Endpoints,
		denied:     cfg.Denied,
		signingKey: cfg.SigningKey,
		corsOrigin: cfg.CORSOrigin,
		logger:     cfg.Logger,
	}
}

// SetSigningKey updates the HMAC signing key for JWT validation.
func (s *HTTPServer) SetSigningKey(key []byte) {
	s.signingKey = key
}

// SetCORSOrigin updates the allowed CORS origin.
func (s *HTTPServer) SetCORSOrigin(origin string) {
	s.corsOrigin = origin
}

// ListenAndServe starts the HTTP server on the given address.
func (s *HTTPServer) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: s}
	return srv.Serve(ln)
}

// ServeHTTP is the main request handler.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w)

	// Handle CORS preflight.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Authenticate via the pivox_session cookie. Every non-OPTIONS
	// request must carry a valid JWT bound to a granted session.
	cookie, err := r.Cookie("pivox_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	claims, err := s.validateJWT(cookie.Value)
	if err != nil {
		s.logger.Debug("JWT validation failed", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, ok := claims["token"].(string)
	if !ok || token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.sessions.Authorize(token, r.URL.Path) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Check denied patterns before serving.
	if s.denied != nil && s.denied.IsDenied(r.URL.Path) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Proxy to the storage endpoint.
	s.endpoints.ServeFile(w, r)
}

func (s *HTTPServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", s.corsOrigin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
}

// validateJWT parses and validates an HS256 JWT using stdlib only.
// Returns the claims map on success.
func (s *HTTPServer) validateJWT(tokenStr string) (map[string]interface{}, error) {
	parts := strings.SplitN(tokenStr, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed JWT: expected 3 parts, got %d", len(parts))
	}

	headerPayload := parts[0] + "." + parts[1]

	// Verify HMAC-SHA256 signature.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(headerPayload))
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(sig, expectedSig) {
		return nil, fmt.Errorf("invalid signature")
	}

	// Decode payload.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Check expiry.
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid exp claim")
	}
	if time.Now().Unix() > int64(exp) {
		return nil, fmt.Errorf("token expired")
	}

	return claims, nil
}
