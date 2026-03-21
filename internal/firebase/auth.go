package firebase

import (
	"context"
	"fmt"
	"os"

	fb "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"

	"github.com/dashkan/pivox-server/internal/config"
)

// AuthService wraps Firebase Auth operations including tenant management,
// ID token verification, and custom token creation.
type AuthService struct {
	authClient *auth.Client
	tenants    *auth.TenantManager
}

// NewAuthService initializes a Firebase app and returns an AuthService.
//
// Credential resolution order:
//  1. Inline service account key JSON (GoogleCloud.ServiceAccountKey)
//  2. Service account key file path (GoogleCloud.ServiceAccountFile)
//  3. GOOGLE_APPLICATION_CREDENTIALS env var (standard ADC)
//  4. Application Default Credentials (metadata server, gcloud CLI, etc.)
func NewAuthService(ctx context.Context, gc config.GoogleCloudConfig) (*AuthService, error) {
	var opts []option.ClientOption

	switch {
	case gc.ServiceAccountKey != "":
		opts = append(opts, option.WithCredentialsJSON([]byte(gc.ServiceAccountKey)))
	case gc.ServiceAccountFile != "":
		// Validate the file exists early for a clear error message.
		if _, err := os.Stat(gc.ServiceAccountFile); err != nil {
			return nil, fmt.Errorf("firebase: service account file: %w", err)
		}
		opts = append(opts, option.WithCredentialsFile(gc.ServiceAccountFile))
		// Otherwise fall through to ADC (GOOGLE_APPLICATION_CREDENTIALS or metadata server).
	}

	fbConfig := &fb.Config{}
	if gc.ProjectID != "" {
		fbConfig.ProjectID = gc.ProjectID
	}

	app, err := fb.NewApp(ctx, fbConfig, opts...)
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

// VerifyIDToken verifies a Firebase ID token and returns the decoded token.
func (s *AuthService) VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error) {
	return s.authClient.VerifyIDToken(ctx, idToken)
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
