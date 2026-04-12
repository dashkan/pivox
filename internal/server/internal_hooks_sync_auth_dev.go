//go:build dev

package server

import (
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/config"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// NewInternalHooks creates a new internal hooks handler with shared secret
// authentication for the accounts:sync endpoint. This is the dev-mode
// fallback for when the Firebase Functions emulator cannot mint OIDC tokens.
func NewInternalHooks(queries db.Querier, cfg config.SyncAuthConfig, dcfg config.DelegatedAuthConfig, logger *slog.Logger, auth authn.Service) (*InternalHooks, error) {
	h := &InternalHooks{
		queries:          queries,
		logger:           logger,
		auth:             auth,
		delegatedAuth:    dcfg,
		exchangeLimiter:  newIPRateLimiter(rate.Every(6*time.Second), 10),
		delegatedLimiter: newIPRateLimiter(rate.Every(10*time.Second), 3),
	}
	h.syncAuth = requireSecret(cfg.SharedSecret)
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
