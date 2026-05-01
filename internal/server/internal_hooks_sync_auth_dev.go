//go:build dev

package server

import (
	"crypto/subtle"
	"net/http"
)

// NewInternalHooks creates a new internal hooks handler with shared secret
// authentication for the auth:syncIdentity endpoint. This is the
// dev-mode fallback for when the Firebase Functions emulator cannot mint
// OIDC tokens. See InternalHooksConfig in internal_hooks.go.
func NewInternalHooks(cfg InternalHooksConfig) (*InternalHooks, error) {
	if cfg.Queries == nil {
		panic("server: InternalHooksConfig.Queries is required")
	}
	if cfg.Logger == nil {
		panic("server: InternalHooksConfig.Logger is required")
	}
	if cfg.Auth == nil {
		panic("server: InternalHooksConfig.Auth is required")
	}
	h := &InternalHooks{
		queries:       cfg.Queries,
		logger:        cfg.Logger,
		auth:          cfg.Auth,
		delegatedAuth: cfg.DelegatedAuth,
		audit:         cfg.AuditResolver,
	}
	h.syncAuth = requireSecret(cfg.SyncAuth.SharedSecret)
	return h, nil
}

// requireSecret validates the Authorization bearer token against the configured secret.
// Constant-time compare so the dev shared-secret path doesn't leak per-byte
// timing — matches the constant-time pattern used elsewhere on secret material.
func requireSecret(secret string) func(http.HandlerFunc) http.HandlerFunc {
	expected := []byte("Bearer " + secret)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			provided := []byte(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare(provided, expected) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
}
