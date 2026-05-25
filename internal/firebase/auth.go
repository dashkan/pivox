package firebase

import (
	"context"
	"fmt"

	"cloud.google.com/go/auth/credentials"
	fb "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"firebase.google.com/go/v4/errorutils"
	"google.golang.org/api/option"

	"github.com/dashkan/pivox/internal/authn"
)

// firebaseScope is the OAuth scope Firebase Admin SDK uses internally
// (Identity Platform, Firebase Auth, IAM). The legacy
// option.WithCredentials{JSON,File} APIs inferred this from the
// credential; credentials.DetectDefault requires it explicit.
const firebaseScope = "https://www.googleapis.com/auth/cloud-platform"

// Compile-time check: *AuthService implements authn.Service.
var _ authn.Service = (*AuthService)(nil)

// AuthService wraps Firebase Auth operations: ID token verification and
// custom token creation.
type AuthService struct {
	authClient *auth.Client
}

// NewAuthService initializes a Firebase app and returns an AuthService.
//
// Both credentials AND project ID are resolved via Google's standard
// Application Default Credentials chain — no Pivox-named config:
//   - Local dev: `gcloud auth application-default login` writes a
//     user credential and sets a quota project.
//   - CI: `GOOGLE_APPLICATION_CREDENTIALS` points at a service
//     account JSON whose `space_id` field is read.
//   - Production: workload identity / metadata server provides both.
//
// Firebase Admin SDK falls through these sources for the project:
// service-account JSON → metadata server → GOOGLE_CLOUD_PROJECT env
// var → gcloud quota project. One of them always has it in any
// reasonable setup.
func NewAuthService(ctx context.Context) (*AuthService, error) {
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: []string{firebaseScope},
	})
	if err != nil {
		return nil, fmt.Errorf("firebase: detect credentials: %w", err)
	}

	app, err := fb.NewApp(ctx, nil, option.WithAuthCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("firebase: init app: %w", err)
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase: init auth: %w", err)
	}
	return &AuthService{
		authClient: authClient,
	}, nil
}

// VerifyToken verifies a Firebase ID token and returns a provider-independent identity.
func (s *AuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	fbToken, err := s.authClient.VerifyIDToken(ctx, token)
	if err != nil {
		return nil, err
	}

	email, _ := fbToken.Claims["email"].(string)

	return &authn.Identity{
		UID:    fbToken.UID,
		Email:  email,
		Claims: fbToken.Claims,
	}, nil
}

// CreateCustomToken creates a custom token for the given UID that can be used
// by a client to sign in with signInWithCustomToken.
func (s *AuthService) CreateCustomToken(ctx context.Context, uid string) (string, error) {
	return s.authClient.CustomToken(ctx, uid)
}

// DeleteUser removes the user from Firebase Auth. Idempotent: a UID
// that no longer exists in Firebase returns nil so the DeleteUser
// LRO can retry the final phase without spuriously failing.
//
// Called as the last step of the DeleteUser cascade (after Pivox-side
// records are gone). A failure here leaves the Firebase identity
// alive while the Pivox state has been cleaned up — the LRO surfaces
// the error and the caller can retry; on retry, idempotency on
// already-deleted UIDs lets the second attempt complete.
func (s *AuthService) DeleteUser(ctx context.Context, uid string) error {
	if err := s.authClient.DeleteUser(ctx, uid); err != nil {
		if auth.IsUserNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// UserExists reports whether Firebase Auth has a user with this UID.
// Returns `(false, nil)` ONLY when Firebase confirms the user does
// not exist — any other error is propagated so callers can
// distinguish "confirmed orphan" from "network blip". The
// identity-reconciliation worker and the syncIdentity defensive
// tombstone path both depend on this distinction.
func (s *AuthService) UserExists(ctx context.Context, uid string) (bool, error) {
	if _, err := s.authClient.GetUser(ctx, uid); err != nil {
		if auth.IsUserNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// firebaseGetUsersMax mirrors the Firebase Admin SDK's documented cap
// for GetUsers — 100 identifiers per call. Callers are responsible
// for chunking; the implementation rejects oversized batches loudly
// rather than silently truncating.
const firebaseGetUsersMax = 100

// MissingUsers returns the subset of `uids` that Firebase reports as
// not existing. Empty slice means all UIDs were found. Batches
// larger than the Firebase Admin SDK's per-call cap of 100 are
// rejected — the caller is the right place to chunk, since chunking
// here would hide quota costs and obscure failure granularity.
func (s *AuthService) MissingUsers(ctx context.Context, uids []string) ([]string, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if len(uids) > firebaseGetUsersMax {
		return nil, fmt.Errorf("firebase: MissingUsers exceeds per-call cap of %d (got %d)",
			firebaseGetUsersMax, len(uids))
	}
	identifiers := make([]auth.UserIdentifier, len(uids))
	for i, uid := range uids {
		identifiers[i] = auth.UIDIdentifier{UID: uid}
	}
	result, err := s.authClient.GetUsers(ctx, identifiers)
	if err != nil {
		return nil, err
	}
	// `result.NotFound` is the list of identifiers the SDK couldn't
	// resolve. Extract the UID from each. The SDK does not return
	// the original identifier — callers that supplied non-UID
	// identifiers (email, phone) would need a different shape; we
	// only supply UIDIdentifiers above, so the cast is exhaustive.
	missing := make([]string, 0, len(result.NotFound))
	for _, ident := range result.NotFound {
		if uidIdent, ok := ident.(auth.UIDIdentifier); ok {
			missing = append(missing, uidIdent.UID)
		}
	}
	return missing, nil
}

// CreateOidcProvider creates a Firebase Auth OIDC provider config
// from the provider-agnostic shape on the authn.Service interface.
// Translates the boolean code/id_token flags into Firebase's
// ResponseType struct.
func (s *AuthService) CreateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	toCreate := (&auth.OIDCProviderConfigToCreate{}).
		ID(cfg.ProviderID).
		DisplayName(cfg.DisplayName).
		Enabled(cfg.Enabled).
		Issuer(cfg.Issuer).
		ClientID(cfg.ClientID).
		CodeResponseType(cfg.CodeFlow).
		IDTokenResponseType(cfg.IDTokenFlow)
	if cfg.ClientSecret != "" {
		toCreate = toCreate.ClientSecret(cfg.ClientSecret)
	}
	_, err := s.authClient.CreateOIDCProviderConfig(ctx, toCreate)
	if err != nil && errorutils.IsAlreadyExists(err) {
		// Wrap with the package-level sentinel so UpdateSsoConfig's
		// fallback can detect-and-flip without taking a hard
		// dependency on the firebase package.
		return fmt.Errorf("%w: %v", authn.ErrAlreadyExists, err)
	}
	return err
}

// UpdateOidcProvider modifies an existing OIDC provider config. An
// empty ClientSecret means "don't touch the existing secret"; the
// builder skips the field rather than clearing it.
func (s *AuthService) UpdateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	toUpdate := (&auth.OIDCProviderConfigToUpdate{}).
		DisplayName(cfg.DisplayName).
		Enabled(cfg.Enabled).
		Issuer(cfg.Issuer).
		ClientID(cfg.ClientID).
		CodeResponseType(cfg.CodeFlow).
		IDTokenResponseType(cfg.IDTokenFlow)
	if cfg.ClientSecret != "" {
		toUpdate = toUpdate.ClientSecret(cfg.ClientSecret)
	}
	_, err := s.authClient.UpdateOIDCProviderConfig(ctx, cfg.ProviderID, toUpdate)
	if err != nil && auth.IsConfigurationNotFound(err) {
		return fmt.Errorf("%w: %v", authn.ErrNotFound, err)
	}
	return err
}

// DeleteOidcProvider removes an OIDC provider config. Idempotent on
// already-deleted ids — the underlying not-found surfaces as a
// gRPC NotFound error from the SDK; we swallow it so cleanup paths
// can call this safely after partial failures.
func (s *AuthService) DeleteOidcProvider(ctx context.Context, providerID string) error {
	if err := s.authClient.DeleteOIDCProviderConfig(ctx, providerID); err != nil {
		if auth.IsConfigurationNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// CreateSamlProvider creates a SAML provider config from the
// provider-agnostic shape on the authn.Service interface.
func (s *AuthService) CreateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	toCreate := (&auth.SAMLProviderConfigToCreate{}).
		ID(cfg.ProviderID).
		DisplayName(cfg.DisplayName).
		Enabled(cfg.Enabled).
		IDPEntityID(cfg.IDPEntityID).
		SSOURL(cfg.SSOURL).
		X509Certificates(cfg.X509Certificates).
		RequestSigningEnabled(cfg.RequestSigningEnabled).
		RPEntityID(cfg.RPEntityID).
		CallbackURL(cfg.CallbackURL)
	_, err := s.authClient.CreateSAMLProviderConfig(ctx, toCreate)
	if err != nil && errorutils.IsAlreadyExists(err) {
		return fmt.Errorf("%w: %v", authn.ErrAlreadyExists, err)
	}
	return err
}

// UpdateSamlProvider modifies an existing SAML provider config.
// Empty X509Certificates means "don't touch the existing certs"
// (the builder skips the field when the slice is nil/empty).
func (s *AuthService) UpdateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	toUpdate := (&auth.SAMLProviderConfigToUpdate{}).
		DisplayName(cfg.DisplayName).
		Enabled(cfg.Enabled).
		IDPEntityID(cfg.IDPEntityID).
		SSOURL(cfg.SSOURL).
		RequestSigningEnabled(cfg.RequestSigningEnabled).
		RPEntityID(cfg.RPEntityID).
		CallbackURL(cfg.CallbackURL)
	if len(cfg.X509Certificates) > 0 {
		toUpdate = toUpdate.X509Certificates(cfg.X509Certificates)
	}
	_, err := s.authClient.UpdateSAMLProviderConfig(ctx, cfg.ProviderID, toUpdate)
	if err != nil && auth.IsConfigurationNotFound(err) {
		return fmt.Errorf("%w: %v", authn.ErrNotFound, err)
	}
	return err
}

// DeleteSamlProvider removes a SAML provider config. Idempotent on
// already-deleted ids.
func (s *AuthService) DeleteSamlProvider(ctx context.Context, providerID string) error {
	if err := s.authClient.DeleteSAMLProviderConfig(ctx, providerID); err != nil {
		if auth.IsConfigurationNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
