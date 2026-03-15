package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/pivoxai/pivox/internal/db/generated"
)

// InternalHooks handles internal webhook endpoints that are not part of the
// public gRPC/REST API. These are called by Firebase Functions and other
// internal services.
type InternalHooks struct {
	queries *db.Queries
	secret  string
	logger  *slog.Logger
}

// NewInternalHooks creates a new internal hooks handler.
func NewInternalHooks(queries *db.Queries, secret string, logger *slog.Logger) *InternalHooks {
	return &InternalHooks{
		queries: queries,
		secret:  secret,
		logger:  logger,
	}
}

// Register mounts the internal endpoints on the given mux.
func (h *InternalHooks) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/v1/accounts:sync", h.requireSecret(h.syncAccount))
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
