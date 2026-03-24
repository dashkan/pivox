package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/time/rate"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/firebase"
)

// InternalHooks handles internal webhook endpoints that are not part of the
// public gRPC/REST API. These are called by Firebase Functions and other
// internal services.
type InternalHooks struct {
	queries  *db.Queries
	logger   *slog.Logger
	firebase *firebase.AuthService

	// syncAuth protects the accounts:sync endpoint. The implementation is
	// selected at compile time via build tags:
	//   - Production (default): Google Cloud OIDC identity token verification
	//   - Dev (go build -tags dev): static shared secret
	syncAuth func(http.HandlerFunc) http.HandlerFunc

	// Per-IP rate limiter for the token exchange endpoint (AUTHN-06).
	exchangeLimiter *ipRateLimiter
}

// Register mounts the internal endpoints on the given mux.
func (h *InternalHooks) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/v1/accounts:sync", h.syncAuth(h.syncAccount))
	mux.HandleFunc("POST /internal/v1/auth:exchangeToken", h.rateLimit(h.exchangeToken))
	mux.HandleFunc("POST /internal/v1/auth:depositToken", h.depositToken)
	mux.HandleFunc("POST /internal/v1/auth:consumeToken", h.consumeToken)
}

// syncAccountRequest is the payload sent by the Firebase onUserCreated function.
type syncAccountRequest struct {
	FirebaseUID   string `json:"firebase_uid"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	DisplayName   string `json:"display_name"`
	PhotoURL      string `json:"photo_url"`
	Disabled      bool   `json:"disabled"`
}

// syncAccount upserts a Firebase Auth user into the accounts table.
func (h *InternalHooks) syncAccount(w http.ResponseWriter, r *http.Request) {
	// AUTHN-05: Limit request body to 8 KB (sync payloads are small JSON).
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)

	var req syncAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid sync account request", "error", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.FirebaseUID == "" {
		http.Error(w, "firebase_uid is required", http.StatusBadRequest)
		return
	}

	account, err := h.queries.UpsertAccount(r.Context(), db.UpsertAccountParams{
		FirebaseUid:   req.FirebaseUID,
		Email:         req.Email,
		EmailVerified: req.EmailVerified,
		DisplayName:   req.DisplayName,
		PhotoUrl:      req.PhotoURL,
		Disabled:      req.Disabled,
		LastLoginTime: pgtype.Timestamptz{}, // not set on creation
	})
	if err != nil {
		h.logger.Error("failed to upsert account", "firebase_uid", req.FirebaseUID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("account synced", "firebase_uid", req.FirebaseUID, "account_id", account.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"account_id": account.ID.String(),
	})
}

// exchangeToken verifies a Firebase ID token and returns a custom token.
// This endpoint authenticates via the Firebase ID token itself (cryptographically
// signed by Google), so no shared secret is required.
func (h *InternalHooks) exchangeToken(w http.ResponseWriter, r *http.Request) {
	// AUTHN-05: Limit request body (this endpoint only reads headers, but defense-in-depth).
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "missing or invalid authorization header", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	token, err := h.firebase.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		h.logger.Warn("failed to verify ID token", "error", err)
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}

	customToken, err := h.firebase.CreateCustomToken(r.Context(), token.UID)
	if err != nil {
		h.logger.Error("failed to create custom token", "uid", token.UID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("token exchanged", "uid", token.UID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"custom_token": customToken,
	})
}

// depositTokenRequest is the payload for the token deposit endpoint.
type depositTokenRequest struct {
	IDToken string `json:"id_token"`
}

// depositToken stores a Firebase ID token behind a short-lived, single-use
// opaque code. The Electron app calls this so the raw ID token never appears
// in a URL query parameter (AUTHN-04).
func (h *InternalHooks) depositToken(w http.ResponseWriter, r *http.Request) {
	// AUTHN-05: ID tokens are ~1-2 KB; 8 KB is generous.
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)

	var req depositTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.IDToken == "" {
		http.Error(w, "id_token is required", http.StatusBadRequest)
		return
	}

	// Verify the token is valid before storing it — reject garbage early.
	if _, err := h.firebase.VerifyIDToken(r.Context(), req.IDToken); err != nil {
		h.logger.Warn("deposit: invalid ID token", "error", err)
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}

	code, err := h.queries.CreateAuthTokenCode(r.Context(), req.IDToken)
	if err != nil {
		h.logger.Error("failed to create auth token code", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"code": code.Code.String(),
	})
}

// consumeTokenRequest is the payload for the token consume endpoint.
type consumeTokenRequest struct {
	Code string `json:"code"`
}

// consumeToken exchanges a single-use opaque code for the original ID token.
// The code is atomically marked as consumed — replay is not possible.
func (h *InternalHooks) consumeToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

	var req consumeTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	codeUUID, err := uuid.Parse(req.Code)
	if err != nil {
		http.Error(w, "invalid code format", http.StatusBadRequest)
		return
	}

	tokenCode, err := h.queries.ConsumeAuthTokenCode(r.Context(), codeUUID)
	if err != nil {
		// No rows = expired, consumed, or nonexistent.
		h.logger.Warn("consume: invalid or expired code", "code", req.Code, "error", err)
		http.Error(w, "invalid or expired code", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id_token": tokenCode.IDToken,
	})
}

// rateLimit wraps a handler with per-IP rate limiting using the exchange limiter.
func (h *InternalHooks) rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Use X-Forwarded-For if behind a reverse proxy, fall back to RemoteAddr.
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		// Strip port from RemoteAddr (e.g., "192.168.1.1:12345" → "192.168.1.1").
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}

		if !h.exchangeLimiter.allow(ip) {
			h.logger.Warn("rate limit exceeded", "ip", ip, "path", r.URL.Path)
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// ipRateLimiter provides per-key token bucket rate limiting.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	burst    int
}

func newIPRateLimiter(r rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        r,
		burst:    burst,
	}
}

func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	lim, exists := l.limiters[key]
	if !exists {
		lim = rate.NewLimiter(l.r, l.burst)
		l.limiters[key] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}
