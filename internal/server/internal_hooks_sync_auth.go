package server

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/api/idtoken"
)

// NewInternalHooks creates a new internal hooks handler with Google Cloud
// OIDC identity token verification for the auth:syncIdentity endpoint.
// Panics on missing required fields (Queries / Logger / Auth) — startup-
// time programmer errors fail loud on boot.
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

	validator, err := idtoken.NewValidator(context.Background())
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]struct{}, len(cfg.SyncAuth.AllowedServiceAccounts))
	for _, sa := range cfg.SyncAuth.AllowedServiceAccounts {
		allowed[sa] = struct{}{}
	}

	h := &InternalHooks{
		queries:       cfg.Queries,
		logger:        cfg.Logger,
		auth:          cfg.Auth,
		delegatedAuth: cfg.DelegatedAuth,
		audit:         cfg.AuditResolver,
	}
	h.syncAuth = h.requireGoogleIdentity(validator, allowed, cfg.SyncAuth.Audience)
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
