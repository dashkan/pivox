package firebase

import (
	"context"
	"fmt"

	fb "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
)

// TenantService wraps Firebase Auth TenantManager operations.
type TenantService struct {
	tenants *auth.TenantManager
}

// NewTenantService initializes a Firebase app via Application Default
// Credentials and returns a TenantService.
func NewTenantService(ctx context.Context) (*TenantService, error) {
	app, err := fb.NewApp(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("firebase: init app: %w", err)
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase: init auth: %w", err)
	}
	return &TenantService{tenants: authClient.TenantManager}, nil
}

// CreateTenant creates a new Firebase Auth tenant with the given display name
// and returns the auto-generated tenant ID.
func (s *TenantService) CreateTenant(ctx context.Context, displayName string) (string, error) {
	tenant := (&auth.TenantToCreate{}).DisplayName(displayName)
	t, err := s.tenants.CreateTenant(ctx, tenant)
	if err != nil {
		return "", fmt.Errorf("firebase: create tenant %q: %w", displayName, err)
	}
	return t.ID, nil
}

// UpdateTenantDisplayName updates the display name of an existing tenant.
func (s *TenantService) UpdateTenantDisplayName(ctx context.Context, tenantID, displayName string) error {
	tenant := (&auth.TenantToUpdate{}).DisplayName(displayName)
	if _, err := s.tenants.UpdateTenant(ctx, tenantID, tenant); err != nil {
		return fmt.Errorf("firebase: update tenant %q: %w", tenantID, err)
	}
	return nil
}

// DeleteTenant deletes a Firebase Auth tenant by ID.
func (s *TenantService) DeleteTenant(ctx context.Context, tenantID string) error {
	if err := s.tenants.DeleteTenant(ctx, tenantID); err != nil {
		return fmt.Errorf("firebase: delete tenant %q: %w", tenantID, err)
	}
	return nil
}
