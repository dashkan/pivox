//go:build dev

package server

import (
	"net/http"
	"time"

	"golang.org/x/time/rate"
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
	prefixes, err := parseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}
	h := &InternalHooks{
		queries:          cfg.Queries,
		logger:           cfg.Logger,
		auth:             cfg.Auth,
		delegatedAuth:    cfg.DelegatedAuth,
		audit:            cfg.AuditResolver,
		rateLimitEnabled: cfg.RateLimitEnabled,
		trustedProxies:   prefixes,
		exchangeLimiter:  newIPRateLimiter(rate.Every(6*time.Second), 10),
		// See internal_hooks_sync_auth.go for the rationale behind each limiter.
		delegatedCreateLimiter:   newIPRateLimiter(rate.Every(10*time.Second), 3),
		delegatedCompleteLimiter: newIPRateLimiter(rate.Every(6*time.Second), 10),
		delegatedPollLimiter:     newIPRateLimiter(rate.Every(3*time.Second), 5),
		resolveProviderLimiter:   newIPRateLimiter(rate.Every(2*time.Second), 10),
	}
	h.syncAuth = requireSecret(cfg.SyncAuth.SharedSecret)
	return h, nil
}

// requireSecret validates the Authorization bearer token against the configured secret.
func requireSecret(secret string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if token != "Bearer "+secret {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
}
