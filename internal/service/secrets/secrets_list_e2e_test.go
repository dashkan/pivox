package secrets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// drainSecretNames follows next_page_token to completion, returning every
// secret's resource name across all pages. Fails if the page loop runs away.
func drainSecretNames(t *testing.T, ctx context.Context, client secretsv1.SecretsClient, req *secretsv1.ListSecretsRequest) []string {
	t.Helper()
	var names []string
	token := ""
	for range 100 {
		req.PageToken = token
		resp, err := client.ListSecrets(ctx, req)
		require.NoError(t, err)
		for _, s := range resp.GetSecrets() {
			names = append(names, s.GetName())
		}
		token = resp.GetNextPageToken()
		if token == "" {
			return names
		}
	}
	t.Fatal("pagination did not terminate within 100 pages")
	return nil
}

// TestE2E_ListSecrets_KeysetBoundary pins the keyset off-by-one: with exactly
// pageSize+1 secrets and a page size that forces one boundary crossing, every
// secret must be returned exactly once — no row dropped at the boundary, none
// duplicated. This fails against the old rows[pageSize] cursor (which encodes
// the first UN-returned row and then resumes strictly past it, skipping it) and
// passes once the cursor is the last RETURNED row via filter.Paginate.
func TestE2E_ListSecrets_KeysetBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "vault-page", "Vault Page", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	const pageSize = 3
	const total = pageSize + 1 // exactly one boundary crossing
	for i := range total {
		_, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
			Parent:   parent,
			SecretId: fmt.Sprintf("s%d", i),
			Secret:   &secretsv1.Secret{Value: []byte("v")},
		})
		require.NoError(t, err)
	}

	got := drainSecretNames(t, ctx, client, &secretsv1.ListSecretsRequest{
		Parent:   parent,
		PageSize: pageSize,
	})
	assert.Len(t, got, total, "every secret returned exactly once across the page boundary (no drop)")
	uniq := map[string]struct{}{}
	for _, n := range got {
		uniq[n] = struct{}{}
	}
	assert.Len(t, uniq, total, "no duplicate secrets across the page boundary")
}
