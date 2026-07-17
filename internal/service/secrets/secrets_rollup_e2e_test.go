package secrets_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// secretRollupHarness wires the org, space, and secret services so a single
// test can create an org-direct secret plus space-scoped secrets and exercise
// the org-level rollup.
func secretRollupHarness(t *testing.T) *grpcharness.Harness {
	t.Helper()
	return grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithSecretsServer())
}

// secretNamesOf returns the resource names of a secret slice, in order.
func secretNamesOf(ss []*secretsv1.Secret) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.GetName())
	}
	return out
}

// TestE2E_ListSecrets_OrgLevelRollup pins the rollup: an org-level list returns
// org-direct secrets AND every space's secrets, each rendered with its actual
// (org-direct or space-scoped) resource name. This is what makes the org-level
// admin view's Space column populate — and what makes a just-created space
// secret visible when the user returns to the org rollup.
func TestE2E_ListSecrets_OrgLevelRollup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := secretRollupHarness(t)
	owned := h.SeedOwnedOrg(t, "srollup", "SRollup Co", "secrets")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	spaceB := h.SeedOwnedSpace(t, owned.Slug, "team-b", "Team B")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	spaceBParent := orgParent + "/spaces/" + spaceB.Slug

	mkSecret(t, ctx, client, orgParent, "org-sec", "Org Direct")
	mkSecret(t, ctx, client, spaceAParent, "a-sec", "Space A Sec")
	mkSecret(t, ctx, client, spaceBParent, "b-sec", "Space B Sec")

	got := drainSecretNames(t, ctx, client, &secretsv1.ListSecretsRequest{Parent: orgParent})
	sort.Strings(got)
	want := []string{
		orgParent + "/secrets/org-sec",
		spaceAParent + "/secrets/a-sec",
		spaceBParent + "/secrets/b-sec",
	}
	sort.Strings(want)
	assert.Equal(t, want, got, "org-level rollup returns org-direct + all space rows with correct names")

	// The org-direct row carries no /spaces/ segment; the space rows do.
	assert.Contains(t, got, orgParent+"/secrets/org-sec")
	for _, n := range got {
		if n == orgParent+"/secrets/org-sec" {
			assert.NotContains(t, n, "/spaces/", "org-direct row is named without a space segment")
		}
	}
}

// TestE2E_ListSecrets_SpaceLevelScoped pins that a space-level list stays scoped
// to that one space — it does NOT roll up org-direct or sibling-space secrets.
func TestE2E_ListSecrets_SpaceLevelScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := secretRollupHarness(t)
	owned := h.SeedOwnedOrg(t, "sspscope", "SSpScope Co", "secrets")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	spaceB := h.SeedOwnedSpace(t, owned.Slug, "team-b", "Team B")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	spaceBParent := orgParent + "/spaces/" + spaceB.Slug

	mkSecret(t, ctx, client, orgParent, "org-sec", "Org Direct")
	mkSecret(t, ctx, client, spaceAParent, "a-sec", "Space A Sec")
	mkSecret(t, ctx, client, spaceBParent, "b-sec", "Space B Sec")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{Parent: spaceAParent})
	require.NoError(t, err)
	assert.Equal(t, []string{spaceAParent + "/secrets/a-sec"}, secretNamesOf(resp.GetSecrets()),
		"a space-level list returns only that space's secret")
}

// TestE2E_ListSecrets_RollupSortFilter pins that sort and a filter both hold
// across the mixed org+space rollup.
func TestE2E_ListSecrets_RollupSortFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := secretRollupHarness(t)
	owned := h.SeedOwnedOrg(t, "srmix", "SRMix Co", "secrets")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	spaceB := h.SeedOwnedSpace(t, owned.Slug, "team-b", "Team B")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	spaceBParent := orgParent + "/spaces/" + spaceB.Slug

	// Display names chosen so displayName order interleaves org and space rows:
	// asc → aaa(spaceA), mmm(org), sss(spaceB), zzz(spaceA).
	orgSec := mkSecret(t, ctx, client, orgParent, "org-sec", "mmm")
	aSec1 := mkSecret(t, ctx, client, spaceAParent, "a-1", "aaa")
	bSec := mkSecret(t, ctx, client, spaceBParent, "b-1", "sss")
	aSec2 := mkSecret(t, ctx, client, spaceAParent, "a-2", "zzz")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: orgParent, OrderBy: "displayName",
	})
	require.NoError(t, err)
	assert.Equal(t,
		[]string{aSec1.GetName(), orgSec.GetName(), bSec.GetName(), aSec2.GetName()},
		secretNamesOf(resp.GetSecrets()),
		"displayName sort orders the mixed org+space rollup correctly")

	// Filter across the rollup narrows to the one org-direct row.
	resp, err = client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{
		Parent: orgParent, Filter: `displayName = "mmm"`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{orgSec.GetName()}, secretNamesOf(resp.GetSecrets()),
		"filter narrows across the rollup")
}

// TestE2E_ListSecrets_RollupNameRoundTrip pins that a name minted by the
// org-level rollup for a space-scoped row resolves the same row via GetSecret —
// the space-scoped name is well-formed and addressable.
func TestE2E_ListSecrets_RollupNameRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := secretRollupHarness(t)
	owned := h.SeedOwnedOrg(t, "srtrip", "SRTrip Co", "secrets")
	spaceA := h.SeedOwnedSpace(t, owned.Slug, "team-a", "Team A")
	ctx := context.Background()
	client := secretsv1.NewSecretsClient(h.Conn())

	orgParent := "organizations/" + owned.Slug
	spaceAParent := orgParent + "/spaces/" + spaceA.Slug
	mkSecret(t, ctx, client, orgParent, "org-sec", "Org Direct")
	created := mkSecret(t, ctx, client, spaceAParent, "a-sec", "Space A Sec")

	resp, err := client.ListSecrets(ctx, &secretsv1.ListSecretsRequest{Parent: orgParent})
	require.NoError(t, err)
	var rolledName string
	for _, s := range resp.GetSecrets() {
		if s.GetDisplayName() == "Space A Sec" {
			rolledName = s.GetName()
		}
	}
	require.Equal(t, spaceAParent+"/secrets/a-sec", rolledName,
		"the rollup names the space row with its space-scoped path")

	got, err := client.GetSecret(ctx, &secretsv1.GetSecretRequest{Name: rolledName})
	require.NoError(t, err)
	assert.Equal(t, created.GetName(), got.GetName())
	assert.Equal(t, "Space A Sec", got.GetDisplayName())
}
