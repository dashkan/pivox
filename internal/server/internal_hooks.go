package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/pivoxai/pivox/internal/db/generated"
	"github.com/pivoxai/pivox/internal/firebase"
)

// InternalHooks handles internal webhook endpoints that are not part of the
// public gRPC/REST API. These are called by Firebase Functions and other
// internal services.
type InternalHooks struct {
	queries  *db.Queries
	secret   string
	logger   *slog.Logger
	firebase *firebase.AuthService
}

// NewInternalHooks creates a new internal hooks handler.
func NewInternalHooks(queries *db.Queries, secret string, logger *slog.Logger, firebase *firebase.AuthService) *InternalHooks {
	return &InternalHooks{
		queries:  queries,
		secret:   secret,
		logger:   logger,
		firebase: firebase,
	}
}

// Register mounts the internal endpoints on the given mux.
func (h *InternalHooks) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/v1/accounts:sync", h.requireSecret(h.syncAccount))
	mux.HandleFunc("POST /internal/v1/auth:exchangeToken", h.exchangeToken)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"custom_token": customToken,
	})
}

// requireSecret validates the Authorization bearer token against the configured secret.
func (h *InternalHooks) requireSecret(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "Bearer "+h.secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
