package firebase

import (
	"context"
	"fmt"

	"cloud.google.com/go/auth/credentials"
	fb "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
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

// AuthService wraps Firebase Auth operations including tenant management,
// ID token verification, and custom token creation.
type AuthService struct {
	authClient *auth.Client
	tenants    *auth.TenantManager
}

// NewAuthService initializes a Firebase app and returns an AuthService.
//
// Both credentials AND project ID are resolved via Google's standard
// Application Default Credentials chain — no Pivox-named config:
//   - Local dev: `gcloud auth application-default login` writes a
//     user credential and sets a quota project.
//   - CI: `GOOGLE_APPLICATION_CREDENTIALS` points at a service
//     account JSON whose `project_id` field is read.
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
		tenants:    authClient.TenantManager,
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
		UID:      fbToken.UID,
		Email:    email,
		TenantID: fbToken.Firebase.Tenant,
		Claims:   fbToken.Claims,
	}, nil
}

// CreateCustomToken creates a custom token for the given UID that can be used
// by a client to sign in with signInWithCustomToken.
func (s *AuthService) CreateCustomToken(ctx context.Context, uid string) (string, error) {
	return s.authClient.CustomToken(ctx, uid)
}

// CreateTenant creates a new Firebase Auth tenant with the given display name
// and returns the auto-generated tenant ID.
func (s *AuthService) CreateTenant(ctx context.Context, displayName string) (string, error) {
	tenant := (&auth.TenantToCreate{}).DisplayName(displayName)
	t, err := s.tenants.CreateTenant(ctx, tenant)
	if err != nil {
		return "", fmt.Errorf("firebase: create tenant %q: %w", displayName, err)
	}
	return t.ID, nil
}

// UpdateTenantDisplayName updates the display name of an existing tenant.
func (s *AuthService) UpdateTenantDisplayName(ctx context.Context, tenantID, displayName string) error {
	tenant := (&auth.TenantToUpdate{}).DisplayName(displayName)
	if _, err := s.tenants.UpdateTenant(ctx, tenantID, tenant); err != nil {
		return fmt.Errorf("firebase: update tenant %q: %w", tenantID, err)
	}
	return nil
}

// DeleteTenant deletes a Firebase Auth tenant by ID.
func (s *AuthService) DeleteTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.DeleteTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("firebase: delete tenant %q: %w", tenantID, err)
	}
	return nil
}
