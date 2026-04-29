// Package authn defines the authentication service interface used throughout
// the application. The concrete implementation lives in internal/firebase;
// this package contains only the contract so consumers never import a
// specific identity provider SDK.
package authn

import (
	"context"
	"errors"
)

// Identity represents a verified user identity, independent of the
// underlying identity provider (Firebase, Auth0, custom JWT, etc.).
type Identity struct {
	UID    string
	Email  string
	Claims map[string]any
}

// Service is the authentication interface that all identity provider
// implementations must satisfy.
type Service interface {
	// VerifyToken validates a bearer token and returns the caller's identity.
	VerifyToken(ctx context.Context, token string) (*Identity, error)

	// CreateCustomToken mints a provider-specific token for the given UID
	// that a client can use to sign in.
	CreateCustomToken(ctx context.Context, uid string) (string, error)

	// DeleteUser removes the user from the underlying identity provider.
	// Called as the LAST step of the DeleteUser LRO so a partial failure
	// leaves the Firebase identity alive (and Pivox state already
	// cleaned up — but recoverable). Implementations should be idempotent
	// (no error for already-deleted UIDs) so the LRO is safe to retry
	// after a transient failure.
	DeleteUser(ctx context.Context, uid string) error

	// CreateOidcProvider creates an OIDC provider configuration in the
	// underlying identity provider. The provider id is server-generated
	// upstream (e.g. "oidc.<org-slug>") and stored on the SsoConfig row.
	// Only OIDC is wired in v1; SAML lands when a customer requires it.
	CreateOidcProvider(ctx context.Context, cfg OidcProviderConfig) error

	// UpdateOidcProvider modifies an existing OIDC provider config. The
	// `cfg.ProviderID` must already exist upstream — the handler enforces
	// the create-vs-update branch by inspecting the local SsoConfig row.
	UpdateOidcProvider(ctx context.Context, cfg OidcProviderConfig) error

	// DeleteOidcProvider removes an OIDC provider config. Idempotent on
	// already-deleted ids — implementations swallow the not-found case
	// so cleanup paths can call this safely after partial failures.
	DeleteOidcProvider(ctx context.Context, providerID string) error

	// CreateSamlProvider creates a SAML provider configuration. Same
	// shape contract as Create*OidcProvider — provider_id is server-
	// derived ("saml.<org-slug>") and stored on the SsoConfig row.
	// Returns ErrAlreadyExists when a provider with this id is
	// already present so UpdateSsoConfig can fall through to update.
	CreateSamlProvider(ctx context.Context, cfg SamlProviderConfig) error

	// UpdateSamlProvider modifies an existing SAML provider config.
	// Returns ErrNotFound when the upstream id is missing so callers
	// can fall through to Create.
	UpdateSamlProvider(ctx context.Context, cfg SamlProviderConfig) error

	// DeleteSamlProvider removes a SAML provider config. Idempotent
	// on already-deleted ids.
	DeleteSamlProvider(ctx context.Context, providerID string) error
}

// ErrAlreadyExists is the sentinel returned by Create*Provider when a
// provider with the same id already exists upstream. ErrNotFound is
// returned by Update*Provider / Delete*Provider when no such provider
// exists. Callers wrap these via errors.Is — implementations may wrap
// the underlying SDK error with %w to preserve the cause chain.
//
// Used by UpdateSsoConfig's create-or-update fallback to recover from
// stale create-vs-update branch decisions on a fresh org.
var (
	ErrAlreadyExists = errAlreadyExists{}
	ErrNotFound      = errNotFound{}
)

type errAlreadyExists struct{}

func (errAlreadyExists) Error() string { return "authn: provider already exists" }

type errNotFound struct{}

func (errNotFound) Error() string { return "authn: provider not found" }

// IsAlreadyExists / IsNotFound are convenience predicates that wrap
// errors.Is for cleaner call sites.
func IsAlreadyExists(err error) bool { return err != nil && errors.Is(err, ErrAlreadyExists) }
func IsNotFound(err error) bool      { return err != nil && errors.Is(err, ErrNotFound) }

// OidcProviderConfig is the provider-agnostic shape passed across the
// authn.Service boundary. Translates to Firebase Admin SDK's
// OIDCProviderConfigToCreate / OIDCProviderConfigToUpdate inside the
// firebase impl, but callers don't import any provider SDK to use it.
//
// ClientSecret travels in plaintext through this struct — the SsoConfig
// handler decrypts the row's KMS-encrypted ciphertext at the call
// boundary. The handler is responsible for not logging or surfacing it.
type OidcProviderConfig struct {
	ProviderID   string // "oidc.<slug>" (server-derived from org slug)
	DisplayName  string
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string // optional; empty on Update means "don't change"
	CodeFlow     bool   // OIDC response_type code (auth-code flow)
	IDTokenFlow  bool   // OIDC response_type id_token (implicit flow)
}

// SamlProviderConfig is the SAML sibling of OidcProviderConfig.
// Translates to Firebase Admin SDK's SAMLProviderConfigToCreate /
// ToUpdate inside the firebase impl.
//
// X509Certificates may contain multiple PEM-encoded certs during IdP
// cert rotation. RPEntityID and CallbackURL are server-derived (the
// handler computes them from the org slug + Firebase project) so the
// IdP can be configured against stable values.
type SamlProviderConfig struct {
	ProviderID            string // "saml.<slug>" (server-derived)
	DisplayName           string
	Enabled               bool
	IDPEntityID           string
	SSOURL                string
	X509Certificates      []string
	RequestSigningEnabled bool
	RPEntityID            string
	CallbackURL           string
}
