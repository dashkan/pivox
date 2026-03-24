//go:build !dev

package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/api/idtoken"

	"github.com/dashkan/pivox/internal/config"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/firebase"
)

// NewInternalHooks creates a new internal hooks handler with Google Cloud OIDC
// identity token verification for the accounts:sync endpoint.
func NewInternalHooks(queries *db.Queries, cfg config.SyncAuthConfig, logger *slog.Logger, fb *firebase.AuthService) (*InternalHooks, error) {
	validator, err := idtoken.NewValidator(context.Background())
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]struct{}, len(cfg.AllowedServiceAccounts))
	for _, sa := range cfg.AllowedServiceAccounts {
		allowed[sa] = struct{}{}
	}

	h := &InternalHooks{
		queries:         queries,
		logger:          logger,
		firebase:        fb,
		exchangeLimiter: newIPRateLimiter(rate.Every(6*time.Second), 10),
	}
	h.syncAuth = h.requireGoogleIdentity(validator, allowed, cfg.Audience)
	return h, nil
}

// requireGoogleIdentity verifies that the request carries a valid Google Cloud
// OIDC identity token issued for the expected audience by an allowed service account.
func (h *InternalHooks) requireGoogleIdentity(
	validator *idtoken.Validator,
	allowed map[string]struct{},
	audience string,
) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")

			payload, err := validator.Validate(r.Context(), token, audience)
			if err != nil {
				h.logger.Warn("OIDC token verification failed", "error", err)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			email, _ := payload.Claims["email"].(string)
			if _, ok := allowed[email]; !ok {
				h.logger.Warn("caller not in allowed service accounts",
					"email", email,
					"allowed", allowed,
				)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			h.logger.Debug("OIDC auth passed", "email", email)
			next(w, r)
		}
	}
}
