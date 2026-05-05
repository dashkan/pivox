//go:build dev

package organizations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestE2E_SsoConfig_OidcRoundTrip pins the canonical OIDC happy
// path: a fresh org's SsoConfig doesn't exist, the first
// UpdateSsoConfig creates it, the second UpdateSsoConfig updates
// it, and Get returns the persisted state both times. Drives the
// upsert-with-Firebase-fallback flow that the deleted unit tests
// covered via mock call ordering.
func TestE2E_SsoConfig_OidcRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "sso-oidc-owner"})
	h.SetCaller(owner)
	createOrg(t, client, "sso-oidc", "SSO OIDC")

	cfgName := "organizations/sso-oidc/ssoConfig"

	// First update: creates the row.
	created, err := client.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name:        cfgName,
			DisplayName: "Acme OIDC",
			Enabled:     true,
			Config: &apiv1.SsoConfig_Oidc{
				Oidc: &apiv1.OidcConfig{
					Issuer:       "https://idp.example.com",
					ClientId:     "client-abc",
					ClientSecret: "super-secret-value-do-not-leak",
					ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
				},
			},
		},
	})
	require.NoError(t, err, "first update creates the SsoConfig")
	assert.Equal(t, cfgName, created.GetName())
	assert.True(t, created.GetEnabled())

	// Second update: persists changes onto the existing row.
	updated, err := client.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name:        cfgName,
			DisplayName: "Acme OIDC v2",
			Enabled:     false,
			Config: &apiv1.SsoConfig_Oidc{
				Oidc: &apiv1.OidcConfig{
					Issuer:       "https://idp.example.com",
					ClientId:     "client-abc",
					ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
				},
			},
		},
	})
	require.NoError(t, err, "second update mutates the row")
	assert.Equal(t, "Acme OIDC v2", updated.GetDisplayName())
	assert.False(t, updated.GetEnabled())

	// Get reflects the latest update.
	got, err := client.GetSsoConfig(ctx, &apiv1.GetSsoConfigRequest{Name: cfgName})
	require.NoError(t, err)
	assert.Equal(t, "Acme OIDC v2", got.GetDisplayName())
	assert.False(t, got.GetEnabled())
}

// TestE2E_SsoConfig_OmitsPlaintextSecret pins the
// information-leak guard: the SsoConfig response NEVER includes
// the plaintext client_secret. Get and Update both return the
// proto, both must omit it.
//
// The deleted unit test covered this via call-shape assertions
// on the conversion layer; the integration test pins it at the
// observable RPC boundary, which is what actually matters
// (anyone who gets a SsoConfig response must not see the secret).
func TestE2E_SsoConfig_OmitsPlaintextSecret(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "sso-leak-owner"})
	h.SetCaller(owner)
	createOrg(t, client, "sso-leak", "SSO Leak Guard")

	const plaintextSecret = "do-not-leak-this-string"
	cfgName := "organizations/sso-leak/ssoConfig"

	updated, err := client.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name:        cfgName,
			DisplayName: "Leak Guard",
			Enabled:     true,
			Config: &apiv1.SsoConfig_Oidc{
				Oidc: &apiv1.OidcConfig{
					Issuer:       "https://idp.example.com",
					ClientId:     "client-leak",
					ClientSecret: plaintextSecret,
					ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, updated.GetOidc().GetClientSecret(),
		"Update response must not echo the plaintext client_secret back to the caller")

	got, err := client.GetSsoConfig(ctx, &apiv1.GetSsoConfigRequest{Name: cfgName})
	require.NoError(t, err)
	require.Empty(t, got.GetOidc().GetClientSecret(),
		"Get response must omit the plaintext client_secret")
}

// TestE2E_SsoConfig_PersistsClientSecret pins that the secret
// reaches the bytea column at all (UpdateSsoConfig actually
// persists it, doesn't drop it on the floor). It does NOT verify
// at-rest encryption — that requires the KMS-backed encryptor,
// not the NoOpEncryptor that `-tags=dev` ships per CLAUDE.md
// "Don't ship a -tags dev binary to a real environment — the
// encryptor passthrough alone is a security hole." Encryption
// boundary verification is a production-mode concern; logged in
// the test-rebuild spec as a follow-up that needs a non-dev
// harness path to land.
func TestE2E_SsoConfig_PersistsClientSecret(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "sso-enc-owner"})
	h.SetCaller(owner)
	createOrg(t, client, "sso-encrypt", "SSO Encrypt")

	_, err := client.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name:        "organizations/sso-encrypt/ssoConfig",
			DisplayName: "Persist Test",
			Enabled:     true,
			Config: &apiv1.SsoConfig_Oidc{
				Oidc: &apiv1.OidcConfig{
					Issuer:       "https://idp.example.com",
					ClientId:     "client-enc",
					ClientSecret: "the-secret-value",
					ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
				},
			},
		},
	})
	require.NoError(t, err)

	var ciphertext []byte
	err = h.Pool.QueryRow(ctx, `
		SELECT client_secret_ciphertext
		  FROM sso_configs sc
		  JOIN organizations o ON o.id = sc.org_id
		 WHERE o.name = $1`, "sso-encrypt").Scan(&ciphertext)
	require.NoError(t, err)
	assert.NotEmpty(t, ciphertext,
		"client_secret_ciphertext must be populated after UpdateSsoConfig")
}

// TestE2E_SsoConfig_RejectsInvalidConfig is the validation matrix.
// Each case fresh-creates an org and submits an UpdateSsoConfig
// request that should be rejected. The cases probe distinct
// validation paths so a regression in one doesn't mask the others.
func TestE2E_SsoConfig_RejectsInvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "sso-invalid-owner"})
	h.SetCaller(owner)

	// Each case is a request that should be rejected. Whether the
	// rejection comes from protovalidate or the handler's
	// validateOidc is an implementation detail — the contract is
	// "invalid → InvalidArgument" at the wire boundary.
	cases := []struct {
		name string
		slug string
		req  *apiv1.UpdateSsoConfigRequest
	}{
		{
			name: "neither oidc nor saml",
			slug: "sso-empty",
			req: &apiv1.UpdateSsoConfigRequest{
				SsoConfig: &apiv1.SsoConfig{
					Name:        "organizations/sso-empty/ssoConfig",
					DisplayName: "Neither",
					// No Config oneof set.
				},
			},
		},
		{
			name: "oidc with empty response_type",
			slug: "sso-rt",
			req: &apiv1.UpdateSsoConfigRequest{
				SsoConfig: &apiv1.SsoConfig{
					Name:        "organizations/sso-rt/ssoConfig",
					DisplayName: "Empty RT",
					Config: &apiv1.SsoConfig_Oidc{
						Oidc: &apiv1.OidcConfig{
							Issuer:       "https://idp.example.com",
							ClientId:     "client",
							ResponseType: &apiv1.OidcConfig_ResponseType{},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			createOrg(t, client, tc.slug, tc.name)
			_, err := client.UpdateSsoConfig(ctx, tc.req)
			require.Error(t, err, "invalid SsoConfig must be rejected")
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"validation rejections surface as InvalidArgument")
		})
	}
}
