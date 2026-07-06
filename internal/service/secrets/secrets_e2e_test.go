package secrets_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// secretAAD mirrors the (unexported) binding in the secrets package: the
// ciphertext is bound to the secret's immutable id. Reconstructing it here
// lets the test prove the binding against the real encryptor.
func secretAAD(id uuid.UUID) []byte { return append([]byte("secret:"), id[:]...) }

func idFromName(t *testing.T, name string) uuid.UUID {
	t.Helper()
	parts := strings.Split(name, "/")
	id, err := uuid.Parse(parts[len(parts)-1])
	require.NoError(t, err, "resource name leaf must be a uuid: %s", name)
	return id
}

// TestE2E_Secret_WriteOnly_AADBinding is the core vault guarantee: the value
// is never returned, and the stored ciphertext is bound (via AAD) to the
// secret's id — a decrypt with the wrong id fails. This runs against the
// harness's REAL Tink encryptor; a fake would silently pass the mis-bound
// decrypt, which is exactly the gap this closes.
func TestE2E_Secret_WriteOnly_AADBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "vault", "Vault Co", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	const plaintext = "postgres://user:pw@db:5432/app"
	created, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "vizrt-hub",
		Secret:   &secretsv1.Secret{DisplayName: "VizRT Hub", Value: []byte(plaintext)},
	})
	require.NoError(t, err)
	assert.Empty(t, created.GetValue(), "Create response must never echo the value")
	assert.Equal(t, "VizRT Hub", created.GetDisplayName())
	require.NotEmpty(t, created.GetName())
	require.NotEmpty(t, created.GetEtag())

	id := idFromName(t, created.GetName())

	var ct []byte
	require.NoError(t, h.Pool.QueryRow(ctx,
		`SELECT value_ciphertext FROM secrets WHERE id = $1`, id).Scan(&ct))
	require.NotEmpty(t, ct)
	assert.NotContains(t, string(ct), plaintext, "value must be encrypted at rest")

	// Correct AAD decrypts to the plaintext.
	got, err := h.Encryptor.Decrypt(ct, secretAAD(id))
	require.NoError(t, err, "decrypt with the correct AAD must succeed")
	assert.Equal(t, plaintext, string(got))

	// Wrong AAD (a different id) must fail — the binding a fake can't catch.
	_, err = h.Encryptor.Decrypt(ct, secretAAD(uuid.New()))
	require.Error(t, err, "decrypt with a mis-bound AAD must fail")

	// Get never returns the value.
	fetched, err := client.GetSecret(ctx, &secretsv1.GetSecretRequest{Name: created.GetName()})
	require.NoError(t, err)
	assert.Empty(t, fetched.GetValue())

	// Delete removes it.
	_, err = client.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: created.GetName()})
	require.NoError(t, err)
	_, err = client.GetSecret(ctx, &secretsv1.GetSecretRequest{Name: created.GetName()})
	require.Error(t, err, "deleted secret must not be gettable")
}

// TestE2E_Secret_EmptyValueRejected pins that a secret can never hold an
// empty value — create rejects it (delete to remove, don't empty).
func TestE2E_Secret_EmptyValueRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "vault-empty", "Vault Empty", "secrets")
	client := secretsv1.NewSecretsClient(h.Conn())

	_, err := client.CreateSecret(context.Background(), &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "empty",
		Secret:   &secretsv1.Secret{Value: nil},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_Secret_MetadataUpdateNoValue pins that a maskless update can change
// metadata (display_name) without resending the value — the value is
// write-only and unreadable, so it only rotates when named in the mask.
func TestE2E_Secret_MetadataUpdateNoValue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "vault-upd", "Vault Upd", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	created, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "s",
		Secret:   &secretsv1.Secret{DisplayName: "Old", Value: []byte("v")},
	})
	require.NoError(t, err)

	// No update_mask, no value — must succeed and change only display_name.
	updated, err := client.UpdateSecret(ctx, &secretsv1.UpdateSecretRequest{
		Secret: &secretsv1.Secret{Name: created.GetName(), DisplayName: "New"},
	})
	require.NoError(t, err, "maskless metadata update must not require a value")
	assert.Equal(t, "New", updated.GetDisplayName())
	assert.NotEqual(t, created.GetEtag(), updated.GetEtag())
}

// TestE2E_Secret_ValidateOnly pins the AIP validate_only contract: the
// request runs through the same validation a live one would (so a would-fail
// request still fails) but persists nothing.
func TestE2E_Secret_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "vault-vo", "Vault VO", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	// A dry-run Create returns the would-be resource but writes nothing.
	dry, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:       "organizations/" + owned.Slug,
		SecretId:     "dry",
		ValidateOnly: true,
		Secret:       &secretsv1.Secret{DisplayName: "Dry", Value: []byte("v")},
	})
	require.NoError(t, err)
	assert.Empty(t, dry.GetValue())

	// Nothing persisted → a real Create can reuse the same secret_id.
	_, err = client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "dry",
		Secret:   &secretsv1.Secret{Value: []byte("v")},
	})
	require.NoError(t, err, "validate_only must not have persisted the secret")

	// A dry-run that WOULD fail live (duplicate secret_id now exists) fails.
	_, err = client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:       "organizations/" + owned.Slug,
		SecretId:     "dry",
		ValidateOnly: true,
		Secret:       &secretsv1.Secret{Value: []byte("v")},
	})
	require.Error(t, err, "validate_only must fail if the live request would")
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

// TestE2E_Secret_ScopeIsolation pins that a secret's uuid can't be read or
// deleted through a different org's name prefix. The resource-name leaf is a
// global uuid; the interceptor gates on the name's org, so the handler must
// verify the fetched secret actually belongs to that org (else a member of
// org A could reach org B's secret via "organizations/A/secrets/{B-uuid}").
func TestE2E_Secret_ScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	// One owner owns both orgs.
	h.SeedOwnedOrg(t, "iso-a", "Iso A", "iso")
	ctx := context.Background()

	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx,
		&apiv1.CreateOrganizationRequest{
			OrganizationId: "iso-b",
			Organization:   &apiv1.Organization{DisplayName: "Iso B"},
		})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	client := secretsv1.NewSecretsClient(h.Conn())
	created, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/iso-b",
		SecretId: "b-secret",
		Secret:   &secretsv1.Secret{Value: []byte("b-value")},
	})
	require.NoError(t, err)
	bID := idFromName(t, created.GetName())

	// Read iso-b's secret through iso-a's name prefix → NotFound, not leaked.
	crossName := "organizations/iso-a/secrets/" + bID.String()
	_, err = client.GetSecret(ctx, &secretsv1.GetSecretRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope read must be NotFound")

	// And delete must not reach across scopes either.
	_, err = client.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope delete must be NotFound")
}
