package secrets_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// mkSecret creates a secret with the given id (slug) + display name under
// parent, and fails the test on error. A secret always requires a non-empty
// value on create.
func mkSecret(t *testing.T, ctx context.Context, client secretsv1.SecretsClient, parent, id, displayName string) *secretsv1.Secret {
	t.Helper()
	s, err := client.CreateSecret(ctx, &secretsv1.CreateSecretRequest{
		Parent:   parent,
		SecretId: id,
		Secret:   &secretsv1.Secret{DisplayName: displayName, Value: []byte("v")},
	})
	require.NoError(t, err)
	return s
}

// listSecretDisplayNames returns the display names of a single ListSecrets page.
func listSecretDisplayNames(ss []*secretsv1.Secret) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.GetDisplayName())
	}
	return out
}

// TestE2E_ListSecrets_FilterByDisplayName pins that the AIP-160 filter narrows
// the list by displayName (exact, substring, and wildcard forms) — the exact
// gap that let the name filter be a silent no-op.
func TestE2E_ListSecrets_FilterByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "sfltr", "SFltr Co", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	mkSecret(t, ctx, client, parent, "a", "VizRT Hub")
	mkSecret(t, ctx, client, parent, "b", "News Hub")
	mkSecret(t, ctx, client, parent, "c", "Weather Service")

	// Exact match.
	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, Filter: `displayName = "VizRT Hub"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"VizRT Hub"}, listSecretDisplayNames(resp.GetSecrets()))

	// Substring (`:`) — both "…Hub" rows, neither the Weather one.
	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, Filter: `displayName : "Hub"`,
	})
	require.NoError(t, err)
	got := listSecretDisplayNames(resp.GetSecrets())
	sort.Strings(got)
	assert.Equal(t, []string{"News Hub", "VizRT Hub"}, got)

	// Wildcard `=` also lowers to ILIKE (AllowPartial).
	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, Filter: `displayName = "Weather*"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Weather Service"}, listSecretDisplayNames(resp.GetSecrets()))
}

// TestE2E_ListSecrets_OrderByDisplayName pins that order_by=displayName sorts
// the list ascending and descending — the sort headers were a no-op before the
// handler consumed order_by.
func TestE2E_ListSecrets_OrderByDisplayName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "sordr", "SOrdr Co", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	// Created in a non-alphabetical order so displayName order differs from the
	// id (creation) order.
	mkSecret(t, ctx, client, parent, "id1", "charlie")
	mkSecret(t, ctx, client, parent, "id2", "alpha")
	mkSecret(t, ctx, client, parent, "id3", "bravo")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, OrderBy: "displayName",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, listSecretDisplayNames(resp.GetSecrets()))

	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, OrderBy: "displayName desc",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"charlie", "bravo", "alpha"}, listSecretDisplayNames(resp.GetSecrets()))
}

// TestE2E_ListSecrets_OrderByCreateTime pins order_by=createTime asc/desc.
func TestE2E_ListSecrets_OrderByCreateTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "sordct", "SOrdCT Co", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	mkSecret(t, ctx, client, parent, "id1", "first")
	mkSecret(t, ctx, client, parent, "id2", "second")
	mkSecret(t, ctx, client, parent, "id3", "third")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, OrderBy: "createTime",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second", "third"}, listSecretDisplayNames(resp.GetSecrets()))

	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, OrderBy: "createTime desc",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"third", "second", "first"}, listSecretDisplayNames(resp.GetSecrets()))
}

// TestE2E_ListSecrets_UnknownFieldsRejected pins that an unknown filter field, a
// non-orderable field (updateTime is filterable-only per the proto — this is the
// factual guard behind "Updated is not sortable"), and a garbage page token each
// return InvalidArgument, not a silent empty result or a default order.
func TestE2E_ListSecrets_UnknownFieldsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "sreject", "SReject Co", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())
	parent := "organizations/" + owned.Slug
	mkSecret(t, ctx, client, parent, "a", "a")

	// Unknown filter field → InvalidArgument.
	_, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, Filter: `valueCiphertext = "x"`,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// updateTime is NOT in the sortable whitelist (the "Updated" column is
	// display-only) → InvalidArgument.
	_, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, OrderBy: "updateTime",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// A garbage page_token → InvalidArgument.
	_, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, PageToken: "not-a-real-token",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestE2E_ListSecrets_InjectionInert pins that a SQL-injection payload in a
// filter value is a literal operand: it matches nothing, errors nothing, and
// leaves the other rows intact and listable.
func TestE2E_ListSecrets_InjectionInert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	owned := h.SeedOwnedOrg(t, "sinject", "SInject Co", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())
	parent := "organizations/" + owned.Slug

	mkSecret(t, ctx, client, parent, "a", "real-one")
	mkSecret(t, ctx, client, parent, "b", "real-two")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, Filter: `displayName = "x' OR '1'='1"`,
	})
	require.NoError(t, err, "an injection payload must be a harmless no-match, not an error")
	assert.Empty(t, resp.GetSecrets(), "payload matched no row — it was NOT executed as SQL")

	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: parent, Filter: `displayName : "'; DROP TABLE secrets;--"`,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetSecrets())

	// The table is intact and the real rows still list.
	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{Parent: parent})
	require.NoError(t, err)
	assert.Len(t, resp.GetSecrets(), 2, "injection attempts left the data intact")
}

// TestE2E_ListSecrets_FilterScopeIsolation pins that a filter can only narrow
// within the org scope — it can never widen a list beyond its org.
func TestE2E_ListSecrets_FilterScopeIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := grpcharness.New(t, grpcharness.WithOrganizationsServer(), grpcharness.WithSecretsServer())
	a := h.SeedOwnedOrg(t, "siso-a", "SIso A", "secrets")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	op, err := apiv1.NewOrganizationsClient(h.Conn()).CreateOrganization(ctx,
		&apiv1.CreateOrganizationRequest{
			OrganizationId: "siso-b",
			Organization:   &apiv1.Organization{DisplayName: "SIso B"},
		})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	bParent := "organizations/siso-b"

	mkSecret(t, ctx, client, "organizations/"+a.Slug, "a-only", "A Only")
	mkSecret(t, ctx, client, bParent, "b-only", "B Only")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: "organizations/" + a.Slug, Filter: `displayName : "Only"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"A Only"}, listSecretDisplayNames(resp.GetSecrets()), "filter can only narrow within the org scope")
}
