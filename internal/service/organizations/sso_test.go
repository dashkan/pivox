package organizations

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fakeEncryptor returns a deterministic ciphertext so tests can
// assert the upsert receives a non-empty ciphertext when the
// caller supplied a new client_secret. Production uses Cloud KMS.
type fakeEncryptor struct{ err error }

func (f *fakeEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]byte, len(plaintext)+4)
	copy(out, []byte("ENC:"))
	copy(out[4:], plaintext)
	return out, nil
}

func (f *fakeEncryptor) Decrypt(ct []byte) ([]byte, error) {
	if len(ct) < 4 || string(ct[:4]) != "ENC:" {
		return nil, errors.New("not encrypted")
	}
	return ct[4:], nil
}

// --- assertSsoConfigName ---

func TestAssertSsoConfigName_Valid(t *testing.T) {
	require.NoError(t, assertSsoConfigName("organizations/acme/ssoConfig", "acme"))
}

func TestAssertSsoConfigName_OrgMismatch(t *testing.T) {
	err := assertSsoConfigName("organizations/different/ssoConfig", "acme")
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAssertSsoConfigName_Malformed(t *testing.T) {
	cases := []string{
		"",
		"organizations/acme",
		"organizations/acme/something",
		"organizations//ssoConfig",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			err := assertSsoConfigName(c, "acme")
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// --- GetSsoConfig ---

func TestGetSsoConfig_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, mock.Anything).Return(db.SsoConfig{}, pgx.ErrNoRows)
	srv := &OrganizationsServer{queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.GetSsoConfig(ctx, &apiv1.GetSsoConfigRequest{Name: "organizations/acme/ssoConfig"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGetSsoConfig_ReturnsRowWithoutSecret(t *testing.T) {
	// The proto's client_secret field must be empty in the
	// response — KMS ciphertext stays at rest, never crosses the
	// wire. SsoConfigToProto enforces this; this test pins it.
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, mock.Anything).Return(db.SsoConfig{
		FirebaseProviderID:     "oidc.acme",
		DisplayName:            "Acme SSO",
		Enabled:                true,
		OidcConfig:             []byte(`{"issuer":"https://idp.example","client_id":"abc","code_flow":true}`),
		ClientSecretCiphertext: []byte("ENC:supersecret"),
	}, nil)
	srv := &OrganizationsServer{queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	got, err := srv.GetSsoConfig(ctx, &apiv1.GetSsoConfigRequest{Name: "organizations/acme/ssoConfig"})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/ssoConfig", got.GetName())
	assert.Equal(t, "oidc.acme", got.GetFirebaseProviderId())
	require.NotNil(t, got.GetOidc())
	// Secret never leaves the server.
	assert.Empty(t, got.GetOidc().GetClientSecret())
}

// --- UpdateSsoConfig ---

func TestUpdateSsoConfig_RejectsNilConfig(t *testing.T) {
	srv := &OrganizationsServer{auth: &mockAuthService{}, encryptor: &fakeEncryptor{}}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.New(), Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateSsoConfig_SamlMissingFieldsRejected(t *testing.T) {
	cases := map[string]*apiv1.SamlConfig{
		"missing idp_entity_id": {SsoUrl: "https://idp/sso", X509Certificates: []string{"cert"}},
		"missing sso_url":       {IdpEntityId: "idp", X509Certificates: []string{"cert"}},
		"missing certs":         {IdpEntityId: "idp", SsoUrl: "https://idp/sso"},
	}
	for name, sc := range cases {
		t.Run(name, func(t *testing.T) {
			// No queries / auth mocks needed: per-config validation
			// runs before any I/O so the handler returns
			// InvalidArgument without touching the DB or Firebase.
			// AssertNotCalled on a fresh mock would also work; the
			// nil queries field here makes the no-I/O contract
			// load-bearing — any future regression that introduces
			// a DB lookup before validation will nil-deref-panic.
			srv := &OrganizationsServer{
				auth: &mockAuthService{}, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
			}
			ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
				ID: uuid.New(), Slug: "acme",
			})
			_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
				SsoConfig: &apiv1.SsoConfig{
					Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO",
					Config: &apiv1.SsoConfig_Saml{Saml: sc},
				},
			})
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// TestUpdateSsoConfig_SamlFirstCreateCallsFirebaseCreate pins the
// happy-path SAML provider creation: handler builds the auth.Service
// SAML config from the proto, calls CreateSamlProvider, and persists
// the local row with the SAML JSONB.
func TestUpdateSsoConfig_SamlFirstCreateCallsFirebaseCreate(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, orgID).Return(db.SsoConfig{}, pgx.ErrNoRows)
	q.On("UpsertSsoConfig", mock.Anything, mock.MatchedBy(func(p db.UpsertSsoConfigParams) bool {
		return p.OrgID == orgID && p.FirebaseProviderID == "saml.acme" &&
			len(p.SamlConfig) > 0 && len(p.OidcConfig) == 0
	})).Return(db.SsoConfig{
		FirebaseProviderID: "saml.acme",
		DisplayName:        "Acme SSO",
		Enabled:            true,
		SamlConfig:         []byte(`{}`),
	}, nil)

	auth := &mockAuthService{}
	auth.On("CreateSamlProvider", mock.Anything, mock.MatchedBy(func(c authn.SamlProviderConfig) bool {
		return c.ProviderID == "saml.acme" && c.IDPEntityID == "https://idp.example/entity" &&
			len(c.X509Certificates) == 1
	})).Return(nil)

	srv := &OrganizationsServer{
		queries: q, auth: auth, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO", Enabled: true,
			Config: &apiv1.SsoConfig_Saml{Saml: &apiv1.SamlConfig{
				IdpEntityId:      "https://idp.example/entity",
				SsoUrl:           "https://idp.example/sso",
				X509Certificates: []string{"-----BEGIN CERT-----..."},
			}},
		},
	})
	require.NoError(t, err)
	auth.AssertExpectations(t)
	q.AssertExpectations(t)
}

func TestUpdateSsoConfig_RejectsEmptyResponseType(t *testing.T) {
	srv := &OrganizationsServer{
		auth: &mockAuthService{}, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.New(), Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name:        "organizations/acme/ssoConfig",
			DisplayName: "Acme SSO",
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				Issuer:       "https://idp.example",
				ClientId:     "abc",
				ResponseType: &apiv1.OidcConfig_ResponseType{}, // both false
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateSsoConfig_FirstCreateCallsFirebaseCreate(t *testing.T) {
	// No existing row → handler calls CreateOidcProvider on
	// Firebase, then upserts the local row with the encrypted
	// secret. Pin the create-vs-update branch.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, orgID).Return(db.SsoConfig{}, pgx.ErrNoRows)
	q.On("UpsertSsoConfig", mock.Anything, mock.MatchedBy(func(p db.UpsertSsoConfigParams) bool {
		// Ciphertext is non-empty (encryptor ran on the new secret).
		return p.OrgID == orgID && p.FirebaseProviderID == "oidc.acme" &&
			len(p.ClientSecretCiphertext) > 0
	})).Return(db.SsoConfig{
		FirebaseProviderID: "oidc.acme",
		DisplayName:        "Acme SSO",
		Enabled:            true,
		OidcConfig:         []byte(`{}`),
	}, nil)

	auth := &mockAuthService{}
	auth.On("CreateOidcProvider", mock.Anything, mock.MatchedBy(func(c authn.OidcProviderConfig) bool {
		return c.ProviderID == "oidc.acme" && c.ClientSecret == "supersecret"
	})).Return(nil)

	srv := &OrganizationsServer{
		queries: q, auth: auth, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO", Enabled: true,
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				Issuer: "https://idp.example", ClientId: "abc",
				ClientSecret: "supersecret",
				ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
			}},
		},
	})
	require.NoError(t, err)
	auth.AssertExpectations(t)
	q.AssertExpectations(t)
	auth.AssertNotCalled(t, "UpdateOidcProvider", mock.Anything, mock.Anything)
}

func TestUpdateSsoConfig_SubsequentUpdateCallsFirebaseUpdate(t *testing.T) {
	// Row exists → handler calls UpdateOidcProvider, preserving
	// the existing FirebaseProviderID.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, orgID).Return(db.SsoConfig{
		FirebaseProviderID: "oidc.acme", Enabled: true,
	}, nil)
	q.On("UpsertSsoConfig", mock.Anything, mock.Anything).Return(db.SsoConfig{
		FirebaseProviderID: "oidc.acme",
	}, nil)

	auth := &mockAuthService{}
	auth.On("UpdateOidcProvider", mock.Anything, mock.MatchedBy(func(c authn.OidcProviderConfig) bool {
		return c.ProviderID == "oidc.acme"
	})).Return(nil)

	srv := &OrganizationsServer{
		queries: q, auth: auth, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO", Enabled: true,
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				Issuer: "https://idp.example", ClientId: "abc",
				// Empty client_secret — handler skips encryption,
				// upsert preserves existing ciphertext via COALESCE.
				ResponseType: &apiv1.OidcConfig_ResponseType{IdToken: true},
			}},
		},
	})
	require.NoError(t, err)
	auth.AssertExpectations(t)
	auth.AssertNotCalled(t, "CreateOidcProvider", mock.Anything, mock.Anything)
}

func TestUpdateSsoConfig_FirebaseFailureDoesNotPersist(t *testing.T) {
	// Firebase failure must NOT result in a local-row upsert —
	// otherwise local state diverges from Firebase. The next
	// retry picks up where this left off.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, orgID).Return(db.SsoConfig{}, pgx.ErrNoRows)

	auth := &mockAuthService{}
	auth.On("CreateOidcProvider", mock.Anything, mock.Anything).Return(errors.New("firebase down"))

	srv := &OrganizationsServer{
		queries: q, auth: auth, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO",
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				Issuer: "https://idp.example", ClientId: "abc",
				ClientSecret: "secret",
				ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	q.AssertNotCalled(t, "UpsertSsoConfig", mock.Anything, mock.Anything)
}

// TestUpdateSsoConfig_CreateAlreadyExistsFallsThroughToUpdate pins
// the race fix for #6: when two concurrent UpdateSsoConfig calls
// both observe ErrNoRows and both decide "creating", the second
// one's CreateOidcProvider returns AlreadyExists — handler must
// fall through to UpdateOidcProvider rather than failing the call.
func TestUpdateSsoConfig_CreateAlreadyExistsFallsThroughToUpdate(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, orgID).Return(db.SsoConfig{}, pgx.ErrNoRows)
	q.On("UpsertSsoConfig", mock.Anything, mock.Anything).Return(db.SsoConfig{
		FirebaseProviderID: "oidc.acme",
	}, nil)

	auth := &mockAuthService{}
	auth.On("CreateOidcProvider", mock.Anything, mock.Anything).
		Return(fmt.Errorf("%w: race", authn.ErrAlreadyExists))
	auth.On("UpdateOidcProvider", mock.Anything, mock.Anything).Return(nil)

	srv := &OrganizationsServer{
		queries: q, auth: auth, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO", Enabled: true,
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				Issuer: "https://idp.example", ClientId: "abc",
				ClientSecret: "supersecret",
				ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
			}},
		},
	})
	require.NoError(t, err)
	auth.AssertCalled(t, "CreateOidcProvider", mock.Anything, mock.Anything)
	auth.AssertCalled(t, "UpdateOidcProvider", mock.Anything, mock.Anything)
}

// TestUpdateSsoConfig_UpdateNotFoundFallsThroughToCreate is the
// inverse: handler thought the row existed (lookup hit) but Firebase
// reports NotFound — fall through to Create instead of erroring.
func TestUpdateSsoConfig_UpdateNotFoundFallsThroughToCreate(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetSsoConfigByOrgID", mock.Anything, orgID).
		Return(db.SsoConfig{FirebaseProviderID: "oidc.acme"}, nil)
	q.On("UpsertSsoConfig", mock.Anything, mock.Anything).Return(db.SsoConfig{
		FirebaseProviderID: "oidc.acme",
	}, nil)

	auth := &mockAuthService{}
	auth.On("UpdateOidcProvider", mock.Anything, mock.Anything).
		Return(fmt.Errorf("%w: stale", authn.ErrNotFound))
	auth.On("CreateOidcProvider", mock.Anything, mock.Anything).Return(nil)

	srv := &OrganizationsServer{
		queries: q, auth: auth, encryptor: &fakeEncryptor{}, caller: stubCaller(t),
	}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "Acme SSO", Enabled: true,
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				Issuer: "https://idp.example", ClientId: "abc",
				ClientSecret: "supersecret",
				ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
			}},
		},
	})
	require.NoError(t, err)
	auth.AssertCalled(t, "UpdateOidcProvider", mock.Anything, mock.Anything)
	auth.AssertCalled(t, "CreateOidcProvider", mock.Anything, mock.Anything)
}

func TestUpdateSsoConfig_NilDepsFailLoud(t *testing.T) {
	// Read-only deployments construct OrganizationsServer with nil
	// auth/encryptor; UpdateSsoConfig must surface that explicitly
	// rather than null-deref'ing later.
	srv := &OrganizationsServer{}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.New(), Slug: "acme",
	})
	_, err := srv.UpdateSsoConfig(ctx, &apiv1.UpdateSsoConfigRequest{
		SsoConfig: &apiv1.SsoConfig{
			Name: "organizations/acme/ssoConfig", DisplayName: "x",
			Config: &apiv1.SsoConfig_Oidc{Oidc: &apiv1.OidcConfig{
				ResponseType: &apiv1.OidcConfig_ResponseType{Code: true},
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
