package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil"
)

// TestResolveProvider_NoMatch_Returns200WithEmptyBody pins the new
// response shape for the no-SSO case. Pre-change, this endpoint
// returned 404 for any "no provider applies" outcome (unknown
// domain, unverified domain, disabled SsoConfig); the 404 leaked
// into the client's network tab as a console-noisy error even
// though it was the documented happy fallback path.
//
// Post-change: 200 + JSON body with `provider_id` absent
// (`omitempty`). The client treats absent provider_id identically to
// the old null-from-404 behavior. Anti-enumeration is preserved —
// every no-match cause returns the same empty body.
func TestResolveProvider_NoMatch_Returns200WithEmptyBody(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	h := newHooksForTest(t, pool, queries, nil)

	resp := callResolveProvider(t, h, resolveProviderRequest{
		Email: "user@no-sso-for-this.example.com",
	})

	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	require.Equal(t, "application/json", resp.Header().Get("Content-Type"))

	// Body decodes to an empty struct — `provider_id` omitted.
	var body resolveProviderResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "", body.ProviderID,
		"no-SSO response must carry an empty provider_id")
}

// TestResolveProvider_HappyPath_Returns200WithProviderID pins the
// existing positive path: when the email's domain is VERIFIED and
// its SsoConfig is enabled, the handler returns the
// `firebase_provider_id` so the broker can drive the OIDC handshake.
func TestResolveProvider_HappyPath_Returns200WithProviderID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Seed: identity (audit FK), org, verified domain, enabled
	// SsoConfig with a known firebase_provider_id.
	founder, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
		FirebaseUid:   "founder-uid",
		Email:         "founder@acme.example.com",
		EmailVerified: true,
		DisplayName:   "Founder",
	})
	require.NoError(t, err)

	org, err := queries.CreateOrganization(ctx, db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        "acme",
		DisplayName: "Acme",
		CreatedBy:   pgtype.UUID{Bytes: founder.ID, Valid: true},
	})
	require.NoError(t, err)

	_, err = queries.CreateDomain(ctx, db.CreateDomainParams{
		OrgID:             org.ID,
		Domain:            "acme.example.com",
		VerificationToken: "any-token",
		CreatedBy:         pgtype.UUID{Bytes: founder.ID, Valid: true},
	})
	require.NoError(t, err)
	// Mark VERIFIED directly — the public verification workflow uses
	// LROs + token checks we don't need to drive for this test.
	_, err = pool.Exec(ctx,
		`UPDATE domains SET state = 'VERIFIED' WHERE org_id = $1`, org.ID)
	require.NoError(t, err)

	_, err = queries.UpsertSsoConfig(ctx, db.UpsertSsoConfigParams{
		OrgID:              org.ID,
		FirebaseProviderID: "oidc.acme",
		DisplayName:        "Acme SSO",
		Enabled:            true,
		OidcConfig:         []byte(`{"issuer":"https://acme.example.com"}`),
		SamlConfig:         nil,
		CreatedBy:          pgtype.UUID{Bytes: founder.ID, Valid: true},
	})
	require.NoError(t, err)

	h := newHooksForTest(t, pool, queries, nil)

	resp := callResolveProvider(t, h, resolveProviderRequest{
		Email: "anyone@acme.example.com",
	})

	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	var body resolveProviderResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "oidc.acme", body.ProviderID)
}

// TestResolveProvider_UnverifiedDomain_Returns200WithEmptyBody pins
// the anti-enumeration property: a domain that exists in the
// `domains` table but isn't yet VERIFIED returns the SAME
// "no provider" response as an unknown domain. The body shape MUST
// be indistinguishable so an attacker can't probe which domains a
// given org has claimed.
func TestResolveProvider_UnverifiedDomain_Returns200WithEmptyBody(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	ctx := context.Background()

	founder, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
		FirebaseUid:   "founder-uid",
		Email:         "founder@acme.example.com",
		EmailVerified: true,
		DisplayName:   "Founder",
	})
	require.NoError(t, err)
	org, err := queries.CreateOrganization(ctx, db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        "acme",
		DisplayName: "Acme",
		CreatedBy:   pgtype.UUID{Bytes: founder.ID, Valid: true},
	})
	require.NoError(t, err)
	// Domain is created but state stays PENDING (default).
	_, err = queries.CreateDomain(ctx, db.CreateDomainParams{
		OrgID:             org.ID,
		Domain:            "acme.example.com",
		VerificationToken: "any-token",
		CreatedBy:         pgtype.UUID{Bytes: founder.ID, Valid: true},
	})
	require.NoError(t, err)

	h := newHooksForTest(t, pool, queries, nil)
	resp := callResolveProvider(t, h, resolveProviderRequest{
		Email: "anyone@acme.example.com",
	})

	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	var body resolveProviderResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "", body.ProviderID,
		"unverified domain must look identical to unknown domain")
}

// TestResolveProvider_BadEmail_Returns400 keeps the input-validation
// surface intact. A malformed payload is a programmer / client bug,
// not the no-provider data case, and 400 surfaces it visibly so the
// caller can fix the request.
func TestResolveProvider_BadEmail_Returns400(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	h := newHooksForTest(t, pool, queries, nil)

	resp := callResolveProvider(t, h, resolveProviderRequest{Email: "not-an-email"})
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

// callResolveProvider drives the handler directly without going
// through the syncAuth gate (resolveProvider is unauthenticated by
// design — pre-sign-in flow).
func callResolveProvider(
	t *testing.T,
	h *InternalHooks,
	req resolveProviderRequest,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/auth:resolveProvider",
		bytes.NewReader(body),
	)
	rec := httptest.NewRecorder()
	h.resolveProvider(rec, httpReq)
	return rec
}
