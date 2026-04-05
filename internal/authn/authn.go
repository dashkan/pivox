// Package authn defines the authentication service interface used throughout
// the application. The concrete implementation lives in internal/firebase;
// this package contains only the contract so consumers never import a
// specific identity provider SDK.
package authn

import "context"

// Identity represents a verified user identity, independent of the
// underlying identity provider (Firebase, Auth0, custom JWT, etc.).
type Identity struct {
	UID      string
	Email    string
	TenantID string
	Claims   map[string]any
}

// Service is the authentication interface that all identity provider
// implementations must satisfy.
type Service interface {
	// VerifyToken validates a bearer token and returns the caller's identity.
	VerifyToken(ctx context.Context, token string) (*Identity, error)

	// CreateCustomToken mints a provider-specific token for the given UID
	// that a client can use to sign in.
	CreateCustomToken(ctx context.Context, uid string) (string, error)

	// CreateTenant provisions a new auth tenant (e.g., for multi-tenant
	// isolation) and returns the provider-assigned tenant ID.
	CreateTenant(ctx context.Context, displayName string) (string, error)

	// DeleteTenant removes an auth tenant by its provider-assigned ID.
	DeleteTenant(ctx context.Context, tenantID string) error
}
