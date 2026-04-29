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
