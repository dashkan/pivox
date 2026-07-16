package secrets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// secretAAD mirrors the (unexported) binding in the secrets package: the
// ciphertext is bound to the secret's immutable id. Reconstructing it here
// lets the test prove the binding against the real encryptor.
func secretAAD(id uuid.UUID) []byte { return append([]byte("secret:"), id[:]...) }

// secretIDBySlug returns a secret's internal uuid, resolved from its slug (the
// resource-name leaf) within the org named orgSlug. The AAD binding test needs
// the immutable uuid, which the slug-keyed resource name no longer carries.
func secretIDBySlug(t *testing.T, h *grpcharness.Harness, orgSlug, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, h.Pool.QueryRow(context.Background(),
		`SELECT s.id FROM secrets s JOIN organizations o ON o.id = s.org_id
		 WHERE o.name = $1 AND s.slug = $2`, orgSlug, slug).Scan(&id))
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
	// The resource name's leaf is the user-assigned slug, not the internal uuid.
	assert.Equal(t, "organizations/"+owned.Slug+"/secrets/vizrt-hub", created.GetName())
	require.NotEmpty(t, created.GetName())
	require.NotEmpty(t, created.GetEtag())

	id := secretIDBySlug(t, h, owned.Slug, "vizrt-hub")

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

// TestE2E_Secret_ValueRotation_AADBinding pins that rotating a secret's value
// (mask = value) re-encrypts under the row's immutable id: the new ciphertext
// must decrypt with secretAAD(id) to the NEW plaintext, and the old plaintext
// must be gone. Guards the invariant the UpdateSecret refactor preserves —
// encryption moved OUTSIDE the locked tx must still bind the AAD to the row uuid.
func TestE2E_Secret_ValueRotation_AADBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "vault-rotate", "Vault Rotate", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	const oldVal, newVal = "old-secret-value", "new-rotated-value"
	created, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/" + owned.Slug,
		SecretId: "rot",
		Secret:   &secretsv1.Secret{Value: []byte(oldVal)},
	})
	require.NoError(t, err)
	id := secretIDBySlug(t, h, owned.Slug, "rot")

	// Rotate the value with an explicit value-only mask.
	updated, err := client.UpdateSecret(ctx, &secretsv1.UpdateSecretRequest{
		Secret:     &secretsv1.Secret{Name: created.GetName(), Value: []byte(newVal)},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"value"}},
	})
	require.NoError(t, err)
	assert.Empty(t, updated.GetValue(), "update response must never echo the value")
	assert.NotEqual(t, created.GetEtag(), updated.GetEtag())

	// The stored ciphertext must decrypt (under the row's id AAD) to the NEW value.
	var ct []byte
	require.NoError(t, h.Pool.QueryRow(ctx,
		`SELECT value_ciphertext FROM secrets WHERE id = $1`, id).Scan(&ct))
	got, err := h.Encryptor.Decrypt(ct, secretAAD(id))
	require.NoError(t, err, "rotated ciphertext must decrypt under the row-id AAD")
	assert.Equal(t, newVal, string(got), "the rotated value must be the new plaintext")
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

// TestE2E_Secret_ScopeIsolation pins that a secret can't be read or deleted
// through a different org's name prefix. The resource-name leaf is the slug,
// unique only within its parent; the handler resolves it by (org, space, slug),
// so naming another org's secret under this org's prefix finds no row (NotFound)
// rather than leaking cross-scope existence.
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
	_, err = client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   "organizations/iso-b",
		SecretId: "b-secret",
		Secret:   &secretsv1.Secret{Value: []byte("b-value")},
	})
	require.NoError(t, err)

	// Read iso-b's secret through iso-a's name prefix → NotFound, not leaked.
	// (iso-a has no secret by that slug, so the scoped lookup finds nothing.)
	crossName := "organizations/iso-a/secrets/b-secret"
	_, err = client.GetSecret(ctx, &secretsv1.GetSecretRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope read must be NotFound")

	// And delete must not reach across scopes either.
	_, err = client.DeleteSecret(ctx, &secretsv1.DeleteSecretRequest{Name: crossName})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-scope delete must be NotFound")
}
