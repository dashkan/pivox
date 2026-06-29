// Package authn defines the authentication interface used throughout the
// application. The sole implementation is the Keycloak OIDC verifier in
// internal/oidc, which validates Bearer access tokens against the issuer's
// JWKS. This package contains only the contract so consumers never import a
// specific identity provider SDK.
package authn

import (
	"context"
)

// Identity represents a verified user identity. For Keycloak tokens the
// standard `sub` claim IS the Pivox identity id (a UUID), surfaced as UID.
type Identity struct {
	UID    string
	Email  string
	Claims map[string]any
}

// Service is the authentication interface. The Keycloak OIDC verifier
// (internal/oidc) is the only implementation.
type Service interface {
	// VerifyToken validates a bearer token and returns the caller's identity.
	VerifyToken(ctx context.Context, token string) (*Identity, error)
}
