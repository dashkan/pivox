package server

import (
	"log/slog"
	"net/http"

	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
)

// RequireAuth returns an HTTP middleware that verifies a Firebase
// bearer token from the Authorization header and augments the request
// context with the same authContextKey + pivoxUserIDKey claims that
// the gRPC AuthInterceptor sets. Both transports converge on the same
// authenticateBearer core in auth_interceptor.go so they cannot drift.
//
// On failure: writes 401 with the body "unauthorized" and logs the
// reason at warn level. The body is intentionally generic so that
// error messages cannot help an attacker distinguish "no header" from
// "bad signature" from "missing pivox_user_id claim" — the gRPC
// interceptor takes the same approach. The reason is recorded
// server-side via slog for diagnostics.
//
// Required for any custom HTTP handler that consumes the augmented
// request context directly (e.g. ContentHandler in service/aichat).
// Not required for grpc-gateway-translated routes — those forward the
// bearer to the gRPC server, where the AuthInterceptor validates it
// downstream. Wrapping the gateway routes with this middleware is
// fine but adds a redundant verification per request.
func RequireAuth(auth authn.Service, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := authenticateBearer(r.Context(), auth, r.Header.Get("Authorization"))
			if err != nil {
				logger.WarnContext(r.Context(), "http auth rejected",
					"reason", status.Convert(err).Message(),
					"path", r.URL.Path,
				)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
